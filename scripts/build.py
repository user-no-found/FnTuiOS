#!/usr/bin/env python3
"""FnTuiOS application steps for the common FnPackKit/BuildFlow engine."""
import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import shutil
import sys

from buildflow.contracts import Step
from buildflow.source import ResolvedSource, fetch_git, resolve_git
from build_inputs import ROOT, bind_runner, capped, digest, goenv, run, toolchain, write_json
from build_support import inputs_digest, runtime_modfile, tools_identity, tree_digest
from build_environment import load_browser_environment
from build_temporary import TemporaryRunner
import browser
import localization
import projections
import package
import licenses
import verify_localization
import verify_archive
import image_paste
import image_backend
import gateway_browser
import verify_package


def plan(project):
    """Provide distinct application steps; the tool owns execution and publication."""
    load_browser_environment(project.root)
    metadata = project.data
    if metadata['app_id'] != 'tuios' or metadata['gateway_socket'] != 'app.sock' or metadata['gateway_prefix'] != '/app/tuios':
        raise ValueError('application identity must match the tuios deployment contract')
    if metadata['upstream_policy'] != 'default-branch-head':
        raise ValueError('invalid source/version policy')
    architectures = metadata['build']['architectures']
    if not architectures or any(arch not in ['amd64', 'arm64'] for arch in architectures) or len(set(architectures)) != len(architectures):
        raise ValueError('build.architectures must contain unique amd64/arm64 values')
    state = {}

    def prepare(ctx):
        ctx.runner = TemporaryRunner(ctx.runner, ctx.work / "tmp")
        bind_runner(ctx.runner)
        verify_archive.verify()
        state['initial_digest'] = inputs_digest()
        state['tools'] = tools_identity()
        now = datetime.now(timezone.utc)
        state['now'] = now
        state['version'] = metadata['version']
        state['reports'] = ctx.work / 'reports'
        state['reports'].mkdir()
        ctx.metadata['application_reports'] = str(state['reports'])
        url = f"https://github.com/{metadata['upstream']}.git"
        saved = os.environ.get('FNTUIOS_RESOLVED_SOURCE')
        if saved:
            upstream = json.loads(Path(saved).read_text())
            if upstream['url'] != url or upstream['repository'] != metadata['upstream'] or not upstream.get('resolved_at'):
                raise ValueError('resolved source does not match project upstream or lacks resolution time')
            resolved = ResolvedSource(url, upstream['sha'], 'HEAD')
            upstream = {**upstream, 'resolution_input': str(Path(saved).resolve())}
        else:
            resolved = resolve_git(url, runner=ctx.runner)
            upstream = {'repository': metadata['upstream'], 'url': url, 'sha': resolved.revision,
                        'ref': resolved.ref, 'resolved_at': now.isoformat()}
        state['upstream'] = upstream
        write_json(ctx.work / 'resolved.json', upstream)
        state['source'] = fetch_git(resolved, ctx.work / 'upstream', ROOT / '.cache/git', runner=ctx.runner)
        ctx.metadata.update({'upstream': upstream, 'tools': state['tools'], 'package_version': state['version']})

    def dependencies(ctx):
        source = state['source']
        go, tool = toolchain(source)
        env = goenv()
        state.update(go=go, tool=tool, env=env)
        run(capped([go, 'mod', 'download']), cwd=source, env=env)
        sip_info = json.loads(run([go, 'list', '-m', '-json', 'github.com/Gaurav-Gosain/sip'], cwd=source, env=env, capture=True))
        sip = ctx.work / 'sip'
        shutil.copytree(sip_info['Dir'], sip)
        for path in [sip, *sip.rglob('*')]:
            path.chmod(path.stat().st_mode | 0o200)
        state.update(sip=sip, sip_info=sip_info)
        modfile, runtime = runtime_modfile(go, ctx.work, env)
        state.update(modfile=modfile, runtime=runtime, gateway_browser=gateway_browser.dependency())
        ctx.metadata.update(go_toolchain=tool, runtime_dependency=runtime, sip_version=sip_info['Version'],
                            gateway_browser_dependency=state['gateway_browser'])

    def translate(ctx):
        go, source, sip, output = (state[key] for key in ['go', 'source', 'sip', 'reports'])
        reviewed = localization.gate(go, source, sip, output)
        verify_localization.verify(argparse.Namespace(go=go, source=source, sip=sip, output=output / 'localization-checks'))
        unchanged()
        run(capped([go, 'test', '-short', './cmd/tuios-web/', './internal/input/']), cwd=source, env=state['env'])
        run(capped([go, 'run', '.', '-root', source, '-catalog', output / 'catalog.json', '-projection-report', output / 'display-projection.json']), cwd=ROOT / 'tools/localize', env=state['env'])
        projections.apply(source, reviewed)
        browser.apply(sip, reviewed)
        browser.adapt_transport(sip)
        image_paste.apply(sip, output)
        gateway_browser.apply(sip, output, state['gateway_browser'])
        image_backend.apply(source, sip, go)
        run([go, 'mod', 'edit', '-replace=github.com/Gaurav-Gosain/sip=' + str(sip)], cwd=source, env=state['env'])

    def check(ctx):
        go, source, sip, env = (state[key] for key in ['go', 'source', 'sip', 'env'])
        run(capped([go, 'test', '-modfile=' + str(state['modfile']), './...']), cwd=ROOT, env=env)
        run(capped([go, 'test', '-short', './internal/config/', './internal/session/', './internal/risk/']), cwd=source, env=env)
        run(capped([go, 'test', '-run', '^TestBrowserImage|^TestSessionSlotPickSendsTheName$|^TestSessionAccentVocabulary$|^TestCreatePath|^TestRenameEntryRefusesAnExistingDestination$|^TestPasteRefusesAFolderIntoItself$', './internal/app/']), cwd=source, env=env)
        web_security = '^TestBrowserImage|^TestPassword|^TestAllowHost|^TestLoopback|^TestNetworkBind|^TestRandomPassword|^TestCheckTransportSecurity|^TestResolveTLSFiles|^TestCertNew|^TestWebTeardown|^TestGuestTerminalEnv|^TestEphemeralWebSession|^TestMobileBar|^TestEveryTheme'
        run(capped([go, 'test', '-short', '-run', web_security, './cmd/tuios-web/']), cwd=source, env=env)
        run(capped([go, 'test', '-run', '^TestDNSRebindingIsRefused$|^TestAllowedHostsAdmitsAProxyName$|^TestIndexRefusesFraming$|^TestOversizedResizeIsClamped$|^TestHostAllowed$', './']), cwd=sip, env=env)
        run(['git', 'add', '-N', '--', 'internal', 'cmd'], cwd=source)
        (state['reports'] / 'source-localization.patch').write_text(run(['git', 'diff', '--', 'internal', 'cmd'], cwd=source, capture=True))

    def compile_binaries(ctx):
        go, source, env, version, upstream, now = (state[key] for key in ['go', 'source', 'env', 'version', 'upstream', 'now'])
        provenance = {'product': metadata, 'package_version': version, 'upstream': upstream,
                      'go_toolchain': state['tool'], 'sip_version': state['sip_info']['Version'],
                      'source_go_sum_sha256': digest(source / 'go.sum'),
                      'project_input_sha256': state['initial_digest'], 'built_at': now.isoformat(),
                      'tools': state['tools'], 'runtime_dependency': state['runtime'],
                      'gateway_browser_dependency': state['gateway_browser'],
                      'localization': json.loads((state['reports'] / 'coverage.json').read_text()),
                      'translation_provenance': json.loads((ROOT / 'locales/provenance.json').read_text())}
        state['provenance'] = provenance
        write_json(state['reports'] / 'build-inputs.json', provenance)
        license_dir = ctx.work / 'licenses'
        licenses.collect(go, source, license_dir)
        state['licenses'] = license_dir
        toolkit_license = Path(state['runtime']['path']).parent / 'LICENSE'
        if toolkit_license.is_file():
            shutil.copy2(toolkit_license, license_dir / 'FnPackKit.LICENSE')
        for arch in architectures:
            binaries = ctx.work / 'bin' / arch
            binaries.mkdir(parents=True)
            target_env = {**env, 'GOOS': 'linux', 'GOARCH': arch}
            for name in ['tuios', 'tuios-web']:
                run(capped([go, 'build', '-trimpath', '-ldflags', f'-s -w -X main.version={version} -X main.commit={upstream["sha"]} -X main.date={now.isoformat()} -X main.builtBy=FnTuiOS', '-o', binaries / name, './cmd/' + name]), cwd=source, env=target_env)
            flags = f"-s -w -X main.version={version} -X main.displayName={metadata['display_name']} -X main.appID={metadata['app_id']}"
            run(capped([go, 'build', '-modfile=' + str(state['modfile']), '-trimpath', '-ldflags', flags, '-o', binaries / 'fntuios', './cmd/fntuios']), cwd=ROOT, env=target_env)
        unchanged()

    def unchanged():
        if inputs_digest() != state['initial_digest']:
            raise ValueError('application build inputs changed during compilation; rebuild')
        if gateway_browser.dependency() != state['gateway_browser']:
            raise ValueError('shared gateway browser inputs changed during compilation; rebuild')
        if tools_identity() != state['tools'] or tree_digest(Path(state['runtime']['path'])) != state['runtime']['source_tree_sha256']:
            raise ValueError('shared tool inputs changed during compilation; rebuild')

    def payloads(ctx):
        unchanged()
        return [package.payload(metadata, state['version'], arch, ctx.work / 'bin' / arch,
                                state['provenance'], ctx.work, state['licenses']) for arch in architectures]

    def verify(ctx, artifacts):
        playwright = os.environ.get('FNTUIOS_PLAYWRIGHT')
        chromium = os.environ.get('FNTUIOS_CHROMIUM')
        if not playwright or not chromium:
            raise ValueError('FNTUIOS_PLAYWRIGHT and FNTUIOS_CHROMIUM are required for actual package verification')
        packages = [Path(item.path) for item in artifacts if Path(item.path).suffix == '.fpk']
        amd64 = next((path for path in packages if path.name.endswith('_amd64.fpk')), None)
        if amd64 is None:
            raise ValueError('native final verification requires amd64 in build.architectures')
        verify_package.verify(argparse.Namespace(fpk=amd64, go=state['go'], playwright=playwright,
                              chromium=chromium, runner=ctx.runner, output=state['reports'] / 'verification'))
        unchanged()

    return {'steps': [Step('固定源码输入', prepare), Step('工具链与共享运行库', dependencies),
                      Step('中文审查门禁与应用适配', translate), Step('应用安全与适配验证', check),
                      Step('原生双架构编译', compile_binaries)], 'payload': payloads, 'verify': verify}


def main():
    from fnpackkit.cli import main as shared_main
    return shared_main(['--project', str(ROOT / 'project.json'), *sys.argv[1:]])


if __name__ == '__main__':
    sys.exit(main())

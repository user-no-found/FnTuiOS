#!/usr/bin/env python3
"""Verify an actual FPK with native processes and explicit privilege boundaries."""
import argparse
import json
import os
from pathlib import Path
import shutil
import tempfile
from build_temporary import TemporaryRunner

from buildflow.contracts import BuildError
from build_inputs import ROOT, activity, bind_runner, capped, digest, goenv, run, write_json
from build_support import runtime_modfile, inputs_digest
import gateway_browser
import verify_gateway
from buildflow.runner import Runner
from elf import check_elf
from safe_archive import extract_archive
from native_verify import browser_check, negative_check


def extract(artifact, output):
    if output.exists():
        shutil.rmtree(output)
    output.mkdir(parents=True)
    extract_archive(artifact, output)
    app = output / 'app'
    app.mkdir()
    extract_archive(output / 'app.tgz', app)
    return app


def verify(args):
    if os.geteuid() == 0:
        raise ValueError('run native package verification as an ordinary user')
    artifact = args.fpk.resolve()
    checksum = digest(artifact)
    output = Path(getattr(args, 'output', None) or ROOT / '.build/verification' / checksum[:16])
    output.mkdir(parents=True, exist_ok=True)
    runner = getattr(args, 'runner', None)
    if runner is None:
        state = ROOT / '.build'
        state.mkdir(exist_ok=True)
        work = Path(tempfile.mkdtemp(prefix='verify-', dir=state))
        runner = TemporaryRunner(Runner(output / 'verification.log'), work / 'tmp')
    bind_runner(runner)
    with activity('解包实际 FPK'):
        app = extract(artifact, output / 'unpacked')
    provenance = json.loads((app / 'build.json').read_text())
    if provenance['project_input_sha256'] != inputs_digest():
        raise ValueError('application source differs from packaged provenance; rebuild before verification')
    if gateway_browser.dependency()['source_tree_sha256'] != provenance['gateway_browser_dependency']['source_tree_sha256']:
        raise ValueError('shared gateway browser source differs from packaged provenance')
    arch = provenance['architecture']
    for name, expected in provenance['binary_sha256'].items():
        binary = app / 'bin' / name
        if digest(binary) != expected:
            raise ValueError('packaged binary digest mismatch: ' + name)
        check_elf(binary, arch)
    if artifact.with_suffix('.fpk.sha256').read_text().split()[0] != checksum:
        raise ValueError('FPK checksum sidecar mismatch')
    if arch != 'amd64':
        raise ValueError('runtime/browser verification requires an amd64 package on this host')
    version = run([app / 'bin/fntuios', 'version'], capture=True)
    if provenance['package_version'] not in version:
        raise ValueError('packaged runtime version mismatch')
    editable = output / 'gateway.json'
    if editable.exists():
        editable.unlink()
    run([app / 'bin/fntuios', 'configure', '--template', app / 'etc/gateway.json', '--output', editable,
         '--app-dir', app, '--state-dir', output / 'state', '--run-dir', output / 'run'])
    run([app / 'bin/fntuios', 'check-config', '--config', editable])
    try:
        run([app / 'bin/fntuios', 'prepare-state', '--config', output / 'absent-policy.json'])
    except BuildError as error:
        if 'prepare-state requires root' not in str(error):
            raise ValueError('prepare-state failed outside its expected privilege boundary') from error
    else:
        raise ValueError('ordinary user was allowed to prepare privileged state')
    data = json.loads(editable.read_text())
    data['max_users'] = 0
    write_json(editable, data)
    try:
        run([app / 'bin/fntuios', 'check-config', '--config', editable])
    except BuildError:
        pass
    else:
        raise ValueError('invalid editable max_users was accepted')
    modfile, dependency = runtime_modfile(args.go, output, goenv())
    if dependency['source_tree_sha256'] != provenance['runtime_dependency']['source_tree_sha256']:
        raise ValueError('test harness shared runtime differs from packaged runtime source')
    harness = output / 'runtime.test'
    with activity('编译原生验证程序'):
        run(capped([args.go, 'test', '-modfile=' + str(modfile), '-c', '-o', harness, './internal/service']), cwd=ROOT, env=goenv())
    env = {**goenv(), 'FNTUIOS_E2E_NATIVE': '1', 'FNTUIOS_E2E_BIN_DIR': str(app / 'bin'),
           'FNTUIOS_E2E_REPORT': str(output / 'runtime.json')}
    with activity('验证原生工作进程与持久会话'):
        run(capped([harness, '-test.run', '^TestNativeWorker$', '-test.v', '-test.timeout', '3m']), env=env)
    with activity('验证浏览器中文界面与图片粘贴'):
        browser_check(args, app, output, harness)
    with activity('验证共享后端控制与终端设置'):
        browser_check(args, app, output / 'runtime-panel', harness, panel=True)
    with activity('验证键盘回归预期负控'):
        negative_check(args, app, output, harness)
    with activity('验证公网网关与来源边界'):
        verify_gateway.verify(args, app, output / 'gateway-browser', harness)
    write_json(output / 'verified.json', {'fpk': str(artifact), 'fpk_sha256': checksum,
               'upstream_sha': provenance['upstream']['sha'], 'binary_sha256': provenance['binary_sha256'],
               'checks': ['ELF and payload checksums', 'actual binary version and editable configuration validation',
                          'packaged prepare-state rejects an ordinary caller before reading configuration',
                          'native current-UID worker lifecycle and persistence', 'peer-verified Unix gateway to actual web',
                          'Chinese iframe Chromium and real clipboard image interaction', 'keyboard regression negative control', 'HTTPS public-authority browser positive and strict-policy negative controls'],
               'not_executed': ['root broker privilege dropping', 'two-real-UID isolation', 'fnOS install/upgrade callbacks', 'live FN Connect login/entitlement'],
               'boundary': 'native current user; temporary owned paths; simulated FN ingress and broker; no installed service changes',
               'verified': True})
    return output / 'verified.json'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('fpk', type=Path)
    parser.add_argument('--go', type=lambda value: Path(value).resolve(), required=True)
    parser.add_argument('--playwright', required=True)
    parser.add_argument('--chromium', required=True)
    print(verify(parser.parse_args()))


if __name__ == '__main__':
    main()

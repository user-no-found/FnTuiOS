#!/usr/bin/env python3
"""Exercise first-time setup and no-export entry scripts in an isolated HOME."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import PIL

ROOT = Path(__file__).resolve().parents[1]


def verify(args):
    checks = []
    with tempfile.TemporaryDirectory(prefix="fntuios setup '") as temporary:
        root = Path(temporary)
        home, config, app = (root / name for name in ['home', 'config', 'application'])
        for directory in [home, config, app / 'scripts']:
            directory.mkdir(parents=True)
        environment = {'HOME': str(home), 'XDG_CONFIG_HOME': str(config),
                       'PATH': os.environ['PATH'], 'LANG': 'C.UTF-8',
                       'PYTHONPATH': str(Path(PIL.__file__).resolve().parent.parent)}

        def run(argv, *, extra=None, expected=0):
            result = subprocess.run(list(map(str, argv)), cwd=root, env={**environment, **(extra or {})},
                                    capture_output=True, text=True, timeout=45)
            if result.returncode != expected:
                raise AssertionError(f'{argv}: expected {expected}, got {result.returncode}\n{result.stdout}\n{result.stderr}')
            return result

        for name in ['build.sh', 'setup.sh']:
            shutil.copy2(ROOT / name, app / name)
        for name in ['setup.py', 'build_environment.py']:
            shutil.copy2(ROOT / 'scripts' / name, app / 'scripts' / name)
        missing = run([app / 'build.sh', '--help'], expected=1)
        assert 'setup.sh' in missing.stderr
        checks.append('missing shared dependencies fail with initialization instructions')
        run([args.buildflow / 'setup.sh'])
        run([args.fnpackkit / 'setup.sh'])
        run([app / 'build.sh', '--help'])
        checks.append('shared initialization lets an existing clean shell launch from an unrelated cwd')
        run(['bash', '--noprofile', '--rcfile', home / '.bashrc', '-ic', '"$1" --help', 'fixture', app / 'build.sh'])
        checks.append('new Bash loads the initialized tool environment')
        for key in ['BUILDFLOW_PATH', 'FNPACKKIT_PATH']:
            failure = run([app / 'build.sh', '--help'], extra={key: ''}, expected=1)
            assert 'Invalid ' + key in failure.stderr
        checks.append('explicit empty tool overrides fail without silently using configured paths')

        browser_dir = root / "browser paths '"
        browser_dir.mkdir()
        playwright = browser_dir / 'playwright'
        chromium = browser_dir / 'chrome'
        playwright.symlink_to(args.playwright, target_is_directory=True)
        chromium.symlink_to(args.chromium)
        run([app / 'setup.sh', '--playwright', playwright, '--chromium', chromium])
        settings = config / 'buildflow/browser.json'
        previous = settings.read_bytes()
        assert settings.stat().st_mode & 0o777 == 0o600
        run([app / 'setup.sh'])
        assert settings.read_bytes() == previous
        run([app / 'setup.sh', '--chromium', '/bin/false'], expected=1)
        assert settings.read_bytes() == previous
        checks.append('browser setup checks installed tools, supports quoted paths, is idempotent and preserves valid settings on failure')
        settings.write_text('{broken')
        run([app / 'setup.sh', '--playwright', args.playwright, '--chromium', args.chromium])
        assert settings.read_bytes() == previous
        checks.append('complete explicit browser paths repair malformed shared configuration')
        bad_package = root / 'bad-playwright'
        bad_package.mkdir()
        (bad_package / 'package.json').write_text('[]')
        failure = run([app / 'setup.sh', '--playwright', bad_package], expected=1)
        assert 'Traceback' not in failure.stderr and settings.read_bytes() == previous
        data = json.loads(previous)
        data['schema'] = True
        settings.write_text(json.dumps(data))
        failure = run([app / 'setup.sh'], expected=1)
        assert 'schema' in failure.stderr
        settings.write_bytes(previous)
        checks.append('non-object package metadata and boolean config schema are rejected cleanly')

        # A real small FPK proves the initialized entry reaches the shared engine.
        run(['fnpack', 'create', 'fixture', '--without-ui'])
        template = root / 'fixture'
        for path in template.rglob('.DS_Store'):
            path.unlink()
        (template / 'manifest').unlink()
        (template / 'manifest.in').write_text('appname=@APP_ID@\nversion=@VERSION@\ndisplay_name=Setup fixture\n'
            'desc=Native setup verification\nplatform=@PLATFORM@\nsource=thirdparty\nmaintainer=Example\ndistributor=Example\n')
        (app / 'hello.txt').write_text('initialized native application entry\n')
        (app / 'payload.json').write_text(json.dumps({'schema': 1, 'template': str(template),
            'files': [{'source': str(app / 'hello.txt'), 'destination': 'app/data/hello.txt'}],
            'values': {'APP_ID': 'fixture', 'VERSION': '1.0.0', 'PLATFORM': 'x86'},
            'artifact_name': 'fixture.fpk', 'required': ['app/data/hello.txt']}))
        (app / 'project.json').write_text(json.dumps({'app_id': 'fixture', 'display_name': 'Setup fixture', 'version': '1.0.0',
            'buildflow': {'schema': 1, 'requires': '>=0.1,<0.2', 'targets': ['fpk'], 'steps': []},
            'fnpackkit': {'schema': 1, 'requires': '>=0.1,<0.2', 'adapter': 'scripts/adapter.py:plan'}}))
        (app / 'scripts/adapter.py').write_text('from build_environment import load_browser_environment\n'
            'def plan(project):\n    load_browser_environment()\n'
            '    return {"steps": [], "payload": project.root / "payload.json"}\n')
        run([app / 'build.sh', '--build-version', '1.0.0'])
        success = (app / '.build/last-success.json').read_bytes()
        final = app / 'output'
        artifact = final / 'fixture.fpk'
        assert artifact.is_file() and artifact.with_suffix('.fpk.sha256').is_file()
        run([app / 'build.sh', '--build-version', '1.0.0'], extra={'FNTUIOS_CHROMIUM': '/bin/false'}, expected=1)
        assert (app / '.build/last-success.json').read_bytes() == success
        checks.append('no-export application entry builds a real FPK and rejects invalid explicit browser override without changing success')
        # Select the real application plan without executing source/network/build steps.
        code = ('import sys;sys.path.insert(0,sys.argv[1]);from build_environment import load_browser_environment;'
                'load_browser_environment();from buildflow.project import load_project;'
                'from buildflow.metadata import version;from fnpackkit.adapter import load_plan;'
                'p=load_project(sys.argv[3],version());plan=load_plan(p,p.data["fnpackkit"]["adapter"]);'
                'assert len(plan["steps"])==5 and callable(plan["verify"]);print("real application plan loaded")')
        paths = str(args.buildflow / 'src') + os.pathsep + str(args.fnpackkit / 'src')
        run(['python3', '-c', code, app / 'scripts', app, ROOT / 'project.json'], extra={'PYTHONPATH': paths})
        checks.append('real FnTuiOS plan loads validated browser defaults without executing source fetch or compilation')
        terminal_config = root / "terminal config '"
        terminal_env = {'XDG_CONFIG_HOME': str(terminal_config)}
        terminal_app = root / 'terminal application'
        shutil.copytree(app, terminal_app, ignore=shutil.ignore_patterns('.build', 'output'))
        run([args.buildflow / 'setup.sh'], extra=terminal_env)
        run([args.fnpackkit / 'setup.sh'], extra=terminal_env)
        missing_browser = run([terminal_app / 'build.sh', '--build-version', '1.0.0'],
                              extra=terminal_env, expected=1)
        assert '未登记' in missing_browser.stderr
        assert str(terminal_config / 'buildflow/browser.json') in missing_browser.stderr
        assert (app / '.build/last-success.json').read_bytes() == success
        run([terminal_app / 'setup.sh', '--playwright', args.playwright, '--chromium', args.chromium], extra=terminal_env)
        run([terminal_app / 'build.sh', '--build-version', '1.0.0'], extra=terminal_env)
        run(['python3', '-c', code, app / 'scripts', app, ROOT / 'project.json'],
            extra={**terminal_env, 'PYTHONPATH': paths})
        assert settings.read_bytes() == previous
        checks.append('terminal XDG context reports its missing registry, then builds a real FPK with registered existing tools without changing the original context')
        args.output.mkdir(parents=True, exist_ok=True)
        shutil.copy2(artifact, args.output / 'fixture.fpk')
        shutil.copy2(artifact.with_suffix('.fpk.sha256'), args.output / 'fixture.fpk.sha256')
        (args.output / 'verified.json').write_text(json.dumps({'verified': True, 'checks': checks,
            'boundary': 'isolated HOME and application fixture; actual shared setup/engine/fnpack and installed browser probes; no TUIOS FPK rebuild'}, indent=2) + '\n')
    print(json.dumps({'verified': True, 'checks': len(checks), 'evidence': str(args.output)}, indent=2))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ['buildflow', 'fnpackkit', 'playwright', 'chromium', 'output']:
        parser.add_argument('--' + name, type=lambda value: Path(value).resolve(), required=True)
    verify(parser.parse_args())


if __name__ == '__main__':
    main()

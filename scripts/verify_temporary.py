#!/usr/bin/env python3
"""Prove native Go execution uses private build storage despite noexec inheritance."""
import argparse
import json
import os
from pathlib import Path
import tempfile

from buildflow.contracts import BuildError
from buildflow.runner import Runner
from build_temporary import TemporaryRunner


def verify(args):
    args.output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="tmp check '", dir=args.output) as temporary:
        work = Path(temporary)
        program = work / 'main.go'
        program.write_text('package main\nimport("fmt";"os")\nfunc main(){fmt.Println(os.TempDir())}\n')
        inherited = {**os.environ, 'TMPDIR': str(args.noexec), 'GOTMPDIR': str(args.noexec), 'GOTOOLCHAIN': 'local'}
        runner = Runner(args.output / 'commands.log')
        try:
            runner.run([args.go, 'run', program], cwd=work, env=inherited)
        except BuildError as error:
            if 'permission denied' not in (args.output / 'commands.log').read_text():
                raise AssertionError('negative control failed for an unrelated reason') from error
        else:
            raise AssertionError('negative control executed: selected inherited directory is not noexec')
        scoped = TemporaryRunner(runner, work / 'tmp')
        actual = scoped.capture([args.go, 'run', program], cwd=work, env=inherited).strip()
        if actual != str(scoped.directory) or scoped.directory.stat().st_mode & 0o777 != 0o700:
            raise AssertionError('Go did not use the private build temporary directory')
        if not os.access(scoped.directory, os.X_OK):
            raise AssertionError('build temporary directory is not accessible')
        owned = scoped.directory
        scoped.close()
        if owned.exists():
            raise AssertionError('owned temporary directory was not cleaned')
        result = {'verified': True, 'negative_control': 'Go binary execution denied by inherited noexec temporary directory',
                  'inherited_tmpdir': str(args.noexec), 'fixed_tmpdir': actual,
                  'checks': ['real Go run succeeds through shared runner', 'TMPDIR and GOTMPDIR override inherited and explicit child env',
                             'private directory mode 0700 with no Git ancestor', 'owned directory cleanup', 'temporary paths with spaces and single quote'],
                  'scope': 'temporary build fixture only; no mount or installed-service changes'}
        (args.output / 'verified.json').write_text(json.dumps(result, indent=2) + '\n')
    print(args.output / 'verified.json')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for option in ['go', 'noexec', 'output']:
        parser.add_argument('--' + option, required=True, type=lambda value: Path(value).resolve())
    verify(parser.parse_args())


if __name__ == '__main__':
    main()

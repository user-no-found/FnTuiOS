#!/usr/bin/env python3
"""Register already installed native browser dependencies for this application."""
import argparse
import json
import os
from pathlib import Path
import sys
import tempfile

from build_environment import KEYS, read_settings, validate_browser

ROOT = Path(__file__).resolve().parents[1]


def configure(args):
    previous = {} if args.playwright is not None and args.chromium is not None else read_settings(ROOT)
    supplied = dict(zip(KEYS, [args.playwright, args.chromium]))
    settings = {key: str(value.expanduser().resolve()) if value is not None else
                os.environ.get(key, previous.get(key)) for key, value in supplied.items()}
    validate_browser(settings)
    directory = ROOT / '.build'
    if directory.is_symlink():
        raise ValueError('Refusing symlinked application build directory')
    directory.mkdir(exist_ok=True)
    target = directory / 'environment.json'
    if target.is_symlink():
        raise ValueError('Refusing symlinked application build configuration')
    temporary = None
    try:
        with tempfile.NamedTemporaryFile('w', dir=directory, prefix='.environment-', delete=False) as stream:
            temporary = Path(stream.name)
            json.dump({'schema': 1, 'browser': settings}, stream, ensure_ascii=False, indent=2)
            stream.write('\n')
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, target)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)
    print(f'Native browser dependencies registered in {target}')
    print('After shared tool setup, run ./build.sh from any working directory.')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--playwright', type=Path, help='existing node_modules/playwright directory')
    parser.add_argument('--chromium', type=Path, help='existing Chrome/Chromium executable')
    args = parser.parse_args()
    try:
        configure(args)
    except (OSError, ValueError) as error:
        print(f'FnTuiOS setup: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())

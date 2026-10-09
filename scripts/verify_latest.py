#!/usr/bin/env python3
"""Verify flat FPK output against the current BuildFlow success record."""
import argparse
import json
import os
from pathlib import Path

from build_inputs import ROOT, digest
from build_environment import load_browser_environment
import verify_package


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--playwright')
    parser.add_argument('--chromium')
    args = parser.parse_args()
    for key, value in [('FNTUIOS_PLAYWRIGHT', args.playwright), ('FNTUIOS_CHROMIUM', args.chromium)]:
        if value is not None:
            os.environ[key] = value
    configured = load_browser_environment(ROOT)
    latest = json.loads((ROOT / '.build/last-success.json').read_text())
    if latest.get('schema') != 2:
        raise ValueError('unsupported build success schema; run the current build entry')
    record = ROOT / '.build' / latest['report']
    if not record.resolve().is_relative_to((ROOT / '.build').resolve()):
        raise ValueError('invalid build report path')
    report = json.loads(record.read_text())
    entry = next(item for item in latest['artifacts'] if item['name'].endswith('_amd64.fpk'))
    if Path(entry['name']).name != entry['name']:
        raise ValueError('expected a flat artifact name')
    fpk = ROOT / 'output' / entry['name']
    if digest(fpk) != entry['sha256']:
        raise ValueError('published FPK differs from the successful build')
    go = ROOT / '.cache/toolchains' / report['metadata']['go_toolchain']['version'] / 'go/bin/go'
    print(verify_package.verify(argparse.Namespace(fpk=fpk, go=go,
        playwright=args.playwright or configured['FNTUIOS_PLAYWRIGHT'],
        chromium=args.chromium or configured['FNTUIOS_CHROMIUM'])))


if __name__ == '__main__':
    main()

#!/usr/bin/env python3
"""Copy verified packages and checksums after final package verification succeeds."""
import argparse
import json
from pathlib import Path
import shutil

from build_inputs import ROOT, digest
from elf import check_elf
from verify_package import extract


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--verified', type=Path, required=True, help='amd64 verified.json from verify_package.py')
    parser.add_argument('--copy-to', type=Path, required=True)
    parser.add_argument('--arm64', type=Path, help='matching cross-built package; architecture/hash inspection only')
    args = parser.parse_args()
    report = json.loads(args.verified.read_text())
    artifact = Path(report['fpk'])
    if report.get('verified') is not True or digest(artifact) != report['fpk_sha256']:
        raise ValueError('final package has not passed verification or its digest changed')
    artifacts = [artifact]
    if args.arm64:
        arm = args.arm64.resolve()
        app = extract(arm, ROOT / '.build/verification' / (digest(arm)[:16] + '-arm64'))
        manifest = json.loads((app / 'build.json').read_text())
        if manifest['upstream']['sha'] != report['upstream_sha']:
            raise ValueError('architecture packages use different upstream commits')
        amd_app = extract(artifact, ROOT / '.build/verification' / (digest(artifact)[:16] + '-delivery'))
        amd = json.loads((amd_app / 'build.json').read_text())
        for key in ['package_version', 'project_input_sha256']:
            if manifest[key] != amd[key]:
                raise ValueError('architecture packages use different build inputs')
        for name, checksum in manifest['binary_sha256'].items():
            if digest(app / 'bin' / name) != checksum:
                raise ValueError('ARM64 binary digest mismatch')
            check_elf(app / 'bin' / name, 'arm64')
        artifacts.append(arm)
    args.copy_to.mkdir(parents=True, exist_ok=True)
    for path in artifacts:
        sidecar = path.with_suffix('.fpk.sha256')
        if sidecar.read_text().split()[0] != digest(path):
            raise ValueError('package checksum sidecar mismatch')
        for source in [path, sidecar]:
            target = args.copy_to / source.name
            shutil.copy2(source, target)
            if digest(target) != digest(source):
                raise ValueError('copy checksum mismatch')
            print(target)


if __name__ == '__main__':
    main()

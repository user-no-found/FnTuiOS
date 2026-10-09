"""Declare the FnTuiOS payload; FnPackKit owns rendering, packing and archive checks."""

from fnpackkit.contracts import Payload, PayloadFile
from build_inputs import ROOT, digest, write_json
from elf import check_elf


def payload(metadata, version, arch, binaries, provenance, work, licenses):
    files = []
    hashes = {}
    for name in ['tuios', 'tuios-web', 'fntuios']:
        source = binaries / name
        check_elf(source, arch)
        hashes[name] = digest(source)
        files.append(PayloadFile(source, 'app/bin/' + name, 0o755))
    generated = work / 'payload' / arch
    generated.mkdir(parents=True)
    sums = generated / 'SHA256SUMS'
    sums.write_text(''.join(f'{sha}  {name}\n' for name, sha in hashes.items()))
    files.append(PayloadFile(sums, 'app/bin/SHA256SUMS', 0o644))
    for source in licenses.rglob('*'):
        if source.is_file():
            files.append(PayloadFile(source, 'app/licenses/' + str(source.relative_to(licenses)), 0o644))
    files.append(PayloadFile(ROOT / 'LICENSE', 'app/licenses/FnTuiOS.LICENSE', 0o644))
    provenance = {**provenance, 'architecture': arch, 'binary_sha256': hashes}
    write_json(generated / 'build.json', provenance)
    files.append(PayloadFile(generated / 'build.json', 'app/build.json', 0o644))
    values = {'APP_ID': metadata['app_id'], 'VERSION': version, 'DISPLAY_NAME': metadata['display_name'],
              'DESCRIPTION': metadata['description'], 'PLATFORM': {'amd64': 'x86', 'arm64': 'arm'}[arch],
              'GATEWAY_PREFIX': metadata['gateway_prefix'], 'GATEWAY_SOCKET': metadata['gateway_socket']}
    return Payload(ROOT / 'packaging/fnos', files, values,
                   f"{metadata['display_name']}_{version}_{arch}.fpk", provenance,
                   ['app/bin/tuios', 'app/bin/tuios-web', 'app/bin/fntuios', 'app/build.json', 'app/etc/gateway.json'])

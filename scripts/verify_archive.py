#!/usr/bin/env python3
"""Verify archive boundaries with disposable real tar payloads."""
import io
from pathlib import Path
import tarfile
import tempfile
from build_inputs import temporary_directory

from safe_archive import extract_archive


def verify():
    with tempfile.TemporaryDirectory(dir=temporary_directory(), prefix='fntuios-archive-') as temporary:
        root = Path(temporary)
        archive = root / 'fixture.tar'
        cases = [('relative', 'dir/file', tarfile.REGTYPE, True),
                 ('parent', '../outside', tarfile.REGTYPE, False),
                 ('absolute', '/outside', tarfile.REGTYPE, False),
                 ('symlink', 'link', tarfile.SYMTYPE, False),
                 ('hardlink', 'link', tarfile.LNKTYPE, False),
                 ('device', 'device', tarfile.CHRTYPE, False),
                 ('windows-separator', 'dir\\file', tarfile.REGTYPE, False)]
        for label, name, kind, accepted in cases:
            with tarfile.open(archive, 'w') as writer:
                member = tarfile.TarInfo(name)
                member.type = kind
                member.linkname = '../outside' if kind in {tarfile.SYMTYPE, tarfile.LNKTYPE} else ''
                member.size = 4 if kind == tarfile.REGTYPE else 0
                writer.addfile(member, io.BytesIO(b'data') if member.size else None)
            destination = root / label
            destination.mkdir()
            try:
                extract_archive(archive, destination)
            except ValueError:
                if accepted:
                    raise
            else:
                if not accepted:
                    raise AssertionError('unsafe archive accepted: ' + label)
                if (destination / name).read_bytes() != b'data':
                    raise AssertionError('archive content changed')
        outside = root / 'sentinel'
        outside.mkdir()
        destination = root / 'existing-link'
        destination.mkdir()
        (destination / 'dir').symlink_to(outside, target_is_directory=True)
        with tarfile.open(archive, 'w') as writer:
            member = tarfile.TarInfo('dir/file'); member.size = 4
            writer.addfile(member, io.BytesIO(b'data'))
        try:
            extract_archive(archive, destination)
        except ValueError:
            pass
        else:
            raise AssertionError('existing destination symlink accepted')
        if list(outside.iterdir()):
            raise AssertionError('archive wrote through symlink')
    return True


if __name__ == '__main__':
    verify()

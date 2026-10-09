"""Extract trusted-size regular archive payloads on Python 3.11 and later."""
from pathlib import Path, PurePosixPath
import shutil
import tarfile


def extract_archive(archive, destination):
    """Reject paths and entry types that can escape or alter extraction authority."""
    destination = Path(destination).resolve()
    destination.mkdir(parents=True, exist_ok=True)
    total = 0
    seen = set()
    with tarfile.open(archive) as source:
        for count, member in enumerate(source, 1):
            relative = PurePosixPath(member.name)
            if (relative.is_absolute() or '..' in relative.parts or '\\' in member.name
                    or not relative.parts or count > 100000):
                raise ValueError('unsafe archive path or entry count: ' + member.name)
            if not (member.isfile() or member.isdir()):
                raise ValueError('archive link or special entry rejected: ' + member.name)
            if member.name in seen:
                raise ValueError('duplicate archive entry: ' + member.name)
            seen.add(member.name)
            total += member.size
            if member.size < 0 or member.size > 1024**3 or total > 2 * 1024**3:
                raise ValueError('archive size limit exceeded')
            target = destination.joinpath(*relative.parts)
            for parent in [target, *target.parents]:
                if parent == destination:
                    break
                if parent.is_symlink():
                    raise ValueError('archive destination contains a symlink: ' + str(parent))
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
                target.chmod(0o755)
                continue
            target.parent.mkdir(parents=True, exist_ok=True)
            data = source.extractfile(member)
            if data is None:
                raise ValueError('unreadable archive entry: ' + member.name)
            with data, target.open('xb') as output:
                shutil.copyfileobj(data, output)
            if target.stat().st_size != member.size:
                raise ValueError('truncated archive entry: ' + member.name)
            target.chmod(0o755 if member.mode & 0o111 else 0o644)

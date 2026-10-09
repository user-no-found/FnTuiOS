"""Validate static Linux ELF architecture before packaging."""

import struct
from pathlib import Path


MACHINE = {"amd64": 62, "arm64": 183}


def check_elf(path: Path, arch: str) -> None:
    """Require a static ELF64 little-endian executable for the selected CPU."""
    with path.open("rb") as source:
        header = source.read(64)
        if len(header) != 64 or header[:7] != b"\x7fELF\x02\x01\x01":
            raise ValueError(f"{path.name} is not a Linux-compatible ELF64 executable")
        executable_type, machine = struct.unpack_from("<HH", header, 16)
        if executable_type != 2 or machine != MACHINE[arch]:
            raise ValueError(f"{path.name} has the wrong ELF type or CPU architecture")
        offset = struct.unpack_from("<Q", header, 32)[0]
        entry_size, count = struct.unpack_from("<HH", header, 54)
        size = path.stat().st_size
        if entry_size < 56 or count == 0 or offset + entry_size * count > size:
            raise ValueError(f"{path.name} has invalid ELF program headers")
        for index in range(count):
            source.seek(offset + entry_size * index)
            program_type = struct.unpack("<I", source.read(4))[0]
            if program_type in (2, 3):  # PT_DYNAMIC or PT_INTERP
                raise ValueError(f"{path.name} requires a dynamic runtime; expected CGO=0")

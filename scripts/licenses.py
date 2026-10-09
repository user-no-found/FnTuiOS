"""Collect dependency notices from the module versions selected for the build."""
import json
from pathlib import Path
import shutil

from build_inputs import capped, goenv, run, write_json


def collect(go, source, destination):
    destination.mkdir(parents=True)
    decoder = json.JSONDecoder()
    data = run(capped([go, 'list', '-m', '-json', 'all']), cwd=source, capture=True, env=goenv())
    modules = []
    while data.strip():
        module, end = decoder.raw_decode(data.lstrip())
        data = data.lstrip()[end:]
        modules.append(module)
    listing = []
    for module in modules:
        effective = module.get('Replace', module)
        directory = effective.get('Dir')
        if not directory:
            continue
        directory = Path(directory)
        name = module['Path'].replace('/', '__')
        notices = []
        for path in sorted(directory.iterdir()):
            if path.is_file() and path.name.upper().startswith(('LICENSE', 'LICENCE', 'COPYING', 'NOTICE')):
                target = destination / name / path.name
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(path, target)
                notices.append(str(target.relative_to(destination)))
        listing.append({'module': module['Path'], 'version': module.get('Version', '(main)'), 'notices': notices})
    write_json(destination / 'modules.json', listing)

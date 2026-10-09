"""Extract computed registry choices through upstream's typed configuration API."""
import hashlib
import json
from pathlib import Path
import shutil

from build_inputs import capped, goenv, run

PROGRAM = '''package main
import ("encoding/json"; "os"; "github.com/Gaurav-Gosain/tuios/internal/config")
func main() { if err := json.NewEncoder(os.Stdout).Encode(config.Options()); err != nil { panic(err) } }
'''


def extract(go, source):
    helper = Path(source) / '.fntuios-registry-extract'
    helper.mkdir()
    try:
        (helper / 'main.go').write_text(PROGRAM)
        options = json.loads(run(capped([go, 'run', './.fntuios-registry-extract']), cwd=source, capture=True, env=goenv()))
    finally:
        shutil.rmtree(helper)
    entries = []
    for option in options:
        for value in option.get('accepted', []):
            entries.append({'id': 'settings.' + option['path'] + '.value.' + value,
                            'source': value, 'source_hash': hashlib.sha256(value.encode()).hexdigest(),
                            'file': 'internal/config/options.go', 'kind': 'display',
                            'context': option['path'] + ' accepted enum display; stored value preserved',
                            'reason': 'typed registry accepted value display projection'})
    return entries

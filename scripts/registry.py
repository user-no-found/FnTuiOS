"""Extract computed registry choices through upstream's typed configuration API."""
import hashlib
import json
from pathlib import Path
import shutil

from build_inputs import capped, goenv, run

PROGRAM = '''package main
import ("encoding/json"; "os"; "github.com/Gaurav-Gosain/tuios/internal/config")
func main() {
 presets := map[string]string{}
 for _, name := range config.Presets() {
  about, err := config.PresetInfo(name); if err != nil { panic(err) }
  presets[name] = about.Summary
 }
 if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"options": config.Options(), "presets": presets}); err != nil { panic(err) }
}
'''


def preset_entries(presets):
    """Inventory preset summaries displayed by the native preset picker."""
    entries = []
    for name, summary in sorted(presets.items()):
        if not isinstance(summary, str) or not summary:
            raise ValueError('preset summary must be nonempty: ' + name)
        entries.append({'id': 'presets.' + name + '.summary', 'source': summary,
                        'source_hash': hashlib.sha256(summary.encode()).hexdigest(),
                        'file': 'internal/config/preset.go' if name == 'tuios' else
                                'internal/config/presets/' + name + '.toml',
                        'kind': 'registry', 'context': name + ' preset picker summary; bindings preserved',
                        'reason': 'typed preset summary display projection'})
    return entries


def extract(go, source):
    helper = Path(source) / '.fntuios-registry-extract'
    helper.mkdir()
    try:
        (helper / 'main.go').write_text(PROGRAM)
        registries = json.loads(run(capped([go, 'run', './.fntuios-registry-extract']), cwd=source, capture=True, env=goenv()))
    finally:
        shutil.rmtree(helper)
    entries = preset_entries(registries['presets'])
    for option in registries['options']:
        for value in option.get('accepted', []):
            entries.append({'id': 'settings.' + option['path'] + '.value.' + value,
                            'source': value, 'source_hash': hashlib.sha256(value.encode()).hexdigest(),
                            'file': 'internal/config/options.go', 'kind': 'display',
                            'context': option['path'] + ' accepted enum display; stored value preserved',
                            'reason': 'typed registry accepted value display projection'})
    return entries

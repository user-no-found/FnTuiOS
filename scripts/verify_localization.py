#!/usr/bin/env python3
"""Exercise localization release gates against real source and controlled regressions."""
import argparse
import copy
import json
from pathlib import Path
import shutil
import tempfile

from build_inputs import ROOT, write_json, temporary_directory
import browser
import localization
import projections
import verify_display_sources


def verify(args):
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    reviewed = localization.gate(args.go, args.source, args.sip, output / 'baseline')
    candidates = json.loads((output / 'baseline/candidates.json').read_text())
    records = localization.memory()
    checks = [{'case': item, 'result': 'verified'} for item in verify_display_sources.verify(args, reviewed)]

    def rejects(name, operation):
        try:
            operation()
        except ValueError as error:
            checks.append({'case': name, 'result': 'rejected', 'diagnostic': str(error)})
            return
        raise AssertionError(name + ' was accepted')

    def require_complete(items, memory):
        _, missing = localization.reconcile(items, memory)
        if missing:
            raise ValueError(f'{len(missing)} unclassified contexts')

    target = next(e for e in reviewed if e['decision'] == 'translate' and e['kind'] == 'candidate')
    rejects('missing translation', lambda: require_complete(candidates, [r for r in records if r['source'] != target['source']]))
    changed = copy.deepcopy(candidates)
    next(e for e in changed if e['id'] == target['id'])['source'] += ' changed upstream'
    rejects('changed source', lambda: require_complete(changed, records))
    formatted = next(e for e in reviewed if e['decision'] == 'translate' and e.get('format') and localization.FORMAT.search(e['source']))
    broken = copy.deepcopy(records)
    for record in broken:
        if record['source'] == formatted['source']:
            record['translation'] = '参数已移除'
    rejects('format mismatch', lambda: require_complete(candidates, broken))
    for malformed in ['%[0]s', '%[3]*.[2]*[1]z', '%']:
        rejects('invalid format ' + malformed, lambda value=malformed: localization.format_tokens(value))
    if localization.format_tokens('%[3]*.[2]*[1]f %%') != ['%[3]*.[2]*[1]f', '%%']:
        raise AssertionError('indexed width/precision format was not preserved')
    with tempfile.TemporaryDirectory(dir=temporary_directory(), prefix='fntuios-l10n-') as temporary:
        work = Path(temporary)
        source = work / 'source'
        shutil.copytree(args.source, source, ignore=shutil.ignore_patterns('.git'))
        new_file = source / 'internal/app/fntuios_negative_probe.go'
        new_file.write_text('package app\nfunc renderNegativeProbe() string { return "Unreviewed new display phrase" }\n')
        rejects('new native display file', lambda: localization.gate(args.go, source, args.sip, output / 'new-source'))
        new_file.write_text('package app\nfunc renderNegativeProbe() string { return custom("appearance.theme", uiLabel("Nested unreviewed display")) }\n')
        rejects('new display nested inside custom call', lambda: localization.gate(args.go, source, args.sip, output / 'nested-source'))
        new_file.unlink()
        # Whitespace/line changes keep semantic contexts and memory reusable.
        path = source / target['file']
        path.write_text('\n' + path.read_text())
        localization.gate(args.go, source, args.sip, output / 'moved-lines')
        checks.append({'case': 'unchanged context after line movement', 'result': 'accepted'})
        settings = source / 'internal/app/settings_registry.go'
        settings.write_text(settings.read_text().replace('func settingLabel(', 'func displacedSettingLabel('))
        rejects('missing display adapter target', lambda: projections.apply(source, reviewed))
        sip = work / 'sip'
        shutil.copytree(args.sip, sip)
        for directory in [sip, *sip.rglob("*")]:
            if directory.is_dir():
                directory.chmod(0o755)
        path = sip / 'static/terminal.js'
        path.chmod(0o644)
        path.write_text(path.read_text() + '\n// dependency asset changed\n')
        rejects('changed browser dependency asset', lambda: localization.check_inventory(localization.inventory(candidates, args.source, sip)))
        path.write_text(path.read_text().replace('const preference = this.client.settings.transport;', 'const preference = "auto";'))
        rejects('missing transport adapter target', lambda: browser.adapt_transport(sip))
        extra = sip / 'static/new-panel.js'
        extra.write_text('document.body.textContent = "New panel";\n')
        rejects('new browser asset', lambda: localization.check_inventory(localization.inventory(candidates, args.source, sip)))
    write_json(output / 'negative-controls.json', {'checks': checks, 'baseline': str(output / 'baseline/coverage.json')})
    return checks


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=lambda s: Path(s).resolve(), required=True)
    parser.add_argument('--sip', type=lambda s: Path(s).resolve(), required=True)
    parser.add_argument('--go', type=lambda s: Path(s).resolve(), required=True)
    parser.add_argument('--output', type=Path, default=ROOT / 'dist/localization-checks')
    parser.add_argument('--verbose', action='store_true', help='print the full negative-control report')
    args = parser.parse_args()
    checks = verify(args)
    print(json.dumps(checks, ensure_ascii=False, indent=2) if args.verbose else str(args.output / 'negative-controls.json'))


if __name__ == '__main__':
    main()

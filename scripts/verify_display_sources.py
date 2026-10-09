"""Verify real generated display APIs and fail-closed source/catalog boundaries."""

import json
from pathlib import Path
import shutil
import tempfile

from buildflow.contracts import BuildError
from build_inputs import ROOT, capped, digest, goenv, run, temporary_directory, write_json
import localization
import projections
import verify_risk_display


BRIDGE = '''package app
import "github.com/Gaurav-Gosain/tuios/internal/config"
func FnDisplayProbePrefix(r *config.KeybindRegistry,p string,s config.MenuState) []config.KeybindingGroup { return fnPrefixMenuGroups(r,p,s) }
func FnDisplayProbeBinding(action,text string) string { return fnBindingDescription(action,text) }
'''


def scan(go, source, path):
    run(capped([go, 'run', '.', '-root', source, '-out', path]), cwd=ROOT / 'tools/localize', env=goenv())
    return json.loads(path.read_text())


def verify(args, reviewed):
    output = args.output / 'display'
    output.mkdir(parents=True, exist_ok=True)
    checks = []
    with tempfile.TemporaryDirectory(dir=temporary_directory(), prefix='display-') as temporary:
        work = Path(temporary)
        source = work / 'source'
        shutil.copytree(args.source, source, ignore=shutil.ignore_patterns('.git'))
        original = {p.relative_to(source): digest(p) for package in ['config', 'risk']
                    for p in (source / 'internal' / package).glob('*.go')}
        catalog = output / 'catalog.json'
        write_json(catalog, reviewed)
        apply = lambda current=catalog: run(capped([args.go, 'run', '.', '-root', source, '-catalog', current,
                                '-projection-report', output / 'projection.json']), cwd=ROOT / 'tools/localize', env=goenv())
        apply()
        projections.apply(source, reviewed)
        if any(digest(source / name) != value for name, value in original.items()):
            raise ValueError('display projection changed canonical configuration or risk source bytes')
        checks.extend(verify_risk_display.verify(args.go, source, reviewed, output / 'risk'))
        helper = source / '.fntuios-display-contract'
        helper.mkdir()
        (helper / 'main.go').write_text((ROOT / 'adapters/display_contract.go.txt').read_text())
        (source / 'internal/app/fntuios_contract_bridge.go').write_text(BRIDGE)
        expected = {}
        for entry in reviewed:
            if entry['decision'] != 'translate':
                continue
            if entry['file'] == 'internal/config/whichkey_menu.go':
                key, value = entry['source'], entry['translation']
                if entry.get('format'):
                    if localization.format_tokens(key) != ['%d']:
                        raise ValueError('prefix fixture must explicitly support its dynamic format')
                    key, value = key.replace('%d', '137'), value.replace('%d', '137')
                expected[key] = value
        payload = helper / 'expected.json'
        write_json(payload, expected)
        # The native program reads a named fixture file through stdin; no user data is inspected.
        program = (helper / 'main.go').read_text().replace('json.NewDecoder(os.Stdin)',
                    'json.NewDecoder(mustOpen(os.Args[1]))')
        program += '\nfunc mustOpen(path string) *os.File { file,err:=os.Open(path);if err!=nil{panic(err)};return file }\n'
        (helper / 'main.go').write_text(program)
        report = json.loads(run(capped([args.go, 'run', './.fntuios-display-contract', payload]),
                                cwd=source, env=goenv(), capture=True))
        write_json(output / 'contract.json', report)
        checks.extend(report['checks'])
        checks.append('canonical configuration and risk source files remain byte-identical')
        display = source / 'internal/app/fntuios_display.go'
        guard = 'if strings.HasPrefix(action, config.CommandActionPrefix) { return text }'
        if display.read_text().count(guard) != 1:
            raise ValueError('custom description negative control lost its guard anchor')
        baseline_display = display.read_text()
        display.write_text(baseline_display.replace(guard, '', 1))
        try:
            run(capped([args.go, 'run', './.fntuios-display-contract', payload]), cwd=source, env=goenv())
        except BuildError as error:
            if 'custom description collided with builtin translation' not in str(error):
                raise ValueError('custom-description negative control failed for an unrelated reason') from error
        else:
            raise ValueError('removing the custom-description guard did not break its contract')
        checks.append('removing custom-description guard reproduces a real builtin translation collision')
        display.write_text(baseline_display)

        # Restore the isolated tree before testing malicious or stale generator inputs.
        shutil.rmtree(source)
        shutil.copytree(args.source, source, ignore=shutil.ignore_patterns('.git'))

        def rejected(name, operation, reason):
            try:
                operation()
            except (BuildError, ValueError) as error:
                if reason not in str(error):
                    raise ValueError(f'{name} failed for an unrelated reason: {error}') from error
                checks.append(name)
                return
            raise ValueError(name + ' was accepted')

        removed = next(e for e in reviewed if e['kind'] == 'config-display' and e['decision'] == 'translate')
        write_json(output / 'missing.json', [e for e in reviewed if e['id'] != removed['id']])
        rejected('direct generator rejects missing display catalog entry', lambda: apply(output / 'missing.json'), 'unclassified')

        menu = source / 'internal/config/whichkey_menu.go'
        baseline_menu = menu.read_text()
        probe = source / 'internal/config/fntuios_negative_display.go'
        menu.write_text(baseline_menu.replace('title: "Windows"', 'title: fntuiosFreshLabel()', 1))
        probe.write_text('package config\nfunc fntuiosFreshLabel() string { return "UnreviewedLabel" }\n')
        entries = scan(args.go, source, output / 'new-helper.json')
        found = [e for e in entries if e['source'] == 'UnreviewedLabel']
        if len(found) != 1 or found[0]['kind'] != 'config-display':
            raise ValueError('single-word display helper in a new source file escaped semantic review')
        rejected('new file display helper rejected by an old catalog', apply, 'unclassified')

        probe.write_text('package config\nimport "fmt"\nfunc fntuiosFreshLabel() string {\n'
                        'warn := func(key, message string, count int) string { return fmt.Sprintf(message,count) }; '
                        'return warn("stable_key","Unreviewed count %d",137) }\n')
        entries = scan(args.go, source, output / 'format-helper.json')
        entry = next(e for e in entries if e['source'] == 'Unreviewed count %d')
        if not entry.get('format'):
            raise ValueError('non-first helper format parameter was not recognized')
        broken = [{'source': entry['source'], 'translation': 'Reviewed %s', 'scope': entry['id'],
                   'decision': 'translate', 'reason': 'Synthetic format-signature negative control'}]
        rejected('non-first helper format signature mismatch rejected', lambda: localization.reconcile([entry], broken), 'signature mismatch')
        probe.write_text('package config\nfunc fntuiosFreshLabel() string { '
                        'row := func(desc string) string { return desc }; return row("ShadowedLabel") }\n')
        entries = scan(args.go, source, output / 'shadow-helper.json')
        if next(e for e in entries if e['source'] == 'ShadowedLabel')['kind'] != 'config-display':
            raise ValueError('local helper shadow was mistaken for the global action helper')
        checks.append('local display helper shadows a machine helper without losing its label')

        menu.write_text(baseline_menu.replace('func menuSections(prefixType string, st MenuState) []menuSection {',
                        'func menuSections(prefixType string, st MenuState) []menuSection { fntuiosCounter++', 1))
        probe.write_text('package config\nvar fntuiosCounter int\n')
        entries = scan(args.go, source, output / 'shared-mutation.json')
        current, missing = localization.reconcile(entries, localization.memory())
        if missing:
            raise ValueError('shared mutation fixture added unrelated unreviewed text')
        write_json(output / 'mutation-catalog.json', current)
        rejected('display producer shared counter mutation rejected', lambda: apply(output / 'mutation-catalog.json'), 'mutates shared state')
    write_json(output / 'verified.json', {'checks': checks, 'verified': True,
               'boundary': 'native configuration/display APIs and controlled source mutations; preformatted LoadWarnings and arbitrary user text are not translated'})
    return checks

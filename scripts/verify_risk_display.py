"""Verify Inbox risk rendering and the unmodified risk identity boundary."""

import hashlib
import json
from pathlib import Path
import shutil

from buildflow.contracts import BuildError
from build_inputs import ROOT, capped, digest, goenv, run, write_json


BRIDGE = '''package app
import "github.com/Gaurav-Gosain/tuios/internal/session"
// FnRiskContractLines calls the actual Inbox risk line renderer.
func FnRiskContractLines(names []string, width int) []string {
    return inboxRiskLines(session.AttentionItem{Risk: names}, width)
}
'''


def expectations(reviewed):
    """Read the independently reviewed catalog without supplying translations."""
    entries = [entry for entry in reviewed if entry['file'] == 'internal/risk/builtin.go'
               and entry['kind'] == 'registry']
    if len(entries) != 26 or len({entry['source'] for entry in entries}) != 26:
        raise ValueError('risk display requires exactly 26 distinct reviewed contexts')
    for entry in entries:
        if (entry['decision'] != 'translate' or not entry.get('translation')
                or entry['translation'] == entry['source']
                or entry['source_hash'] != hashlib.sha256(entry['source'].encode()).hexdigest()):
            raise ValueError('risk display fixture has an invalid reviewed translation: ' + entry['id'])
    return {entry['source']: entry['translation'] for entry in entries}


def verify(go, source, reviewed, output):
    """Run native positive and semantic negative controls on an isolated tree."""
    source, output = Path(source), Path(output)
    output.mkdir(parents=True, exist_ok=True)
    expected = output / 'expected.json'
    write_json(expected, expectations(reviewed))
    helper = source / '.fntuios-risk-contract'
    bridge = source / 'internal/app/fntuios_risk_contract_bridge.go'
    adapter = source / 'internal/app/inbox_approvals_ext.go'
    if helper.exists() or bridge.exists():
        raise ValueError('risk contract temporary paths already exist')
    original = adapter.read_bytes()
    original_text = original.decode()
    canonical = {str(path.relative_to(source)): digest(path)
                 for path in (source / 'internal/risk').glob('*.go')}
    controls = [
        ('name', 'printableTitle(fnRiskName(name))', 'printableTitle(name)',
         'risk display name is untranslated: recursive delete'),
        ('explanation', 'return fnRiskText(why)', 'return why',
         'risk display explanation is untranslated: recursive delete'),
    ]
    for _, anchor, _, _ in controls:
        if original_text.count(anchor) != 1:
            raise ValueError('risk display negative control lost its unique adapter anchor: ' + anchor)
    command = capped([go, 'run', './.fntuios-risk-contract', expected.resolve()])
    negatives = []
    try:
        helper.mkdir()
        (helper / 'main.go').write_text((ROOT / 'adapters/risk_display_contract.go.txt').read_text())
        bridge.write_text(BRIDGE)
        report = json.loads(run(command, cwd=source, env=goenv(), capture=True))
        write_json(output / 'contract.json', report)
        for name, anchor, replacement, diagnostic in controls:
            adapter.write_text(original_text.replace(anchor, replacement, 1))
            try:
                run(command, cwd=source, env=goenv(), capture=True)
            except BuildError as error:
                if diagnostic not in str(error):
                    raise ValueError('risk display negative control failed for an unrelated reason') from error
                negatives.append({'adapter': name, 'expected_diagnostic': diagnostic,
                                  'failure': str(error), 'verified': True})
            else:
                raise ValueError('removing the risk ' + name + ' adapter did not break rendering')
            finally:
                adapter.write_bytes(original)
        if any(digest(source / name) != value for name, value in canonical.items()):
            raise ValueError('risk display contract changed canonical risk source bytes')
        checks = [*report['checks'],
                  'removing the name and explanation display adapters independently restores English and fails rendering checks',
                  'canonical risk source files remain byte-identical throughout the renderer contract']
        write_json(output / 'negative-controls.json', negatives)
        write_json(output / 'verified.json', {'verified': True, 'checks': checks,
                   'canonical_source_sha256': canonical,
                   'boundary': 'actual Inbox risk lines and pure risk APIs; commands are inert match inputs'})
        return checks
    finally:
        adapter.write_bytes(original)
        bridge.unlink(missing_ok=True)
        if helper.exists():
            shutil.rmtree(helper)

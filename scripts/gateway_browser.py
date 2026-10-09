"""Consume shared connection diagnostics through guarded sip lifecycle seams."""
import ast
import hashlib
import json
import os
from pathlib import Path
import re
import shutil

from build_inputs import ROOT, write_json
from build_support import tree_digest
import localization


def dependency():
    """Record the explicit shared web source separately from Go runtime inputs."""
    configured = os.environ.get('FNPACKKIT_PATH')
    if not configured:
        raise ValueError('FNPACKKIT_PATH is required for shared gateway browser assets')
    root = Path(configured).resolve() / 'web'
    for name in ['gateway-status.js', 'gateway-status.css', 'gateway-transport.js']:
        if not (root / name).is_file():
            raise ValueError('missing FnPackKit browser asset: ' + name)
    return {'path': str(root), 'source_tree_sha256': tree_digest(root)}


def contract(source):
    """Fail on missing or ambiguous shared message declarations."""
    result = {}
    for name, pattern in {
        'required': r'const requiredLabels = (\[[\s\S]*?\]);',
        'reasons': r'const reasons = new Set\((\[[\s\S]*?\])\);',
    }.items():
        matches = re.findall(pattern, source)
        if len(matches) != 1:
            raise ValueError('shared gateway message contract drift: ' + name)
        values = ast.literal_eval(matches[0])
        if not isinstance(values, list) or not all(isinstance(value, str) for value in values):
            raise ValueError('invalid shared gateway message contract')
        result[name] = set(values)
    return result


def messages(output, shared_source):
    source = ROOT / 'assets/gateway-status.messages.json'
    values = json.loads(source.read_text())
    required = contract(shared_source)
    expected = required['required'] | (required['reasons'] - {'unknown'}) | {'retry'}
    if set(values) != expected:
        raise ValueError('gateway message catalog keys differ from shared contract: ' +
                         ', '.join(sorted(set(values) ^ expected)))
    candidates = [{'id': 'gateway-status.' + key, 'kind': 'browser',
                   'file': 'assets/gateway-status.messages.json', 'source': text,
                   'source_hash': hashlib.sha256(text.encode()).hexdigest(), 'context': key}
                  for key, text in values.items()]
    reviewed, missing = localization.reconcile(candidates, localization.memory())
    write_json(output / 'gateway-status-unclassified.json', missing)
    if missing:
        raise ValueError(f'{len(missing)} unreviewed gateway status messages')
    write_json(output / 'gateway-status-catalog.json', reviewed)
    labels = {item['context']: item['translation'] if item['decision'] == 'translate' else item['source']
              for item in reviewed}
    return {**{key: labels[key] for key in required['required'] | {'retry'}},
            'reasons': {key: labels[key] for key in required['reasons'] - {'unknown'}}}


def replace(path, anchor, replacement):
    text = path.read_text()
    if text.count(anchor) != 1:
        raise ValueError(f'gateway lifecycle adapter drift in {path.name}: {anchor}')
    path.write_text(text.replace(anchor, replacement))


def apply(sip, output, shared):
    """Install checked shared assets without replacing renderer or input hooks."""
    root = Path(shared['path'])
    if tree_digest(root) != shared['source_tree_sha256']:
        raise ValueError('shared gateway browser source changed during build')
    labels = messages(output, (root / 'gateway-status.js').read_text())
    static = Path(sip) / 'static'
    for name in ['gateway-status.js', 'gateway-status.css', 'gateway-transport.js']:
        shutil.copyfile(root / name, static / ('fnpackkit-' + name))
    shutil.copyfile(ROOT / 'assets/gateway-status.js', static / 'gateway-status.js')
    (static / 'gateway-status.messages.js').write_text('window.fnGatewayStatusLabels = ' +
                                                    json.dumps(labels, ensure_ascii=False) + ';\n')
    replace(static / 'index.html', '<link rel="stylesheet" href="static/terminal.css">',
            '<link rel="stylesheet" href="static/terminal.css">\n'
            '    <link rel="stylesheet" href="static/fnpackkit-gateway-status.css">')
    replace(static / 'index.html', '<script src="static/terminal.js"></script>',
            '<script src="static/gateway-status.messages.js"></script>\n'
            '    <script src="static/fnpackkit-gateway-status.js"></script>\n'
            '    <script src="static/fnpackkit-gateway-transport.js"></script>\n'
            '    <script src="static/gateway-status.js"></script>\n'
            '    <script src="static/terminal.js"></script>')
    path = static / 'terminal.js'
    replace(path, 'sipTerm.init().catch(console.error);',
            'window.FnGatewayStatus.install(sipTerm);\n'
            '        sipTerm.init().catch(error => { window.FnGatewayStatus.failed(sipTerm); console.error(error); });')
    replace(path, 'async connect() {\n            this.updateStatus(',
            'async connect() {\n            window.FnGatewayStatus.connecting(this);\n            this.updateStatus(')
    replace(path, 'onConnected(name, status, text) {',
            'onConnected(name, status, text) {\n            window.FnGatewayStatus.connected(this);')
    replace(path, 'handleDisconnect() {',
            'handleDisconnect() {\n            window.FnGatewayStatus.failed(this);')
    replace(path, 'case MSG_OUTPUT:',
            'case MSG_OUTPUT:\n                    if (data.length > 1) window.FnGatewayStatus.output(this);')

    replace(path, 'list.push(W.webSocketTransport(client.urls.wsUrl));',
            'list.push(window.FnGatewayStatus.transport(W, client.urls.wsUrl));')

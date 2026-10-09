"""Install the image-paste browser surface into a uniquely matched sip source."""
import hashlib
import json
from pathlib import Path

from build_inputs import ROOT, write_json
import localization


def messages(output):
    """Require a reviewed translation for every locally owned browser message."""
    source = ROOT / 'assets/image-paste.messages.json'
    values = json.loads(source.read_text())
    candidates = [{
        'id': 'image-paste.' + key, 'kind': 'browser', 'file': 'assets/image-paste.messages.json',
        'source': text, 'source_hash': hashlib.sha256(text.encode()).hexdigest(),
        'context': key,
    } for key, text in values.items()]
    reviewed, missing = localization.reconcile(candidates, localization.memory())
    write_json(output / 'image-paste-unclassified.json', missing)
    if missing:
        raise ValueError(f'{len(missing)} unreviewed image paste messages')
    write_json(output / 'image-paste-catalog.json', reviewed)
    return {item['context']: item['translation'] if item['decision'] == 'translate' else item['source']
            for item in reviewed}


def replace(path, anchor, replacement):
    text = path.read_text()
    if text.count(anchor) != 1:
        raise ValueError(f'image-paste adapter no longer matches uniquely in {path.name}: {anchor}')
    path.write_text(text.replace(anchor, replacement))


def apply(sip, output):
    """Connect the maintained browser plugin without patching dependency caches."""
    labels = messages(output)
    static = Path(sip) / 'static'
    for name in ['image-paste.js', 'paste-input.js', 'image-paste.css']:
        (static / name).write_bytes((ROOT / 'assets' / name).read_bytes())
    (static / 'image-paste.messages.js').write_text('window.fnImagePasteMessages = ' +
                                                 json.dumps(labels, ensure_ascii=False) + ';\n')
    replace(static / 'index.html', '<link rel="stylesheet" href="static/terminal.css">',
            '<link rel="stylesheet" href="static/terminal.css">\n'
            '    <link rel="stylesheet" href="static/image-paste.css">')
    replace(static / 'index.html', '<script src="static/terminal.js"></script>',
            '<script src="static/image-paste.messages.js"></script>\n'
            '    <script src="static/paste-input.js"></script>\n'
            '    <script src="static/image-paste.js"></script>\n'
            '    <script src="static/terminal.js"></script>')
    replace(static / 'terminal.js', 'sipClient = sipTerm;\n        sipTerm.init()',
            'sipClient = sipTerm;\n        window.FnImagePaste.install(sipTerm);\n        sipTerm.init()')
    replace(static / 'terminal.js', 'async sendMessage(type, payload) {',
            'async sendMessage(type, payload) {\n'
            '            return window.FnImagePaste.transmit(this, type, payload);\n'
            '        }\n\n        async sendMessageRaw(type, payload) {')
    replace(static / 'terminal.js', 'handleMessage(data) {',
            'handleMessage(data) {\n            if (window.FnImagePaste.receive(this, data)) return;')
    replace(static / 'terminal.js', 'handleDisconnect() {',
            'handleDisconnect() {\n            window.FnImagePaste.disconnect(this);')
    replace(static / 'terminal.js', 'case MSG_CLOSE:',
            'case MSG_CLOSE:\n                    window.FnImagePaste.disconnect(this);')
    replace(static / 'terminal.js', 'onCopyKey(ev) {',
            'onCopyKey(ev) {\n            if (!window.FnImagePaste.key(this, ev)) return false;')

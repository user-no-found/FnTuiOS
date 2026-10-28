"""Project explicit phone keyboard ownership without rewriting the vendored terminal."""
from pathlib import Path
import shutil


def apply(sip, root):
    sip, root = Path(sip), Path(root)
    path = sip / 'static/terminal.js'
    text = path.read_text()
    hooks = [
        ('focusTarget: () => (this.webterm ? this.webterm.xterm.textarea : null),',
         'focusTarget: () => window.FnMobileKeyboard.focusTarget(this),'),
        ('                actions,\n                keyBar:',
         '                actions,\n                keyboardKey: false,\n                keyBar:'),
        ('            this.setupTouchMouse();',
         '            window.FnMobileKeyboard.install(this);\n            this.setupTouchMouse();'),
    ]
    for anchor, replacement in hooks:
        if text.count(anchor) != 1:
            raise ValueError('mobile keyboard source hook changed: ' + anchor)
        text = text.replace(anchor, replacement, 1)
    path.write_text(text)
    shutil.copyfile(root / 'assets/mobile-input.js', sip / 'static/mobile-input.js')
    shutil.copyfile(root / 'assets/mobile-keyboard.js', sip / 'static/mobile-keyboard.js')
    index = sip / 'static/index.html'
    text = index.read_text()
    anchor = '<script src="static/terminal.js"></script>'
    if text.count(anchor) != 1:
        raise ValueError('mobile keyboard HTML hook changed')
    scripts = '<script src="static/mobile-input.js"></script>\n    <script src="static/mobile-keyboard.js"></script>\n    '
    index.write_text(text.replace(anchor, scripts + anchor, 1))

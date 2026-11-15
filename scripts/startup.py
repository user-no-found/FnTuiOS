"""Keep the regular terminal face critical; defer optional faces until connected."""
from pathlib import Path
import re
import shutil


def apply(sip, root):
    static = Path(sip) / 'static'
    terminal = static / 'terminal.js'
    text = terminal.read_text()
    for face, weight, style in [('Bold', '700', 'normal'), ('Italic', '400', 'italic'),
                                ('BoldItalic', '700', 'italic')]:
        anchor = f"                    {{ source: font('{face}'), weight: '{weight}', style: '{style}' }},\n"
        if text.count(anchor) != 1:
            raise ValueError('startup font contract changed: ' + face)
        text = text.replace(anchor, '', 1)
    anchor = '            await this.connect();\n            this.webterm.focus();'
    if text.count(anchor) != 1:
        raise ValueError('startup connection contract changed')
    text = text.replace(anchor, anchor + '\n            window.FnStartupFonts.load(this.fontFamily);', 1)
    page = static / 'index.html'
    html = page.read_text()
    bold = '    <link rel="preload" href="static/fonts/JetBrainsMonoNerdFontMono-Bold.woff2" as="font" type="font/woff2" crossorigin>\n'
    script = '<script src="static/terminal.js"></script>'
    if html.count(bold) != 1 or html.count(script) != 1:
        raise ValueError('startup HTML contract changed')
    html = html.replace(bold, '', 1).replace(script,
        '<script src="static/startup-fonts.js"></script>\n    ' + script, 1)
    stylesheet = static / 'terminal.css'
    css = stylesheet.read_text()
    for face in ['Bold', 'Italic', 'BoldItalic']:
        pattern = r'@font-face\s*\{[^}]*JetBrainsMonoNerdFontMono-' + face + r'\.woff2[^}]*\}\n'
        css, count = re.subn(pattern, '', css)
        if count != 1:
            raise ValueError('startup font stylesheet contract changed: ' + face)
    shutil.copyfile(Path(root) / 'assets/startup-fonts.js', static / 'startup-fonts.js')
    terminal.write_text(text)
    page.write_text(html)
    stylesheet.write_text(css)

"""Project client-local phone navigation onto the selected TUIOS/sip inputs."""
from pathlib import Path
import shutil
import mobile_keyboard

from build_inputs import ROOT, run


def replace(path, anchor, replacement):
    text = path.read_text()
    if text.count(anchor) != 1:
        raise ValueError(f'mobile adapter context drift: {path.name}: {anchor[:80]}')
    path.write_text(text.replace(anchor, replacement))


def apply(source, sip, go):
    source, sip = Path(source), Path(sip)
    files = []
    for module, destination in [('app', 'internal/app'), ('web', 'cmd/tuios-web')]:
        for template in (ROOT / 'adapters/mobile' / module).glob('*.go.txt'):
            path = source / destination / template.name.removesuffix('.txt')
            if path.exists():
                raise ValueError(f'mobile adapter destination already exists: {path}')
            shutil.copyfile(template, path)
            files.append(path)
    geometry = source / 'internal/app/os_geometry.go'
    anchor = 'func (m *OS) sidebarWidthFor(prefer int) int {'
    replace(geometry, anchor, anchor + '\n\tif width, handled := m.fnMobileSidebarWidth(); handled { return width }')
    anchor = 'func (m *OS) secondarySidebarWidth() int {'
    replace(geometry, anchor, anchor + '\n\tif m.fnCompactTouch() { return 0 }')
    replace(geometry, 'if !m.SidebarRevealedForFocus {',
            'if !m.SidebarRevealedForFocus && !m.fnCompactTouch() {')
    main = source / 'cmd/tuios-web/main.go'
    anchor = 'sipConfig.MobilePrefix, sipConfig.MobileRows = mobileBar(config.NewKeybindRegistry(userConfig), leader)'
    replace(main, anchor, anchor + '\n\tinstallFnMobileControls(&sipConfig, config.NewKeybindRegistry(userConfig), leader)')
    replace(main, '\t\tTouchClient:    touch,',
            '\t\tTouchClient:    touch,\n\t\tStartInTerminalMode: touch,')
    terminal = sip / 'static/terminal.js'
    # Controls are app-owned targets projected by the shared runtime panel.
    for name in ['panel-controls.js', 'text-copy.js', 'text-copy.css']:
        shutil.copyfile(ROOT / 'assets' / name, sip / 'static' / name)
    replace(sip / 'static/index.html', '<script src="static/terminal.js"></script>',
            '<script src="static/text-copy.js"></script>\n    <script src="static/panel-controls.js"></script>\n    <script src="static/terminal.js"></script>')
    replace(terminal, 'window.FnImagePaste.install(sipTerm);',
            'window.FnImagePaste.install(sipTerm);\n        window.FnPanelControls.install(sipTerm);')
    # Native modal dialogs make the rest of the page inert, including a body-level copy textarea.
    replace(terminal, 'document.body.appendChild(ta);',
            '(document.querySelector("#fn-text-copy[open]") || document.body).appendChild(ta);')
    replace(terminal, "ta.setAttribute('readonly', '');",
            "ta.setAttribute('readonly', '');\n            ta.setAttribute('inputmode', 'none');")
    replace(terminal, 'if (window.FnImagePaste.receive(this, data)) return;',
            'if (window.FnPanelControls.receive(this, data)) return;\n            if (window.FnImagePaste.receive(this, data)) return;')
    for anchor in ['this.connected = true;', 'this.connected = false;', 'this.readOnly = options.readOnly || false;']:
        text = terminal.read_text()
        if anchor not in text:
            raise ValueError('panel control connection adapter drift')
        terminal.write_text(text.replace(anchor, anchor + '\n            window.FnPanelControls?.sync(this);'))
    text = terminal.read_text()
    terminal.write_text(text.replace('window.FnImagePaste.disconnect(this);',
                                    'window.FnPanelControls.disconnect(this); window.FnImagePaste.disconnect(this);'))
    replace(source / 'cmd/tuios-web/browser_image.go',
            'req := browserImageRequest{}',
            'if browserPanelControl(sess, data, reply, readOnly) { return true }\n\treq := browserImageRequest{}')
    replace(source / 'internal/app/browser_image.go', 'case *browserImageJob:',
            'case browserPanelSplit:\n\t\tmsg.answer <- m.applyBrowserPanelSplit(msg)\n\t\treturn true, nil\n\tcase browserPanelCopy:\n\t\tmsg.answer <- m.copyBrowserPanel(msg)\n\t\treturn true, nil\n\tcase *browserImageJob:')
    files.extend([source / 'internal/app/browser_image.go', source / 'cmd/tuios-web/browser_image.go'])
    mobile_keyboard.apply(sip, ROOT)
    run([str(Path(go).with_name('gofmt')), '-w', *files, geometry, main], cwd=source)

"""Apply bounded browser image upload integration to reviewed upstream sources."""
from pathlib import Path
import json
import re
import shutil
from build_inputs import run

import localization

ROOT = Path(__file__).resolve().parents[1]


def browser_hint():
    message = json.loads((ROOT / 'assets/image-paste.messages.json').read_text())['internal_hint']
    reviewed, missing = localization.reconcile([{'id': 'image-paste.internal_hint', 'kind': 'browser',
        'file': 'assets/image-paste.messages.json', 'source': message, 'context': 'internal_hint'}], localization.memory())
    if missing or reviewed[0]['decision'] != 'translate':
        raise ValueError('browser image paste menu hint lacks reviewed translation')
    return reviewed[0]['translation']


def replace(path, before, after):
    text = path.read_text()
    if text.count(before) != 1:
        raise ValueError(f'image paste adapter anchor drift: {path.name}: {before[:70]}')
    path.write_text(text.replace(before, after))


def apply(source, sip, go=None):
    source, sip = Path(source), Path(sip)
    templates = ROOT / 'adapters/imagepaste'
    for module, destination in [('app', 'internal/app'), ('web', 'cmd/tuios-web'), ('session', 'internal/session')]:
        for template in (templates / module).glob('*.go.txt'):
            path = source / destination / template.name.removesuffix('.txt')
            if path.exists():
                raise ValueError(f'image paste adapter destination already exists: {path}')
            shutil.copyfile(template, path)
    upload = source / 'internal/imageupload'
    if upload.exists():
        raise ValueError('image upload package already exists upstream')
    shutil.copytree(ROOT / 'internal/imageupload', upload)
    replace(source / 'internal/app/os.go', 'type OS struct {',
            'type OS struct {\n\tbrowserImages *BrowserImageBridge\n\tbrowserImageEpoch uint64')
    replace(source / 'internal/app/os.go', 'func (m *OS) SwitchToSession(targetSession string) error {',
            'func (m *OS) SwitchToSession(targetSession string) error {\n\tm.browserImageEpoch++')
    replace(source / 'internal/app/host_attach.go', 'func (m *OS) SwitchToHostSession(host, name string, create bool) error {',
            'func (m *OS) SwitchToHostSession(host, name string, create bool) error {\n\tm.browserImageEpoch++')
    replace(source / 'internal/app/update.go', 'func (m *OS) update(msg tea.Msg) (tea.Model, tea.Cmd) {',
            'func (m *OS) update(msg tea.Msg) (tea.Model, tea.Cmd) {\n'
            '\tif handled, cmd := m.handleBrowserImage(msg); handled { return m, cmd }')
    image = source / 'internal/app/image_paste.go'
    text = image.read_text()
    text, count = re.subn(r'(case m\.BrowserClient:\n\s*return )"(?:[^"\\]|\\.)*"',
                          lambda match: match[1] + json.dumps(browser_hint(), ensure_ascii=False), text)
    if count != 1:
        raise ValueError('browser image paste menu hint adapter drift')
    start = text.index('func (m *OS) applyImagePasted(msg ImagePastedMsg) {')
    end = text.index('\n// pastePathText ', start)
    body = text[start:end]
    if body.count('\t\treturn\n') != 7 or not body.endswith('}\n'):
        raise ValueError('image paste completion adapter drift')
    body = body.replace('func (m *OS) applyImagePasted(msg ImagePastedMsg) {',
                        'func (m *OS) applyImagePasted(msg ImagePastedMsg) bool {')
    body = body.replace('\t\treturn\n', '\t\treturn false\n')
    body = body[:-2] + '\treturn true\n}\n'
    image.write_text(text[:start] + body + text[end:])
    main = source / 'cmd/tuios-web/main.go'
    replace(main, '\tinstallInboxLink(&sipConfig)', '\tinstallInboxLink(&sipConfig)\n\tinstallBrowserImages(&sipConfig)')
    replace(main, '\tmodel.BindProgram(program)', '\tmodel.BindProgram(program)\n\tregisterBrowserImages(sess, model, program)')
    replace(sip / 'sip.go', '\tSessionMiddleware []SessionMiddleware',
            '\tSessionMiddleware []SessionMiddleware\n\n'
            '\t// InputControl handles bounded WS controls in terminal input order.\n'
            '\t// The handler acknowledges a marker processed by its model before returning.\n'
            '\tInputControl func(SessionIO, []byte, func([]byte)) bool')
    handlers = sip / 'handlers.go'
    before = '\t\tif !s.processInput(data, session, info, apply) {'
    after = '''\t\tif len(data) > 0 && data[0] == '9' {
            if s.config.InputControl != nil {
                reply := func(payload []byte) {
                    replyCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
                    defer cancel()
                    _ = conn.Write(replyCtx, websocket.MessageBinary, append([]byte{'9'}, payload...))
                }
                if !s.config.InputControl(session, data[1:], reply) { return }
            }
            continue
        }
'''+before
    replace(handlers, before, after)
    if go is not None:
        paths = [source / 'internal/app/os.go', source / 'internal/app/update.go', image,
                 source / 'internal/app/host_attach.go', main, sip / 'sip.go', handlers]
        for module, destination in [('app', 'internal/app'), ('web', 'cmd/tuios-web'), ('session', 'internal/session')]:
            paths.extend(source / destination / p.name.removesuffix('.txt')
                         for p in (templates / module).glob('*.go.txt'))
        paths.extend(upload.glob('*.go'))
        run([Path(go).with_name('gofmt'), '-w', *paths], cwd=source)

"""Extract bounded browser display spans and guard the complete dependency asset set."""
import hashlib
from html.parser import HTMLParser
from pathlib import Path
import re


class Page(HTMLParser):
    """Record actual text nodes and accessible labels without changing attributes."""

    def __init__(self, text):
        super().__init__(convert_charrefs=False)
        self.text = text
        self.lines = text.splitlines(keepends=True)
        self.spans = []
        self.stack = []

    def source_offset(self):
        line, col = self.getpos()
        return sum(map(len, self.lines[:line - 1])) + col

    def handle_starttag(self, tag, attrs):
        if tag not in {"meta", "link", "input", "br"}:
            self.stack.append(tag)
        raw = self.get_starttag_text()
        for match in re.finditer(r'(?:title|aria-label|placeholder)="([^"]*)"', raw):
            self.spans.append((self.source_offset() + match.start(1), self.source_offset() + match.end(1), "attribute"))

    def handle_endtag(self, tag):
        if tag in self.stack:
            self.stack = self.stack[:len(self.stack) - self.stack[::-1].index(tag) - 1]

    def handle_data(self, data):
        if re.search('[A-Za-z]', data) and "{{" not in data and not any(x in self.stack for x in ["script", "style"]):
            self.spans.append((self.source_offset(), self.source_offset() + len(data), "text"))


def record(file, text, start, end, context, encoding="raw"):
    source = text[start:end]
    return {"id": f"sip:{file}:{hashlib.sha256(source.encode()).hexdigest()[:16]}:{start}",
            "source": source, "source_hash": hashlib.sha256(source.encode()).hexdigest(),
            "file": "sip/" + file, "context": context, "kind": "browser",
            "reason": "browser display span", "start": start, "end": end,
            "encoding": encoding}


def js_literals(text):
    """Walk JS lexical strings, skipping comments and escaped quote characters."""
    i = 0
    while i < len(text):
        if text.startswith('//', i):
            end = text.find('\n', i)
            i = len(text) if end < 0 else end
        elif text.startswith('/*', i):
            end = text.find('*/', i + 2)
            if end < 0:
                raise ValueError("unterminated JS comment")
            i = end + 2
        elif text[i] in "\"'`":
            quote = text[i]
            start = i = i + 1
            while i < len(text) and text[i] != quote:
                i += 2 if text[i] == '\\' else 1
            if i >= len(text):
                raise ValueError("unterminated JS string")
            yield start, i
            i += 1
        else:
            i += 1


def extract(root):
    root = Path(root)
    result = []
    page = (root / "static/index.html").read_text()
    parser = Page(page)
    parser.feed(page)
    for start, end, context in parser.spans:
        result.append(record("static/index.html", page, start, end, context))
    for name in ("terminal.js",):
        text = (root / "static" / name).read_text()
        # Literals are selected by display call/property context. Whole-asset hashes
        # separately make any dependency edit a mandatory classification review.
        for start, end in js_literals(text):
            line_start = text.rfind('\n', 0, start - 1) + 1
            line_end = text.find('\n', end + 1)
            line = text[line_start:line_end]
            prefix = text[line_start:start - 1]
            value = text[start:end]
            display = bool(re.search(r"(?:title:|\.title\s*=|TOUCH_HINT_TEXT\s*=|textContent\s*=)\s*$", prefix))
            display |= "updateStatus(" in line and (" " in value or value[:1].isupper())
            display |= "onConnected(" in line and value.startswith("Connected")
            display |= value.startswith("\\r\\n\\x1b[33m[Session ended")
            display |= "btn.title =" in line and (" " in value)
            if display and re.search('[A-Za-z]', value):
                result.append(record("static/" + name, text, start, end, line.strip(), "js"))
    # WebTerm owns mobile labels in sip's pinned, minified bundle. Select only
    # display properties, never keyboard codes or terminal data strings.
    bundle = (root / "static/webterm.js").read_text()
    spans = set()
    patterns = [r'\btitle:"([^"\\]*)"', r'\.setAttribute\("aria-label","([^"\\]*)"\)',
                r'\.title=[^,;]{0,80}\?"([^"\\]*)":"([^"\\]*)"',
                r'\b[A-Za-z_$][\w$]*="(Hold, then drag to move or select\.)"']
    for pattern in patterns:
        for match in re.finditer(pattern, bundle):
            for group in range(1, len(match.groups()) + 1):
                start, end = match.span(group)
                if re.search('[A-Za-z]', bundle[start:end]):
                    spans.add((start, end))
    if not spans:
        raise ValueError("webterm mobile display selectors no longer match")
    for start, end in sorted(spans):
        result.append(record("static/webterm.js", bundle, start, end, bundle[max(0, start-45):end+35], "js"))
    return result


def asset_inventory(root):
    return {str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in sorted(Path(root).rglob('*')) if p.is_file() and p.suffix in {'.html', '.js', '.css', '.go'}}


def apply(root, entries):
    files = {}
    for e in entries:
        if e["kind"] != "browser" or e["decision"] != "translate":
            continue
        files.setdefault(e["file"].removeprefix("sip/"), []).append(e)
    for file, changes in files.items():
        path = Path(root) / file
        text = path.read_text()
        for e in sorted(changes, key=lambda e: e["start"], reverse=True):
            if text[e["start"]:e["end"]] != e["source"]:
                raise ValueError(f"browser context drift: {e['id']}")
            value = e["translation"]
            if e.get("encoding") == "js" and "'" in value:
                value = value.replace("'", "\\'")
            text = text[:e["start"]] + value + text[e["end"]:]
        path.write_text(text)


def adapt_transport(root):
    path = Path(root) / "static/terminal.js"
    text = path.read_text()
    anchor = "const preference = this.client.settings.transport;"
    if text.count(anchor) != 1:
        raise ValueError("sip WebSocket transport adapter no longer matches uniquely")
    path.write_text(text.replace(anchor, "const preference = 'websocket';"))
    path = Path(root) / "static/index.html"
    text = path.read_text()
    text, count = re.subn(r'(<select id="transport-select">).*?(</select>)',
                          r'\1<option value="websocket">WebSocket</option>\2', text, flags=re.S)
    if count != 1:
        raise ValueError("sip transport selector no longer matches uniquely")
    path.write_text(text.replace('<html lang="en">', '<html lang="zh-CN">'))

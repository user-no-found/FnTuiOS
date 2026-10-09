"""Reconcile reviewed translation memory with exact semantic source contexts."""
import collections
import hashlib
import json
import re

from build_inputs import ROOT, activity, capped, digest, goenv, run, status, write_json
import browser
import registry

KINDS = {'candidate', 'registry', 'display', 'browser', 'config-display'}
FORMAT = re.compile(r'%(?:\[[1-9]\d*\])?[-+# 0]*(?:(?:\[[1-9]\d*\])?\*|\d+)?(?:\.(?:(?:\[[1-9]\d*\])?\*|\d*))?(?:\[[1-9]\d*\])?[vTtbcdoOqxXUeEfFgGspw%]')


def format_tokens(value):
    tokens = []
    position = 0
    while (position := value.find('%', position)) >= 0:
        match = FORMAT.match(value, position)
        if not match:
            raise ValueError('unsupported or malformed Go format directive: ' + value[position:position + 32])
        tokens.append(match[0])
        position = match.end()
    return tokens



def memory():
    entries = []
    for path in sorted((ROOT / 'locales/memory').glob('*.json')):
        data = json.loads(path.read_text())
        for record in (data if isinstance(data, list) else data['entries']):
            if record.get('decision', 'translate') == 'translate':
                record.setdefault('reason', 'Reviewed Simplified Chinese UI translation; preserve arguments and user data.')
            entries.append(record)
    return entries


def signatures(value, formatted=False):
    return (format_tokens(value) if formatted else [], re.findall(r'\$\{[^}]*\}', value),
            re.findall(r'\\(?:x[0-9A-Fa-f]{2}|[nrte])', value))


def reconcile(candidates, records):
    result, missing = [], []
    indexed = collections.defaultdict(list)
    for record in records:
        indexed[record['source']].append(record)
    for entry in candidates:
        if entry['kind'] not in KINDS:
            continue
        options = []
        for record in indexed[entry['source']]:
            scope = record.get('scope', 'native-ui')
            generic = scope == ('browser' if entry['kind'] == 'browser' else 'native-ui') or (scope == 'registry' and entry['kind'] in {'registry', 'display'})
            if generic or entry['file'].startswith(scope) or entry['id'] == scope:
                options.append((0 if generic else len(scope), record))
        if not options:
            missing.append(entry)
            continue
        specificity = max(x[0] for x in options)
        options = [x[1] for x in options if x[0] == specificity]
        choices = {(x.get('decision', 'translate'), x.get('translation', ''), x.get('reason', '')) for x in options}
        if len({(x[0], x[1]) for x in choices}) != 1:
            raise ValueError('ambiguous translation memory: ' + entry['id'])
        record = options[0]
        decision = record.get('decision', 'translate')
        translation = record.get('translation', '')
        if decision not in {'keep', 'translate'} or not record.get('reason'):
            raise ValueError('translation/classification requires decision and reason: ' + entry['id'])
        if decision == 'translate':
            if not translation or translation == entry['source']:
                raise ValueError('empty or unchanged translation: ' + entry['id'])
            if signatures(entry['source'], entry.get('format', False)) != signatures(translation, entry.get('format', False)):
                raise ValueError('format/template/control signature mismatch: ' + entry['id'])
        result.append({**entry, 'decision': decision, 'translation': translation, 'review_reason': record['reason']})
    return result, missing


def inventory(candidates, source, sip):
    files = collections.defaultdict(list)
    for e in candidates:
        if e['kind'] != 'browser':
            files[e['file']].append([e['id'], e['source_hash'], e['kind'], e['context']])
    hashes = {file: hashlib.sha256(json.dumps(entries, ensure_ascii=False, sort_keys=True).encode()).hexdigest()
              for file, entries in sorted(files.items())}
    assets = {}
    embeds = {}
    for directory in ['cmd', 'internal', 'assets']:
        for path in sorted((source / directory).rglob('*')):
            if not path.is_file() or 'testdata' in path.parts or path.name.endswith('_test.go'):
                continue
            relative = str(path.relative_to(source))
            if path.suffix == '.go':
                directives = re.findall(r'^//go:embed .+$', path.read_text(), re.M)
                if directives:
                    embeds[relative] = directives
            else:
                assets[relative] = digest(path)
    return {'go_literal_contexts': hashes, 'runtime_assets': assets, 'embed_directives': embeds,
            'sip_assets': browser.asset_inventory(sip),
            'go_mod_sha256': digest(source / 'go.mod'), 'scanner_sha256': scanner_digest()}


def scanner_digest():
    paths = sorted((ROOT / 'tools/localize').glob('*.go')) + [ROOT / 'scripts/browser.py', ROOT / 'scripts/registry.py', ROOT / 'scripts/projections.py', ROOT / 'scripts/config_display.py', ROOT / 'scripts/localization.py']
    return hashlib.sha256(''.join(digest(p) for p in paths).encode()).hexdigest()


def extract(go, source, sip, output):
    output.parent.mkdir(parents=True, exist_ok=True)
    with activity('扫描 Go 显示文案'):
        run(capped([go, 'run', '.', '-root', source, '-out', output.resolve()]), cwd=ROOT / 'tools/localize', env=goenv())
    with activity('提取配置注册表与浏览器文案'):
        candidates = sorted(json.loads(output.read_text()) + registry.extract(go, source) + browser.extract(sip), key=lambda entry: entry['id'])
    ids = [entry['id'] for entry in candidates]
    if len(ids) != len(set(ids)):
        raise ValueError('duplicate semantic source IDs')
    write_json(output, candidates)
    return candidates


def check_inventory(current):
    path = ROOT / 'localization/reviewed-inventory.json'
    if not path.exists():
        raise ValueError('missing reviewed source inventory; classify current candidates before approving it')
    expected = json.loads(path.read_text())
    if current != expected:
        differences = {}
        for group, value in current.items():
            old = expected.get(group)
            if value != old:
                differences[group] = sorted(k for k in set(value) | set(old or {}) if value.get(k) != (old or {}).get(k)) if isinstance(value, dict) else 'changed'
        write_json(ROOT / 'dist/localization-inventory-diff.json', differences)
        raise ValueError('upstream/dependency/scanner inventory changed; review dist/localization-inventory-diff.json')


def gate(go, source, sip, output, approve_inventory=False):
    candidates = extract(go, source, sip, output / 'candidates.json')
    with activity('核对翻译记忆'):
        reviewed, missing = reconcile(candidates, memory())
        status(f'{len(reviewed)} 个已审上下文，{len(missing)} 个待审')
    write_json(output / 'unclassified.json', missing)
    write_json(output / 'catalog.json', reviewed)
    if missing:
        summary = '; '.join(f'{e["file"]}:{e.get("line", "?")}: {e["source"][:160]!r}' for e in missing[:3])
        raise ValueError(f'{len(missing)} unclassified contexts: {summary}; see {output / "unclassified.json"}')
    with activity('核对已审源码与依赖清单'):
        current = inventory(candidates, source, sip)
        if approve_inventory:
            write_json(ROOT / 'localization/reviewed-inventory.json', current)
        else:
            check_inventory(current)
    report = {'contexts': len(candidates), 'reviewed': len(reviewed),
              'translated': sum(e['decision'] == 'translate' for e in reviewed),
              'retained_with_reason': sum(e['decision'] == 'keep' for e in reviewed),
              'kinds': dict(collections.Counter(e['kind'] for e in reviewed)),
              'scope': 'Go literals across non-test source, native display packages and setting/action registries; sip static display spans and complete asset hashes'}
    write_json(output / 'coverage.json', report)
    return reviewed

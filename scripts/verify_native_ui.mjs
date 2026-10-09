// Verify native UI text from the real browser terminal grid after trusted keyboard input.
import { readFile, readdir, writeFile } from 'node:fs/promises';

const prefixFile = 'internal/config/whichkey_menu.go';
const managerFile = 'internal/app/render_keybind_manager.go';
const plain = value => value.replace(/[\s│┃╎╏║]/gu, '');

async function reviewedLabels() {
    const directory = new URL('../locales/memory/', import.meta.url);
    const records = [];
    for (const name of (await readdir(directory)).filter(name => name.endsWith('.json')).sort()) {
        const data = JSON.parse(await readFile(new URL(name, directory), 'utf8'));
        records.push(...(Array.isArray(data) ? data : data.entries));
    }
    return (source, file) => {
        const matching = records.filter(record => {
            if (record.source !== source || (record.decision || 'translate') !== 'translate') return false;
            const scope = record.scope || 'native-ui';
            return scope === 'native-ui' || file.startsWith(scope) || scope.startsWith(file + ':');
        });
        const priority = record => (record.scope || 'native-ui') === 'native-ui' ? 0 : record.scope.length;
        const specificity = Math.max(...matching.map(priority));
        const translations = [...new Set(matching.filter(record => priority(record) === specificity).map(record => record.translation))];
        if (translations.length !== 1 || !translations[0]) throw new Error(`native UI fixture lacks one reviewed label: ${file}: ${source}`);
        return translations[0];
    };
}

export async function verifyNativeUI({ page, frame, output, until, screen, capture }) {
    const label = await reviewedLabels();
    const checks = [], pages = [], failures = [];
    const originalViewport = page.viewportSize();
    const originalGrid = await frame.evaluate(() => ({ cols: window.sipTerm.term.cols, rows: window.sipTerm.term.rows }));
    const matches = (text, value) => plain(text).includes(plain(value));
    const focus = () => frame.evaluate(() => window.sipTerm.webterm.focus());
    const inspect = async (name, sources, file = prefixFile) => {
        const expected = sources.map(source => ({ source, translation: label(source, file) }));
        await until(async () => {
            const text = await screen();
            return expected.every(item => matches(text, item.source) || matches(text, item.translation));
        }, `native UI page did not open: ${name}`);
        const text = await screen();
        const missing = expected.filter(item => !matches(text, item.translation));
        const english = expected.filter(item => matches(text, item.source) && !matches(text, item.translation));
        const size = await frame.evaluate(() => ({ cols: window.sipTerm.term.cols, rows: window.sipTerm.term.rows }));
        await capture(name);
        pages.push({ name, size, expected, missing, untranslated_english: english,
            text: `${name}.txt`, screenshot: `${name}.png` });
        if (missing.length) failures.push({ page: name, missing: missing.map(item => item.source) });
        return expected;
    };
    const close = async expected => {
        await page.keyboard.press('Escape');
        await until(async () => {
            const text = await screen();
            return expected.some(item => !matches(text, item.source) && !matches(text, item.translation));
        }, 'native UI overlay did not close');
    };
    const prefix = async key => {
        await focus();
        await page.keyboard.press('Control+b');
        if (key) await page.keyboard.press(key);
    };
    try {
        await focus();
        await page.keyboard.press('Escape');
        await prefix();
        await close(await inspect('native-prefix-main', ['Create window', 'Toggle tiling']));
        for (const [name, key, sources] of [
            ['workspace', 'w', ['Switch to workspace', 'Rename workspace']],
            ['minimize', 'm', ['Minimize focused window', 'Restore all']],
            ['window', 't', ['New window', 'Toggle tiling mode']],
            ['layout', 'L', ['Load layout', 'Save layout']],
            ['debug', 'D', ['Toggle log viewer', 'Toggle cache statistics']],
            ['tape', 'T', ['Open tape manager', 'Start recording']],
        ]) {
            await prefix(key);
            await close(await inspect('native-prefix-' + name, sources));
        }
        checks.push('real terminal grid shows reviewed Chinese rows in the main prefix menu and all six submenus');

        await prefix('k');
        const tabs = await inspect('native-keybind-bindings', ['Bindings', 'Conflicts'], 'internal/app/keybind_manager.go');
        await page.keyboard.press('Tab');
        await inspect('native-keybind-conflicts', ['No key is used twice in the same scope'], managerFile);
        await page.keyboard.press('Tab');
        await inspect('native-keybind-guests', ['Keys tuios uses that a program also wants'], managerFile);
        const guestSource = 'prefix (every tmux command starts here)';
        const guestTranslation = label(guestSource, 'internal/config/keybind_guest.go');
        const guestScreen = await screen();
        if (!matches(guestScreen, guestTranslation)) failures.push({ page: 'native-keybind-guests', missing: [guestSource] });
        pages[pages.length - 1].guest_description = { source: guestSource, translation: guestTranslation,
            visible: matches(guestScreen, guestTranslation) };
        await page.keyboard.press('Tab');
        await inspect('native-keybind-record', ['  Press ctrl+r to record a key and see what tuios already does with it.'], managerFile);
        await page.keyboard.press('Control+r');
        await until(async () => matches(await screen(), label('  Press any key. It will be recorded, not run.', managerFile)), 'key recorder did not arm');
        await page.keyboard.press('F12');
        await until(async () => /f12/i.test(await screen()), 'key recorder did not capture the real function key');
        await inspect('native-keybind-recorded-key', ['free: tuios does not use it, so the pane gets it'], managerFile);
        Object.assign(pages[pages.length - 1], { recorded_key: 'f12', writes: false });
        await close(tabs);
        checks.push('real keybinding manager renders Chinese tabs, conflict summary and guest description; function key recording is inspected without committing a binding');

        const metrics = await frame.evaluate(() => ({
            width: innerWidth / window.sipTerm.term.cols, height: innerHeight / window.sipTerm.term.rows,
        }));
        await page.setViewportSize({ width: Math.round(metrics.width * 80), height: Math.round(metrics.height * 24) });
        await until(async () => frame.evaluate(() => window.sipTerm.term.cols <= 82 && window.sipTerm.term.rows <= 25), 'native UI did not resize near 80x24');
        await prefix('w');
        await close(await inspect('native-prefix-80x24', ['Switch to workspace', 'Rename workspace']));
        checks.push('reviewed workspace menu rows remain visible in an approximately 80x24 real terminal grid');
    } finally {
        await page.keyboard.press('Escape').catch(() => {});
        await page.setViewportSize(originalViewport);
        await until(() => frame.evaluate(size => window.sipTerm.term.cols === size.cols && window.sipTerm.term.rows === size.rows, originalGrid),
            'native UI terminal did not restore its original grid');
        await focus();
        await writeFile(`${output}/native-ui.json`, JSON.stringify({ verified: failures.length === 0 && pages.length === 13,
            pages, failures, text_source: 'window.sipTerm.term.buffer.active rendered terminal cells',
            input: 'real Playwright keyboard input; no model injection or generated menu arrays',
            expected_labels: 'reviewed locale memory, read only; independent from rendered terminal observations' }, null, 2));
    }
    if (failures.length) throw new Error('native UI remains untranslated: ' + failures.map(item => item.page + ': ' + item.missing.join(', ')).join('; '));
    if (pages.length !== 13) throw new Error(`native UI capture incomplete: ${pages.length}`);
    return checks;
}

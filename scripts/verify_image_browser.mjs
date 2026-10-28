// Exercise real image uploads and trusted keyboard paste in an isolated browser profile.
import { createHash } from 'node:crypto';
import { writeFile } from 'node:fs/promises';
import { verifyAuthority } from './verify_image_authority.mjs';

const PNG = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAMAAAACCAYAAACddGYaAAAAE0lEQVR4nGOs0NjynwEKmBiQAAA13AJX+/hyRQAAAABJRU5ErkJggg==', 'base64');
const pasteChord = text => /\x16|\x1b\[(?:86|118)(?::[0-9:]*)?;(?:5|6|9|10|13|14)(?::[123])?(?:;[0-9:]*)?u/.test(text);
const checksum = data => createHash('sha256').update(data).digest('hex');

export async function verifyImages({ page, frame, entry, output, until, wire, capture, identity }) {
    const checks = [], requests = [];
    const origin = new URL(entry).origin;
    const fixture = async (name, params = {}, method = 'GET') => {
        const url = new URL('/__fixture_image_' + name, entry);
        for (const [key, value] of Object.entries(params)) url.searchParams.set(key, value);
        const response = await fetch(url, { method });
        if (!response.ok) throw new Error(`image fixture ${name}: ${response.status}`);
        return response.json();
    };
    const panes = () => fixture('panes');
    const focused = async () => (await panes()).focused_window_id;
    const text = async pane => (await fixture('capture', { pane })).text.replace(/[ \t]*\n/g, '');
    const files = async () => (await fixture('files')).files;
    const focusTerminal = () => frame.evaluate(() => window.sipTerm.webterm.focus());
    const status = frame.locator('#fn-image-paste-status');
    let imageStart = 0;
    const clear = async () => { await focusTerminal(); await page.keyboard.press('i'); await page.keyboard.press('Control+u'); };
    const waitDone = async () => {
        await until(async () => {
            const controls = wire.slice(imageStart).filter(item => item.bytes[0] === 0x39).flatMap(item => {
                try { return [{ direction: item.direction, ...JSON.parse(item.bytes.subarray(1).toString()) }]; }
                catch { return []; }
            });
            const request = controls.find(item => item.direction === 'sent' && item.type === 'image-begin');
            return request && controls.some(item => item.direction === 'received' && item.type === 'image-done' && item.id === request.id) &&
                (await status.textContent()) === await frame.evaluate(() => window.fnImagePasteMessages.complete);
        },
            'image paste did not confirm delivery', 90000);
    };
    const clipboard = async (kind, plain = '') => {
        await frame.evaluate(async ({ kind, plain, image }) => {
            const data = kind === 'text' ? {} : { 'image/png': new Blob([
                Uint8Array.from(atob(image), character => character.charCodeAt(0)),
            ], { type: 'image/png' }) };
            if (kind !== 'image') data['text/plain'] = new Blob([plain], { type: 'text/plain' });
            await navigator.clipboard.write([new ClipboardItem(data)]);
        }, { kind, plain, image: PNG.toString('base64') });
    };
    const pasteKey = async () => {
        await focusTerminal();
        imageStart = wire.length;
        await page.keyboard.press('Control+v');
    };
    const pasteImage = async () => { await clipboard('image'); await pasteKey(); };
    const layout = async () => {
        const result = await frame.evaluate(() => {
            const container = document.querySelector('#terminal-container').getBoundingClientRect();
            const terminal = document.querySelector('#terminal').getBoundingClientRect();
            return {
                controls: document.querySelectorAll('#fn-image-paste, #fn-image-paste-button, #fn-image-choose-button, #fn-image-file').length,
                containerTop: container.top, containerBottom: container.bottom,
                terminalTop: terminal.top, width: document.documentElement.scrollWidth,
                viewportWidth: innerWidth, viewportHeight: innerHeight,
            };
        });
        if (result.controls || Math.abs(result.containerTop) > 1 || Math.abs(result.terminalTop) > 1 ||
            Math.abs(result.containerBottom - result.viewportHeight) > 1 || result.width > result.viewportWidth) {
            throw new Error(`image toolbar or unused terminal space remains: ${JSON.stringify(result)}`);
        }
        return result;
    };
    const inputSince = start => wire.slice(start).filter(item => item.direction === 'sent' && item.bytes[0] === 0x30)
        .map(item => item.bytes.subarray(1).toString()).join('');
    page.on('request', request => {
        if (new URL(request.url()).pathname.includes('/image-paste/')) requests.push(request.url());
    });
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write'], { origin });
    await clear();
    let initial = await focused();
    if (!initial) throw new Error('no focused pane for image tests');
    const desktopLayout = await layout();
    await capture('image-no-toolbar');
    checks.push('desktop terminal fills the viewport without image toolbar buttons, file input or reserved space');
    const beforeSynthetic = wire.length;
    const syntheticTrusted = await frame.evaluate(image => {
        const clipboardData = new DataTransfer();
        clipboardData.items.add(new File([Uint8Array.from(atob(image), character => character.charCodeAt(0))],
            'synthetic.png', { type: 'image/png' }));
        const event = new ClipboardEvent('paste', { clipboardData, bubbles: true, cancelable: true });
        document.querySelector('#terminal').dispatchEvent(event);
        return event.isTrusted;
    }, PNG.toString('base64'));
    await frame.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    if (syntheticTrusted || wire.slice(beforeSynthetic).some(item => item.direction === 'sent' && item.bytes[0] === 0x39)) {
        throw new Error('an untrusted synthetic paste initiated an image operation');
    }
    checks.push('an explicitly synthetic untrusted image paste event cannot initiate an upload');

    await clipboard('text', 'FN_NATIVE_TEXT_54321');
    let start = wire.length;
    await pasteKey();
    await until(async () => (await text(initial)).includes('FN_NATIVE_TEXT_54321'), 'native text paste was lost');
    if (pasteChord(inputSince(start))) throw new Error('native text paste leaked Ctrl+V to the program');
    await page.keyboard.press('Control+u');
    await clipboard('mixed', 'FN_MIXED_TEXT_54321');
    const count = requests.length;
    await pasteKey();
    await until(async () => (await text(initial)).includes('FN_MIXED_TEXT_54321'), 'mixed clipboard text was lost');
    if (requests.length !== count) throw new Error('ordinary mixed paste uploaded an image');
    await page.keyboard.press('Control+u');
    checks.push('trusted native text and mixed clipboard paste preserve text; mixed paste uploads no image');

    const keyPane = (await fixture('action', { action: 'keys' }, 'POST')).window_id;
    await until(async () => (await text(keyPane)).includes('FN_KEYS_READY'), 'key protocol fixture not ready');
    await focusTerminal();
    await page.keyboard.press('i');
    await frame.evaluate(() => { window.__fnPreventPaste = true; window.__fnKeyEvents = []; });
    start = wire.length;
    await focusTerminal();
    await page.keyboard.down('Control');
    await page.keyboard.down('v');
    await page.keyboard.down('v');
    await page.keyboard.up('v');
    await page.keyboard.up('Control');
    const keyEvents = await frame.evaluate(() => window.__fnKeyEvents);
    if (keyEvents.length !== 3) throw new Error('fixture did not emit press/repeat/release events');
    const keyFrames = keyEvents.map(event => event.encoded ?? (event.type === 'keydown' ? '\x16' : null)).filter(Boolean);
    const actualKeys = () => inputSince(start).match(/\x16|\x1b\[(?:86|118)(?::[0-9:]*)?;[0-9]+(?::[123])?(?:;[0-9:]*)?u/g) || [];
    try {
        await until(() => actualKeys().length >= keyFrames.length, 'Ctrl+V fallback key frames were swallowed');
    } catch (error) {
        throw Object.assign(error, { code: 'keyboard.fallback.swallowed',
            evidence: { expected: keyFrames, actual: actualKeys(), events: keyEvents } });
    }
    if (JSON.stringify(actualKeys()) !== JSON.stringify(keyFrames)) {
        throw new Error('Ctrl+V press/repeat/release fallback changed order or duplicated input');
    }
    await frame.evaluate(() => { window.__fnPreventPaste = false; });
    const nativeKeysStart = wire.length;
    const nativeEventsStart = await frame.evaluate(() => window.__fnPasteEvents.length);
    await pasteKey();
    await until(async () => (await frame.evaluate(() => window.__fnPasteEvents.length)) > nativeEventsStart, 'native paste with release reporting did not run');
    if (pasteChord(inputSince(nativeKeysStart))) throw new Error('native paste left a Kitty chord press or orphan release');
    checks.push('without native paste, Ctrl+V press/repeat/release preserve the negotiated protocol bytes and ordering');
    await clipboard('text', '');
    start = wire.length;
    const emptyEvents = await frame.evaluate(() => window.__fnPasteEvents.length);
    await pasteKey();
    await until(() => pasteChord(inputSince(start)), 'empty clipboard paste swallowed application Ctrl+V');
    if ((await frame.evaluate(() => window.__fnPasteEvents.length)) <= emptyEvents) throw new Error('empty clipboard test did not emit a native paste');
    checks.push('a trusted empty clipboard event preserves the application Ctrl+V fallback');

    initial = (await fixture('action', { action: 'reader' }, 'POST')).window_id;
    await until(async () => (await text(initial)).includes('FN_IMAGE_READY'), 'image reader did not become ready');
    await clipboard('image');
    start = wire.length;
    const beforeFiles = new Set((await files()).map(file => file.path));
    const before = beforeFiles.size;
    await pasteKey();
    await waitDone();
    await until(async () => (await files()).length === before + 1, 'native image file missing');
    const native = (await files()).find(file => !beforeFiles.has(file.path));
    if (pasteChord(inputSince(start))) throw new Error('image paste leaked Ctrl+V to the terminal application');
    if (inputSince(start).includes('\r') || inputSince(start).includes('\n')) throw new Error('image paste sent Enter');
    if ((await text(initial)).includes('FN_IMAGE_SUBMITTED')) throw new Error('server image delivery submitted the application input line');
    await until(async () => (await text(initial)).includes(native.path), 'original pane did not receive image path');
    if (native.mode !== '0600' || native.uid !== identity.uid) throw new Error('uploaded image ownership/mode wrong');
    const events = await frame.evaluate(() => window.__fnPasteEvents || []);
    if (!events.some(event => event.trusted && event.items.some(item => item.kind === 'file' && item.type === 'image/png'))) throw new Error('image test did not produce a trusted native paste');
    await capture('image-native');
    await clear();
    checks.push(`real Ctrl+V emits trusted image paste without terminal chord bytes or Enter; image UID ${identity.uid}, mode0600`);

    let release, intercepted, continued;
    const held = new Promise(resolve => { release = resolve; });
    const arrived = new Promise(resolve => { intercepted = resolve; });
    const resumed = new Promise(resolve => { continued = resolve; });
    let tokenURL, uploadedBytes;
    const route = async route => {
        if (route.request().headers()['x-fixture-user'] === 'bob') { await route.continue(); return; }
        tokenURL = route.request().url(); uploadedBytes = route.request().postDataBuffer();
        intercepted(); await held; await route.continue(); continued();
    };
    await page.route('**/image-paste/*', route);
    const priorPaths = new Set((await files()).map(file => file.path));
    await pasteImage();
    await arrived;
    const foreign = identity.single_user ? null : await frame.evaluate(async ({ url, image }) => (await fetch(url, { method: 'POST',
        headers: { 'Content-Type': 'image/png', 'X-Fixture-User': 'bob' },
        body: Uint8Array.from(atob(image), character => character.charCodeAt(0)),
    })).status, { url: tokenURL, image: PNG.toString('base64') });
    if (!identity.single_user && (![403, 410].includes(foreign) || (await files()).some(file => file.uid === 45001))) {
        throw new Error('another NAS user used the live Alice token');
    }
    const created = await fixture('action', { action: 'new' }, 'POST');
    const second = created.window_id;
    await until(async () => (await focused()) === second, 'second pane did not get focus');
    release();
    await resumed;
    await page.unroute('**/image-paste/*', route);
    await waitDone();
    const expected = (await files()).find(file => !priorPaths.has(file.path) && file.sha256 === checksum(uploadedBytes));
    if (expected) await until(async () => (await text(initial)).includes(expected.path), 'captured pane echo did not arrive');
    if (!expected || !(await text(initial)).includes(expected.path) || (await text(second)).includes(expected.path)) {
        throw new Error('delayed image paste did not stay bound to original pane');
    }
    if ((await text(initial)).includes('FN_IMAGE_SUBMITTED')) throw new Error('server submitted the captured pane input line');
    await capture('image-original-pane');
    checks.push('trusted native image paste uploads clipboard bytes via prefixed HTTP; focus changed during upload but original pane alone receives the path');

    const replay = await frame.evaluate(async ({ url, image, singleUser }) => {
        const body = Uint8Array.from(atob(image), character => character.charCodeAt(0));
        const same = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'image/png' }, body });
        const other = singleUser ? null : await fetch(url, { method: 'POST', headers: { 'Content-Type': 'image/png', 'X-Fixture-User': 'bob' }, body });
        return other ? [same.status, other.status] : [same.status];
    }, { url: tokenURL, image: PNG.toString('base64'), singleUser: identity.single_user });
    if (replay.some(code => code !== 410 && code !== 403)) throw new Error(`replay/user isolation failed: ${replay}`);
    checks.push(identity.single_user ? 'consumed token replay is rejected; cross-UID isolation requires privileged fixture' : 'live token rejects another real NAS user and consumed token replay is rejected');

    await focusTerminal();
    await page.keyboard.press('i');
    let uploadRelease, uploadArrival, uploadContinued;
    const waitUpload = new Promise(resolve => { uploadRelease = resolve; });
    const gotUpload = new Promise(resolve => { uploadArrival = resolve; });
    const uploadResumed = new Promise(resolve => { uploadContinued = resolve; });
    const closingRoute = async route => { uploadArrival(); await waitUpload; await route.continue(); uploadContinued(); };
    await page.route('**/image-paste/*', closingRoute);
    await pasteImage();
    await gotUpload;
    await fixture('action', { action: 'close', pane: second }, 'POST');
    uploadRelease();
    await uploadResumed;
    await page.unroute('**/image-paste/*', closingRoute);
    await until(async () => (await status.textContent()) === await frame.evaluate(() => window.fnImagePasteMessages.target_closed),
        'closed target did not reject image paste', 90000);
    checks.push('closing the captured pane rejects delivery instead of pasting into another pane');

    await frame.evaluate(() => {
        window.__fnClipboardRead = navigator.clipboard.read.bind(navigator.clipboard);
        window.__fnClipboardReadCalls = 0;
        navigator.clipboard.read = async () => {
            window.__fnClipboardReadCalls++;
            throw new DOMException('fixture policy denial', 'NotAllowedError');
        };
    });
    await pasteImage();
    await waitDone();
    if (await frame.evaluate(() => window.__fnClipboardReadCalls)) throw new Error('native paste still depends on optional clipboard.read permission');
    await frame.evaluate(() => { navigator.clipboard.read = window.__fnClipboardRead; });
    checks.push('trusted native image paste succeeds without calling the denied optional clipboard.read API');

    await clear();
    // Corrupt the HTTP body, not the native event: browsers reject invalid PNG clipboard writes themselves.
    const corrupt = route => route.continue({ postData: Buffer.from('not an image') });
    await page.route('**/image-paste/*', corrupt);
    await pasteImage();
    await until(async () => (await status.textContent()) === await frame.evaluate(() => window.fnImagePasteMessages.invalid_image), 'invalid image was not rejected', 90000);
    await page.unroute('**/image-paste/*', corrupt);
    checks.push('an explicitly corrupted HTTP request body is rejected by the backend as invalid_image');
    const oversizedBytes = await frame.evaluate(async () => {
        const canvas = document.createElement('canvas');
        canvas.width = canvas.height = 1800;
        const context = canvas.getContext('2d'), pixels = context.createImageData(1800, 1800);
        const bytes = new Uint8Array(pixels.data.buffer);
        for (let offset = 0; offset < bytes.length; offset += 65536) {
            crypto.getRandomValues(bytes.subarray(offset, Math.min(offset + 65536, bytes.length)));
        }
        for (let offset = 3; offset < bytes.length; offset += 4) bytes[offset] = 255;
        context.putImageData(pixels, 0, 0);
        const image = await new Promise(resolve => canvas.toBlob(resolve, 'image/png'));
        if (!image || image.size <= 8 * 1024 * 1024) throw new Error('oversized clipboard fixture did not exceed upload budget');
        await navigator.clipboard.write([new ClipboardItem({ 'image/png': image })]);
        return image.size;
    });
    const beforeOversize = requests.length;
    const oversizedEventStart = await frame.evaluate(() => window.__fnPasteEvents.length);
    await pasteKey();
    await until(async () => (await status.textContent()) === await frame.evaluate(() => window.fnImagePasteMessages.too_large), 'large image was not rejected');
    if (requests.length !== beforeOversize) throw new Error('oversized native image bypassed browser upload budget');
    const oversizedEvents = await frame.evaluate(start => window.__fnPasteEvents.slice(start), oversizedEventStart);
    if (!oversizedEvents.some(event => event.trusted && event.items.some(item => item.type === 'image/png'))) {
        throw new Error('oversized image check did not produce a trusted native paste event');
    }
    await pasteImage();
    await waitDone();
    checks.push('a real oversized PNG clipboard paste emits a trusted event and is rejected before HTTP upload; the next native paste succeeds');
    await until(() => status.isHidden(), 'completed image status did not disappear', 10000);

    await page.setViewportSize({ width: 420, height: 740 });
    await until(async () => (await frame.evaluate(() => window.sipTerm.term.cols)) < 65, 'narrow image terminal did not resize');
    await capture('image-narrow');
    const narrowLayout = await layout();
    await page.setViewportSize({ width: 1440, height: 960 });
    await until(async () => (await frame.evaluate(() => window.sipTerm.term.cols)) > 120, 'image terminal did not restore size');
    if (requests.some(url => !new URL(url).pathname.startsWith('/app/tuios/image-paste/'))) throw new Error('image upload escaped gateway prefix');
    checks.push('narrow terminal fills the viewport without toolbar, blank strip or horizontal overflow; status disappears and all uploads use the same-origin gateway prefix');
    checks.push(...await verifyAuthority({ page, frame, entry, output, until, wire, capture, identity }));
    const result = { checks, clipboard: 'synthetic data written to isolated headless browser clipboard only',
        image: { native_uid: native.uid, native_mode: native.mode, native_file_sha256: native.sha256, fixture_png_sha256: checksum(PNG), oversized_clipboard_bytes: oversizedBytes },
        layout: { desktop: desktopLayout, narrow: narrowLayout },
        native_events: await frame.evaluate(() => window.__fnPasteEvents), oversized_native_events: oversizedEvents,
        live_foreign_status: foreign, upload_count: requests.length, replay_statuses: replay };
    await writeFile(`${output}/images.json`, JSON.stringify(result, null, 2));
    return checks;
}

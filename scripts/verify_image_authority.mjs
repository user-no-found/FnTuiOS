// Real daemon checks for authority retained across the browser upload delay.
import { writeFile } from 'node:fs/promises';

const PNG = 'iVBORw0KGgoAAAANSUhEUgAAAAMAAAACCAYAAACddGYaAAAAE0lEQVR4nGOs0NjynwEKmBiQAAA13AJX+/hyRQAAAABJRU5ErkJggg==';

export async function verifyAuthority({ frame, entry, output, until, wire, capture }) {
    const checks = [];
    const fixture = async (name, params = {}, method = 'GET') => {
        const url = new URL('/__fixture_authority_' + name, entry);
        for (const [key, value] of Object.entries(params)) url.searchParams.set(key, value);
        const response = await fetch(url, { method });
        if (!response.ok) throw new Error(`authority fixture ${name}: ${response.status}`);
        return response.json();
    };
    const files = async () => (await (await fetch(new URL('/__fixture_image_files', entry))).json()).files;
    const panes = () => fixture('panes');
    const text = async pane => (await fixture('capture', { pane })).text.replace(/[ \t]*\n/g, '');
    const replies = (start, id) => wire.slice(start).filter(event => event.direction === 'received' && event.bytes[0] === 0x39)
        .flatMap(event => { try { return [JSON.parse(event.bytes.subarray(1).toString())]; } catch { return []; } })
        .filter(reply => reply.id === id);
    const waitReply = async (start, id, type) => {
        await until(() => replies(start, id).some(reply => reply.type === type || reply.type === 'image-error'),
            `authority request ${id} had no ${type}`, 90000);
        const result = replies(start, id).find(reply => reply.type === type || reply.type === 'image-error');
        if (type !== 'image-error' && result.type === 'image-error') throw new Error(`authority ${id}: ${result.code}`);
        return result;
    };
    let sequence = 0;
    const begin = async (before = '', after = '') => {
        const id = 'authority-' + (++sequence), start = wire.length;
        await frame.evaluate(({ id, before, after }) => {
            const connection = window.sipTerm.connection, encode = value => new TextEncoder().encode(value);
            // All three sends run in the same JS turn without waiting for a
            // render or an acknowledgment between the focus key and capture.
            if (before) connection.sendMessageRaw(0x30, encode(before));
            connection.sendMessageRaw(0x39, encode(JSON.stringify({ type: 'image-begin', id })));
            if (after) connection.sendMessageRaw(0x30, encode(after));
        }, { id, before, after });
        return { id, start, ready: await waitReply(start, id, 'image-ready') };
    };
    const post = async operation => frame.evaluate(async ({ token, image }) => {
        const url = new URL('image-paste/' + token, document.baseURI);
        const response = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'image/png' },
            body: Uint8Array.from(atob(image), character => character.charCodeAt(0)) });
        return response.status;
    }, { token: operation.ready.token, image: PNG });
    const cancel = async operation => {
        await frame.evaluate(id => window.sipTerm.connection.sendMessageRaw(0x39,
            new TextEncoder().encode(JSON.stringify({ type: 'image-cancel', id }))), operation.id);
        await waitReply(operation.start, operation.id, 'image-error');
    };

    const created = await fixture('prepare', {}, 'POST');
    const { first, second } = created;
    await until(async () => (await panes()).windows.length === 2 &&
        (await text(first)).includes('FN_AUTHORITY_READY') && (await text(second)).includes('FN_AUTHORITY_READY'),
    'authority readers did not become ready');
    // This first marker proves the initial focus key reached the model, even
    // if the daemon's canonical state already reported that pane as focused.
    await cancel(await begin('\x021'));
    const beforePaths = new Set((await files()).map(file => file.path));
    const ordered = await begin('\x022', '\x021');
    await until(async () => (await panes()).focused_window_id === first, 'post-capture focus key did not run');
    if (await post(ordered) !== 202) throw new Error('ordered capture upload not accepted');
    await waitReply(ordered.start, ordered.id, 'image-done');
    let saved;
    await until(async () => {
        saved = (await files()).find(file => !beforePaths.has(file.path));
        return saved && (await text(second)).includes(saved.path);
    }, 'image did not reach the pane selected immediately before capture');
    if ((await text(first)).includes(saved.path)) throw new Error('image followed focus after capture');
    if ((await text(second)).includes('FN_AUTHORITY_SUBMITTED')) throw new Error('ordered upload submitted the reader');
    await capture('image-authority-order');
    checks.push('focus key, capture and a second focus key share consecutive WS frames; only the pane focused at the capture receives the new path');

    const stale = await begin();
    const beforeRoundtrip = new Set((await files()).map(file => file.path));
    const switched = await fixture('roundtrip', {}, 'POST');
    if (switched.away.session !== 'web' || switched.back.session !== 'image-authority' ||
        switched.away.already || switched.back.already) throw new Error('session did not actually switch away and back');
    if (await post(stale) !== 202) throw new Error('stale authority did not reach model validation');
    const rejected = await waitReply(stale.start, stale.id, 'image-error');
    if (rejected.code !== 'target_changed') throw new Error(`session roundtrip accepted old authority: ${rejected.code}`);
    if ((await files()).some(file => !beforeRoundtrip.has(file.path))) throw new Error('session-stale upload wrote a file');
    checks.push('switching the real browser client to another daemon session and back invalidates the original token target before any file is written');
    await fixture('restore', {}, 'POST');

    const detached = await begin();
    const beforeDisconnect = new Set((await files()).map(file => file.path));
    const attached = (await fixture('clients')).find(client => client.session === 'web');
    if (!attached) throw new Error('browser daemon client missing before disconnect');
    await frame.evaluate(() => window.sipTerm.connection.ws.close());
    await until(async () => !await frame.evaluate(() => window.sipTerm.connected), 'browser WS did not disconnect');
    await until(async () => !(await fixture('clients')).some(client => client.client_id === attached.client_id),
        'browser daemon client remained attached after WS closed');
    const disconnectedStatus = await post(detached);
    if (disconnectedStatus !== 410) throw new Error(`disconnected browser token returned ${disconnectedStatus}`);
    if ((await files()).some(file => !beforeDisconnect.has(file.path))) throw new Error('disconnected upload wrote a file');
    checks.push('closing the browser WS revokes its unused token; a later HTTP upload returns410 and writes no image');
    await writeFile(`${output}/image-authority.json`, JSON.stringify({ checks, panes: { first, second },
        ordered_path: saved.path, roundtrip: switched, stale_code: rejected.code, disconnected_status: disconnectedStatus }, null, 2));
    return checks;
}

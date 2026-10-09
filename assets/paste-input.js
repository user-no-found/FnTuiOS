// Let native paste run before deciding whether a terminal paste chord is input.
(() => {
    const INPUT = 0x30;
    function install(client) {
        let pending = null;
        const suppressed = new Set();
        const encoder = new TextEncoder();
        function flush(clipboard = false) {
            if (!pending) return;
            const held = pending;
            pending = null;
            clearTimeout(held.timer);
            if (clipboard) suppressed.add(held.code);
            for (const frame of held.frames) {
                if (clipboard && frame.chord) frame.resolve();
                else frame.connection.sendMessageRaw(frame.type, frame.payload).then(frame.resolve, frame.reject);
            }
        }
        function rememberKey(event) {
            const flags = client.webterm?.keyboard?.flags || 0;
            let encoded = flags ? window.WebTerm.encodeKey(event, flags) : null;
            if (encoded === null && event.type === 'keydown' && event.ctrlKey && !event.shiftKey && !event.metaKey) encoded = '\x16';
            if (encoded) pending.frames.push({ connection: client.connection, type: INPUT,
                payload: encoder.encode(encoded), chord: true, resolve() {}, reject() {} });
        }
        document.addEventListener('pointerdown', () => flush(), true);
        return {
            key(event) {
                if (!event.isTrusted || !client.connected || client.readOnly) return true;
                const release = event.type !== 'keydown' && event.code === 'KeyV' && (pending || suppressed.has(event.code));
                const chord = release || (event.code === 'KeyV' && (event.ctrlKey || event.metaKey) && !event.altKey);
                if (!chord) { if (event.type === 'keydown') flush(); return true; }
                if (event.type === 'keydown') {
                    flush();
                    suppressed.delete(event.code);
                    pending = { code: event.code, frames: [], timer: setTimeout(() => flush(), 120) };
                    rememberKey(event);
                    return false;
                }
                if (event.type === 'keyup' && suppressed.delete(event.code)) return false;
                if (pending) {
                    if (event.type === 'keyup') rememberKey(event);
                    return false;
                }
                return !suppressed.has(event.code);
            },
            transmit(connection, type, payload) {
                if (!pending) return connection.sendMessageRaw(type, payload);
                return new Promise((resolve, reject) => pending.frames.push({
                    connection, type, payload: payload?.slice(), resolve, reject, chord: false,
                }));
            },
            consume() { flush(true); },
            disconnect() {
                if (pending) {
                    clearTimeout(pending.timer);
                    for (const frame of pending.frames) frame.resolve();
                    pending = null;
                }
                suppressed.clear();
            },
        };
    }
    window.FnPasteInput = Object.freeze({ install });
})();

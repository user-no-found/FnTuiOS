// Upload clipboard images without moving the terminal's input target.
(() => {
    const CONTROL = 0x39;
    const instances = new WeakMap();
    const types = new Set(['image/png', 'image/jpeg', 'image/gif', 'image/webp']);
    const aliases = { invalid_request: 'failed', upload_failed: 'failed', paste_failed: 'failed',
        target_changed: 'expired', unavailable: 'disconnected' };

    function install(client) {
        const labels = window.fnImagePasteMessages;
        const input = window.FnPasteInput.install(client);
        const status = document.createElement('div');
        status.id = 'fn-image-paste-status';
        status.setAttribute('role', 'status');
        status.setAttribute('aria-live', 'polite');
        document.body.append(status);
        let current = null;
        let sequence = 0;
        let statusTimer;

        function display(key) {
            clearTimeout(statusTimer);
            status.textContent = labels[key] || labels.failed;
            if (!current) statusTimer = setTimeout(() => { status.textContent = ''; }, 6000);
        }
        function finish(operation, key, serverEnded = false) {
            if (!operation || current !== operation) return;
            current = null;
            if (!serverEnded && client.connected && client.connection) {
                const cancel = new TextEncoder().encode(JSON.stringify({ type: 'image-cancel', id: operation.id }));
                client.connection.sendMessage(CONTROL, cancel).catch(() => {});
            }
            clearTimeout(operation.timer);
            operation.controller.abort();
            operation.reject(new Error(key));
            display(key);
            if (key === 'complete') client.webterm?.focus();
        }
        function begin() {
            if (current) { display('busy'); return null; }
            if (!client.connected || !client.connection || client.readOnly) {
                display(client.readOnly ? 'read_only' : 'disconnected');
                return null;
            }
            const operation = { id: 'paste-' + (++sequence), controller: new AbortController() };
            operation.ready = new Promise((resolve, reject) => {
                operation.resolve = resolve;
                operation.reject = reject;
            });
            operation.ready.catch(() => {});
            current = operation;
            operation.timer = setTimeout(() => finish(operation, 'expired'), 35000);
            display('preparing');
            const request = new TextEncoder().encode(JSON.stringify({ type: 'image-begin', id: operation.id }));
            client.connection.sendMessage(CONTROL, request).catch(() => finish(operation, 'disconnected'));
            return operation;
        }
        async function upload(operation, image) {
            if (!operation || current !== operation) return;
            if (!image || !types.has(image.type)) { finish(operation, 'unsupported'); return; }
            try {
                const ready = await operation.ready;
                if (current !== operation) return;
                if (image.size > ready.max_bytes) { finish(operation, 'too_large'); return; }
                clearTimeout(operation.timer);
                display('uploading');
                const endpoint = new URL('image-paste/' + encodeURIComponent(ready.token), document.baseURI);
                if (endpoint.origin !== location.origin) throw new Error('unauthorized');
                const response = await fetch(endpoint, { method: 'POST', credentials: 'same-origin',
                    headers: { 'Content-Type': image.type }, body: image, signal: operation.controller.signal });
                if (!response.ok) {
                    const result = await response.json().catch(() => ({}));
                    throw new Error(aliases[result.error] || result.error || 'failed');
                }
                // The server's final WebSocket response confirms injection into the captured pane.
            } catch (error) {
                if (current === operation) finish(operation, error.message);
            }
        }
        document.addEventListener('paste', event => {
            if (!event.isTrusted || !(event.target instanceof Element) || !event.target.closest('#terminal')) return;
            const clipboard = event.clipboardData;
            if (!clipboard) return;
            if (clipboard.getData('text/plain')) { input.consume(); return; }
            const image = [...clipboard.items].find(item => item.kind === 'file' && item.type.startsWith('image/'));
            if (!image) return;
            input.consume();
            event.preventDefault();
            event.stopImmediatePropagation();
            const file = image.getAsFile();
            const operation = begin();
            if (operation) upload(operation, file);
        }, true);
        instances.set(client, {
            input,
            receive(data) {
                let result;
                try { result = JSON.parse(new TextDecoder().decode(data.subarray(1))); } catch { return; }
                const operation = current;
                if (!operation || result.id !== operation.id) return;
                if (result.type === 'image-ready') {
                    if (typeof result.token !== 'string' || !/^[A-Za-z0-9_-]+$/.test(result.token) ||
                        !Number.isInteger(result.max_bytes) || result.max_bytes < 1) {
                        finish(operation, 'failed');
                    } else operation.resolve(result);
                } else if (result.type === 'image-done') finish(operation, 'complete', true);
                else if (result.type === 'image-error') finish(operation, aliases[result.code] || result.code, true);
            },
            disconnect() {
                input.disconnect();
                if (current) finish(current, 'disconnected', true);
            },
        });
    }
    window.FnImagePaste = Object.freeze({
        install,
        key(client, event) { return instances.get(client)?.input.key(event) ?? true; },
        transmit(connection, type, payload) {
            const instance = instances.get(connection.client);
            return instance ? instance.input.transmit(connection, type, payload) :
                connection.sendMessageRaw(type, payload);
        },
        receive(client, data) {
            if (data?.[0] !== CONTROL) return false;
            instances.get(client)?.receive(data);
            return true;
        },
        disconnect(client) { instances.get(client)?.disconnect(); },
    });
})();

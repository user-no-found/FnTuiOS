(function () {
    const instances = new WeakMap();
    const CONTROL = 0x39;
    const directions = ['up', 'down', 'left', 'right'];
    const labels = ['上', '下', '左', '右'];
    const errors = { too_small: '当前窗格太小，无法分屏。', requires_tiling: '请先启用 BSP 平铺，再进行分屏。',
        busy: '正在创建窗格，请稍后重试。', read_only: '当前终端为只读。', target_changed: '终端已切换，请重试。' };

    function install(client) {
        if (instances.has(client)) return;
        const controls = document.createElement('div');
        controls.hidden = true;
        const status = document.createElement('p');
        status.id = 'fn-panel-status'; status.setAttribute('role', 'status');
        status.style.cssText = 'position:fixed;left:16px;bottom:150px;z-index:20000;max-width:80vw;background:#182230;color:white;padding:10px;border-radius:8px';
        status.hidden = true;
        document.body.append(controls, status);
        const buttons = new Map(), mods = { ctrl: false, shift: false, alt: false };
        const copy = window.FnTextCopy.install(client);
        let pending = '', sequence = 0, timeout, statusTimer;
        const connected = () => client.connected && Boolean(client.connection);
        const ready = () => connected() && !client.readOnly;
        const tell = message => {
            status.textContent = message; status.hidden = !message;
            window.dispatchEvent(new CustomEvent('fnpackkit:notice', { detail: { message } }));
            clearTimeout(statusTimer); statusTimer = setTimeout(() => { status.hidden = true; }, 5000);
        };
        function button(id, run, toggle = false) {
            const node = document.createElement('button'); node.type = 'button'; node.id = 'fn-panel-' + id;
            if (toggle) node.setAttribute('aria-pressed', 'false');
            node.addEventListener('click', () => { if (id === 'copy-text' ? connected() : ready()) run(node); });
            controls.append(node); buttons.set(id, node);
        }
        function reset() {
            for (const key of Object.keys(mods)) {
                mods[key] = false; buttons.get(key)?.setAttribute('aria-pressed', 'false');
            }
        }
        function sync() {
            for (const [id, node] of buttons) node.disabled = id === 'copy-text' ? !connected() : !ready() || id.startsWith('split-') && Boolean(pending);
            copy.sync();
            if (!ready()) { reset(); window.FnMobileKeyboard.close(client); }
            buttons.get('keyboard')?.setAttribute('aria-pressed', String(window.FnMobileKeyboard.isOpen(client)));
        }
        for (const key of Object.keys(mods)) button(key, node => {
            mods[key] = !mods[key]; node.setAttribute('aria-pressed', String(mods[key]));
        }, true);
        for (const [index, dir] of directions.entries()) {
            button('key-' + dir, () => {
                const key = 'Arrow' + dir[0].toUpperCase() + dir.slice(1);
                const event = type => new KeyboardEvent(type, { key, code: key,
                    ctrlKey: mods.ctrl, shiftKey: mods.shift, altKey: mods.alt });
                const flags = client.webterm?.keyboard?.flags || 0;
                const encoded = window.WebTerm.encodeKey(event('keydown'), flags) ??
                    window.WebTerm.mobile.encodeKeySpec({ key }, mods);
                const release = window.WebTerm.encodeKey(event('keyup'), flags);
                reset();
                if (encoded) client.sendInput(encoded + (release || '')).catch(() => tell('按键发送失败，请重试。'));
            });
            button('split-' + dir, () => {
                const id = 'split-' + (++sequence); pending = id; sync();
                const data = new TextEncoder().encode(JSON.stringify({ type: 'panel-split', id, direction: dir }));
                client.connection.sendMessage(CONTROL, data).catch(() => finish('连接已断开，请重试。'));
                timeout = setTimeout(() => finish('分屏未确认，请刷新终端后检查。'), 7000);
            });
            buttons.get('key-' + dir).textContent = labels[index];
        }
        button('photo', () => { reset(); window.FnImagePaste.choose(client); });
        button('copy-text', () => { reset(); window.FnMobileKeyboard.close(client); copy.open(); });
        button('keyboard', () => { reset(); window.FnMobileKeyboard.toggle(client); }, true);
        addEventListener('fntuios:keyboard-change', sync);
        function finish(message) { clearTimeout(timeout); pending = ''; sync(); if (message) tell(message); }
        instances.set(client, {
            sync,
            disconnect() { finish(''); reset(); sync(); },
            receive(data) {
                let result;
                try { result = JSON.parse(new TextDecoder().decode(data.subarray(1))); } catch { return false; }
                if (copy.receive(result)) return true;
                if (result.type !== 'panel-result') return false;
                if (result.id === pending) finish(result.code ? errors[result.code] || '无法分屏，请检查当前终端状态。' : '');
                return true;
            },
        });
        sync();
    }
    window.FnPanelControls = Object.freeze({ install,
        sync(client) { instances.get(client)?.sync(); },
        disconnect(client) { instances.get(client)?.disconnect(); },
        receive(client, data) { return data?.[0] === CONTROL && (instances.get(client)?.receive(data) || false); },
    });
}());

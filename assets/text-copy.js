(function () {
    const stylesheet = new URL('text-copy.css', document.currentScript.src).href;
    // A snapshot viewer uses browser text selection, independently of terminal mouse reporting.
    function install(client) {
        const style = document.createElement('link');
        style.rel = 'stylesheet'; style.href = stylesheet;
        const dialog = document.createElement('dialog');
        dialog.id = 'fn-text-copy'; dialog.setAttribute('aria-label', '复制文本');
        function element(tag, text) {
            const node = document.createElement(tag); node.textContent = text; return node;
        }
        function button(text, action) {
            const node = element('button', text); node.type = 'button'; node.addEventListener('click', action); return node;
        }
        const title = element('strong', '复制当前窗格文本');
        const close = button('关闭', () => dialog.close());
        const head = element('header', ''); head.append(title, close);
        const screen = button('当前屏幕', () => load(false)), history = button('历史输出', () => load(true));
        const refresh = button('重新读取', () => load(mode));
        const toolbar = element('nav', ''); toolbar.setAttribute('aria-label', '文本范围'); toolbar.append(screen, history, refresh);
        const text = element('pre', ''); text.tabIndex = 0; text.setAttribute('aria-label', '窗格文本');
        const status = element('p', ''); status.setAttribute('role', 'status');
        const selected = button('复制所选', () => copy(true)), all = button('复制全部', () => copy(false));
        const footer = element('footer', ''); footer.append(selected, all);
        dialog.append(head, toolbar, text, status, footer); document.head.append(style); document.body.append(dialog);
        let pending = '', sequence = 0, timer, mode = false;
        const connected = () => client.connected && Boolean(client.connection);
        function sync() {
            const ready = connected() && !pending;
            for (const node of [screen, history, refresh]) node.disabled = !ready;
            selected.disabled = all.disabled = !text.textContent || Boolean(pending);
            if (!connected()) {
                clearTimeout(timer); pending = ''; text.textContent = '';
                selected.disabled = all.disabled = true;
                if (dialog.open) status.textContent = '连接已断开，请重新连接后读取。';
            }
        }
        function load(historyMode) {
            if (!connected() || pending) return;
            mode = historyMode;
            screen.setAttribute('aria-pressed', String(!mode)); history.setAttribute('aria-pressed', String(mode));
            text.textContent = ''; status.textContent = '正在读取当前窗格…';
            pending = 'copy-' + (++sequence); sync();
            const id = pending;
            timer = setTimeout(() => finish('读取超时，请重试。'), 7000);
            const data = new TextEncoder().encode(JSON.stringify({ type: 'panel-copy', id, history: mode }));
            client.connection.sendMessage(0x39, data).catch(() => { if (pending === id) finish('读取失败，请重试。'); });
        }
        function finish(message) { clearTimeout(timer); pending = ''; status.textContent = message; sync(); }
        async function copy(selectionOnly) {
            let value = text.textContent;
            if (selectionOnly) {
                const selection = getSelection();
                if (!selection?.rangeCount || !text.contains(selection.anchorNode) || !text.contains(selection.focusNode)) value = '';
                else value = selection.toString();
            }
            if (!value) { status.textContent = '请先长按或拖动选择要复制的文字。'; return; }
            status.textContent = '正在复制…';
            const ok = await client.copyText(value);
            status.textContent = ok ? '已复制。' : '复制未成功，请使用系统选择菜单复制。';
        }
        for (const node of [selected, all]) node.addEventListener('pointerdown', event => event.preventDefault());
        dialog.addEventListener('keydown', event => event.stopPropagation());
        dialog.addEventListener('close', () => {
            clearTimeout(timer); pending = ''; text.textContent = ''; status.textContent = '';
        });
        return {
            sync,
            open() {
                if (!connected() || dialog.open) return;
                dialog.showModal(); close.focus({ preventScroll: true }); load(false);
            },
            receive(result) {
                if (!['panel-copy-result', 'panel-result'].includes(result.type) || !result.id?.startsWith('copy-')) return false;
                if (result.id !== pending || !dialog.open) return true;
                if (result.code || typeof result.text !== 'string') finish('无法读取当前窗格，请返回终端后重试。');
                else {
                    text.textContent = result.text;
                    finish(result.truncated ? '内容过长，显示最近的部分输出。' : result.text ? '长按或拖动文字选择，再点击复制所选。' : '当前窗格没有可复制的文本。');
                }
                return true;
            },
        };
    }
    window.FnTextCopy = Object.freeze({ install });
}());

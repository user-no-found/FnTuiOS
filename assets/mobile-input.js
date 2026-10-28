// The phone IME edits the native terminal textarea; terminal keys keep their encoder.
(function () {
    function install(client, target, isOpen) {
        const parent = target.parentElement;
        // Android may emit no delete operation when the editable is empty.
        // This local anchor is never sent to the terminal or used as terminal history.
        const anchor = '\u200b';
        let composition = null, finishing = null, key = null, pending = null, cancelled = false;
        const blockedKeys = new Set();
        const ready = () => isOpen() && client.connected && !client.readOnly;
        const composing = event => composition !== null || event.isComposing;
        const imeKey = event => event.keyCode === 229 || ['Unidentified', 'Process'].includes(event.key);
        const preediting = event => event.isComposing || event.inputType === 'insertCompositionText'
            || composition && !(event.inputType === 'insertFromComposition' || !composition.explicit
                && ['insertText', 'insertReplacementText'].includes(event.inputType));
        const deletion = event => ['deleteContentBackward', 'deleteContentForward'].includes(event.inputType);
        const paste = event => event.inputType === 'insertFromPaste';
        const line = event => ['insertLineBreak', 'insertParagraph'].includes(event.inputType);
        function listen(type, listener) {
            parent.addEventListener(type, event => {
                if (event.target === target) listener(event);
            }, true);
        }
        function seed() {
            if (composition) return;
            target.value = ready() ? anchor : '';
            target.setSelectionRange(target.value.length, target.value.length);
        }
        function reset() {
            composition = null; finishing = null; key = null; pending = null;
            cancelled = false;
            blockedKeys.clear(); seed();
        }
        function text() { return target.value.startsWith(anchor) ? target.value.slice(1) : target.value; }
        function beginPaste() {
            if (composition) cancelled = true;
            composition = null; finishing = null;
        }
        function encoded(name) {
            const flags = client.webterm.keyboard?.flags || 0;
            const event = action => new KeyboardEvent(action, { key: name, code: name });
            const fallback = { Backspace: '\x7f', Delete: '\x1b[3~', Enter: '\r' }[name];
            return (window.WebTerm.encodeKey(event('keydown'), flags) ?? fallback)
                + (window.WebTerm.encodeKey(event('keyup'), flags) || '');
        }
        function send(value, asPaste = false) {
            if (!ready() || !value) return;
            if (asPaste) client.webterm.paste(value);
            else client.sendInput(value).catch(() => {});
        }
        function edit(event) {
            if (key?.defaultPrevented && (deletion(event) || line(event))) return;
            if (deletion(event)) send(encoded(event.inputType === 'deleteContentForward' ? 'Delete' : 'Backspace'));
            else if (line(event)) send(encoded('Enter'));
            else send(event.data ?? event.dataTransfer?.getData('text/plain') ?? text(), paste(event));
        }
        // Deduplication is scoped to one browser edit, never to previously sent text.
        function remember(slot, value) {
            const token = { ...value };
            if (slot === 'pending') pending = token; else finishing = token;
            setTimeout(() => {
                if (pending === token) pending = null;
                if (finishing === token) finishing = null;
            }, 0);
        }

        listen('keydown', event => {
            pending = null; finishing = null; key = event;
            if (!ready() || composing(event) || imeKey(event)) {
                blockedKeys.add(event.code || event.keyCode);
                key = null;
                event.stopImmediatePropagation();
            }
        });
        listen('keypress', event => {
            if (!ready() || composing(event) || imeKey(event)) event.stopImmediatePropagation();
        });
        listen('keyup', event => {
            const blocked = blockedKeys.delete(event.code || event.keyCode);
            if (blocked || !ready() || composing(event) || imeKey(event)) event.stopImmediatePropagation();
            key = null; pending = null;
            queueMicrotask(seed);
        });
        listen('compositionstart', event => {
            event.stopImmediatePropagation();
            finishing = null; pending = null; key = null;
            cancelled = false;
            composition = { data: '', explicit: true };
        });
        listen('compositionupdate', event => {
            event.stopImmediatePropagation();
            if (composition) composition.data = event.data;
        });
        listen('compositionend', event => {
            event.stopImmediatePropagation();
            const value = typeof event.data === 'string' ? event.data : text() || composition?.data || '';
            if (cancelled) {
                cancelled = false; composition = null;
                remember('finishing', { data: value }); seed(); return;
            }
            if (!composition && finishing?.data === value) { seed(); return; }
            composition = null;
            send(value);
            remember('finishing', { data: value });
            seed();
        });
        listen('beforeinput', event => {
            pending = null;
            if (!ready()) {
                if (event.cancelable) event.preventDefault();
                event.stopImmediatePropagation();
                return;
            }
            if (paste(event)) beginPaste();
            if (!paste(event) && preediting(event)) {
                event.stopImmediatePropagation();
                return;
            }
            if (!deletion(event) && !paste(event) && !line(event)) return;
            event.stopImmediatePropagation();
            if (!event.cancelable) return; // input observes the completed edit.
            if (paste(event) && event.data == null && !event.dataTransfer) return;
            event.preventDefault();
            edit(event);
            remember('pending', { type: event.inputType });
            seed();
        });
        listen('input', event => {
            if (!ready()) { event.stopImmediatePropagation(); seed(); return; }
            if (paste(event)) beginPaste();
            if (!paste(event) && preediting(event)) {
                event.stopImmediatePropagation();
                if (!composition) { cancelled = false; composition = { data: '', explicit: false }; }
                composition.data = event.data ?? text();
                return;
            }
            const completed = composition !== null;
            composition = null;
            if (finishing && event.data === finishing.data
                && (!text() || ['insertText', 'insertFromComposition'].includes(event.inputType))) {
                event.stopImmediatePropagation(); seed(); return;
            }
            if (event.inputType === 'insertText' && key && !completed) {
                // A physical key may already have been encoded by xterm.
                queueMicrotask(seed);
                return;
            }
            event.stopImmediatePropagation();
            if (pending?.type !== event.inputType) edit(event);
            pending = null;
            if (completed) remember('finishing', { data: event.data }); else finishing = null;
            seed();
        });
        listen('paste', event => {
            const clipboard = event.clipboardData;
            if (!clipboard || [...clipboard.items].some(item => item.kind === 'file')) return;
            event.preventDefault(); event.stopImmediatePropagation();
            beginPaste();
            send(clipboard.getData('text/plain'), true);
            remember('pending', { type: 'insertFromPaste' });
            seed();
        });
        listen('focus', seed);
        return { reset, isComposing: () => composition !== null };
    }
    window.FnMobileInput = Object.freeze({ install });
}());

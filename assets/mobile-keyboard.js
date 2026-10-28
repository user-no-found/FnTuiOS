// Only the phone panel may authorize the terminal's soft keyboard.
(function () {
    const instances = new WeakMap();
    function install(client) {
        if (instances.has(client) || !client.mobile?.enabled) return;
        const target = client.webterm?.xterm.textarea;
        if (!target) throw new Error('FnTuiOS mobile keyboard input is unavailable');
        const nativeFocus = target.focus.bind(target);
        const inputMode = target.getAttribute('inputmode') || 'text';
        let opened = false, observed = false, dismissTimer;
        let baselineHeight = height(), baselineWidth = innerWidth;
        const keyboard = navigator.virtualKeyboard;
        const input = window.FnMobileInput.install(client, target, () => opened);
        function height() { return window.visualViewport?.height || innerHeight; }
        function changed() { dispatchEvent(new Event('fntuios:keyboard-change')); }
        function close() {
            clearTimeout(dismissTimer);
            const wasOpen = opened;
            opened = false; observed = false;
            input.reset();
            client.mobile.rescueUntil = 0;
            if (wasOpen && keyboard?.hide) { try { keyboard.hide(); } catch {} }
            target.setAttribute('inputmode', 'none');
            target.blur();
            client.mobile.setKeyboardOpen(false);
            if (wasOpen) changed();
        }
        function toggle() {
            if (opened) { close(); return; }
            if (!client.connected || client.readOnly) return;
            baselineHeight = height(); baselineWidth = innerWidth;
            opened = true; observed = false;
            input.reset();
            target.setAttribute('inputmode', inputMode);
            nativeFocus({ preventScroll: true });
            if (keyboard?.show) { try { keyboard.show(); } catch {} }
            client.mobile.setKeyboardOpen(true);
            changed();
        }
        function measure() {
            if (!opened) return;
            if (innerWidth !== baselineWidth) {
                baselineWidth = innerWidth;
                baselineHeight = height() + (keyboard?.boundingRect?.height || 0);
                observed = false;
            }
            const inset = keyboard?.boundingRect?.height || Math.max(0, baselineHeight - height());
            clearTimeout(dismissTimer);
            if (inset >= 48) observed = true;
            else if (observed) dismissTimer = setTimeout(() => {
                // IME candidate/clipboard transitions can briefly report no inset.
                if (opened && !input.isComposing()) close();
                else if (opened) measure();
            }, 300);
        }
        // This instance guard also covers xterm, initial connection, copy and photo refocus.
        // It does not replace browser prototypes or alter desktop input handling.
        target.focus = options => { if (opened) nativeFocus(options); };
        target.setAttribute('virtualkeyboardpolicy', 'auto');
        target.addEventListener('pointerdown', event => { if (!opened) event.preventDefault(); });
        target.addEventListener('focus', () => { if (!opened) target.blur(); });
        // A transient IME blur can precede its final composition/input event.
        // Explicit close, stable viewport dismissal and page exit revoke access.
        window.visualViewport?.addEventListener('resize', measure, true);
        keyboard?.addEventListener('geometrychange', measure, true);
        addEventListener('resize', measure, true);
        addEventListener('pagehide', close);
        document.addEventListener('visibilitychange', () => { if (document.hidden) close(); });
        instances.set(client, { toggle, close, target: () => opened ? target : null, isOpen: () => opened });
        close();
    }
    window.FnMobileKeyboard = Object.freeze({
        install,
        focusTarget(client) { return instances.get(client)?.target() || null; },
        isOpen(client = window.sipTerm) { return instances.get(client)?.isOpen() || false; },
        toggle(client) { instances.get(client)?.toggle(); },
        close(client) { instances.get(client)?.close(); },
    });
}());

/* Optional faces load after the regular font has established terminal metrics. */
(function (global) {
    'use strict';
    let started = false;
    global.FnStartupFonts = {
        load(family) {
            if (started) return;
            started = true;
            global.requestAnimationFrame(() => global.setTimeout(() => {
                for (const [name, weight, style] of [
                    ['Bold', '700', 'normal'], ['Italic', '400', 'italic'], ['BoldItalic', '700', 'italic']
                ]) {
                    const url = new URL(`static/fonts/JetBrainsMonoNerdFontMono-${name}.woff2`, document.baseURI);
                    const face = new FontFace(family, `url("${url.href}") format("woff2")`, { weight, style });
                    void face.load().then(loaded => document.fonts.add(loaded)).catch(() => {
                        console.warn('Optional terminal font unavailable');
                    });
                }
            }, 0));
        }
    };
})(window);

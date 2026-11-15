import assert from 'node:assert/strict';

// Real connection and output must precede optional fonts, on desktop and phones.
export async function verifyStartup(browser, entry) {
  const results = [];
  for (const mobile of [false, true]) {
    const context = await browser.newContext(mobile ? {
      viewport: {width: 390, height: 844}, isMobile: true, hasTouch: true,
      userAgent: 'Mozilla/5.0 (Linux; Android 14; Mobile) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/155.0.0.0 Mobile Safari/537.36'
    } : {viewport: {width: 1280, height: 800}});
    const page = await context.newPage(), errors = [], held = [];
    let release;
    const pending = new Promise(resolve => { release = resolve; });
    await page.route(/JetBrainsMonoNerdFontMono-(Bold|Italic|BoldItalic)\.woff2/, async route => {
      held.push(route.request().url());
      await pending;
      await route.continue();
    });
    page.on('pageerror', error => errors.push(error.message));
    let output = 0;
    page.on('websocket', socket => socket.on('framereceived', ({payload}) => {
      if (Buffer.from(payload)[0] === 0x31) output++;
    }));
    try {
      await page.goto(entry.href, {waitUntil: 'domcontentloaded'});
      await page.waitForFunction(() => [...document.querySelectorAll('iframe')].some(f => f.contentWindow?.sipTerm?.connected));
      const frame = page.frames().find(f => new URL(f.url() || 'about:blank').pathname.startsWith('/app/tuios/'));
      assert(frame);
      await frame.waitForFunction(() => window.sipTerm.term.buffer.active.length > 0);
      await page.waitForFunction(() => window.frames[0].sipTerm.webtermOptions().fonts.length === 1);
      const grid = () => frame.evaluate(() => ({cols: window.sipTerm.term.cols, rows: window.sipTerm.term.rows}));
      const before = await grid();
      assert(before.cols > 0 && before.rows > 0);
      const deadline = Date.now() + 5000;
      while (held.length < 3 && Date.now() < deadline) await page.waitForTimeout(20);
      assert.equal(held.length, 3, 'all optional faces must be loaded after connecting');
      assert(output > 0, 'terminal output waited for optional fonts');
      release();
      await frame.waitForFunction(() => [...document.fonts].filter(face =>
        face.status === 'loaded' && (face.weight === '700' || face.style === 'italic')).length >= 3);
      assert.deepEqual(await grid(), before, 'optional fonts changed the cell grid');
      assert.deepEqual(errors, []);
      results.push({mobile, optionalFontsHeld: 3, connectedWithOutput: true, gridStable: true});
    } finally {
      release();
      await context.close();
    }
  }
  // Use real browser cache semantics, without routing (routing disables caching).
  const context = await browser.newContext();
  try {
    const page = await context.newPage();
    await page.goto(new URL('/app/tuios/', entry).href, {waitUntil: 'domcontentloaded'});
    const result = await page.evaluate(async () => {
      const url = new URL('static/webterm.js', location.href);
      const first = await fetch(url, {cache: 'no-cache'});
      const bytes = (await first.arrayBuffer()).byteLength;
      const second = await fetch(url, {headers: {'If-None-Match': first.headers.get('etag')}, cache: 'no-store'});
      const document = await fetch(location.href, {cache: 'no-store'});
      return {bytes, encoding: first.headers.get('content-encoding'), cache: first.headers.get('cache-control'),
        conditionalStatus: second.status, conditionalBytes: (await second.arrayBuffer()).byteLength,
        documentCache: document.headers.get('cache-control')};
    });
    assert.equal(result.encoding, 'gzip');
    assert.equal(result.cache, 'private, no-cache');
    assert.equal(result.conditionalStatus, 304);
    assert.equal(result.conditionalBytes, 0);
    assert.equal(result.documentCache, 'no-store');
    results.push(result);
  } finally { await context.close(); }
  return results;
}

#!/usr/bin/env node
// Exercise real browser origin metadata across the isolated TLS-to-Unix gateway.
import { mkdir, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { resolve } from 'node:path';
import { parseArgs } from 'node:util';

const { values } = parseArgs({ options: {
  url: { type: 'string' }, output: { type: 'string' }, policy: { type: 'string' },
  playwright: { type: 'string' }, chromium: { type: 'string' }, mobile: { type: 'boolean', default: false },
} });
const entry = new URL(values.url);
if (entry.protocol !== 'https:' || entry.hostname !== '127.0.0.1' || !['strict', 'repaired'].includes(values.policy)) {
  throw new Error('gateway browser checks require an isolated TLS fixture and explicit policy');
}
const { chromium } = createRequire(import.meta.url)(resolve(values.playwright));
const output = resolve(values.output);
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ executablePath: resolve(values.chromium), headless: true,
  args: ['--no-sandbox', '--disable-dev-shm-usage'] });
const context = await browser.newContext({ ignoreHTTPSErrors: true,
  viewport: values.mobile ? { width: 390, height: 844 } : { width: 1280, height: 800 },
  isMobile: values.mobile, hasTouch: values.mobile });
const page = await context.newPage();
const checks = [], errors = [];
page.on('pageerror', error => errors.push(error.message));
await page.addInitScript(() => localStorage.setItem('sip-web-settings', JSON.stringify({
  renderer: 'canvas', fontSize: 14, transport: 'websocket', copyOnSelect: false,
})));
async function until(predicate, message, timeout = 25000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    if (await predicate()) return;
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  throw new Error(message);
}
async function evidence() { return (await context.request.get(new URL('/__fixture_gateway_events', entry).href)).json(); }
let frame;
async function screen() {
  return frame.evaluate(() => {
    const terminal = window.sipTerm.term, buffer = terminal.buffer.active;
    return Array.from({ length: terminal.rows }, (_, row) =>
      buffer.getLine(buffer.viewportY + row)?.translateToString(true) ?? '').join('\n');
  });
}
try {
  await page.goto(entry.href, { waitUntil: 'domcontentloaded' });
  await until(async () => {
    frame = page.frames().find(item => item.url().includes('/app/tuios/'));
    return frame && await frame.locator('.fnpackkit-connection-status').count();
  }, 'connection status surface not installed');
  if (values.mobile && await frame.evaluate(() => innerWidth) > 450) throw new Error('mobile fixture is not a narrow CSS viewport');
  if (values.policy === 'strict') {
    // Fonts may fail before sip reaches connect. Independently use the browser's
    // own WebSocket API so the old-policy negative control covers both routes.
    await frame.evaluate(() => new Promise(resolve => {
      const socket = new WebSocket(location.origin.replace('https:', 'wss:') + '/app/tuios/ws');
      socket.onopen = () => { socket.close(); resolve('open'); };
      socket.onerror = () => resolve('error');
      setTimeout(() => { socket.close(); resolve('timeout'); }, 4000);
    }));
    await until(async () => (await evidence()).events.some(item => item.route === 'diagnostic' && item.status === 403),
      'failed connection did not diagnose its gateway rejection');
    const events = (await evidence()).events;
    for (const route of ['font', 'websocket', 'diagnostic']) {
      if (!events.some(item => item.route === route && item.status === 403 && (route === 'websocket' ? item.fetch_site === '' : item.fetch_site === 'same-origin') && item.has_origin)) {
        throw new Error(`strict policy did not reproduce real-browser ${route} 403`);
      }
    }
    const panel = frame.locator('.fnpackkit-connection-status');
    if (!await panel.isVisible() || !await panel.textContent()) throw new Error('gateway rejection remained blank');
    if (values.mobile) {
      if (!await frame.locator('#webterm-keybar, #sip-keybar').first().isVisible()) throw new Error('mobile key bar missing');
      const before = events.filter(item => item.route === 'diagnostic').length;
      await panel.locator('button').click();
      await until(async () => (await evidence()).events.filter(item => item.route === 'diagnostic').length > before,
        'mobile retry button did not start a new diagnostic cycle');
      await until(async () => (await panel.textContent()).includes('ID'), 'retry diagnostic not visible');
      checks.push('touch viewport keeps failure and retry actionable above the mobile key bar');
    }
    await page.screenshot({ path: `${output}/strict-rejection.png` });
    checks.push('explicit false reproduces font, WebSocket and POST diagnostic 403; browser metadata is real and absent on WebSocket');
    checks.push('localized rejection and server request ID are visible over the otherwise blank terminal');
  } else {
    await until(() => frame.evaluate(() => !!window.sipTerm?.connected), 'repaired gateway did not connect');
    const identity = await (await context.request.get(new URL('/__fixture_identity', entry).href)).json();
    await frame.locator('.xterm-screen').click();
    await page.keyboard.press('Control+b');
    await page.keyboard.press('c');
    await until(async () => (await screen()).includes(identity.username + '@'), 'isolated shell not ready');
    await page.keyboard.press('i');
    await page.keyboard.press('Control+u');
    await page.keyboard.type("printf 'FNTUIOS_REMOTE_%s\\n' 'OK'");
    await page.keyboard.press('Enter');
    await until(async () => (await screen()).includes('FNTUIOS_REMOTE_OK'), 'real terminal output missing');
    await until(async () => !await frame.locator('.fnpackkit-connection-status').isVisible(), 'success status covers terminal');
    const events = (await evidence()).events;
    if (!events.some(item => item.route === 'font' && item.status === 200 && item.fetch_site === 'same-origin' && item.has_origin) ||
        !events.some(item => item.route === 'websocket' && item.status === 101 && item.fetch_site === '' && item.has_origin)) {
      throw new Error('browser evidence lacks accepted font and WebSocket with real origin metadata');
    }
    if (values.mobile && !await frame.locator('#webterm-keybar, #sip-keybar').first().isVisible()) throw new Error('mobile key bar missing');
    await page.screenshot({ path: `${output}/repaired-terminal.png` });
    checks.push('same-origin HTTPS browser survives translated authority: fonts 200, WebSocket 101, actual shell output');
    checks.push('first terminal output hides diagnostics without stealing terminal focus');
    const attacker = await context.newPage();
    await attacker.goto((await evidence()).attacker);
    await attacker.evaluate(async target => {
      await fetch(target + '/app/tuios/_gateway/status', { method: 'POST', body: '', credentials: 'include' }).catch(() => {});
      await fetch(target + '/app/tuios/_gateway/grant', { method: 'POST', body: '', credentials: 'include' }).catch(() => {});
      await new Promise(resolve => {
        const socket = new WebSocket(target.replace('https:', 'wss:') + '/app/tuios/ws');
        socket.onerror = () => resolve();
        socket.onopen = () => { socket.close(); resolve(); };
        setTimeout(() => { socket.close(); resolve(); }, 4000);
      });
    }, entry.origin);
    const after = (await evidence()).events;
    for (const route of ['diagnostic', 'grant', 'websocket']) {
      if (!after.some(item => item.route === route && item.status === 403 && (route === 'websocket' ? item.fetch_site === '' : item.fetch_site === 'same-site'))) {
        throw new Error(`different-port malicious browser ${route} was not rejected`);
      }
    }
    await attacker.close();
    checks.push('different-port browser diagnostic, grant POST and WebSocket remain rejected');
    if (errors.length) throw new Error('unexpected browser errors: ' + errors.join('; '));
  }
  await writeFile(`${output}/gateway-browser.json`, JSON.stringify({ policy: values.policy, profile: values.mobile ? 'Chromium mobile emulation' : 'Chromium desktop', checks,
    events: (await evidence()).events,
    boundary: 'isolated current-UID worker; real HTTPS Chromium metadata through a test ingress that rewrites Host; no live fnOS/FN Connect login' }, null, 2));
} catch (error) {
  await page.screenshot({ path: `${output}/failure.png` }).catch(() => {});
  await writeFile(`${output}/failure.json`, JSON.stringify({ error: error.message, events: (await evidence()).events, errors }, null, 2));
  throw error;
} finally {
  await context.request.post(new URL('/__fixture_stop', entry).href).catch(() => {});
  await browser.close();
}

#!/usr/bin/env node
// Exercise the real terminal in a same-origin iframe through the isolated gateway fixture.
import { mkdir, rm, writeFile } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';
import { resolve } from 'node:path';
import { parseArgs } from 'node:util';
import { verifyImages } from './verify_image_browser.mjs';
import { verifyNativeUI } from './verify_native_ui.mjs';

const { values } = parseArgs({ options: {
  url: { type: 'string' }, output: { type: 'string' },
  playwright: { type: 'string' }, chromium: { type: 'string' },
  english: { type: 'boolean', default: false },
  quiet: { type: 'boolean', default: false },
  'negative-image-key': { type: 'boolean', default: false },
} });
for (const key of ['url', 'output', 'playwright', 'chromium']) {
  if (!values[key]) throw new Error(`--${key} is required`);
}
const entry = new URL(values.url);
if (!['127.0.0.1', 'localhost'].includes(entry.hostname)) {
  throw new Error('This harness accepts only the isolated loopback fixture');
}
const { launchBrowser, closeBrowser } = await import(pathToFileURL(process.env.BUILDFLOW_BROWSER_RUNTIME));
const identity = await (await fetch(new URL('/__fixture_identity', entry))).json();
const output = resolve(values.output);
await mkdir(output, { recursive: true });
const browser = await launchBrowser({ args: ['--no-sandbox', '--disable-dev-shm-usage'] });
const checks = [], captures = {}, sockets = [], failures = [], wire = [];
const page = await browser.newPage({ viewport: { width: 1440, height: 960 } });
page.on('websocket', socket => {
  sockets.push(socket.url());
  for (const [event, direction] of [['framesent', 'sent'], ['framereceived', 'received']]) {
    socket.on(event, ({ payload }) => wire.push({ direction, bytes: Buffer.from(payload) }));
  }
});
page.on('pageerror', error => failures.push(error.message));
page.on('response', response => {
  if (response.status() >= 400 && !response.url().includes('/image-paste/')) failures.push(`${response.status()} ${response.url()}`);
});
await page.addInitScript(() => {
  window.__fnPasteEvents = [];
  window.__fnKeyEvents = [];
  for (const type of ['keydown', 'keyup']) window.addEventListener(type, event => {
    if (event.code !== 'KeyV' || !window.sipTerm?.webterm) return;
    const flags = window.sipTerm.webterm.keyboard.flags;
    window.__fnKeyEvents.push({ type: event.type, repeat: event.repeat, flags,
      encoded: window.WebTerm.encodeKey(event, flags) });
  }, true);
  window.addEventListener('paste', event => {
    window.__fnPasteEvents.push({ trusted: event.isTrusted, types: [...(event.clipboardData?.types || [])],
      items: [...(event.clipboardData?.items || [])].map(item => ({ kind: item.kind, type: item.type })) });
    if (window.__fnPreventPaste) { event.preventDefault(); event.stopImmediatePropagation(); }
  }, true);
});
await page.addInitScript(({ english }) => localStorage.setItem('sip-web-settings', JSON.stringify({
  transport: english ? 'websocket' : 'webtransport', renderer: 'canvas', fontSize: 14,
  copyOnSelect: false, cursorBlink: false,
})), { english: values.english });

async function until(predicate, reason, timeout = 20000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    if (await predicate()) return;
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  throw new Error(reason);
}
let frame;
let mutationApplied = false;
async function screen() {
  return frame.evaluate(() => {
    const terminal = window.sipTerm.term, buffer = terminal.buffer.active;
    return Array.from({ length: terminal.rows }, (_, row) =>
      buffer.getLine(buffer.viewportY + row)?.translateToString(true) ?? '').join('\n');
  });
}
async function capture(name) {
  captures[name] = await screen();
  await writeFile(`${output}/${name}.txt`, captures[name]);
  await page.screenshot({ path: `${output}/${name}.png` });
}
async function prefix(key) {
  await page.keyboard.press('Control+b');
  await page.keyboard.press(key);
}

async function clickText(text) {
  const cell = await frame.evaluate(text => {
    const terminal = window.sipTerm.term, buffer = terminal.buffer.active;
    for (let row = 0; row < terminal.rows; row++) {
      const line = buffer.getLine(buffer.viewportY + row);
      for (let col = 0; col < terminal.cols; col++) {
        let suffix = '';
        for (let x = col; x < terminal.cols && suffix.length < text.length; x++) suffix += line?.getCell(x)?.getChars() || '';
        if (suffix.startsWith(text)) return { row, col, cols: terminal.cols, rows: terminal.rows };
      }
    }
    return null;
  }, text);
  if (!cell) throw new Error(`terminal label missing: ${text}`);
  const bounds = await frame.locator('.xterm-screen').boundingBox();
  await page.mouse.click(bounds.x + (cell.col + 0.5) * bounds.width / cell.cols,
    bounds.y + (cell.row + 0.5) * bounds.height / cell.rows);
}

async function option(value) {
  const endpoint = new URL('/__fixture_option', entry);
  if (value !== undefined) endpoint.searchParams.set('value', value);
  const response = await fetch(endpoint, { method: value === undefined ? 'GET' : 'POST' });
  if (!response.ok) throw new Error('fixture option query failed');
  return response.json();
}

try {
  if (values['negative-image-key']) {
    await page.route('**/static/paste-input.js', async route => {
      const response = await route.fetch();
      const source = await response.text();
      if (source.split('key(event) {').length !== 2) throw new Error('negative key control anchor drift');
      await route.fulfill({ response, body: source.replace('key(event) {', 'key(event) { return true;') });
      mutationApplied = true;
    });
  }
  await page.goto(entry.href, { waitUntil: 'domcontentloaded' });
  await until(async () => {
    frame = page.frames().find(item => item.url().includes('/app/tuios/'));
    return frame && await frame.evaluate(() => !!window.sipTerm?.connected);
  }, 'iframe terminal did not connect', 60000);
  await frame.locator('.xterm-screen').click();
  await prefix('c');
  await until(async () => (await screen()).includes(identity.username + '@'), 'new shell did not become ready');
  await page.keyboard.press('i');
  await page.keyboard.press('Control+u');
  await page.keyboard.type("printf 'FN_BROWSER_%s\\n' 'OK'; printf 'FN_UID_%s\\n' \"$(id -u)\"", { delay: 2 });
  await page.keyboard.press('Enter');
  await until(async () => (await screen()).includes('FN_BROWSER_OK'), 'real WebSocket shell marker absent');
  if (!(await screen()).includes('FN_UID_' + identity.uid)) throw new Error('browser shell has wrong UID');
  await capture('terminal');
  checks.push(`same-origin iframe loads resources and real UID ${identity.uid} shell over WebSocket`);

  await prefix(',');
  await until(async () => (values.english ? /Settings/ : /设置/).test(await screen()), 'settings translation absent');
  await capture('settings');
  const originalOption = await option();
  if (!['top', 'bottom', 'hidden'].includes(originalOption.value)) throw new Error('machine enum is translated');
  await clickText(values.english ? 'Dock' : '停靠栏');
  await until(async () => (values.english ? /Dock position/ : /停靠栏位置/).test(await screen()), 'dock setting absent');
  await page.keyboard.press('ArrowLeft');
  await until(async () => frame.evaluate(() => {
    const terminal = window.sipTerm.term, buffer = terminal.buffer.active;
    return (buffer.getLine(buffer.viewportY + terminal.rows - 1)?.translateToString(true) ?? '').includes('\uf011');
  }), 'changing displayed dock position did not move dock to bottom');
  const afterUI = await option();
  if (afterUI.value !== originalOption.value) throw new Error('read-only web settings unexpectedly persisted');
  const canonical = await option('bottom');
  if (canonical.value !== 'bottom') throw new Error('CLI enum roundtrip corrupted canonical value');
  await option(originalOption.value);
  checks.push(`${values.english ? 'Displayed' : 'Translated'} enum changes the client layout; daemon value stays canonical; CLI bottom roundtrip is exact`);
  const wideTerminal = await frame.evaluate(() => ({
    cols: window.sipTerm.term.cols, rows: window.sipTerm.term.rows,
  }));
  await page.setViewportSize({ width: 800, height: 600 });
  await until(async () => frame.evaluate(() => window.sipTerm.term.cols < 120), 'terminal did not resize');
  await capture('settings-narrow');
  await page.setViewportSize({ width: 1440, height: 960 });
  await until(async () => frame.evaluate(({ cols, rows }) =>
    window.sipTerm.term.cols === cols && window.sipTerm.term.rows === rows,
  wideTerminal), 'terminal did not restore its wide dimensions');
  // Focusing through a mouse click can activate a setting beneath the pointer.
  await frame.evaluate(() => window.sipTerm.term.focus());
  await page.keyboard.press('Escape');
  await until(async () => !(values.english ? /Settings \(this session only\)/ : /设置（仅此会话）/).test(await screen()), 'settings did not close');
  await prefix('?');
  await until(async () => (values.english ? /Help|Keybindings|Keyboard/ : /帮助|快捷键/).test(await screen()), 'help translation absent');
  await capture('help');
  await page.keyboard.press('Escape');
  await until(async () => !(values.english ? /Keybindings/ : /快捷键/).test(await screen()), 'help did not close');
  const closeCell = await frame.evaluate(() => {
    const terminal = window.sipTerm.term, buffer = terminal.buffer.active;
    for (let row = 0; row < terminal.rows; row++) {
      const line = buffer.getLine(buffer.viewportY + row);
      for (let col = 0; col < terminal.cols; col++) {
        if (line?.getCell(col)?.getChars() === '\uf011') return { row, col, cols: terminal.cols, rows: terminal.rows };
      }
    }
    return null;
  });
  if (!closeCell) throw new Error('session-close control is missing');
  const bounds = await frame.locator('.xterm-screen').boundingBox();
  await page.mouse.click(bounds.x + (closeCell.col + 0.5) * bounds.width / closeCell.cols,
    bounds.y + (closeCell.row + 0.5) * bounds.height / closeCell.rows);
  await until(async () => (values.english ? /close session|Close session/ : /关闭会话|结束会话/).test(await screen()), 'session-close confirmation absent');
  await capture('confirmation');
  await page.keyboard.press('Escape');
  checks.push(values.english ? 'settings/help/confirmation and narrow/restore layout exercised' :
    'Chinese settings/help/session-close confirmation, narrow viewport and restored layout exercised');

  if (!values.english) {
    checks.push(...await verifyNativeUI({ page, frame, output, until, screen, capture }));
    checks.push(...await verifyImages({ page, frame, entry, output, until, wire, capture, identity }));
  }

  if (!sockets.length || sockets.some(url => !new URL(url).pathname.startsWith('/app/tuios/'))) {
    throw new Error(`WebSocket escaped gateway prefix: ${sockets.join(', ')}`);
  }
  if (failures.length) throw new Error(failures.join('\n'));
  checks.push('assets and WebSocket remain under gateway prefix; no HTTP or page errors');
  await writeFile(`${output}/browser.json`, JSON.stringify({ checks, sockets,
    language: values.english ? 'upstream English fixture' : 'zh-CN',
    boundary: identity.single_user ? 'native current-UID worker and Unix gateway; privilege dropping, second UID, FN entitlement and installation not exercised' : 'disposable two-UID fixture; not a live FN Connect login' }, null, 2));
  await Promise.all(['failure.json', 'failure.txt', 'failure.png'].map(name => rm(`${output}/${name}`, { force: true })));
  if (!values.quiet) console.log(JSON.stringify({ checks, output }, null, 2));
} catch (error) {
  if (frame) await capture('failure').catch(() => {});
  await writeFile(`${output}/failure.json`, JSON.stringify({ error: error.message, code: error.code || null, evidence: error.evidence || null, mutation: { id: 'disable-native-paste-key', applied: mutationApplied }, failures, sockets, input: wire.filter(item => item.direction === 'sent' && item.bytes[0] === 0x30).slice(-30).map(item => item.bytes.subarray(1).toString()), paste: await frame?.evaluate(() => window.__fnPasteEvents), keys: await frame?.evaluate(() => window.__fnKeyEvents), imageStatus: await frame?.locator('#fn-image-paste-status').textContent().catch(() => '') }, null, 2));
  throw error;
} finally {
  await closeBrowser(browser);
  await fetch(new URL('/__fixture_stop', entry), { method: 'POST' }).catch(() => {});
}

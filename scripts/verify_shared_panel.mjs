import assert from 'node:assert/strict';
import { parseArgs } from 'node:util';
import { pathToFileURL } from 'node:url';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { activateLauncher, activateLauncherByKeyboard, clickLauncher } from './panel_launcher.mjs';
import { verifyPanelDrag } from './verify_panel_drag.mjs';

const { values } = parseArgs({ options: { url: { type: 'string' }, output: { type: 'string' } } });
const entry = new URL(values.url);
if (entry.hostname !== '127.0.0.1') throw new Error('isolated loopback fixture required');
const { withBrowser } = await import(pathToFileURL(process.env.BUILDFLOW_BROWSER_RUNTIME));
await mkdir(values.output, { recursive: true });

await withBrowser({}, async browser => {
  const results = [];
  const selected = process.env.FNTUIOS_PANEL_BROWSER_CASE || 'all';
  if (!['all', 'focus', 'keyboard'].includes(selected)) throw new Error('unknown consumer panel browser case');
  for (const mobile of selected === 'all' ? [false, true] : [selected === 'focus']) {
    const context = await browser.newContext({ viewport: mobile ? { width: 390, height: 844 } : { width: 1280, height: 800 },
      isMobile: mobile, hasTouch: mobile });
    const page = await context.newPage();
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    const button = name => page.getByRole('button', { name, exact: true });
    const launcher = button('后端控制');
    const state = () => launcher.getAttribute('data-ui-state');
    const keyboard = () => page.evaluate(() => ({ open: window.FnMobileKeyboard.isOpen(),
      focused: document.activeElement === window.sipTerm.webterm.xterm.textarea,
      mode: window.sipTerm.webterm.xterm.textarea.inputMode }));
    const calls = () => page.evaluate(() => window.__calls.splice(0));
    let keyboardTransitions = null;
    try {
      await page.goto(entry.href, { waitUntil: 'load' });
      await launcher.waitFor();
      const drag = selected === 'all' ? await verifyPanelDrag(page, page.mainFrame(), mobile) : 'not run: ' + selected + '-only case';
      await activateLauncher(page.mainFrame());
      await page.getByText('运行中', { exact: true }).waitFor();
      assert.equal(await page.getByRole('dialog').getAttribute('aria-modal'), 'false');
      if (!mobile) {
        for (const [name, direction] of [['上分屏', 'up'], ['下分屏', 'down'], ['左分屏', 'left'], ['右分屏', 'right']]) {
          await activateLauncher(page.mainFrame());
          await button(name).click();
          await page.waitForFunction(() => window.__calls.length > 0);
          assert.deepEqual(await calls(), [direction]);
        }
        for (const [name, expected] of [['终端显示', 'terminal-settings'], ['复制文本', 'copy-text']]) {
          await activateLauncher(page.mainFrame()); await button(name).click();
          assert.deepEqual(await calls(), [expected]);
        }
        keyboardTransitions = await activateLauncherByKeyboard(page.mainFrame());
        await page.getByRole('dialog').waitFor();
        await page.keyboard.press('Escape');
        assert.equal(await state(), 'half');
        assert.deepEqual(await calls(), [], 'runtime control keys reached terminal transport');
        assert.deepEqual(await page.evaluate(() => window.__keys), [], 'runtime control keys reached terminal input');
      } else {
        assert.deepEqual(await keyboard(), { open: false, focused: false, mode: 'none' });
        await button('键盘').tap();
        assert.deepEqual(await keyboard(), { open: true, focused: true, mode: 'text' });
        await activateLauncher(page.mainFrame());
        assert.equal((await keyboard()).focused, true, 'launcher stole terminal editable focus');
        await page.waitForFunction(() => {
          const root = document.querySelector('[data-fnpackkit-runtime]').shadowRoot;
          const panel = root.querySelector('.panel').getBoundingClientRect();
          const photo = root.querySelector('[data-action="photo"]').getBoundingClientRect();
          const key = root.querySelector('[data-action="keyboard"]').getBoundingClientRect();
          return panel.bottom <= document.querySelector('#sip-keybar').getBoundingClientRect().top &&
            panel.width <= 320 && Math.abs(photo.width - key.width) < 1 && photo.y === key.y;
        }, null, { timeout: 2000 });
        const geometry = await page.evaluate(() => {
          const root = document.querySelector('[data-fnpackkit-runtime]').shadowRoot;
          const panel = root.querySelector('.panel').getBoundingClientRect();
          const photo = root.querySelector('[data-action="photo"]').getBoundingClientRect();
          const key = root.querySelector('[data-action="keyboard"]').getBoundingClientRect();
          return { bottom: panel.bottom, bar: document.querySelector('#sip-keybar').getBoundingClientRect().top,
            width: panel.width, height: panel.height, columns: Math.abs(photo.width - key.width) < 1 && photo.y === key.y };
        });
        assert(geometry.bottom <= geometry.bar && geometry.width <= 320 && geometry.columns);
        for (const name of ['Ctrl', 'Shift', 'Alt']) {
          await button(name).tap();
          assert.equal(await state(), 'open', 'KeepOpen modifier target click closed panel');
          assert.equal(await button(name).getAttribute('aria-pressed'), 'true');
        }
        for (const [name, key, mods] of [['↑', 'Up', '111'], ['↓', 'Down', '000'], ['←', 'Left', '000'], ['→', 'Right', '000']]) {
          await button(name).tap();
          assert.deepEqual(await calls(), ['input:Arrow' + key + ':' + mods]);
          assert.equal(await state(), 'open', 'KeepOpen arrow closed panel');
          assert.equal((await keyboard()).focused, true, 'KeepOpen action stole terminal focus');
          for (const modifier of ['Ctrl', 'Shift', 'Alt']) assert.equal(await button(modifier).getAttribute('aria-pressed'), 'false');
        }
        await page.evaluate(() => document.body.click());
        assert.equal(await state(), 'open', 'synthetic outside click closed panel');
        await page.touchscreen.tap(40, 40);
        assert.equal(await state(), 'half', 'real outside pointer did not close panel');
        assert.equal((await keyboard()).focused, true, 'outside textarea interaction lost terminal focus');
        await activateLauncher(page.mainFrame());
        await button('刷新状态').tap();
        assert.equal((await keyboard()).focused, true, 'refresh stole terminal focus');
        await button('关闭控制面板').tap();
        assert.equal(await state(), 'half');
        assert.equal((await keyboard()).focused, true, 'close stole terminal focus');
        await clickLauncher(page.mainFrame());
        assert.equal(await state(), 'revealed'); assert.equal((await keyboard()).focused, true);
        await page.waitForTimeout(3200);
        assert.equal(await state(), 'half'); assert.equal((await keyboard()).focused, true);
        await activateLauncher(page.mainFrame());
        await page.waitForTimeout(5200);
        assert.equal(await state(), 'half'); assert.equal((await keyboard()).focused, true, 'idle stole terminal focus');
        await activateLauncher(page.mainFrame());
        await page.keyboard.down('Escape');
        assert.equal(await state(), 'half', 'Escape from focused terminal did not close panel');
        await page.keyboard.down('Escape');
        await page.keyboard.up('Escape');
        assert.deepEqual(await page.evaluate(() => window.__keys), [], 'panel Escape repeat/key pair reached business input');
        assert.equal((await keyboard()).focused, true, 'Escape stole terminal input focus');
        await page.keyboard.press('Escape');
        assert.deepEqual(await page.evaluate(() => window.__keys.splice(0)),
          [{ type: 'keydown', key: 'Escape' }, { type: 'keyup', key: 'Escape' }], 'closed-panel business Escape was suppressed');
        await activateLauncher(page.mainFrame()); await button('键盘').tap();
        assert.deepEqual(await keyboard(), { open: false, focused: false, mode: 'none' });
        await activateLauncher(page.mainFrame()); await button('添加照片').tap();
        assert.deepEqual(await calls(), ['photo']);
        await activateLauncher(page.mainFrame());
        await page.evaluate(() => { window.sipTerm.readOnly = true; window.FnPanelControls.sync(window.sipTerm); });
        for (const name of ['Ctrl', 'Shift', 'Alt', '↑', '↓', '←', '→', '添加照片', '键盘']) assert(await button(name).isDisabled());
        await page.evaluate(() => { window.sipTerm.readOnly = false; window.sipTerm.connected = false; window.FnPanelControls.sync(window.sipTerm); });
        for (const name of ['Ctrl', '↑', '添加照片', '键盘']) assert(await button(name).isDisabled());
      }
      assert.deepEqual(errors, []);
      await page.screenshot({ path: resolve(values.output, mobile ? 'touch-panel.png' : 'pointer-panel.png'), animations: 'disabled' });
      results.push({ mobile, drag, businessAdapters: 'real mobile-input/mobile-keyboard/panel-controls; inert terminal client',
        actions: 'real selector targets', keyboardTransitions,
        focusedEditable: mobile ? 'open/close/idle/reveal/KeepOpen/Escape passed' : 'not applicable', pageErrors: errors });
    } catch (error) {
      await page.screenshot({ path: resolve(values.output, mobile ? 'touch-failure.png' : 'pointer-failure.png'), animations: 'disabled' });
      throw error;
    } finally { await context.close(); }
  }
  const report = { results, boundary: 'No real backend, xterm transport, physical mobile IME or fnOS host was exercised.' };
  await writeFile(resolve(values.output, 'consumer-panel.json'), JSON.stringify(report, null, 2) + '\n');
  console.log(JSON.stringify(report));
});

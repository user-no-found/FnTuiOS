import { activateLauncher } from './panel_launcher.mjs';
import assert from 'node:assert/strict';

// Input ownership is observable in Chrome; physical Android IME still needs a device.
export async function verifyPhoneKeyboard(page, frame) {
  const client = () => frame.evaluate(() => ({
    open: window.FnMobileKeyboard.isOpen(),
    focused: document.activeElement === window.sipTerm.webterm.xterm.textarea,
    mode: window.sipTerm.webterm.xterm.textarea.inputMode,
  }));
  const button = name => frame.getByRole('button', { name, exact: true });
  const closed = async () => {
    const state = await client();
    assert.equal(state.open, false); assert.equal(state.focused, false); assert.equal(state.mode, 'none');
  };
  await closed();
  assert.equal(await frame.locator('#sip-keybar button[title="显示或隐藏屏幕键盘"]').count(), 0);
  assert.equal(await frame.locator('[data-fnpackkit-launcher]').count(), 0);
  await frame.evaluate(() => {
    const c = window.sipTerm;
    c.webterm.focus(); c.mobile.focusInput(); c.mobile.keepFocus(); c.mobile.toggleKeyboard();
  });
  await closed();
  await frame.locator('#terminal .xterm-screen').tap({ position: { x: 20, y: 20 } });
  await closed();
  const fold = frame.locator('#sip-keybar-pin button').first();
  await fold.tap(); await closed(); await fold.tap(); await closed();
  await activateLauncher(frame); await frame.getByRole('dialog').waitFor();
  await frame.evaluate(() => {
    const root = document.querySelector('[data-fnpackkit-runtime]').shadowRoot;
    const a = root.querySelector('[data-action="photo"]').getBoundingClientRect();
    const b = root.querySelector('[data-action="keyboard"]').getBoundingClientRect();
    if (Math.abs(a.width - b.width) > 1 || a.y !== b.y || a.right >= b.left) throw new Error('phone tools are not equal columns');
  });
  await button('键盘').tap();
  assert.deepEqual(await client(), { open: true, focused: true, mode: 'text' });
  assert.equal(await frame.getByRole('dialog').isVisible(), false);
  await activateLauncher(frame);
  assert.equal((await client()).open, true, 'opening the panel dismissed the keyboard');
  assert.equal((await client()).focused, true);
  assert.equal(await button('键盘').getAttribute('aria-pressed'), 'true');
  await button('键盘').tap(); await closed();
  await activateLauncher(frame); await button('键盘').tap();
  await frame.evaluate(() => {
    const viewport = window.visualViewport;
    window.__keyboardViewportHeight = viewport.height;
    Object.defineProperty(viewport, 'height', { configurable: true, value: viewport.height - 260 });
    viewport.dispatchEvent(new Event('resize'));
  });
  assert.equal((await client()).open, true);
  await frame.evaluate(() => {
    window.sipTerm.mobile.rescueUntil = performance.now() + 1000;
    Object.defineProperty(visualViewport, 'height', { configurable: true, value: window.__keyboardViewportHeight });
    visualViewport.dispatchEvent(new Event('resize'));
    delete visualViewport.height; delete window.__keyboardViewportHeight;
  });
  await frame.waitForFunction(() => !window.FnMobileKeyboard.isOpen());
  await closed();
  await frame.evaluate(() => window.sipTerm.mobile.focusInput()); await closed();
  await openPhoneKeyboard(frame);
  await frame.evaluate(() => {
    window.__fnStableHeight = visualViewport.height;
    Object.defineProperty(visualViewport, 'height', { configurable: true, value: visualViewport.height - 260 });
    visualViewport.dispatchEvent(new Event('resize'));
    Object.defineProperty(visualViewport, 'height', { configurable: true, value: window.__fnStableHeight });
    visualViewport.dispatchEvent(new Event('resize'));
  });
  await page.waitForTimeout(100);
  assert.equal((await client()).open, true, 'transient IME geometry revoked keyboard authorization');
  await frame.evaluate(() => {
    Object.defineProperty(visualViewport, 'height', { configurable: true, value: window.__fnStableHeight - 260 });
    visualViewport.dispatchEvent(new Event('resize'));
  });
  await page.waitForTimeout(350);
  assert.equal((await client()).open, true);
  await frame.evaluate(() => {
    delete visualViewport.height; delete window.__fnStableHeight;
    visualViewport.dispatchEvent(new Event('resize'));
  });
  await frame.waitForFunction(() => !window.FnMobileKeyboard.isOpen());
  await closed();
  return { initialClosed: true, soleEntry: true, equalColumns: true, explicitToggle: true,
    panelPreservesFocus: true, transientGeometry: true, systemDismissal: 'simulated geometry', autoRefocusBlocked: true };
}

export async function openPhoneKeyboard(frame) {
  if (await frame.evaluate(() => window.FnMobileKeyboard.isOpen())) return;
  if (!await frame.getByRole('dialog').isVisible()) await activateLauncher(frame);
  await frame.getByRole('button', { name: '键盘', exact: true }).tap();
}

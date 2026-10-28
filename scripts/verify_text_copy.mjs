import { activateLauncher } from './panel_launcher.mjs';
import assert from 'node:assert/strict';
import { resolve } from 'node:path';

// Real pane capture and browser clipboard round-trips share the normal authenticated transport.
export async function verifyTextCopy(page, frame, entry, output, mobile) {
  const action = async (name, pane = '') => {
    const url = new URL('/__fixture_image_action', entry);
    url.searchParams.set('action', name); if (pane) url.searchParams.set('pane', pane);
    const response = await fetch(url, { method: 'POST' }); assert(response.ok); return response.json();
  };
  const panes = async () => (await fetch(new URL('/__fixture_image_panes', entry))).json();
  const before = await panes();
  await action('copy');
  let pane;
  for (let i = 0; i < 100; i++) {
    const current = await panes();
    pane = current.windows.find(w => !before.windows.some(old => old.window_id === w.window_id));
    if (pane && pane.window_id === current.focused_window_id) break;
    await page.waitForTimeout(100);
  }
  assert(pane, 'copy fixture pane absent');
  const dialog = frame.locator('#fn-text-copy'), text = dialog.locator('pre');
  const button = name => frame.getByRole('button', { name, exact: true });
  try {
    await frame.waitForFunction(() => window.sipTerm?.connected);
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write'], { origin: entry.origin });
    // Wait for model output rather than copying a pane that is still spawning.
    await frame.waitForFunction(() => {
      const b = window.sipTerm.term.buffer.active;
      return Array.from({ length: b.length }, (_, i) => b.getLine(i)?.translateToString() || '').join('\n').includes('COPY_SCREEN_中文');
    });
    await activateLauncher(frame);
    await button('复制文本').click();
    await text.waitFor();
    await frame.waitForFunction(() => document.querySelector('#fn-text-copy pre').textContent.includes('COPY_SCREEN_中文'));
    const screen = await text.textContent();
    assert(!screen.includes('COPY_HISTORY_START')); assert(!screen.includes('\x1b'));
    assert(screen.replaceAll('\n', '').includes('<tag>&')); assert.equal(await text.locator('tag').count(), 0);
    assert.equal(await text.evaluate(node => getComputedStyle(node).userSelect), 'text');
    if (mobile) assert.equal(await frame.evaluate(() => window.FnMobileKeyboard.isOpen()), false);
    assert.equal(await frame.evaluate(() => document.activeElement === window.sipTerm.term.textarea), false);
    await button('复制全部').click();
    await frame.waitForFunction(() => !document.querySelector('#fn-text-copy [role="status"]').textContent.includes('正在复制'));
    assert.equal(await page.evaluate(() => navigator.clipboard.readText()), screen);
    await text.evaluate(node => {
      const start = node.textContent.indexOf('COPY_SCREEN_中文');
      const range = document.createRange(); range.setStart(node.firstChild, start); range.setEnd(node.firstChild, start + 'COPY_SCREEN_中文'.length);
      getSelection().removeAllRanges(); getSelection().addRange(range);
    });
    await button('复制所选').click();
    await frame.waitForFunction(() => !document.querySelector('#fn-text-copy [role="status"]').textContent.includes('正在复制'));
    assert.equal(await page.evaluate(() => navigator.clipboard.readText()), 'COPY_SCREEN_中文');
    await button('历史输出').click();
    await frame.waitForFunction(() => document.querySelector('#fn-text-copy pre').textContent.includes('COPY_HISTORY_START'));
    const history = await text.textContent();
    assert(history.includes('COPY_LINE_000')); assert(history.includes('COPY_LINE_099'));
    assert(!history.includes('FN_IMAGE_READY')); assert(history.length > screen.length);
    // Exercise the same upstream fallback used by non-secure origins without changing terminal code.
    await frame.evaluate(() => Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined }));
    await button('复制全部').click();
    await frame.waitForFunction(() => !document.querySelector('#fn-text-copy [role="status"]').textContent.includes('正在复制'));
    assert.equal(await page.evaluate(() => navigator.clipboard.readText()), history);
    await frame.evaluate(() => { delete navigator.clipboard; window.sipTerm.readOnly = true; window.FnPanelControls.sync(window.sipTerm); });
    await button('重新读取').click();
    await frame.waitForFunction(() => document.querySelector('#fn-text-copy pre').textContent.includes('COPY_HISTORY_START'));
    assert.equal(await button('复制全部').isEnabled(), true);
    await page.screenshot({ path: resolve(output, mobile ? 'mobile-text-copy.png' : 'desktop-text-copy.png'), animations: 'disabled' });
    await frame.evaluate(() => { window.sipTerm.readOnly = false; window.sipTerm.connected = false; window.FnPanelControls.sync(window.sipTerm); });
    assert.equal(await text.textContent(), ''); assert.equal(await button('复制全部').isEnabled(), false);
    await frame.evaluate(() => { window.sipTerm.connected = true; window.FnPanelControls.sync(window.sipTerm); });
    await dialog.getByRole('button', { name: '关闭', exact: true }).click();
    assert.equal(await dialog.isVisible(), false);
    if (mobile) assert.equal(await frame.evaluate(() => window.FnMobileKeyboard.isOpen()), false);
    return { currentPane: true, screenAndHistory: true, nativeSelection: true, copySelected: true,
      copyAll: true, clipboardFallback: true, readOnly: true, disconnectClears: true, keyboardClosed: true };
  } catch (error) {
    await page.screenshot({ path: resolve(output, mobile ? 'mobile-copy-failure.png' : 'desktop-copy-failure.png'), animations: 'disabled' });
    error.message += '\nCopy viewer: ' + await dialog.textContent();
    throw error;
  } finally {
    await frame.evaluate(() => {
      delete navigator.clipboard;
      window.sipTerm.readOnly = false; window.sipTerm.connected = true; window.FnPanelControls.sync(window.sipTerm);
      document.querySelector('#fn-text-copy')?.close();
    });
    await action('close', pane.window_id);
    if (before.focused_window_id) await action('focus', before.focused_window_id);
  }
}

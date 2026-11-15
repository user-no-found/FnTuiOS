import assert from 'node:assert/strict';
import { activateLauncher, clickLauncher } from './panel_launcher.mjs';

export async function verifyPanelDrag(page, frame, mobile) {
  const button = frame.getByRole('button', { name: '后端控制', exact: true });
  const bounds = () => frame.evaluate(() => ({ width: innerWidth,
    height: Math.min(innerHeight, document.querySelector('#sip-keybar')?.getBoundingClientRect().top || innerHeight) }));
  const geometry = () => button.evaluate(node => {
    const r = node.getBoundingClientRect(), edge = node.dataset.edge;
    const x = r.x + r.width * (edge === 'left' ? .25 : edge === 'right' ? .75 : .5);
    const y = r.y + r.height * (edge === 'top' ? .25 : edge === 'bottom' ? .75 : .5);
    return { x: r.x, y: r.y, width: r.width, height: r.height, edge,
      state: node.dataset.uiState, clip: getComputedStyle(node).clipPath,
      hiddenSideHitsLauncher: document.elementFromPoint(x, y) === node.getRootNode().host };
  });
  const center = async () => {
    const r = await geometry(), size = await bounds();
    return { x: (Math.max(0, r.x) + Math.min(size.width, r.x + r.width)) / 2,
      y: (Math.max(0, r.y) + Math.min(size.height, r.y + r.height)) / 2 };
  };
  await button.waitFor();
  await button.evaluate(node => node.blur());
  await page.waitForTimeout(5300);
  assert.equal((await geometry()).state, 'half', 'idle launcher did not collapse');
  const cdp = mobile ? await page.context().newCDPSession(page) : null;
  const size = await bounds();
  for (const [edge, x, y] of [['left', 2, 180], ['right', size.width - 2, 180],
    ['top', size.width / 2, 2], ['bottom', size.width / 2, size.height - 2]]) {
    const start = await center();
    if (mobile) {
      const points = (px, py) => [{ x: px, y: py, id: 1, radiusX: 2, radiusY: 2, force: 1 }];
      await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: points(start.x, start.y) });
      for (let i = 1; i <= 8; i++) await cdp.send('Input.dispatchTouchEvent', {
        type: 'touchMove', touchPoints: points(start.x + (x - start.x) * i / 8, start.y + (y - start.y) * i / 8) });
      assert.equal(await button.getAttribute('data-dragging'), 'true');
      await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
    } else {
      await page.mouse.move(start.x, start.y); await page.mouse.down();
      await page.mouse.move(x, y, { steps: 8 }); await page.mouse.up();
    }
    assert.equal(await button.getAttribute('aria-expanded'), 'false', 'drag opened the panel');
    assert.equal((await geometry()).edge, edge);
  }
  await page.waitForTimeout(5300);
  const final = await geometry();
  assert.equal(final.state, 'half'); assert.notEqual(final.clip, 'none');
  assert.equal(final.hiddenSideHitsLauncher, false, 'half-hidden outside pixels covered the key bar');
  await clickLauncher(frame);
  assert.equal((await geometry()).state, 'revealed', 'first half click did not reveal');
  assert.equal(await frame.getByRole('dialog').isVisible(), false, 'first half click opened the panel');
  await page.waitForTimeout(3300);
  assert.equal((await geometry()).state, 'half', 'revealed launcher did not half hide after 3 seconds');
  await activateLauncher(frame);
  await frame.getByRole('dialog').waitFor();
  await page.waitForTimeout(3500);
  await frame.getByRole('button', { name: '刷新状态', exact: true }).click();
  await page.waitForTimeout(2000);
  assert.equal(await button.getAttribute('aria-expanded'), 'true', 'interaction did not reset panel idle');
  await page.waitForTimeout(3300);
  assert.equal(await button.getAttribute('aria-expanded'), 'false', 'idle panel did not close');
  assert.equal((await geometry()).state, 'half', 'idle panel did not half hide');
  await activateLauncher(frame);
  await frame.getByRole('dialog').waitFor();
  await frame.getByRole('button', { name: '关闭控制面板', exact: true }).click();
  await page.waitForTimeout(50);
  assert.equal((await geometry()).state, 'half', 'close did not immediately half hide');
  if (cdp) await cdp.detach();
  await frame.goto(frame.url(), { waitUntil: 'domcontentloaded' });
  await frame.waitForFunction(() => window.sipTerm?.connected);
  await button.waitFor();
  assert.equal((await geometry()).edge, 'bottom', 'reload lost the saved edge');
  return { nativeDrag: 'four edges', idle: 'normal/open/drag 5 seconds, half reveal 3 seconds, close immediate',
    interactionResetsIdle: true, clippedAtKeybar: true, firstTapReveals: true, secondTapOpens: true, savedPosition: true };
}

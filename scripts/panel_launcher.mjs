import assert from 'node:assert/strict';

// Open through the shared half → revealed → open interaction using real pointer input.
export async function activateLauncher(frame) {
  const button = frame.getByRole('button', { name: '后端控制', exact: true });
  if (await button.getAttribute('aria-expanded') === 'true') return;
  await clickLauncher(frame);
  if (await button.getAttribute('data-ui-state') === 'revealed') await clickLauncher(frame);
}

export async function activateLauncherByKeyboard(frame) {
  const button = frame.getByRole('button', { name: '后端控制', exact: true });
  assert.equal(await button.getAttribute('data-ui-state'), 'half');
  const observed = ['half'];
  await button.focus();
  for (const expected of ['revealed', 'open']) {
    await frame.page().keyboard.press('Enter');
    const actual = await button.getAttribute('data-ui-state');
    assert.equal(actual, expected);
    assert.equal(await button.getAttribute('aria-expanded'), String(expected === 'open'));
    observed.push(actual);
  }
  return observed;
}

export async function clickLauncher(frame) {
  const button = frame.getByRole('button', { name: '后端控制', exact: true });
  await button.evaluate(async node => {
    await new Promise(done => requestAnimationFrame(() => requestAnimationFrame(done)));
    await Promise.all(node.getAnimations().map(animation => animation.finished.catch(() => {})));
  });
  const target = await button.evaluate(node => {
    const r = node.getBoundingClientRect(), v = window.visualViewport;
    const left = v?.offsetLeft || 0, top = v?.offsetTop || 0;
    let bottom = top + (v?.height || innerHeight);
    const bar = document.querySelector('#sip-keybar')?.getBoundingClientRect();
    if (bar?.height && bar.bottom >= bottom - 2) bottom = Math.min(bottom, bar.top);
    const x = (Math.max(left, r.left) + Math.min(left + (v?.width || innerWidth), r.right)) / 2;
    const y = (Math.max(top, r.top) + Math.min(bottom, r.bottom)) / 2;
    return { position: { x: x - r.left, y: y - r.top }, touch: matchMedia('(pointer:coarse)').matches };
  });
  if (target.touch) await button.tap({ position: target.position });
  else await button.click({ position: target.position });
}

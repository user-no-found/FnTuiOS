// Click the visible half of a docked launcher, avoiding clipped pixels over the key bar.
export async function activateLauncher(frame) {
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

// Button activity controls docking; runtime state and commands remain in the shared panel.
export function installIdle(button, panel, drag, close) {
  let timer;
  let settling = false, dragging = false, opened = !panel.hidden, hidden = button.hidden, delay = 5000;
  const shadow = button.getRootNode();
  shadow.addEventListener('pointerdown', event => {
    if (window.FnMobileKeyboard?.isOpen() && event.target.closest?.('button')) event.preventDefault();
  }, true);
  function clear() { clearTimeout(timer); }
  function wake() {
    if (settling) return;
    clear();
    button.dataset.idle = 'false';
    if (!document.hidden && button.dataset.dragging !== 'true') timer = setTimeout(collapse, delay);
  }
  function collapse() {
    if (!button.isConnected || button.hidden || button.dataset.dragging === 'true') return;
    settling = true;
    clear();
    if (!panel.hidden) close();
    if (shadow.activeElement === button) button.blur();
    drag.dock();
    button.dataset.idle = 'true';
    queueMicrotask(() => { settling = false; });
  }
  for (const event of ['pointermove', 'pointerdown', 'pointerup', 'pointercancel', 'focusin', 'click', 'keydown', 'input', 'wheel']) {
    shadow.addEventListener(event, () => {
      if (event === 'click' && panel.hidden) collapse();
      else wake();
    });
  }
  const observer = new MutationObserver(() => {
    const wasOpen = opened, wasDragging = dragging, wasHidden = hidden;
    opened = !panel.hidden; dragging = button.dataset.dragging === 'true';
    hidden = button.hidden;
    if (settling || wasOpen === opened && wasDragging === dragging && wasHidden === hidden) return;
    if (wasOpen && !opened) { collapse(); return; }
    if (opened) delay = 5000;
    else if (wasDragging && !dragging) delay = 3000;
    wake();
  });
  observer.observe(button, { attributes: true, attributeFilter: ['hidden', 'data-open', 'data-dragging'] });
  observer.observe(panel, { attributes: true, attributeFilter: ['hidden'] });
  document.addEventListener('visibilitychange', () => { if (document.hidden) clear(); else wake(); });
  addEventListener('pagehide', clear);
  addEventListener('pageshow', wake);
  wake();
}

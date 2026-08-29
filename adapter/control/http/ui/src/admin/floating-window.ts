import { enableWindowMove, enableWindowResize, raiseFloatingWindow } from 'classicstack-web/ui/window-resize';

/** Shared chrome + drag/resize for admin tool windows (log, monitor, leases, info). */
export function mountFloatingWindow(
  el: HTMLElement,
  opts: { chromeClass: string; minWidth?: number; minHeight?: number },
): void {
  enableWindowResize(el, { minWidth: opts.minWidth ?? 320, minHeight: opts.minHeight ?? 160 });
  enableWindowMove(el, `.${opts.chromeClass}`);
  el.addEventListener('pointerdown', () => raiseFloatingWindow(el), { capture: true });
}

export function raise(el: HTMLElement): void {
  raiseFloatingWindow(el);
}

export function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]!);
}

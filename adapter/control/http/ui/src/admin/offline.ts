/** Freeze Finder and most chrome when the ClassicStack HTTP/SSE session drops. */

import { telemetry, type LiveConn } from '../telemetry';

const SHOW_DELAY_MS = 700;

export type OfflineDismissible = HTMLElement & { hide?: () => void; close?: () => void };

export type OfflineGate = {
  overlay: HTMLElement;
  isOffline: () => boolean;
};

function hideWindow(el: OfflineDismissible): void {
  if (typeof el.hide === 'function') el.hide();
  else if (typeof el.close === 'function') el.close();
  else el.hidden = true;
}

/**
 * Overlay + `cs-offline` class while ClassicStack is unreachable.
 * About, Notifications, and the header status control stay usable.
 */
export function mountOfflineGate(opts: {
  app: HTMLElement;
  finder: HTMLElement;
  overlayHost: HTMLElement;
  dismiss: OfflineDismissible[];
  onChange?: (offline: boolean) => void;
}): OfflineGate {
  const overlay = document.createElement('div');
  overlay.className = 'offline-overlay';
  overlay.hidden = true;
  overlay.setAttribute('role', 'status');
  overlay.setAttribute('aria-live', 'polite');
  overlay.innerHTML = `
    <div class="offline-overlay__card">
      <span class="badge bad">offline</span>
      <h2>ClassicStack is offline</h2>
      <p>The server isn’t responding. Finder and most menus will return when it reconnects.</p>
    </div>
  `;
  opts.overlayHost.append(overlay);

  let shown = false;
  let showTimer: ReturnType<typeof setTimeout> | null = null;

  const apply = (offline: boolean): void => {
    if (shown === offline) return;
    shown = offline;
    opts.app.classList.toggle('cs-offline', offline);
    overlay.hidden = !offline;
    if ('inert' in opts.finder) opts.finder.inert = offline;
    opts.finder.setAttribute('aria-hidden', offline ? 'true' : 'false');
    if (offline) opts.dismiss.forEach(hideWindow);
    opts.onChange?.(offline);
  };

  const onConn = (s: LiveConn): void => {
    if (s === 'offline') {
      if (shown || showTimer) return;
      showTimer = setTimeout(() => {
        showTimer = null;
        apply(true);
      }, SHOW_DELAY_MS);
      return;
    }
    if (showTimer) {
      clearTimeout(showTimer);
      showTimer = null;
    }
    apply(false);
  };

  telemetry.onConn.add(onConn);
  if (telemetry.conn === 'offline') onConn('offline');

  return {
    overlay,
    isOffline: () => shown,
  };
}

/** ClassicStack and Advanced application menus. */

import { bindMenuBarTracking, MENUBAR_CHANGE, menubarOpenKey, setMenubarOpen } from 'classicstack-web/ui/menu-bar-track';
import type { SettingsSection } from './server-settings-window';
import { api } from '../api';
import { isServerOffline, telemetry } from '../telemetry';

export interface AppMenuHost {
  settings: { open: (section?: SettingsSection) => void };
  about: { open: () => void };
  log: { toggle: () => void; hidden: boolean };
  sharing: { toggle: () => void; hidden: boolean };
  leases: { toggle: () => void; hidden: boolean };
  notify: { toggle: () => void; hidden: boolean };
  topology: { toggle: () => void; hidden: boolean };
  openByPath: () => void;
}

/** macOS-style ClassicStack + Advanced menus in the admin header. */
export function mountAppMenu(header: HTMLElement, host: AppMenuHost): void {
  const wrap = document.createElement('div');
  wrap.className = 'app-menubar__menus app-brand-menus';
  const h1 = header.querySelector('h1');
  if (h1) h1.replaceWith(wrap);
  else header.insertBefore(wrap, header.firstChild);

  const brand = document.createElement('div');
  brand.dataset.menu = 'app';
  const advanced = document.createElement('div');
  advanced.className = 'app-advanced-menu';
  advanced.dataset.menu = 'advanced';
  wrap.append(brand, advanced);

  const paint = (): void => {
    const open = menubarOpenKey(wrap);
    const appOpen = open === 'app';
    const advancedOpen = open === 'advanced';
    const logOpen = !host.log.hidden;
    const sharingOpen = !host.sharing.hidden;
    const leasesOpen = !host.leases.hidden;
    const notifyOpen = !host.notify.hidden;
    const topologyOpen = !host.topology.hidden;
    const offline = isServerOffline();
    const off = offline ? ' disabled' : '';
    brand.className = `app-menu${appOpen ? ' open' : ''} app-brand-menu`;
    brand.dataset.menu = 'app';
    brand.innerHTML = `
      <button type="button" class="app-menu__trigger app-menubar__brand" data-act="toggle-app" aria-haspopup="true" aria-expanded="${appOpen}">
        ClassicStack
      </button>
      <div class="app-menu__dropdown" role="menu" ${appOpen ? '' : 'hidden'}>
        <button type="button" role="menuitem" data-act="about" class="app-menu__item">About ClassicStack…</button>
        <hr />
        <button type="button" role="menuitem" data-act="settings" class="app-menu__item"${off}>Settings…</button>
        <hr />
        <button type="button" role="menuitem" data-act="restart-stack" class="app-menu__item"${off}>Restart ClassicStack…</button>
        <button type="button" role="menuitem" data-act="shutdown" class="app-menu__item"${off}>Shut Down ClassicStack…</button>
      </div>
    `;
    advanced.className = `app-menu${advancedOpen ? ' open' : ''} app-advanced-menu`;
    advanced.dataset.menu = 'advanced';
    advanced.innerHTML = `
      <button type="button" class="app-menu__trigger" data-act="toggle-advanced" aria-haspopup="true" aria-expanded="${advancedOpen}">
        Advanced
      </button>
      <div class="app-menu__dropdown" role="menu" ${advancedOpen ? '' : 'hidden'}>
        <button type="button" role="menuitem" data-act="open-by-path" class="app-menu__item"${off}>
          <span class="app-menu__check"></span>Open by Path…
        </button>
        <hr />
        <button type="button" role="menuitemcheckbox" aria-checked="${logOpen}" data-act="show-log" class="app-menu__item"${off}>
          <span class="app-menu__check">${logOpen ? '✓' : ''}</span>Show Log
        </button>
        <button type="button" role="menuitemcheckbox" aria-checked="${sharingOpen}" data-act="sharing" class="app-menu__item"${off}>
          <span class="app-menu__check">${sharingOpen ? '✓' : ''}</span>Sharing Monitor
        </button>
        <button type="button" role="menuitemcheckbox" aria-checked="${leasesOpen}" data-act="leases" class="app-menu__item"${off}>
          <span class="app-menu__check">${leasesOpen ? '✓' : ''}</span>MacIP Leases
        </button>
        <button type="button" role="menuitemcheckbox" aria-checked="${topologyOpen}" data-act="topology" class="app-menu__item"${off}>
          <span class="app-menu__check">${topologyOpen ? '✓' : ''}</span>Topology
        </button>
        <hr />
        <button type="button" role="menuitemcheckbox" aria-checked="${notifyOpen}" data-act="notify" class="app-menu__item">
          <span class="app-menu__check">${notifyOpen ? '✓' : ''}</span>Notifications
        </button>
      </div>
    `;
  };

  wrap.addEventListener(MENUBAR_CHANGE, paint);
  telemetry.onConn.add(() => paint());
  bindMenuBarTracking(wrap);

  wrap.addEventListener('click', (e) => {
    const el = e.target instanceof Element ? e.target : e.target instanceof Node ? e.target.parentElement : null;
    if (el?.closest('.finder-view-menu, .finder-file-menu')) return;
    const t = el?.closest<HTMLElement>('[data-act]');
    if (!t) return;
    const act = t.dataset.act;
    if (t instanceof HTMLButtonElement && t.disabled) return;
    if (act === 'toggle-app' || act === 'toggle-file' || act === 'toggle-view' || act === 'toggle-advanced') return;
    e.stopPropagation();
    setMenubarOpen(wrap, null);
    if (act === 'about') host.about.open();
    if (act === 'settings') host.settings.open('general');
    if (act === 'open-by-path') host.openByPath();
    if (act === 'show-log') host.log.toggle();
    if (act === 'sharing') host.sharing.toggle();
    if (act === 'leases') host.leases.toggle();
    if (act === 'topology') host.topology.toggle();
    if (act === 'notify') host.notify.toggle();
    if (act === 'restart-stack') void requestStackRestart();
    if (act === 'shutdown') void requestShutdown();
    paint();
  });

  paint();
}

async function requestShutdown(): Promise<void> {
  if (
    !confirm(
      'Shut down ClassicStack?\n\nRunning services will stop and connected clients will be disconnected.',
    )
  ) {
    return;
  }
  try {
    await api.shutdown();
  } catch (e) {
    alert(e instanceof Error ? e.message : String(e));
  }
}

async function requestStackRestart(): Promise<void> {
  if (
    !confirm(
      'Restart ClassicStack?\n\nThe server will shut down gracefully and start again with the same configuration.',
    )
  ) {
    return;
  }
  try {
    await api.stackRestart();
  } catch (e) {
    alert(e instanceof Error ? e.message : String(e));
  }
}

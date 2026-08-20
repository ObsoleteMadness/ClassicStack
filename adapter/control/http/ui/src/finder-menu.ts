/** Finder File and View menus shared with the PWA (ClassicStack-web). */

import type { FinderWindow } from 'classicstack-web/ui/finder-window';
import type { ExtensionEditorDialog } from 'classicstack-web/ui/extension-editor-dialog';
import {
  FILE_MENU_KEY,
  applyFileMenuAction,
  fileMenuInnerHTML,
  isFileMenuToggle,
} from 'classicstack-web/ui/finder-file-menu';
import {
  VIEW_MENU_KEY,
  applyViewMenuAction,
  isViewMenuToggle,
  viewMenuInnerHTML,
} from 'classicstack-web/ui/finder-view-menu';
import { MENUBAR_CHANGE, menubarOpenKey, setMenubarOpen } from 'classicstack-web/ui/menu-bar-track';

type FinderMenuHost = { finder: FinderWindow; extensionEditor?: ExtensionEditorDialog };

function mountFinderMenuItem(
  header: HTMLElement,
  key: string,
  className: string,
  inner: (host: FinderMenuHost, open: boolean) => string,
  isToggle: (act: string | undefined) => boolean,
  apply: (act: string | undefined, host: FinderMenuHost) => Promise<boolean>,
  host: FinderMenuHost,
  before?: Element | null,
): void {
  const menus = header.querySelector('.app-brand-menus') as HTMLElement | null;
  const wrap = document.createElement('div');
  wrap.className = `app-menu ${className}`;
  wrap.dataset.menu = key;
  if (before) menus?.insertBefore(wrap, before);
  else if (menus) menus.append(wrap);
  else header.insertBefore(wrap, header.querySelector('#conn'));

  const paint = (): void => {
    const open = (menus ? menubarOpenKey(menus) : null) === key;
    wrap.classList.toggle('open', open);
    wrap.innerHTML = inner(host, open);
  };

  const dismiss = (): void => {
    if (menus) setMenubarOpen(menus, null);
    else {
      wrap.classList.remove('open');
      paint();
    }
  };

  wrap.addEventListener('click', (e) => {
    const el = e.target instanceof Element ? e.target : e.target instanceof Node ? e.target.parentElement : null;
    const t = el?.closest('[data-act]') as HTMLElement | null;
    if (!t) return;
    const act = t.dataset.act;
    if (isToggle(act)) {
      e.stopPropagation();
      if (menus) {
        setMenubarOpen(menus, menubarOpenKey(menus) === key ? null : key);
      } else {
        wrap.classList.toggle('open');
        paint();
      }
      return;
    }
    e.stopPropagation();
    void apply(act, host).then((handled) => {
      if (handled) {
        dismiss();
        paint();
      }
    });
  });

  menus?.addEventListener(MENUBAR_CHANGE, paint);
  paint();
}

export function mountFinderMenu(
  header: HTMLElement,
  finder: FinderWindow,
  extensionEditor: ExtensionEditorDialog,
): void {
  const host = { finder, extensionEditor };
  const menus = header.querySelector('.app-brand-menus') as HTMLElement | null;
  const before = menus?.querySelector('.app-advanced-menu') ?? null;
  mountFinderMenuItem(header, FILE_MENU_KEY, 'finder-file-menu', fileMenuInnerHTML, isFileMenuToggle, applyFileMenuAction, host, before);
  mountFinderMenuItem(header, VIEW_MENU_KEY, 'finder-view-menu', viewMenuInnerHTML, isViewMenuToggle, applyViewMenuAction, host, before);
}

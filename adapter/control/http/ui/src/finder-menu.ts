/** Finder View menu: auto-expand, fork icons, zip layout, resource explorer. */

import type { FinderWindow } from 'classicstack-web/ui/finder-window';
import type { ExtensionEditorDialog } from 'classicstack-web/ui/extension-editor-dialog';
import { loadPrefs, savePrefs } from 'classicstack-web/util/prefs';
import { iconCache } from 'classicstack-web/fs/icon-cache';

export function mountFinderMenu(
  header: HTMLElement,
  finder: FinderWindow,
  extensionEditor: ExtensionEditorDialog,
): void {
  const wrap = document.createElement('div');
  wrap.className = 'app-menu finder-view-menu';
  header.insertBefore(wrap, header.querySelector('#conn'));

  const paint = (): void => {
    const open = wrap.classList.contains('open');
    const hidden = finder.getShowHiddenFiles();
    const autoExpand = finder.getAutoExpandFiles();
    const icons = finder.getReadFinderIcons();
    const zipStyle = loadPrefs().zipExportStyle;
    wrap.innerHTML = `
      <button type="button" class="app-menu__trigger" data-act="toggle" aria-haspopup="true" aria-expanded="${open}">
        View
      </button>
      <div class="app-menu__dropdown" role="menu" ${open ? '' : 'hidden'}>
        <button type="button" role="menuitemcheckbox" aria-checked="${hidden}" data-act="toggle-show-hidden" class="app-menu__item">
          <span class="app-menu__check">${hidden ? '✓' : ''}</span>
          Show hidden files
        </button>
        <button type="button" role="menuitemcheckbox" aria-checked="${autoExpand}" data-act="toggle-auto-expand" class="app-menu__item">
          <span class="app-menu__check">${autoExpand ? '✓' : ''}</span>
          Auto-expand files
        </button>
        <button type="button" role="menuitemcheckbox" aria-checked="${icons}" data-act="toggle-read-finder-icons" class="app-menu__item">
          <span class="app-menu__check">${icons ? '✓' : ''}</span>
          Read finder icons
        </button>
        <hr />
        <button type="button" role="menuitemradio" aria-checked="${zipStyle === 'appledouble'}" data-act="zip-appledouble" class="app-menu__item">
          <span class="app-menu__check">${zipStyle === 'appledouble' ? '✓' : ''}</span>
          AppleDouble zip
        </button>
        <button type="button" role="menuitemradio" aria-checked="${zipStyle === 'macosx'}" data-act="zip-macosx" class="app-menu__item">
          <span class="app-menu__check">${zipStyle === 'macosx' ? '✓' : ''}</span>
          Mac OS X zip
        </button>
        <hr />
        <button type="button" role="menuitem" data-act="resource-fork" class="app-menu__item">
          Resource Fork…
        </button>
        <button type="button" role="menuitem" data-act="extension-editor" class="app-menu__item">
          Extension editor…
        </button>
        <button type="button" role="menuitem" data-act="clear-icon-cache" class="app-menu__item">
          Clear icon cache
        </button>
      </div>
    `;
  };

  const close = (): void => {
    wrap.classList.remove('open');
    paint();
  };

  wrap.addEventListener('click', (e) => {
    const t = (e.target as HTMLElement).closest('[data-act]') as HTMLElement | null;
    if (!t) return;
    e.stopPropagation();
    const act = t.dataset.act;
    if (act === 'toggle') {
      wrap.classList.toggle('open');
      paint();
      return;
    }
    if (act === 'toggle-show-hidden') finder.setShowHiddenFiles(!finder.getShowHiddenFiles());
    else if (act === 'toggle-auto-expand') finder.setAutoExpandFiles(!finder.getAutoExpandFiles());
    else if (act === 'toggle-read-finder-icons') finder.setReadFinderIcons(!finder.getReadFinderIcons());
    else if (act === 'zip-appledouble') savePrefs({ zipExportStyle: 'appledouble' });
    else if (act === 'zip-macosx') savePrefs({ zipExportStyle: 'macosx' });
    else if (act === 'resource-fork') finder.openResourceExplorer();
    else if (act === 'extension-editor') extensionEditor.open();
    else if (act === 'clear-icon-cache') {
      void iconCache.clear().then(() => finder.invalidateIcons());
    }
    close();
  });

  window.addEventListener('click', (e) => {
    if (!wrap.contains(e.target as Node) && wrap.classList.contains('open')) close();
  });
  window.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && wrap.classList.contains('open')) close();
  });

  paint();
}

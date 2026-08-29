/** OS X Aqua-style tab view (`cs-tabs` / `cs-tab` / `cs-tabpanel`). */

let seq = 0;

export class CsTab extends HTMLElement {
  connectedCallback(): void {
    this.setAttribute('role', 'tab');
  }
}

export class CsTabPanel extends HTMLElement {
  connectedCallback(): void {
    this.setAttribute('role', 'tabpanel');
  }
}

export class CsTabs extends HTMLElement {
  private primed = false;

  connectedCallback(): void {
    this.classList.add('osx-tabs');
    this.prime();
    this.addEventListener('click', this.onClick);
    this.addEventListener('keydown', this.onKey);
  }

  disconnectedCallback(): void {
    this.removeEventListener('click', this.onClick);
    this.removeEventListener('keydown', this.onKey);
  }

  get value(): string {
    return this.querySelector('cs-tab[aria-selected="true"]')?.getAttribute('panel') || '';
  }

  set value(panel: string) {
    this.select(panel, false);
  }

  private prime(): void {
    if (this.primed) return;
    this.primed = true;
    let bar = this.querySelector(':scope > .osx-tabs__bar');
    if (!bar) {
      bar = document.createElement('div');
      bar.className = 'osx-tabs__bar';
      bar.setAttribute('role', 'tablist');
      this.prepend(bar);
    }
    for (const tab of [...this.querySelectorAll(':scope > cs-tab')]) bar.append(tab);
    const uid = `osx-tabs-${++seq}`;
    const tabs = [...bar.querySelectorAll<CsTab>('cs-tab')];
    const panels = [...this.querySelectorAll<CsTabPanel>(':scope > cs-tabpanel')];
    tabs.forEach((tab, i) => {
      const name = tab.getAttribute('panel') || String(i);
      tab.setAttribute('panel', name);
      const tabId = `${uid}-tab-${name}`;
      const panelId = `${uid}-panel-${name}`;
      tab.id = tabId;
      const panel = panels.find((p) => p.getAttribute('name') === name) ?? panels[i];
      if (!panel) return;
      panel.setAttribute('name', name);
      panel.id = panelId;
      tab.setAttribute('aria-controls', panelId);
      panel.setAttribute('aria-labelledby', tabId);
    });
    const initial =
      tabs.find((t) => t.hasAttribute('selected')) ||
      tabs.find((t) => t.getAttribute('aria-selected') === 'true') ||
      tabs[0];
    if (initial) this.select(initial.getAttribute('panel') || '', false);
  }

  private select(panel: string, notify: boolean): void {
    const prev = this.value;
    const tabs = [...this.querySelectorAll<CsTab>('.osx-tabs__bar > cs-tab')];
    const panels = [...this.querySelectorAll<CsTabPanel>(':scope > cs-tabpanel')];
    tabs.forEach((tab, i) => {
      const on = tab.getAttribute('panel') === panel;
      tab.setAttribute('aria-selected', on ? 'true' : 'false');
      tab.tabIndex = on ? 0 : -1;
      tab.toggleAttribute('selected', on);
      const p = panels.find((x) => x.getAttribute('name') === tab.getAttribute('panel')) ?? panels[i];
      if (p) p.toggleAttribute('hidden', !on);
    });
    this.classList.toggle('osx-tabs--first', tabs[0]?.getAttribute('panel') === panel);
    if (notify && panel && panel !== prev) {
      this.dispatchEvent(new CustomEvent('tabchange', { detail: { panel }, bubbles: true }));
    }
  }

  private onClick = (e: MouseEvent): void => {
    const tab = (e.target as HTMLElement).closest('cs-tab');
    if (!tab || !this.contains(tab)) return;
    const panel = tab.getAttribute('panel');
    if (panel) this.select(panel, true);
  };

  private onKey = (e: KeyboardEvent): void => {
    if (!(e.target instanceof HTMLElement) || e.target.localName !== 'cs-tab') return;
    const tabs = [...this.querySelectorAll<CsTab>('.osx-tabs__bar > cs-tab')];
    const i = tabs.indexOf(e.target as CsTab);
    if (i < 0) return;
    let next = i;
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') next = (i + 1) % tabs.length;
    else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') next = (i - 1 + tabs.length) % tabs.length;
    else if (e.key === 'Home') next = 0;
    else if (e.key === 'End') next = tabs.length - 1;
    else return;
    e.preventDefault();
    const panel = tabs[next].getAttribute('panel') || '';
    this.select(panel, true);
    tabs[next].focus();
  };
}

declare global {
  interface HTMLElementTagNameMap {
    'cs-tabs': CsTabs;
    'cs-tab': CsTab;
    'cs-tabpanel': CsTabPanel;
  }
  interface HTMLElementEventMap {
    tabchange: CustomEvent<{ panel: string }>;
  }
}

customElements.define('cs-tab', CsTab);
customElements.define('cs-tabpanel', CsTabPanel);
customElements.define('cs-tabs', CsTabs);

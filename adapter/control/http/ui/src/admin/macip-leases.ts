import { api, type MacIPLeaseInfo } from '../api';
import { escapeHtml, mountFloatingWindow, raise } from './floating-window';

function atalk(net: number, node: number): string {
  return `${net}.${node}`;
}

/** MacIP gateway lease table. */
export class MacIPLeasesWindow extends HTMLElement {
  private timer: ReturnType<typeof setInterval> | null = null;

  connectedCallback(): void {
    this.classList.add('activity-window', 'macip-leases');
    this.hidden = true;
    this.style.left = '80px';
    this.style.top = '96px';
    this.innerHTML = `
      <div class="activity-window__chrome">
        <div class="activity-window__title">MacIP Leases</div>
        <button type="button" class="btn log-panel__btn" data-act="close" aria-label="Close">✕</button>
      </div>
      <div class="activity-window__body macip-leases__body"></div>
    `;
    mountFloatingWindow(this, { chromeClass: 'activity-window__chrome', minWidth: 360, minHeight: 160 });
    this.addEventListener('click', (e) => {
      if ((e.target as HTMLElement).closest('[data-act="close"]')) this.hide();
    });
    window.addEventListener('keydown', this.onKey);
  }

  disconnectedCallback(): void {
    this.stop();
    window.removeEventListener('keydown', this.onKey);
  }

  show(): void {
    this.hidden = false;
    void this.refresh();
    this.start();
    raise(this);
  }

  hide(): void {
    this.hidden = true;
    this.stop();
  }

  toggle(): void {
    if (this.hidden) this.show();
    else this.hide();
  }

  private onKey = (e: KeyboardEvent): void => {
    if (e.key === 'Escape' && !this.hidden) this.hide();
  };

  private start(): void {
    this.stop();
    this.timer = setInterval(() => void this.refresh(), 3000);
  }

  private stop(): void {
    if (this.timer) clearInterval(this.timer);
    this.timer = null;
  }

  private async refresh(): Promise<void> {
    const body = this.querySelector('.macip-leases__body');
    if (!body) return;
    try {
      const leases: MacIPLeaseInfo[] = await api.macipLeases();
      if (!leases.length) {
        body.innerHTML = `<p class="muted">No active leases.</p>`;
        return;
      }
      const rows = leases
        .map(
          (l) =>
            `<tr><td>${escapeHtml(l.ip)}</td><td>${escapeHtml(atalk(l.at_network, l.at_node))}</td><td>${escapeHtml(l.source || '—')}</td></tr>`,
        )
        .join('');
      body.innerHTML = `<table class="monitor-table"><thead><tr><th>IPv4</th><th>AppleTalk</th><th>Source</th></tr></thead><tbody>${rows}</tbody></table>`;
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      body.innerHTML = `<p class="muted">${escapeHtml(msg.includes('unavailable') || msg.includes('501') ? 'MacIP gateway is not running.' : msg)}</p>`;
    }
  }
}

customElements.define('cs-macip-leases', MacIPLeasesWindow);

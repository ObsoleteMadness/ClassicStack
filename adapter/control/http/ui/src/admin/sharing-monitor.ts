import { api, type AFPSessionInfo, type EtherDFSSessionInfo, type NCPSessionInfo, type SMBSessionInfo } from '../api';
import { telemetry, type SessionChange } from '../telemetry';
import { formatBytes } from './dom';
import { escapeHtml, mountFloatingWindow, raise } from './floating-window';
import './tabs';

type Tab = 'AFP' | 'SMB' | 'NCP' | 'EtherDFS';

const TABS: Tab[] = ['AFP', 'SMB', 'NCP', 'EtherDFS'];

function guest(user: string, loggedIn?: boolean): string {
  if (user) return user;
  return loggedIn === false ? '(connecting)' : 'Guest';
}

function lastSeen(ns: number): string {
  if (!ns) return '—';
  const ms = ns > 1e12 ? ns / 1e6 : ns;
  return new Date(ms).toLocaleTimeString();
}

function num(v: unknown): number {
  return typeof v === 'number' && Number.isFinite(v) ? v : 0;
}

function componentStats(name: string): { bytesRx: number; bytesTx: number; rxRate: number; txRate: number } {
  const s = telemetry.stats[name] || {};
  const counters = (s.Counters || s.counters || {}) as Record<string, number>;
  const rates = (s.Rates || s.rates || {}) as Record<string, number>;
  return {
    bytesRx: num(counters.bytes_rx),
    bytesTx: num(counters.bytes_tx),
    rxRate: num(rates.bytes_rx),
    txRate: num(rates.bytes_tx),
  };
}

function formatCount(n: number): string {
  if (!n) return '0 B';
  return formatBytes(n);
}

function rateLabel(n: number): string {
  if (!n) return '—';
  return `${formatCount(n)}/s`;
}

/** Sharing Monitor: live sessions per file-sharing protocol. */
export class SharingMonitorWindow extends HTMLElement {
  private tab: Tab = 'AFP';
  private onSession: ((s: SessionChange) => void) | null = null;

  connectedCallback(): void {
    this.classList.add('activity-window', 'sharing-monitor');
    this.hidden = true;
    this.style.left = '48px';
    this.style.top = '72px';
    this.innerHTML = `
      <div class="activity-window__chrome">
        <div class="activity-window__title">Sharing Monitor</div>
        <button type="button" class="btn log-panel__btn" data-act="close" aria-label="Close">✕</button>
      </div>
      <div class="activity-window__body sharing-monitor__body">
        <cs-tabs>
          ${TABS.map((t) => `<cs-tab panel="${t}"${t === this.tab ? ' selected' : ''}>${t}</cs-tab>`).join('')}
          ${TABS.map(
            (t) =>
              `<cs-tabpanel name="${t}"><div class="sharing-monitor__summary muted"></div><div class="sharing-monitor__table"></div></cs-tabpanel>`,
          ).join('')}
        </cs-tabs>
      </div>
    `;
    mountFloatingWindow(this, { chromeClass: 'activity-window__chrome', minWidth: 420, minHeight: 220 });
    this.addEventListener('click', (e) => this.onClick(e));
    this.addEventListener('tabchange', (e) => {
      this.tab = e.detail.panel as Tab;
      void this.refresh();
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

  private onClick(e: MouseEvent): void {
    const t = (e.target as HTMLElement).closest('[data-act]') as HTMLElement | null;
    if (!t) return;
    if (t.dataset.act === 'close') this.hide();
  }

  private start(): void {
    this.stop();
    this.onSession = (s) => {
      if (s.Component === this.tab) void this.refresh();
    };
    telemetry.onSession.add(this.onSession);
  }

  private stop(): void {
    if (this.onSession) telemetry.onSession.delete(this.onSession);
    this.onSession = null;
  }

  private async refresh(): Promise<void> {
    const panel = this.querySelector(`cs-tabpanel[name="${this.tab}"]`);
    const summary = panel?.querySelector('.sharing-monitor__summary');
    const table = panel?.querySelector('.sharing-monitor__table');
    if (!summary || !table) return;
    try {
      if (this.tab === 'AFP') await this.paintAFP(summary, table);
      else if (this.tab === 'SMB') await this.paintSMB(summary, table);
      else if (this.tab === 'NCP') await this.paintNCP(summary, table);
      else await this.paintEtherDFS(summary, table);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      summary.textContent = msg.includes('unavailable') || msg.includes('501') ? 'Service not running.' : msg;
      table.innerHTML = '';
    }
  }

  private paintSummary(el: Element, name: string, users: number, files: number): void {
    const st = componentStats(name);
    el.innerHTML = `${users} connected · ${files} files open · ${formatCount(st.bytesRx)} in / ${formatCount(st.bytesTx)} out · ${rateLabel(st.rxRate)} ↓ ${rateLabel(st.txRate)} ↑`;
  }

  private async paintAFP(summary: Element, table: Element): Promise<void> {
    const rows: AFPSessionInfo[] = await api.afpSessions();
    const files = 0;
    this.paintSummary(summary, 'AFP', rows.length, files);
    table.innerHTML = this.table(
      ['Session', 'Address', 'User', 'Logged in', 'Last seen'],
      rows.map((s) => [
        String(s.id),
        `${s.network}.${s.node}`,
        guest(s.user, s.logged_in),
        s.logged_in ? 'yes' : 'no',
        lastSeen(s.last_seen),
      ]),
    );
  }

  private async paintSMB(summary: Element, table: Element): Promise<void> {
    const rows: SMBSessionInfo[] = await api.smbSessions();
    const files = rows.reduce((n, s) => n + (s.open_files || 0), 0);
    this.paintSummary(summary, 'SMB', rows.length, files);
    table.innerHTML = this.table(
      ['Client', 'User', 'Dialect', 'Trees', 'Files', 'OS'],
      rows.map((s) => [
        s.netbios_name || s.client || s.mac || '—',
        guest(s.user),
        s.dialect || '—',
        String(s.open_trees || 0),
        String(s.open_files || 0),
        [s.native_os, s.native_lanman].filter(Boolean).join(' / ') || '—',
      ]),
    );
  }

  private async paintNCP(summary: Element, table: Element): Promise<void> {
    const rows: NCPSessionInfo[] = await api.ncpSessions();
    const files = rows.reduce((n, s) => n + (s.open_files || 0), 0);
    this.paintSummary(summary, 'NCP', rows.length, files);
    table.innerHTML = this.table(
      ['Conn', 'Endpoint', 'User', 'Logged in', 'Files', 'Last seen'],
      rows.map((s) => [
        String(s.number),
        s.endpoint || '—',
        guest(s.user, s.logged_in),
        s.logged_in ? 'yes' : 'no',
        String(s.open_files || 0),
        lastSeen(s.last_seen),
      ]),
    );
  }

  private async paintEtherDFS(summary: Element, table: Element): Promise<void> {
    const rows: EtherDFSSessionInfo[] = await api.etherdfsSessions();
    const files = rows.reduce((n, s) => n + (s.open_files || 0), 0);
    this.paintSummary(summary, 'EtherDFS', rows.length, files);
    table.innerHTML = this.table(
      ['MAC', 'Files', 'Last seen'],
      rows.map((s) => [s.mac || '—', String(s.open_files || 0), lastSeen(s.last_seen)]),
    );
  }

  private table(headers: string[], rows: string[][]): string {
    if (!rows.length) return `<p class="muted">No connected users.</p>`;
    const head = headers.map((h) => `<th>${escapeHtml(h)}</th>`).join('');
    const body = rows
      .map((r) => `<tr>${r.map((c) => `<td>${escapeHtml(c)}</td>`).join('')}</tr>`)
      .join('');
    return `<table class="monitor-table"><thead><tr>${head}</tr></thead><tbody>${body}</tbody></table>`;
  }
}

customElements.define('cs-sharing-monitor', SharingMonitorWindow);

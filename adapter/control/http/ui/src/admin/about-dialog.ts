/** Server About box: build info from GET /host_info. */

import { api, type HostInfo } from '../api';

const REPO = 'https://github.com/ObsoleteMadness/ClassicStack';
const ICONS8 = 'https://icons8.com/';
const GPL = 'https://www.gnu.org/licenses/gpl-3.0.html';

function extLink(href: string, label: string): string {
  return `<a href="${href}" target="_blank" rel="noopener noreferrer">${label}</a>`;
}

function versionLine(info: HostInfo | null): string {
  const ver = info?.version?.trim() || 'dev';
  const sha = info?.gitSha?.trim() || '';
  if (!sha) return ver;
  return `${ver} (${extLink(`${REPO}/commit/${sha}`, sha.slice(0, 7))})`;
}

function formatBytes(n?: number): string {
  if (n == null || !Number.isFinite(n) || n <= 0) return '';
  const mb = n / (1024 * 1024);
  return `${mb >= 1024 ? (mb / 1024).toFixed(1) + ' GB' : mb.toFixed(0) + ' MB'}`;
}

/** Modal About box opened from the ClassicStack menu. */
export class ServerAboutDialog extends HTMLElement {
  private info: HostInfo | null = null;

  connectedCallback(): void {
    this.classList.add('about-dialog', 'server-about-dialog');
    this.hidden = true;
    this.addEventListener('click', (e) => this.onClick(e));
    this.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && !this.hidden) this.close();
    });
  }

  open(): void {
    this.hidden = false;
    void this.loadAndRender();
  }

  close(): void {
    this.hidden = true;
    this.innerHTML = '';
    this.info = null;
  }

  private async loadAndRender(): Promise<void> {
    this.render(null);
    this.info = await api.hostInfo().catch(() => null);
    if (!this.hidden) this.render(this.info);
    this.querySelector<HTMLButtonElement>('.btn.primary')?.focus();
  }

  private render(info: HostInfo | null): void {
    const mem =
      info?.totalMemory && info?.freeMemory
        ? `${formatBytes(info.freeMemory)} free of ${formatBytes(info.totalMemory)}`
        : '';
    const hostBits = [
      info?.osName,
      info?.architecture,
      info?.boardName,
      info?.hostIp ? `host ${info.hostIp}` : '',
      info?.goVersion ? `Go ${info.goVersion}` : '',
      mem,
    ].filter(Boolean);

    this.innerHTML = `
      <div class="netboot-dialog__backdrop" data-act="close"></div>
      <div class="netboot-dialog__card about-dialog__card" role="dialog" aria-labelledby="about-title" aria-modal="true">
        <header class="netboot-dialog__header">
          <h2 id="about-title">About ClassicStack</h2>
          <button type="button" class="btn" data-act="close" aria-label="Close">✕</button>
        </header>
        <div class="about-dialog__body">
          <p class="about-dialog__name">ClassicStack</p>
          <p class="about-dialog__ver">Version ${versionLine(info)}</p>
          <p>AppleTalk Phase 2 router and legacy file-server stack (AFP, SMB, NCP, EtherDFS).</p>
          ${hostBits.length ? `<p class="muted">${hostBits.join(' · ')}</p>` : ''}
          <p>${extLink(REPO, 'GitHub')}</p>
          <h3>License</h3>
          <p>
            ClassicStack is free software under the
            ${extLink(GPL, 'GNU General Public License v3.0')}.
          </p>
          <h3>Credits</h3>
          <ul>
            <li>Icons by ${extLink(ICONS8, 'Icons8')}</li>
          </ul>
        </div>
        <footer class="netboot-dialog__footer">
          <button type="button" class="btn primary" data-act="close">OK</button>
        </footer>
      </div>
    `;
  }

  private onClick(e: MouseEvent): void {
    const t = (e.target as HTMLElement).closest('[data-act]') as HTMLElement | null;
    if (t?.dataset.act === 'close') this.close();
  }
}

customElements.define('server-about-dialog', ServerAboutDialog);

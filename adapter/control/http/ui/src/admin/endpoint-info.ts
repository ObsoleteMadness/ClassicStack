import type { RemoteEndpoint } from 'classicstack-web/ui/finder-host';
import type { FinderSession } from '../api';
import { escapeHtml, mountFloatingWindow, raise } from './floating-window';

export type EndpointInfoKind = 'server' | 'share';

/** Discovery extras carried on Finder sidebar endpoints (not part of classicstack-web). */
export type EndpointLocation = {
  address?: string;
  uri?: string;
  os?: string;
  version?: string;
};

export type EndpointInfoModel = {
  kind: EndpointInfoKind;
  endpoint: RemoteEndpoint & EndpointLocation;
  volume?: string;
  session?: FinderSession | null;
  mountpoint?: string;
};

function looksLikeURI(s: string): boolean {
  return /^[a-z][a-z0-9+.-]*:\/\//i.test(s);
}

function trimSlash(s: string): string {
  return s.replace(/\/+$/, '');
}

function withVolume(base: string, volume?: string): string {
  if (!volume) return base;
  const t = trimSlash(base);
  const suffix = '/' + volume;
  if (t.toLowerCase().endsWith(suffix.toLowerCase())) return t;
  return t + suffix;
}

function infoAddress(m: EndpointInfoModel): string {
  return (m.endpoint.address || '').trim();
}

function infoOS(m: EndpointInfoModel): string {
  return (m.session?.os || m.endpoint.os || '').trim();
}

function infoSMBVersion(m: EndpointInfoModel): string {
  return (m.session?.dialect || m.endpoint.version || '').trim();
}

function infoDescription(m: EndpointInfoModel): string {
  const sub = (m.endpoint.subtitle || '').trim();
  if (!sub || sub === '*') return '';
  const addr = infoAddress(m);
  if (addr && (addr === sub || addr.endsWith(', ' + sub))) return '';
  return sub;
}

function infoURI(m: EndpointInfoModel): string {
  const ep = m.endpoint;
  const volume = m.kind === 'share' ? m.volume : undefined;
  const base =
    (ep.uri || '').trim() ||
    (looksLikeURI(m.session?.target || '') ? trimSlash(m.session!.target!) : '') ||
    (looksLikeURI(ep.id) ? trimSlash(ep.id) : '');
  if (!base) return '';
  return withVolume(base, volume);
}

function infoAuthLabel(kind: string): string {
  switch (kind) {
    case 'smb':
      return 'Capabilities';
    case 'ncp':
      return 'Login';
    case 'afp':
      return 'UAMs';
    default:
      return '';
  }
}

function row(label: string, value: string | undefined): string {
  const v = (value || '').trim();
  if (!v) return '';
  return `<div class="info-row"><dt>${escapeHtml(label)}</dt><dd>${escapeHtml(v)}</dd></div>`;
}

function uriRow(value: string): string {
  const v = value.trim();
  if (!v) return '';
  return `<div class="info-row info-row--uri"><dt>URI</dt><dd>
    <code class="info-uri" title="Click to copy">${escapeHtml(v)}</code>
    <button type="button" class="btn log-panel__btn" data-act="copy-uri">Copy</button>
  </dd></div>`;
}

async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.setAttribute('readonly', '');
    ta.style.position = 'fixed';
    ta.style.left = '-9999px';
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand('copy');
    ta.remove();
    return ok;
  }
}

/** Get Info for Finder servers and remote shares. */
export class EndpointInfoWindow extends HTMLElement {
  private model: EndpointInfoModel | null = null;
  private copyTimer: number | null = null;

  connectedCallback(): void {
    this.classList.add('get-info-window', 'endpoint-info');
    this.hidden = true;
    this.style.right = '32px';
    this.style.top = '88px';
    this.style.left = 'auto';
    this.innerHTML = `
      <div class="get-info-window__chrome">
        <div class="get-info-window__title">Get Info</div>
        <button type="button" class="btn log-panel__btn" data-act="close" aria-label="Close">✕</button>
      </div>
      <div class="get-info-window__body endpoint-info__body"></div>
    `;
    mountFloatingWindow(this, { chromeClass: 'get-info-window__chrome', minWidth: 280, minHeight: 160 });
    this.addEventListener('click', (e) => this.onClick(e));
    window.addEventListener('keydown', this.onKey);
  }

  disconnectedCallback(): void {
    window.removeEventListener('keydown', this.onKey);
    if (this.copyTimer != null) window.clearTimeout(this.copyTimer);
  }

  open(model: EndpointInfoModel): void {
    this.model = model;
    this.hidden = false;
    this.paint();
    raise(this);
  }

  hide(): void {
    this.hidden = true;
  }

  private onKey = (e: KeyboardEvent): void => {
    if (e.key === 'Escape' && !this.hidden) this.hide();
  };

  private onClick(e: MouseEvent): void {
    const t = (e.target as HTMLElement).closest('[data-act], .info-uri') as HTMLElement | null;
    if (!t) return;
    if (t.closest('[data-act="close"]')) {
      this.hide();
      return;
    }
    if (t.closest('[data-act="copy-uri"]') || t.classList.contains('info-uri')) {
      void this.copyURI(t.closest('[data-act="copy-uri"]') as HTMLButtonElement | null);
    }
  }

  private async copyURI(btn: HTMLButtonElement | null): Promise<void> {
    const uri = this.model ? infoURI(this.model) : '';
    if (!uri) return;
    const ok = await copyText(uri);
    const label = btn || this.querySelector<HTMLButtonElement>('[data-act="copy-uri"]');
    if (!label) return;
    label.textContent = ok ? 'Copied' : 'Failed';
    if (this.copyTimer != null) window.clearTimeout(this.copyTimer);
    this.copyTimer = window.setTimeout(() => {
      label.textContent = 'Copy';
      this.copyTimer = null;
    }, 1500);
  }

  private paint(): void {
    const m = this.model;
    const body = this.querySelector('.endpoint-info__body');
    const title = this.querySelector('.get-info-window__title');
    if (!m || !body || !title) return;
    const ep = m.endpoint;
    const sess = m.session;
    const name = m.kind === 'share' ? m.volume || ep.title : ep.title;
    title.textContent = `${name} Info`;
    const protocol = (ep.protocol || ep.kind || '').toUpperCase();
    const transport = (ep.transport || sess?.transport || '').toUpperCase();
    const volumes = sess?.volumes?.length ? sess.volumes.join(', ') : m.volume || '';
    const uams = sess?.uams?.length ? sess.uams.join(', ') : '';
    const authLabel = infoAuthLabel(ep.kind);
    body.innerHTML = `<dl class="info-list">
      ${row('Name', name)}
      ${row('Kind', m.kind === 'share' ? 'Share' : 'Server')}
      ${row('Protocol', protocol)}
      ${row('Transport', transport)}
      ${row('Address', infoAddress(m))}
      ${uriRow(infoURI(m))}
      ${row('OS', infoOS(m))}
      ${row('SMB version', infoSMBVersion(m))}
      ${row('Description', infoDescription(m))}
      ${m.kind === 'share' ? row('Server', sess?.serverName || ep.title) : ''}
      ${m.kind === 'share' ? row('Volume', m.volume) : row('Volumes', volumes)}
      ${authLabel ? row(authLabel, uams) : ''}
      ${row('Mount point', m.mountpoint)}
    </dl>`;
  }
}

customElements.define('cs-endpoint-info', EndpointInfoWindow);

import { api, type Unit } from '../api';
import { telemetry, type ServerMessage } from '../telemetry';
import { escapeHtml, mountFloatingWindow, raise } from './floating-window';

export type NoticeKind = 'failure' | 'messenger' | 'afp' | 'info';

export type Notice = {
  id: string;
  kind: NoticeKind;
  title: string;
  text: string;
  time: number;
  read: boolean;
};

export function kindLabel(k: NoticeKind): string {
  switch (k) {
    case 'failure':
      return 'Service';
    case 'messenger':
      return 'Net send';
    case 'afp':
      return 'AFP';
    default:
      return 'Notice';
  }
}

/** Bell + floating notification centre for failed units, net send, and AFP alerts. */
export class NotificationCentre extends HTMLElement {
  private notices: Notice[] = [];
  private seenFailures = new Set<string>();
  private poll: ReturnType<typeof setInterval> | null = null;
  private onMessage: ((m: ServerMessage) => void) | null = null;
  private bell: HTMLButtonElement | null = null;
  readonly onChange = new Set<() => void>();

  connectedCallback(): void {
    this.classList.add('activity-window', 'notify-centre');
    this.hidden = true;
    this.style.right = '16px';
    this.style.top = '56px';
    this.style.left = 'auto';
    this.innerHTML = `
      <div class="activity-window__chrome">
        <div class="activity-window__title">Notifications</div>
        <button type="button" class="btn log-panel__btn" data-act="clear">Clear</button>
        <button type="button" class="btn log-panel__btn" data-act="close" aria-label="Close">✕</button>
      </div>
      <div class="activity-window__body notify-centre__body"></div>
    `;
    mountFloatingWindow(this, { chromeClass: 'activity-window__chrome', minWidth: 320, minHeight: 180 });
    this.addEventListener('click', (e) => this.onClick(e));
    window.addEventListener('keydown', this.onKey);
    this.onMessage = (m) => this.ingestMessage(m);
    telemetry.onMessage.add(this.onMessage);
    void this.pollStatus();
    this.poll = setInterval(() => void this.pollStatus(), 5000);
  }

  disconnectedCallback(): void {
    if (this.poll) clearInterval(this.poll);
    if (this.onMessage) telemetry.onMessage.delete(this.onMessage);
    window.removeEventListener('keydown', this.onKey);
  }

  bindBell(btn: HTMLButtonElement): void {
    this.bell = btn;
    btn.addEventListener('click', (e) => {
      e.stopPropagation();
      this.toggle();
    });
    this.paintBell();
  }

  list(): Notice[] {
    return this.notices;
  }

  unread(): number {
    return this.notices.filter((n) => !n.read).length;
  }

  markAllRead(): void {
    let changed = false;
    for (const n of this.notices) {
      if (n.read) continue;
      n.read = true;
      changed = true;
    }
    if (!changed) return;
    this.paint();
    this.paintBell();
  }

  clearAll(): void {
    this.notices = [];
    this.paint();
    this.paintBell();
  }

  show(): void {
    this.hidden = false;
    for (const n of this.notices) n.read = true;
    this.paint();
    this.paintBell();
    raise(this);
  }

  hide(): void {
    this.hidden = true;
  }

  toggle(): void {
    if (this.hidden) this.show();
    else this.hide();
  }

  push(kind: NoticeKind, title: string, text: string, id?: string): void {
    const notice: Notice = {
      id: id || `${kind}:${title}:${Date.now()}`,
      kind,
      title,
      text,
      time: Date.now(),
      read: !this.hidden,
    };
    this.notices.unshift(notice);
    if (this.notices.length > 80) this.notices.length = 80;
    this.paint();
    this.paintBell();
  }

  private onKey = (e: KeyboardEvent): void => {
    if (e.key === 'Escape' && !this.hidden) this.hide();
  };

  private onClick(e: MouseEvent): void {
    const t = (e.target as HTMLElement).closest('[data-act]') as HTMLElement | null;
    if (!t) return;
    if (t.dataset.act === 'close') this.hide();
    if (t.dataset.act === 'clear') this.clearAll();
  }

  private ingestMessage(m: ServerMessage): void {
    const text = (m.Text || '').trim();
    if (!text) return;
    const from = (m.From || '').trim() || 'Server';
    if (m.Kind === 'messenger') {
      this.push('messenger', `Message from ${from}`, text);
      return;
    }
    this.push('afp', from, text);
  }

  private async pollStatus(): Promise<void> {
    let units: Unit[] = [];
    try {
      units = await api.status();
    } catch {
      return;
    }
    const live = new Set<string>();
    for (const u of units) {
      const err = (u.Error || '').trim();
      if (!err || u.Running) continue;
      const key = `${u.Name}:${err}`;
      live.add(key);
      if (this.seenFailures.has(key)) continue;
      this.seenFailures.add(key);
      this.push('failure', `${u.Name} failed`, err, key);
    }
    for (const k of [...this.seenFailures]) {
      if (!live.has(k)) this.seenFailures.delete(k);
    }
  }

  private emit(): void {
    this.onChange.forEach((cb) => cb());
  }

  private paintBell(): void {
    const n = this.unread();
    if (this.bell) {
      this.bell.classList.toggle('has-unread', n > 0);
      const badge = this.bell.querySelector('.notify-bell__count');
      if (badge) badge.textContent = n > 0 ? String(n) : '';
      this.bell.setAttribute('aria-label', n > 0 ? `Notifications (${n} unread)` : 'Notifications');
    }
  }

  private paint(): void {
    const body = this.querySelector('.notify-centre__body');
    if (!body) {
      this.emit();
      return;
    }
    if (!this.notices.length) {
      body.innerHTML = `<p class="muted">No notifications.</p>`;
      this.emit();
      return;
    }
    body.innerHTML = this.notices
      .map((n) => {
        const t = new Date(n.time).toLocaleTimeString();
        return `<article class="notify-item notify-item--${n.kind}${n.read ? '' : ' unread'}">
          <div class="notify-item__meta"><span>${escapeHtml(kindLabel(n.kind))}</span><time>${escapeHtml(t)}</time></div>
          <h3>${escapeHtml(n.title)}</h3>
          <p>${escapeHtml(n.text)}</p>
        </article>`;
      })
      .join('');
    this.emit();
  }
}

customElements.define('cs-notify-centre', NotificationCentre);

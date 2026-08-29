import { telemetry } from '../telemetry';
import type { LogRecord } from '../api';
import { mountFloatingWindow, raise } from './floating-window';

const LEVELS = ['TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR'];

const KIND_STR = 0;
const KIND_INT = 1;
const KIND_BOOL = 2;

const TIME_FMT = {
  hour12: false,
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  fractionalSecondDigits: 3,
} as Intl.DateTimeFormatOptions;

function fieldVal(f: NonNullable<LogRecord['Fields']>[number]): string {
  if (f.Kind === KIND_INT) return String(f.Int ?? 0);
  if (f.Kind === KIND_BOOL) return String(!!f.Bool);
  if (f.Kind === KIND_STR) return f.Str ?? '';
  if (f.Str != null) return String(f.Str);
  if (f.Int != null) return String(f.Int);
  if (f.Value != null) return String(f.Value);
  return '';
}

function cell(cls: string, text: string): HTMLSpanElement {
  const el = document.createElement('span');
  el.className = cls;
  el.textContent = text;
  return el;
}

/** Floating Event Log, matching ClassicStack-web’s log panel. */
export class LogWindow extends HTMLElement {
  private minLevel = 2;
  private follow = true;
  private onLog: (() => void) | null = null;

  connectedCallback(): void {
    this.classList.add('log-panel');
    this.hidden = true;
    this.style.left = '24px';
    this.style.bottom = '24px';
    this.style.top = 'auto';
    this.renderShell();
    mountFloatingWindow(this, { chromeClass: 'log-panel__chrome', minWidth: 480, minHeight: 180 });
    this.addEventListener('click', (e) => this.onClick(e));
    this.addEventListener('change', (e) => this.onChange(e));
    window.addEventListener('keydown', this.onKey);
  }

  disconnectedCallback(): void {
    if (this.onLog) telemetry.onLog.delete(this.onLog);
    window.removeEventListener('keydown', this.onKey);
  }

  show(): void {
    this.hidden = false;
    this.reload();
    raise(this);
  }

  hide(): void {
    this.hidden = true;
  }

  toggle(): void {
    if (this.hidden) this.show();
    else this.hide();
  }

  private onKey = (e: KeyboardEvent): void => {
    if (e.key === 'Escape' && !this.hidden) this.hide();
  };

  private renderShell(): void {
    this.innerHTML = `
      <div class="log-panel__chrome">
        <div class="log-panel__title">Event Log</div>
        <label class="log-panel__level">
          Level
          <select data-act="level">
            ${LEVELS.map((l, i) => `<option value="${i}"${i === this.minLevel ? ' selected' : ''}>${l}</option>`).join('')}
          </select>
        </label>
        <label class="log-panel__level"><input type="checkbox" data-act="follow" ${this.follow ? 'checked' : ''} /> Follow</label>
        <button type="button" class="btn log-panel__btn" data-act="clear">Clear</button>
        <button type="button" class="btn log-panel__btn" data-act="close" aria-label="Close">✕</button>
      </div>
      <div class="log-panel__head" aria-hidden="true">
        <span class="log-row__time">Time</span>
        <span class="log-row__level">Level</span>
        <span class="log-row__component">Component</span>
        <span class="log-row__msg">Message</span>
      </div>
      <div class="log-panel__body" role="log" aria-live="polite"></div>
    `;
    this.onLog = () => {
      if (!this.hidden) this.appendLatest();
    };
    telemetry.onLog.add(this.onLog);
  }

  private onClick(e: MouseEvent): void {
    const t = (e.target as HTMLElement).closest('[data-act]') as HTMLElement | null;
    if (!t) return;
    if (t.dataset.act === 'close') this.hide();
    if (t.dataset.act === 'clear') {
      telemetry.logs = [];
      this.reload();
    }
  }

  private onChange(e: Event): void {
    const t = e.target as HTMLInputElement | HTMLSelectElement;
    if (t instanceof HTMLSelectElement && t.dataset.act === 'level') {
      this.minLevel = Number(t.value);
      this.reload();
    }
    if (t instanceof HTMLInputElement && t.dataset.act === 'follow') this.follow = t.checked;
  }

  private reload(): void {
    const body = this.querySelector('.log-panel__body');
    if (!body) return;
    body.replaceChildren();
    for (const rec of telemetry.logs) {
      if ((rec.Level ?? 2) < this.minLevel) continue;
      body.append(this.row(rec));
    }
    this.scrollToBottom();
  }

  private appendLatest(): void {
    const rec = telemetry.logs[telemetry.logs.length - 1];
    if (!rec || (rec.Level ?? 2) < this.minLevel) return;
    const body = this.querySelector('.log-panel__body');
    if (!body) return;
    body.append(this.row(rec));
    if (this.follow) this.scrollToBottom();
  }

  private row(r: LogRecord): HTMLElement {
    const lvl = r.Level == null ? 2 : r.Level;
    const name = LEVELS[lvl] || 'INFO';
    const ts = r.Time ? new Date(r.Time).toLocaleTimeString(undefined, TIME_FMT) : '';
    const extra = (r.Fields || []).map((f) => `${f.Key}=${fieldVal(f)}`).join(' ');
    const row = document.createElement('div');
    row.className = `log-row log-row--${name.toLowerCase()}`;
    const component = cell('log-row__component', r.Component || '');
    if (r.Component) component.title = r.Component;
    const msg = cell('log-row__msg', r.Msg || '');
    if (extra) {
      const fields = cell('log-row__fields', extra);
      msg.append(' ', fields);
    }
    row.append(cell('log-row__time', ts), cell('log-row__level', name), component, msg);
    return row;
  }

  private scrollToBottom(): void {
    const body = this.querySelector('.log-panel__body');
    if (body) body.scrollTop = body.scrollHeight;
  }
}

customElements.define('cs-log-window', LogWindow);

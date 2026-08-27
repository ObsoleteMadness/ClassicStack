import { ApiError } from '../api';
import { escapeHtml } from './floating-window';

export type PromptTextOptions = {
  okLabel?: string;
  multiline?: boolean;
  placeholder?: string;
  hint?: string;
};

/**
 * Modal error dialog. The control adapter answers a failed lifecycle operation with a
 * 500 carrying the reason a service would not come up ("pcap: no such device en9"), and
 * that reason is the whole point of the interaction — the operator is repairing a
 * config. An inline status line beside a field is too easy to miss for something that
 * left a service down, so failures that reach the server get a dialog the operator has
 * to dismiss.
 *
 * Repeated calls collapse onto the open dialog: a settings form that fans out into
 * several requests must not stack a wall of overlays.
 */
export function alertError(title: string, message: string): void {
  const existing = document.querySelector('.error-overlay');
  if (existing) {
    const body = existing.querySelector('.error-message');
    if (body) body.textContent = message;
    const head = existing.querySelector('h2');
    if (head) head.textContent = title;
    return;
  }
  const overlay = document.createElement('div');
  overlay.className = 'modal-overlay error-overlay';
  overlay.innerHTML = `
    <div class="modal" role="alertdialog" aria-modal="true">
      <div class="modal-head"><h2>${escapeHtml(title)}</h2></div>
      <div class="modal-body"><p class="error-message"></p></div>
      <div class="modal-foot">
        <button type="button" class="btn primary" data-act="ok">OK</button>
      </div>
    </div>
  `;
  // Assigned rather than interpolated: a server error message is arbitrary text.
  const body = overlay.querySelector<HTMLElement>('.error-message');
  if (body) body.textContent = message;
  const close = (): void => {
    overlay.remove();
    window.removeEventListener('keydown', onKey);
  };
  const onKey = (e: KeyboardEvent): void => {
    if (e.key === 'Escape' || e.key === 'Enter') {
      e.preventDefault();
      close();
    }
  };
  overlay.addEventListener('click', (e) => {
    const t = (e.target as HTMLElement).closest('[data-act]') as HTMLElement | null;
    if (e.target === overlay || t?.dataset.act === 'ok') close();
  });
  window.addEventListener('keydown', onKey);
  document.body.append(overlay);
  overlay.querySelector<HTMLButtonElement>('[data-act="ok"]')?.focus();
}

/**
 * Surface a failed control-plane call as a modal, but only when the SERVER failed.
 *
 * A 5xx is the adapter reporting that it could not do the thing — a service refused to
 * start, a rebuild failed — and carries the reason as its message. A 4xx is the operator's
 * own input being rejected (a bad field, a name clash); those stay as the inline status
 * beside the field they belong to, where they read as validation rather than breakage.
 * A transport failure (server gone) has no status and is treated as a server failure.
 *
 * Returns whether it raised the dialog, so a caller can fall back to its own reporting.
 */
export function reportServerError(title: string, e: unknown): boolean {
  const status = e instanceof ApiError ? e.status : 0;
  if (status >= 400 && status < 500) return false;
  alertError(title, e instanceof Error ? e.message : String(e));
  return true;
}

/** Promise-based text prompt used for Send Message and Open by Path. */
export function promptText(
  title: string,
  label: string,
  initial = '',
  opts?: PromptTextOptions,
): Promise<string | null> {
  const multiline = opts?.multiline !== false;
  const okLabel = opts?.okLabel ?? (multiline ? 'Send' : 'OK');
  const placeholder = opts?.placeholder ? ` placeholder="${escapeHtml(opts.placeholder)}"` : '';
  const hint = opts?.hint ? `<p class="muted prompt-hint">${escapeHtml(opts.hint)}</p>` : '';
  const field = multiline
    ? `<textarea class="settings-input prompt-text" rows="4"${placeholder}>${escapeHtml(initial)}</textarea>`
    : `<input type="text" class="settings-input prompt-text prompt-text--single" value="${escapeHtml(initial)}"${placeholder} />`;
  return new Promise((resolve) => {
    const overlay = document.createElement('div');
    overlay.className = 'modal-overlay prompt-overlay';
    overlay.innerHTML = `
      <div class="modal" role="dialog" aria-modal="true">
        <div class="modal-head"><h2>${escapeHtml(title)}</h2></div>
        <div class="modal-body">
          <label class="prompt-label">${escapeHtml(label)}
            ${field}
          </label>
          ${hint}
        </div>
        <div class="modal-foot">
          <button type="button" class="btn" data-act="cancel">Cancel</button>
          <button type="button" class="btn primary" data-act="ok">${escapeHtml(okLabel)}</button>
        </div>
      </div>
    `;
    const finish = (v: string | null) => {
      overlay.remove();
      resolve(v);
    };
    const readValue = (): string => {
      const input = overlay.querySelector<HTMLTextAreaElement | HTMLInputElement>('textarea, input');
      return input?.value.trim() || '';
    };
    overlay.addEventListener('click', (e) => {
      const t = (e.target as HTMLElement).closest('[data-act]') as HTMLElement | null;
      if (e.target === overlay || t?.dataset.act === 'cancel') finish(null);
      if (t?.dataset.act === 'ok') finish(readValue() || null);
    });
    overlay.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        finish(null);
      }
      if (e.key === 'Enter' && !multiline && !(e.target instanceof HTMLButtonElement)) {
        e.preventDefault();
        finish(readValue() || null);
      }
    });
    document.body.append(overlay);
    overlay.querySelector<HTMLTextAreaElement | HTMLInputElement>('textarea, input')?.focus();
  });
}

/** Choose one of the listed volumes after a path-open login that omitted a share. */
export function promptChoice(title: string, label: string, options: string[]): Promise<string | null> {
  if (!options.length) return Promise.resolve(null);
  return new Promise((resolve) => {
    const overlay = document.createElement('div');
    overlay.className = 'modal-overlay prompt-overlay';
    const choices = options
      .map(
        (name, i) => `
          <label class="prompt-choice">
            <input type="radio" name="prompt-choice" value="${escapeHtml(name)}" ${i === 0 ? 'checked' : ''} />
            <span>${escapeHtml(name)}</span>
          </label>`,
      )
      .join('');
    overlay.innerHTML = `
      <div class="modal" role="dialog" aria-modal="true">
        <div class="modal-head"><h2>${escapeHtml(title)}</h2></div>
        <div class="modal-body">
          <p class="prompt-label">${escapeHtml(label)}</p>
          <div class="prompt-choices">${choices}</div>
        </div>
        <div class="modal-foot">
          <button type="button" class="btn" data-act="cancel">Cancel</button>
          <button type="button" class="btn primary" data-act="ok">Open</button>
        </div>
      </div>
    `;
    const finish = (v: string | null) => {
      overlay.remove();
      resolve(v);
    };
    const selected = (): string | null => {
      const radio = overlay.querySelector<HTMLInputElement>('input[name="prompt-choice"]:checked');
      return radio?.value || null;
    };
    overlay.addEventListener('click', (e) => {
      const t = (e.target as HTMLElement).closest('[data-act]') as HTMLElement | null;
      if (e.target === overlay || t?.dataset.act === 'cancel') finish(null);
      if (t?.dataset.act === 'ok') finish(selected());
    });
    overlay.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        finish(null);
      }
      if (e.key === 'Enter') {
        e.preventDefault();
        finish(selected());
      }
    });
    document.body.append(overlay);
    overlay.querySelector<HTMLInputElement>('input[name="prompt-choice"]:checked')?.focus();
  });
}

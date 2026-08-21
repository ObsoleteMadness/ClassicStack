import { escapeHtml } from './floating-window';

export type PromptTextOptions = {
  okLabel?: string;
  multiline?: boolean;
  placeholder?: string;
  hint?: string;
};

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

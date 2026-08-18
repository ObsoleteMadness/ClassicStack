import { escapeHtml } from './floating-window';

/** Promise-based text prompt used for Send Message. */
export function promptText(title: string, label: string, initial = ''): Promise<string | null> {
  return new Promise((resolve) => {
    const overlay = document.createElement('div');
    overlay.className = 'modal-overlay prompt-overlay';
    overlay.innerHTML = `
      <div class="modal" role="dialog" aria-modal="true">
        <div class="modal-head"><h2>${escapeHtml(title)}</h2></div>
        <div class="modal-body">
          <label class="prompt-label">${escapeHtml(label)}
            <textarea class="settings-input prompt-text" rows="4">${escapeHtml(initial)}</textarea>
          </label>
        </div>
        <div class="modal-foot">
          <button type="button" class="btn" data-act="cancel">Cancel</button>
          <button type="button" class="btn primary" data-act="ok">Send</button>
        </div>
      </div>
    `;
    const finish = (v: string | null) => {
      overlay.remove();
      resolve(v);
    };
    overlay.addEventListener('click', (e) => {
      const t = (e.target as HTMLElement).closest('[data-act]') as HTMLElement | null;
      if (e.target === overlay || t?.dataset.act === 'cancel') finish(null);
      if (t?.dataset.act === 'ok') {
        const ta = overlay.querySelector('textarea');
        finish(ta?.value.trim() || null);
      }
    });
    overlay.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') finish(null);
    });
    document.body.append(overlay);
    overlay.querySelector('textarea')?.focus();
  });
}

import type { LogRecord } from '../api';
import { telemetry } from '../telemetry';
import { btn, el } from './dom';

const LEVELS = ['TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR'];

function fieldVal(f: NonNullable<LogRecord['Fields']>[number]): string {
  if (f.Str != null) return String(f.Str);
  if (f.Int != null) return String(f.Int);
  if (f.Value != null) return String(f.Value);
  return '';
}

export function renderLogs(root: HTMLElement): void {
  let minLevel = 2;
  let follow = true;
  const output = el('pre', { class: 'log-output' });
  const sel = el(
    'select',
    {},
    LEVELS.map((lv, i) => el('option', i === minLevel ? { value: String(i), selected: '' } : { value: String(i) }, [lv])),
  );
  sel.addEventListener('change', () => {
    minLevel = Number(sel.value);
    repaint();
  });
  const followBox = el('input', { type: 'checkbox' }) as HTMLInputElement;
  followBox.checked = true;
  followBox.addEventListener('change', () => {
    follow = followBox.checked;
  });

  root.replaceChildren(
    el('div', { class: 'panel' }, [
      el('div', { class: 'log-controls' }, [
        el('label', { class: 'inline' }, ['Level', sel]),
        el('label', { class: 'inline' }, [followBox, 'Follow']),
        btn('Clear', '', () => {
          telemetry.logs = [];
          repaint();
        }),
        btn('Download', '', download),
      ]),
      output,
    ]),
  );
  repaint();

  function repaint() {
    const frag = document.createDocumentFragment();
    for (const r of telemetry.logs) {
      const lvl = r.Level == null ? 2 : r.Level;
      if (lvl < minLevel) continue;
      const name = LEVELS[lvl] || 'INFO';
      const ts = r.Time ? new Date(r.Time).toLocaleTimeString() : '';
      const extra = (r.Fields || []).map((f) => `${f.Key}=${fieldVal(f)}`).join(' ');
      frag.append(
        el('div', { class: 'log-line log-' + name.toLowerCase() }, [
          `${ts}  ${name.padEnd(5)}  ${r.Component ? '[' + r.Component + '] ' : ''}${r.Msg || ''}${extra ? '  ' + extra : ''}`,
        ]),
      );
    }
    output.replaceChildren(frag);
    if (follow) output.scrollTop = output.scrollHeight;
  }

  function download() {
    const text = telemetry.logs
      .map((r) => {
        const name = LEVELS[r.Level ?? 2] || 'INFO';
        return `${r.Time || ''} ${name} [${r.Component || ''}] ${r.Msg || ''}`;
      })
      .join('\n');
    const a = el('a', {
      href: URL.createObjectURL(new Blob([text], { type: 'text/plain' })),
      download: 'classicstack.log',
    });
    document.body.append(a);
    a.click();
    a.remove();
  }

  const onLog = () => repaint();
  telemetry.onLog.add(onLog);
  const obs = new MutationObserver(() => {
    if (!root.contains(output)) telemetry.onLog.delete(onLog);
  });
  obs.observe(root, { childList: true });
}

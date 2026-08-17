import { api } from '../api';
import { btn, el } from './dom';

/** First-run admin-creation form (POST /setup, then reload for Basic auth). */
export function renderSetup(root: HTMLElement): void {
  const userIn = el('input', { type: 'text', autocomplete: 'username' });
  const passIn = el('input', { type: 'password', autocomplete: 'new-password' });
  const errBox = el('div', { class: 'err' });
  const submit = btn('Create admin', 'primary', async () => {
    errBox.textContent = '';
    try {
      await api.setup(userIn.value.trim(), passIn.value);
      location.reload();
    } catch (e) {
      errBox.textContent = e instanceof Error ? e.message : String(e);
    }
  });
  root.replaceChildren(
    el('div', { class: 'panel' }, [
      el('h2', {}, ['First-run setup']),
      el('p', { class: 'muted' }, ['Create the web-admin account that gates this interface.']),
      el('label', {}, ['Username']),
      userIn,
      el('label', {}, ['Password']),
      passIn,
      errBox,
      el('div', { class: 'row' }, [submit]),
    ]),
  );
}

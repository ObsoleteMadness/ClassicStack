import { api, type AuthUser } from '../api';
import { btn, el } from './dom';

const GUEST = 'Guest';

export async function renderUsers(root: HTMLElement): Promise<void> {
  const wrap = el('div');
  root.replaceChildren(wrap);
  await refresh();

  async function refresh() {
    let res: { unavailable: boolean; list: AuthUser[] };
    try {
      res = await api.users();
    } catch (e) {
      wrap.replaceChildren(el('div', { class: 'panel err' }, [e instanceof Error ? e.message : String(e)]));
      return;
    }
    if (res.unavailable) {
      wrap.replaceChildren(
        el('div', { class: 'panel' }, [
          el('h2', {}, ['Users']),
          el('p', { class: 'muted' }, ['No user store is wired in this build / configuration.']),
        ]),
      );
      return;
    }
    paint(res.list);
  }

  function paint(list: AuthUser[]) {
    const rows = list.map((u) => row(u));
    if (!list.some((u) => String(u.Name).toLowerCase() === GUEST.toLowerCase())) {
      rows.unshift(row({ Name: GUEST, Disabled: false }));
    }
    const nameIn = el('input', { type: 'text', placeholder: 'username' });
    const passIn = el('input', { type: 'password', placeholder: 'password' });
    const addStat = el('div', { class: 'err' });
    wrap.replaceChildren(
      el('div', { class: 'panel' }, [
        el('h2', {}, ['Users']),
        el('p', { class: 'field-hint' }, [
          'Guest controls anonymous logins for AFP, SMB, and NCP. When Guest is disabled, clients must present credentials. EtherDFS has no login.',
        ]),
        el('table', {}, [
          el('thead', {}, [el('tr', {}, ['User', 'State', 'Actions'].map((c) => el('th', {}, [c])))]),
          el('tbody', {}, rows.length ? rows : [el('tr', {}, [el('td', { class: 'muted', colspan: '3' }, ['No users.'])])]),
        ]),
      ]),
      el('div', { class: 'panel' }, [
        el('h2', {}, ['Add / reset user']),
        el('div', { class: 'row' }, [
          nameIn,
          passIn,
          btn('Add user', 'primary', async () => {
            addStat.textContent = '';
            const name = nameIn.value.trim();
            if (name.toLowerCase() === GUEST.toLowerCase()) {
              addStat.textContent = 'Guest is a built-in account — enable or disable it in the list above.';
              return;
            }
            try {
              await api.setUser(name, passIn.value);
              nameIn.value = '';
              passIn.value = '';
              await refresh();
            } catch (e) {
              addStat.textContent = e instanceof Error ? e.message : String(e);
            }
          }),
        ]),
        addStat,
      ]),
    );
  }

  function row(u: AuthUser): HTMLTableRowElement {
    const isGuest = String(u.Name).toLowerCase() === GUEST.toLowerCase();
    const actions = [btn(u.Disabled ? 'Enable' : 'Disable', '', () => void toggle(u))];
    if (!isGuest) {
      actions.push(btn('Reset password', '', () => void setPassword(u.Name)));
      actions.push(btn('Remove', 'danger', () => void remove(u.Name)));
    }
    return el('tr', {}, [
      el('td', {}, [isGuest ? el('em', { class: 'builtin' }, [GUEST]) : u.Name]),
      el('td', {}, [u.Disabled ? el('span', { class: 'muted' }, ['disabled']) : 'active']),
      el('td', {}, [el('div', { class: 'row' }, actions)]),
    ]);
  }

  async function toggle(u: AuthUser) {
    try {
      await api.setUserDisabled(u.Name, !u.Disabled);
      await refresh();
    } catch (e) {
      alert(e instanceof Error ? e.message : String(e));
    }
  }

  async function setPassword(name: string) {
    if (name.toLowerCase() === GUEST.toLowerCase()) return;
    const pw = prompt(`New password for ${name}:`);
    if (pw == null) return;
    try {
      await api.setUser(name, pw);
      await refresh();
    } catch (e) {
      alert(e instanceof Error ? e.message : String(e));
    }
  }

  async function remove(name: string) {
    if (name.toLowerCase() === GUEST.toLowerCase()) return;
    if (!confirm(`Remove user ${name}?`)) return;
    try {
      await api.removeUser(name);
      await refresh();
    } catch (e) {
      alert(e instanceof Error ? e.message : String(e));
    }
  }
}

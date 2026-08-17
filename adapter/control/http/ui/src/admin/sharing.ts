import { api, type ConfigModel, type FieldInfo, type Schemas } from '../api';
import { btn, el } from './dom';

const SERVICES = [
  { owner: 'AFP', key: 'AFPVolumes', add: 'volume' },
  { owner: 'SMB', key: 'SMBShares', add: 'share' },
  { owner: 'NCP', key: 'NCPVolumes', add: 'volume' },
  { owner: 'EtherDFS', key: 'EtherDFSDrives', add: 'drive' },
] as const;

const GUEST = 'Guest';

function instName(inst: Record<string, unknown>): string {
  return String(inst.VName || inst.SName || inst.DName || inst.Name || inst.name || '');
}

function nameKey(key: string): string {
  if (key === 'SMBShares') return 'SName';
  if (key === 'EtherDFSDrives') return 'DName';
  return 'VName';
}

export async function renderSharing(root: HTMLElement): Promise<void> {
  const wrap = el('div');
  root.replaceChildren(wrap);
  await refresh();

  async function refresh() {
    wrap.replaceChildren(el('p', { class: 'muted' }, ['Loading…']));
    let model: ConfigModel;
    let schemas: Schemas;
    try {
      model = await api.config();
      schemas = await api.schemas();
    } catch (e) {
      wrap.replaceChildren(el('div', { class: 'panel err' }, [e instanceof Error ? e.message : String(e)]));
      return;
    }
    const sections: Node[] = [
      el('p', { class: 'field-hint' }, [
        'File services and their exported trees. Operator browse of live volumes is admin-privileged (same as this page).',
      ]),
    ];
    let any = false;
    for (const svc of SERVICES) {
      const schema = schemas.sections.find((s) => s.key === svc.key);
      const list = model.Lists?.[svc.key] || [];
      if (!schema && !list.length) continue;
      any = true;
      sections.push(el('h3', { class: 'group-head' }, [svc.owner]));
      sections.push(listTable(svc.owner, svc.key, svc.add, list, schema?.fields || []));
    }
    if (!any) sections.push(el('div', { class: 'panel muted' }, ['No file services in this build.']));
    const saveRow = el('div', { class: 'row' }, [
      btn('Save config', 'primary', async () => {
        try {
          await api.save();
          alert('Saved.');
        } catch (e) {
          alert(e instanceof Error ? e.message : String(e));
        }
      }),
    ]);
    wrap.replaceChildren(...sections, saveRow);
  }

  function listTable(
    owner: string,
    key: string,
    add: string,
    list: Record<string, unknown>[],
    fields: FieldInfo[],
  ): HTMLElement {
    const rows = list.map((inst) => {
      const name = instName(inst);
      return el('tr', {}, [
        el('td', {}, [name]),
        el('td', { class: 'muted' }, [String(inst.Path || '')]),
        el('td', { class: 'muted' }, [String(inst.FSType || '')]),
        el('td', {}, [inst.ReadOnly ? 'ro' : 'rw']),
        el('td', {}, [
          el('div', { class: 'row' }, [
            btn('Edit', '', () => void openEditor(owner, key, inst, fields, false)),
            btn('Delete', 'danger', () => void remove(owner, key, name)),
          ]),
        ]),
      ]);
    });
    return el('div', { class: 'panel' }, [
      el('table', {}, [
        el('thead', {}, [
          el('tr', {}, ['Name', 'Path', 'FS', 'Mode', ''].map((c) => el('th', {}, [c]))),
        ]),
        el('tbody', {}, rows.length ? rows : [el('tr', {}, [el('td', { class: 'muted', colspan: '5' }, ['No entries.'])])]),
      ]),
      el('div', { class: 'row' }, [
        btn('Add ' + add, 'primary', () => void openEditor(owner, key, blank(key, list), fields, true)),
      ]),
    ]);
  }

  function blank(key: string, list: Record<string, unknown>[]): Record<string, unknown> {
    const nk = nameKey(key);
    if (list[0]) {
      const out: Record<string, unknown> = {};
      for (const [k, v] of Object.entries(list[0])) {
        out[k] = typeof v === 'boolean' ? false : Array.isArray(v) ? [] : typeof v === 'number' ? 0 : '';
      }
      return out;
    }
    return {
      [nk]: '',
      FSType: 'local_fs',
      Path: '',
      ReadOnly: false,
      Options: [],
      ForkBackend: '',
      FilenameCodec: '',
      Metastore: '',
      MetaBackend: '',
      ...(key !== 'EtherDFSDrives' ? { AllowedUsers: [] as string[] } : {}),
    };
  }

  async function remove(owner: string, key: string, name: string) {
    if (!name || !confirm(`Remove ${name}?`)) return;
    try {
      await api.removeInstance(owner, key, name);
      await refresh();
    } catch (e) {
      alert(e instanceof Error ? e.message : String(e));
    }
  }

  async function openEditor(
    owner: string,
    key: string,
    inst: Record<string, unknown>,
    fields: FieldInfo[],
    isNew: boolean,
  ) {
    const overlay = el('div', { class: 'modal-overlay' });
    const body = el('div', { class: 'modal-body' });
    const status = el('div', { class: 'err' });
    const close = () => overlay.remove();
    overlay.append(
      el('div', { class: 'modal' }, [
        el('div', { class: 'modal-head' }, [
          el('h2', {}, [(isNew ? 'Add to ' : 'Edit ') + key]),
          btn('✕', '', close),
        ]),
        body,
        status,
        el('div', { class: 'modal-foot' }, [
          btn('Cancel', '', close),
          btn(isNew ? 'Create' : 'Save', 'primary', () => void save()),
        ]),
      ]),
    );
    overlay.addEventListener('click', (e) => {
      if (e.target === overlay) close();
    });
    document.body.append(overlay);

    const fsTypes = await api.fsTypes().catch(() => [] as string[]);
    let userNames: string[] = [];
    try {
      const res = await api.users();
      userNames = res.list.map((u) => u.Name).filter(Boolean);
    } catch {
      userNames = [];
    }
    if (!userNames.some((n) => n.toLowerCase() === GUEST.toLowerCase())) userNames = [GUEST, ...userNames];

    const inputs = new Map<string, HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>();
    const nodes: Node[] = [];
    const keys = fields.length ? fields.map((f) => f.key) : Object.keys(inst);
    const meta = new Map(fields.map((f) => [f.key, f]));

    for (const k of keys) {
      if (!(k in inst) && fields.length) {
        const t = meta.get(k)?.type;
        inst[k] = t === 'bool' ? false : t === 'strings' ? [] : t === 'int' || t === 'uint' ? 0 : '';
      }
      if (!(k in inst)) continue;
      const field = meta.get(k);
      const label = field?.display_name || k;
      const v = inst[k];
      if (k === 'Path') {
        const inp = el('input', { type: 'text', value: String(v || '') });
        inputs.set(k, inp);
        nodes.push(
          el('div', { class: 'field-group' }, [
            el('label', {}, [label]),
            el('div', { class: 'row' }, [
              inp,
              btn('Browse…', '', () => openPathBrowser(inp.value, (p) => { inp.value = p; })),
            ]),
            field?.description ? el('p', { class: 'field-hint' }, [field.description]) : el('span'),
          ]),
        );
        continue;
      }
      if (k === 'FSType' && fsTypes.length) {
        const sel = el('select', {}, fsTypes.map((t) => el('option', t === v ? { value: t, selected: '' } : { value: t }, [t])));
        inputs.set(k, sel);
        nodes.push(el('div', { class: 'field-group' }, [el('label', {}, [label]), sel]));
        continue;
      }
      if (k === 'DName' && key === 'EtherDFSDrives') {
        const letters = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ'.split('');
        const sel = el('select', {}, letters.map((t) => el('option', t === v ? { value: t, selected: '' } : { value: t }, [t])));
        inputs.set(k, sel);
        nodes.push(el('div', { class: 'field-group' }, [el('label', {}, [label]), sel]));
        continue;
      }
      if (k === 'AllowedUsers' && key !== 'EtherDFSDrives') {
        const chosen = new Set((Array.isArray(v) ? v : []).map(String));
        const boxes = userNames.map((n) => {
          const cb = el('input', { type: 'checkbox' }) as HTMLInputElement;
          cb.checked = chosen.has(n);
          cb.dataset.user = n;
          return el('label', { class: 'inline' }, [cb, n]);
        });
        const holder = el('div', { class: 'checklist' }, boxes);
        holder.dataset.kind = 'users';
        nodes.push(el('div', { class: 'field-group' }, [el('label', {}, [label]), holder]));
        continue;
      }
      if (typeof v === 'boolean' || field?.type === 'bool') {
        const inp = el('input', { type: 'checkbox' }) as HTMLInputElement;
        inp.checked = !!v;
        inputs.set(k, inp);
        nodes.push(el('label', { class: 'inline' }, [inp, label]));
        continue;
      }
      if (Array.isArray(v) || field?.type === 'strings') {
        const ta = el('textarea', { rows: '3' }, [Array.isArray(v) ? v.join('\n') : '']);
        inputs.set(k, ta);
        nodes.push(el('div', { class: 'field-group' }, [el('label', {}, [label]), ta]));
        continue;
      }
      const inp = el('input', {
        type: field?.secret ? 'password' : field?.type === 'int' || field?.type === 'uint' ? 'number' : 'text',
        value: String(v ?? ''),
      });
      inputs.set(k, inp);
      nodes.push(el('div', { class: 'field-group' }, [el('label', {}, [label]), inp]));
    }
    body.replaceChildren(...nodes);

    async function save() {
      status.textContent = '';
      const section: Record<string, unknown> = { ...inst };
      for (const [k, input] of inputs) {
        const orig = inst[k];
        if (input instanceof HTMLInputElement && input.type === 'checkbox') section[k] = input.checked;
        else if (typeof orig === 'number' || (input instanceof HTMLInputElement && input.type === 'number'))
          section[k] = Number(input.value);
        else if (Array.isArray(orig) || input instanceof HTMLTextAreaElement)
          section[k] = input.value.split('\n').map((s) => s.trim()).filter(Boolean);
        else section[k] = input.value;
      }
      const userHolder = body.querySelector<HTMLElement>('[data-kind="users"]');
      if (userHolder) {
        section.AllowedUsers = [...userHolder.querySelectorAll<HTMLInputElement>('input[type=checkbox]')]
          .filter((c) => c.checked)
          .map((c) => c.dataset.user || '');
      }
      try {
        const prev = instName(inst);
        const next = instName(section);
        if (!isNew && prev && next && prev !== next) await api.removeInstance(owner, key, prev);
        await api.addInstance(owner, key, section);
        close();
        await refresh();
      } catch (e) {
        status.textContent = e instanceof Error ? e.message : String(e);
      }
    }
  }
}

function openPathBrowser(startDir: string, onPick: (p: string) => void): void {
  const overlay = el('div', { class: 'modal-overlay' });
  const listBox = el('div');
  const here = el('div', { class: 'muted path-here' });
  const close = () => overlay.remove();
  let cur = startDir || '';

  async function go(dir: string) {
    try {
      const res = await api.browsePath(dir);
      cur = res.path;
      here.textContent = cur;
      const items: Node[] = [btn('‹ parent', '', () => void go(res.parent))];
      for (const e of res.entries) {
        items.push(btn('📁 ' + e.name, '', () => void go(cur.replace(/[\\/]+$/, '') + '/' + e.name)));
      }
      listBox.replaceChildren(el('div', { class: 'row wrap' }, items));
    } catch (err) {
      listBox.replaceChildren(el('p', { class: 'err' }, [err instanceof Error ? err.message : String(err)]));
    }
  }

  overlay.append(
    el('div', { class: 'modal' }, [
      el('div', { class: 'modal-head' }, [el('h2', {}, ['Choose a directory']), btn('✕', '', close)]),
      el('div', { class: 'modal-body' }, [here, listBox]),
      el('div', { class: 'modal-foot' }, [
        btn('Cancel', '', close),
        btn('Select this folder', 'primary', () => {
          onPick(cur);
          close();
        }),
      ]),
    ]),
  );
  overlay.addEventListener('click', (e) => {
    if (e.target === overlay) close();
  });
  document.body.append(overlay);
  void go(startDir);
}

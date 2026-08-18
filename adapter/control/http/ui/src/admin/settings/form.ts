import { api, type ConfigModel, type FieldInfo, type Schemas, type ShareBackends } from '../../api';
import { btn, el } from '../dom';
import {
  BACKEND_OPTIONS,
  CLIENT_SERVICE_OPTIONS,
  FALLBACK_FILENAME_CODECS,
  FALLBACK_FORK_BACKENDS,
  FALLBACK_FS_TYPES,
  FALLBACK_META_BACKENDS,
  FALLBACK_METASTORES,
  GLOBAL_EXTMAP_PATH,
  IPX_FRAME_OPTIONS,
  LOG_LEVELS,
  MACIP_MODE_OPTIONS,
  transportOptions,
  type CheckOption,
} from './field-options';

export type FormContext = {
  ifaceNames?: string[];
  hostDevices?: { value: string; label: string }[];
  serialPorts?: { value: string; label: string }[];
  userNames?: string[];
  bridgeMac?: string;
  schemaKey?: string;
  portMembers?: CheckOption[];
  hideFields?: Set<string>;
  shareBackends?: ShareBackends;
  zones?: string[];
  onEditExtMap?: () => void;
  onViewLeases?: () => void;
};

export type ApplyHandler = (section: Record<string, unknown>) => Promise<void>;

const CAP_LABELS: Record<string, string> = {
  wire_binding: 'Binding',
  capture: 'Capture',
  appletalk_seed: 'AppleTalk seed',
  serial: 'Serial',
  ipx_network: 'IPX network',
  ipx_framing: 'IPX framing',
  localtalk_pace: 'LocalTalk pacing',
};

/** Collected from the combined `_ipx_framing` checklist, not as individual inputs. */
const SKIP_KEYS = new Set(['IPXFrameType', 'IPXFrameTypes']);
const ENABLE_KEYS = new Set(['Enabled', 'IsEnabled']);
/** LocalTalk transports have no Ethernet identity (spec/06-port-ethertalk.md). */
const LOCALTALK_HIDE = new Set(['Iface', 'MAC']);
/** MacIP NAT-only vs bridge-only fields (core/service/macip.Section). */
const MACIP_VISIBLE_WHEN: Record<string, string> = {
  GatewayIP: 'nat',
  Network: 'nat',
  Nameserver: 'nat',
  Broadcast: 'nat',
  SubnetMask: 'nat',
  HostCount: 'nat',
  DefaultGateway: 'bridge',
  DHCPRelay: 'bridge',
};

function hiddenKeys(ctx: FormContext): Set<string> {
  const out = new Set(ctx.hideFields);
  if (ctx.schemaKey === 'TashTalk' || ctx.schemaKey === 'LToUDP') {
    for (const k of LOCALTALK_HIDE) out.add(k);
  }
  return out;
}

function findEnableField(fields: FieldInfo[]): FieldInfo | null {
  return fields.find((f) => ENABLE_KEYS.has(f.key) && f.type === 'bool') ?? null;
}

function syncGatedOptions(wrap: HTMLElement, open: boolean, animate: boolean): void {
  if (open) {
    wrap.hidden = false;
    wrap.classList.remove('is-collapsed');
    if (animate) void wrap.offsetHeight;
    return;
  }
  wrap.classList.add('is-collapsed');
  if (!animate) {
    wrap.hidden = true;
    return;
  }
  const onEnd = (e: TransitionEvent): void => {
    if (e.target !== wrap || e.propertyName !== 'opacity') return;
    wrap.removeEventListener('transitionend', onEnd);
    if (wrap.classList.contains('is-collapsed')) wrap.hidden = true;
  };
  wrap.addEventListener('transitionend', onEnd);
}

function debounce(fn: () => void, ms: number): () => void {
  let t: ReturnType<typeof setTimeout> | undefined;
  return () => {
    if (t) clearTimeout(t);
    t = setTimeout(fn, ms);
  };
}

function isIpxFramingField(f: FieldInfo): boolean {
  return (
    f.capability === 'ipx_framing' ||
    f.widget === 'frame_type' ||
    f.key === 'IPXFrameType' ||
    f.key === 'IPXFrameTypes'
  );
}

function groupFields(fields: FieldInfo[]): Map<string, FieldInfo[]> {
  const groups = new Map<string, FieldInfo[]>();
  for (const f of fields) {
    // Keep IPXFrameType/IPXFrameTypes in the ipx_framing group so the checklist
    // is injected; collectValues still skips them in favour of `_ipx_framing`.
    const cap = isIpxFramingField(f) ? 'ipx_framing' : f.capability || '';
    const list = groups.get(cap) ?? [];
    list.push(f);
    groups.set(cap, list);
  }
  return groups;
}

export function renderLiveForm(opts: {
  fields: FieldInfo[];
  data: Record<string, unknown>;
  context?: FormContext;
  onApply: ApplyHandler;
  onBrowsePath?: (input: HTMLInputElement) => void;
  debounceMs?: number;
}): { root: HTMLElement; destroy: () => void; collect: () => Record<string, unknown> } {
  const ctx = opts.context ?? {};
  const status = el('span', { class: 'settings-live-status muted' });
  let applying = false;
  let destroyed = false;

  const scheduleApply = debounce(() => void applyNow(), opts.debounceMs ?? 450);

  async function applyNow(): Promise<void> {
    if (destroyed || applying) return;
    applying = true;
    status.textContent = 'Saving…';
    status.className = 'settings-live-status muted';
    try {
      await opts.onApply(collectValues(opts.fields, root, opts.data, ctx));
      if (!destroyed) {
        status.textContent = 'Saved';
        status.className = 'settings-live-status ok';
      }
    } catch (e) {
      if (!destroyed) {
        status.textContent = e instanceof Error ? e.message : String(e);
        status.className = 'settings-live-status err';
      }
    } finally {
      applying = false;
    }
  }

  function onFieldChange(): void {
    scheduleApply();
  }

  const hidden = hiddenKeys(ctx);
  const visibleFields = opts.fields.filter((f) => !hidden.has(f.key));
  const enableField = findEnableField(visibleFields);
  const bodyFields = enableField ? visibleFields.filter((f) => f.key !== enableField.key) : visibleFields;
  const groups = groupFields(bodyFields);
  const sections: Node[] = [];

  for (const [cap, fields] of groups) {
    const nodes: Node[] = [];
    if (cap === 'ipx_framing' || fields.some(isIpxFramingField)) {
      nodes.push(ipxFramingNode(opts.data, onFieldChange));
    }
    for (const field of fields) {
      if (isIpxFramingField(field)) continue;
      const node = fieldNode(field, opts.data[field.key], ctx, onFieldChange, opts.onBrowsePath);
      if (!node) continue;
      if (ctx.schemaKey === 'MacIP') {
        const when = MACIP_VISIBLE_WHEN[field.key];
        if (when) node.dataset.visibleWhen = when;
      }
      nodes.push(node);
    }
    if (!nodes.length) continue;
    const title = cap ? CAP_LABELS[cap] || cap : undefined;
    sections.push(
      el('div', { class: 'settings-group' }, [
        title ? el('div', { class: 'settings-group__title' }, [title]) : el('span'),
        ...nodes,
      ]),
    );
  }

  const formKids: Node[] = [];
  let optionsWrap: HTMLElement | null = null;

  if (enableField) {
    const enableRow = fieldNode(enableField, opts.data[enableField.key], ctx, onFieldChange, opts.onBrowsePath);
    if (enableRow) {
      enableRow.classList.add('settings-form__enable');
      formKids.push(enableRow);
    }
    optionsWrap = el('div', { class: 'settings-form__options' }, sections);
    const enabled = !!opts.data[enableField.key];
    if (!enabled) {
      optionsWrap.classList.add('is-collapsed');
      optionsWrap.hidden = true;
    }
    formKids.push(optionsWrap);
    const toggle = enableRow?.querySelector<HTMLInputElement>(`input[data-key="${enableField.key}"]`);
    toggle?.addEventListener('change', () => syncGatedOptions(optionsWrap!, toggle.checked, true));
  } else {
    formKids.push(...sections);
  }

  if (ctx.onViewLeases) {
    formKids.push(
      el('div', { class: 'settings-row settings-row--select settings-form__leases' }, [
        el('div', { class: 'settings-row__main' }, [
          el('div', { class: 'settings-row__label' }, ['TCP leases']),
          el('div', { class: 'settings-row__desc' }, ['Active MacIP client address assignments.']),
        ]),
        btn('View TCP Leases…', '', () => ctx.onViewLeases?.()),
      ]),
    );
  }

  const root = el('div', { class: 'settings-form' }, [...formKids, status]);
  bindFSOptions(root, ctx, opts.data, onFieldChange);
  bindMacIPMode(root);

  return {
    root,
    destroy: () => {
      destroyed = true;
    },
    collect: () => collectValues(opts.fields, root, opts.data, ctx),
  };
}

/** @deprecated use renderLiveForm */
export function renderSchemaForm(opts: {
  fields: FieldInfo[];
  data: Record<string, unknown>;
  ifaceNames?: string[];
  serialPorts?: string[];
  userNames?: string[];
  onBrowsePath?: (input: HTMLInputElement) => void;
}): { root: HTMLElement; inputs: Map<string, HTMLElement>; collect: () => Record<string, unknown> } {
  const form = renderLiveForm({
    fields: opts.fields,
    data: opts.data,
    context: {
      ifaceNames: opts.ifaceNames,
      serialPorts: opts.serialPorts?.map((p) => ({ value: p, label: p })),
      userNames: opts.userNames,
    },
    onApply: async () => undefined,
    debounceMs: 999999,
  });
  return { root: form.root, inputs: new Map(), collect: form.collect };
}

function fieldNode(
  field: FieldInfo,
  value: unknown,
  ctx: FormContext,
  onChange: () => void,
  onBrowsePath?: (input: HTMLInputElement) => void,
): HTMLElement | null {
  const label = field.display_name || field.key;
  const hint = field.description ? el('div', { class: 'settings-row__desc' }, [field.description]) : null;

  if (field.widget === 'client_services' || (field.key === 'Services' && ctx.schemaKey === 'Client')) {
    return checklistRow(label, hint, 'Services', [...CLIENT_SERVICE_OPTIONS], arr(value), onChange);
  }

  if (field.widget === 'port_members' || field.key === 'Members') {
    return checklistRow(label, hint, 'Members', ctx.portMembers || [], arr(value), onChange);
  }

  if (field.widget === 'nbp_bindings' || (field.key === 'Bindings' && ctx.schemaKey === 'IPXGW')) {
    return nbpBindingsRow(label, hint, field.key, arr(value), ctx.zones || [], onChange);
  }

  if (field.widget === 'mode' || (field.key === 'Mode' && ctx.schemaKey === 'MacIP')) {
    const current = String(value || 'bridge');
    const opts: CheckOption[] = [...MACIP_MODE_OPTIONS];
    if (current && !opts.some((o) => o.value === current)) opts.unshift({ value: current, label: current });
    return selectRow(label, hint, field.key, opts.map((o) => o.value), current, onChange, opts);
  }

  if (field.widget === 'extmap' || (field.key === 'ExtMapPath' && ctx.schemaKey === 'AFPVolumes')) {
    return extMapRow(label, hint, field.key, value, ctx, onChange, onBrowsePath);
  }

  if (field.widget === 'zone') {
    const zones = ctx.zones || [];
    if (zones.length) {
      const current = String(value || '');
      const listed = current && !zones.includes(current) ? [current, ...zones] : ['', ...zones];
      return selectRow(label, hint, field.key, listed, current, onChange, [
        { value: '', label: '(default)' },
        ...listed.filter(Boolean).map((z) => ({ value: z, label: z })),
      ]);
    }
  }

  if (field.key === 'Transports' && ctx.schemaKey) {
    const opts = transportOptions(ctx.schemaKey);
    if (opts) return checklistRow(label, hint, 'Transports', opts, arr(value), onChange);
  }

  if (field.key === 'AllowedUsers' && ctx.userNames?.length) {
    return checklistRow(
      label,
      hint,
      'AllowedUsers',
      ctx.userNames.map((n) => ({ value: n, label: n })),
      arr(value),
      onChange,
    );
  }

  if (field.type === 'bool' || typeof value === 'boolean') {
    const inp = el('input', { type: 'checkbox', 'data-key': field.key }) as HTMLInputElement;
    inp.checked = !!value;
    inp.addEventListener('change', onChange);
    return row(label, hint, inp, 'toggle');
  }

  if (field.key === 'Level') {
    return selectRow(label, hint, field.key, LOG_LEVELS, String(value || 'info'), onChange);
  }

  if (field.key === 'Backend' || field.widget === 'backend') {
    return selectRow(label, hint, field.key, BACKEND_OPTIONS, String(value || 'pcap'), onChange);
  }

  const shareSel = shareSelectRow(field, value, ctx, onChange);
  if (shareSel) return shareSel;

  if (field.key === 'Options') {
    return el('div', { class: 'settings-row settings-row--fs-options' }, [
      el('div', { class: 'settings-row__main' }, [el('div', { class: 'settings-row__label' }, [label]), hint ?? el('span')]),
      el('div', { class: 'settings-fs-options', 'data-key': 'Options' }),
    ]);
  }

  if (field.widget === 'frame_type') {
    return null; // handled by ipxFramingNode
  }

  if (field.widget === 'host_device' || (field.key === 'Device' && ctx.hostDevices?.length && field.widget !== 'serial')) {
    const opts: CheckOption[] = [{ value: '', label: '(none)' }, ...(ctx.hostDevices || [])];
    return selectRow(label, hint, field.key, opts.map((o) => o.value), String(value || ''), onChange, opts);
  }

  if (field.widget === 'serial' || (field.key === 'Device' && ctx.schemaKey === 'TashTalk')) {
    if (!ctx.serialPorts?.length) return null;
    return selectRow(
      label,
      hint,
      field.key,
      ctx.serialPorts.map((p) => p.value),
      String(value || ''),
      onChange,
      ctx.serialPorts,
    );
  }

  if (field.widget === 'iface' && ctx.ifaceNames?.length) {
    const names = ['', ...ctx.ifaceNames];
    return selectRow(label, hint, field.key, names, String(value || ''), onChange, [
      { value: '', label: '(default)' },
      ...ctx.ifaceNames.map((n) => ({ value: n, label: n })),
    ]);
  }

  if (field.type === 'strings' || Array.isArray(value)) {
    const known = knownStringOptions(field, ctx);
    if (known) return optionGridRow(label, hint, field.key, known, arr(value), onChange);
    return freeformGridRow(label, hint, field.key, arr(value), onChange);
  }

  if (field.key === 'Path' || field.key === 'path' || field.key === 'Mountpoint' || field.widget === 'path') {
    const inp = el('input', { type: 'text', value: String(value ?? ''), 'data-key': field.key, class: 'settings-input' }) as HTMLInputElement;
    inp.addEventListener('change', onChange);
    inp.addEventListener('blur', onChange);
    const browse = onBrowsePath ? btn('Browse…', '', () => onBrowsePath(inp)) : null;
    return el('div', { class: 'settings-row settings-row--path' }, [
      el('div', { class: 'settings-row__main' }, [el('div', { class: 'settings-row__label' }, [label]), hint ?? el('span')]),
      el('div', { class: 'row' }, browse ? [inp, browse] : [inp]),
    ]);
  }

  const isMac = field.key === 'MAC' || field.key === 'HWAddress';
  const inp = el('input', {
    type: field.secret ? 'password' : field.type === 'int' || field.type === 'uint' ? 'number' : 'text',
    value: String(value ?? ''),
    'data-key': field.key,
    class: 'settings-input',
    ...(isMac && ctx.bridgeMac && !value ? { placeholder: ctx.bridgeMac } : {}),
  }) as HTMLInputElement;
  inp.addEventListener('change', onChange);
  inp.addEventListener('blur', onChange);
  if (inp.type === 'text' || inp.type === 'number' || inp.type === 'password') inp.addEventListener('input', onChange);
  return row(label, hint, inp, 'text');
}

const SHARE_SELECTS: Record<string, { list: keyof Omit<ShareBackends, 'fs_params'>; fallback: readonly string[]; optional: boolean }> = {
  fs_type: { list: 'fs_types', fallback: FALLBACK_FS_TYPES, optional: false },
  fork_backend: { list: 'fork_backends', fallback: FALLBACK_FORK_BACKENDS, optional: true },
  filename_codec: { list: 'filename_codecs', fallback: FALLBACK_FILENAME_CODECS, optional: true },
  metastore: { list: 'metastores', fallback: FALLBACK_METASTORES, optional: true },
  meta_backend: { list: 'meta_backends', fallback: FALLBACK_META_BACKENDS, optional: true },
};

function shareSelectRow(
  field: FieldInfo,
  value: unknown,
  ctx: FormContext,
  onChange: () => void,
): HTMLElement | null {
  const spec = field.widget ? SHARE_SELECTS[field.widget] : undefined;
  if (!spec) return null;
  const label = field.display_name || field.key;
  const hint = field.description ? el('div', { class: 'settings-row__desc' }, [field.description]) : null;
  let values = [...(ctx.shareBackends?.[spec.list] || spec.fallback)];
  const current = String(value || '');
  if (current && !values.includes(current)) values = [current, ...values];
  const listed = spec.optional ? ['', ...values] : values;
  const selected = spec.optional
    ? current
    : current || (values.includes('local_fs') ? 'local_fs' : values[0] || '');
  return selectRow(label, hint, field.key, listed, selected, onChange, undefined, true);
}

const PATH_OPT = 'path';

function parseOptionPairs(list: string[]): Map<string, string> {
  const out = new Map<string, string>();
  for (const item of list) {
    const i = item.indexOf('=');
    if (i <= 0) continue;
    out.set(item.slice(0, i), item.slice(i + 1));
  }
  return out;
}

function bindFSOptions(
  root: HTMLElement,
  ctx: FormContext,
  orig: Record<string, unknown>,
  onChange: () => void,
): void {
  const holder = root.querySelector<HTMLElement>('.settings-fs-options[data-key="Options"]');
  if (!holder) return;
  const values = parseOptionPairs(arr(orig.Options));

  const paint = (): void => {
    for (const inp of holder.querySelectorAll<HTMLInputElement>('[data-opt]')) {
      const k = inp.dataset.opt || '';
      if (k) values.set(k, inp.value);
    }
    const sel = root.querySelector<HTMLSelectElement>('[data-key="FSType"]');
    const fsType = (sel?.value || String(orig.FSType || 'local_fs')).toLowerCase();
    const params = (ctx.shareBackends?.fs_params?.[fsType] || []).filter((p) => p.key.toLowerCase() !== PATH_OPT);
    holder.replaceChildren();
    const known = new Set(params.map((p) => p.key.toLowerCase()));

    if (!params.length) {
      const leftovers = [...values.entries()].filter(([k, v]) => v && !known.has(k.toLowerCase()));
      if (!leftovers.length) {
        holder.append(el('div', { class: 'settings-fs-options__empty' }, ['No extra options for this filesystem type.']));
        return;
      }
    }

    for (const p of params) {
      const val = values.get(p.key) ?? [...values.entries()].find(([k]) => k.toLowerCase() === p.key.toLowerCase())?.[1] ?? '';
      const inp = el('input', {
        type: p.secret ? 'password' : 'text',
        class: 'settings-input',
        'data-opt': p.key,
        value: val,
        ...(p.required ? { placeholder: 'required' } : {}),
      }) as HTMLInputElement;
      inp.addEventListener('change', onChange);
      inp.addEventListener('blur', onChange);
      inp.addEventListener('input', onChange);
      holder.append(
        el('label', { class: 'settings-fs-options__field' }, [
          el('span', { class: 'settings-fs-options__key' }, [p.required ? `${p.key} *` : p.key]),
          inp,
          p.doc ? el('span', { class: 'settings-row__desc' }, [p.doc]) : el('span'),
        ]),
      );
    }

    for (const [k, v] of values) {
      if (!v || known.has(k.toLowerCase())) continue;
      const inp = el('input', {
        type: 'text',
        class: 'settings-input',
        'data-opt': k,
        value: v,
      }) as HTMLInputElement;
      inp.addEventListener('change', onChange);
      inp.addEventListener('blur', onChange);
      inp.addEventListener('input', onChange);
      holder.append(
        el('label', { class: 'settings-fs-options__field' }, [
          el('span', { class: 'settings-fs-options__key' }, [k]),
          inp,
        ]),
      );
    }
  };

  root.querySelector<HTMLSelectElement>('[data-key="FSType"]')?.addEventListener('change', () => {
    paint();
    onChange();
  });
  paint();
}

function bindMacIPMode(root: HTMLElement): void {
  const sel = root.querySelector<HTMLSelectElement>('[data-key="Mode"]');
  if (!sel) return;
  const apply = (): void => {
    const mode = (sel.value || 'bridge').toLowerCase();
    for (const row of root.querySelectorAll<HTMLElement>('[data-visible-when]')) {
      row.hidden = row.dataset.visibleWhen !== mode;
    }
  };
  sel.addEventListener('change', apply);
  apply();
}

function ipxFramingNode(data: Record<string, unknown>, onChange: () => void): HTMLElement {
  const multi = arr(data.IPXFrameTypes);
  const primary = String(data.IPXFrameType || 'ethernet_ii');
  const selected = multi.length ? multi : primary ? [primary] : ['ethernet_ii'];
  return checklistRow(
    'Frame types',
    el('div', { class: 'settings-row__desc' }, ['Outbound encapsulation and advertised framing. Inbound accepts all.']),
    '_ipx_framing',
    [...IPX_FRAME_OPTIONS],
    selected,
    onChange,
  );
}

function checklistRow(
  label: string,
  hint: HTMLElement | null,
  key: string,
  options: CheckOption[],
  selected: string[],
  onChange: () => void,
): HTMLElement {
  const chosen = new Set(selected);
  const holder = el('div', { class: 'settings-checklist', 'data-key': key });
  for (const opt of options) {
    const cb = el('input', { type: 'checkbox', 'data-value': opt.value }) as HTMLInputElement;
    cb.checked = chosen.has(opt.value);
    cb.addEventListener('change', onChange);
    holder.append(el('label', { class: 'settings-checklist__item' }, [cb, el('span', {}, [opt.label])]));
  }
  return el('div', { class: 'settings-row settings-row--checklist' }, [
    el('div', { class: 'settings-row__main' }, [el('div', { class: 'settings-row__label' }, [label]), hint ?? el('span')]),
    holder,
  ]);
}

function selectRow(
  label: string,
  hint: HTMLElement | null,
  key: string,
  values: readonly string[],
  current: string,
  onChange: () => void,
  labeled?: CheckOption[],
  wide?: boolean,
): HTMLElement {
  const options = labeled ?? values.map((v) => ({ value: v, label: v || '(default)' }));
  const sel = el(
    'select',
    { class: wide ? 'settings-select settings-select--wide' : 'settings-select', 'data-key': key },
    options.map((opt) => el('option', opt.value === current ? { value: opt.value, selected: '' } : { value: opt.value }, [opt.label])),
  ) as HTMLSelectElement;
  sel.addEventListener('change', onChange);
  return row(label, hint, sel, 'select');
}

function optionGridRow(
  label: string,
  hint: HTMLElement | null,
  key: string,
  options: CheckOption[],
  selected: string[],
  onChange: () => void,
): HTMLElement {
  let values = [...selected];
  const grid = el('div', { class: 'settings-option-grid', 'data-key': key });

  const syncDataset = (): void => {
    grid.dataset.values = JSON.stringify(values);
  };

  const paint = (): void => {
    syncDataset();
    grid.replaceChildren();
    const chosen = new Set(values);
    for (const val of values) {
      const opt = options.find((o) => o.value === val);
      const chipEl = el('div', { class: 'settings-chip' }, [
        el('span', {}, [opt?.label || val]),
        el('button', { type: 'button', class: 'settings-chip__remove', 'aria-label': 'Remove' }, ['×']),
      ]);
      chipEl.querySelector('.settings-chip__remove')?.addEventListener('click', () => {
        values = values.filter((v) => v !== val);
        paint();
        onChange();
      });
      grid.append(chipEl);
    }
    const remaining = options.filter((o) => !chosen.has(o.value));
    if (remaining.length) grid.append(addMenuButton(remaining, (val) => {
      values = [...values, val];
      paint();
      onChange();
    }));
  };

  paint();
  return el('div', { class: 'settings-row settings-row--grid' }, [
    el('div', { class: 'settings-row__main' }, [el('div', { class: 'settings-row__label' }, [label]), hint ?? el('span')]),
    grid,
  ]);
}

function freeformGridRow(
  label: string,
  hint: HTMLElement | null,
  key: string,
  values: string[],
  onChange: () => void,
): HTMLElement {
  let items = [...values];
  const grid = el('div', { class: 'settings-option-grid', 'data-key': key });

  const syncDataset = (): void => {
    grid.dataset.values = JSON.stringify(items);
  };

  const paint = (): void => {
    syncDataset();
    grid.replaceChildren();
    for (const val of items) {
      const chipEl = el('div', { class: 'settings-chip' }, [
        el('span', {}, [val]),
        el('button', { type: 'button', class: 'settings-chip__remove', 'aria-label': 'Remove' }, ['×']),
      ]);
      chipEl.querySelector('.settings-chip__remove')?.addEventListener('click', () => {
        items = items.filter((v) => v !== val);
        paint();
        onChange();
      });
      grid.append(chipEl);
    }
    grid.append(
      btn('+ Add…', 'settings-grid-add', () => {
        const v = prompt('Enter value:');
        if (!v?.trim()) return;
        items = [...items, v.trim()];
        paint();
        onChange();
      }),
    );
  };

  paint();
  return el('div', { class: 'settings-row settings-row--grid' }, [
    el('div', { class: 'settings-row__main' }, [el('div', { class: 'settings-row__label' }, [label]), hint ?? el('span')]),
    grid,
  ]);
}

const DEFAULT_IPXGW_OBJECT = 'IPX Gateway';

function parseBinding(raw: string): { object: string; zone: string } {
  const i = raw.indexOf(':');
  if (i < 0) return { object: raw.trim() || DEFAULT_IPXGW_OBJECT, zone: '' };
  return { object: raw.slice(0, i).trim() || DEFAULT_IPXGW_OBJECT, zone: raw.slice(i + 1).trim() };
}

function nbpBindingsRow(
  label: string,
  hint: HTMLElement | null,
  key: string,
  values: string[],
  zones: string[],
  onChange: () => void,
): HTMLElement {
  let items = values.length ? values.map(parseBinding) : [];
  const box = el('div', { class: 'nbp-bindings', 'data-key': key });

  const sync = (): void => {
    box.dataset.values = JSON.stringify(
      items.filter((b) => b.object && b.zone).map((b) => `${b.object}:${b.zone}`),
    );
  };

  const paint = (): void => {
    sync();
    box.replaceChildren();
    box.append(
      el('div', { class: 'nbp-bindings__head' }, [
        el('span', {}, ['Gateway name']),
        el('span', {}, ['Zone']),
        el('span', { 'aria-hidden': 'true' }),
      ]),
    );
    items.forEach((b, i) => {
      const name = el('input', {
        type: 'text',
        class: 'settings-input',
        value: b.object,
        placeholder: DEFAULT_IPXGW_OBJECT,
      }) as HTMLInputElement;
      name.addEventListener('input', () => {
        items[i] = { ...items[i]!, object: name.value.trim() || DEFAULT_IPXGW_OBJECT };
        sync();
        onChange();
      });
      let zoneEl: HTMLElement;
      if (zones.length) {
        const zoneOpts = [...zones];
        if (b.zone && !zoneOpts.includes(b.zone)) zoneOpts.unshift(b.zone);
        const zone = el('select', { class: 'settings-select settings-select--wide' }) as HTMLSelectElement;
        for (const z of zoneOpts) zone.append(new Option(z, z));
        if (b.zone) zone.value = b.zone;
        else if (zoneOpts[0]) {
          items[i] = { ...items[i]!, zone: zoneOpts[0] };
          zone.value = zoneOpts[0];
        }
        zone.addEventListener('change', () => {
          items[i] = { ...items[i]!, zone: zone.value };
          sync();
          onChange();
        });
        zoneEl = zone;
      } else {
        const zone = el('input', {
          type: 'text',
          class: 'settings-input',
          value: b.zone,
          placeholder: 'Zone',
        }) as HTMLInputElement;
        zone.addEventListener('input', () => {
          items[i] = { ...items[i]!, zone: zone.value.trim() };
          sync();
          onChange();
        });
        zoneEl = zone;
      }
      const row = el('div', { class: 'nbp-bindings__row' }, [
        name,
        zoneEl,
        btn('Remove', '', () => {
          items = items.filter((_, j) => j !== i);
          paint();
          onChange();
        }),
      ]);
      box.append(row);
    });
    box.append(
      btn('+ Add binding', 'settings-grid-add', () => {
        items = [...items, { object: DEFAULT_IPXGW_OBJECT, zone: zones[0] || '' }];
        paint();
        onChange();
      }),
    );
    sync();
  };

  paint();
  return el('div', { class: 'settings-row settings-row--nbp' }, [
    el('div', { class: 'settings-row__main' }, [el('div', { class: 'settings-row__label' }, [label]), hint ?? el('span')]),
    box,
  ]);
}

function extMapRow(
  label: string,
  hint: HTMLElement | null,
  key: string,
  value: unknown,
  ctx: FormContext,
  onChange: () => void,
  onBrowsePath?: (input: HTMLInputElement) => void,
): HTMLElement {
  const current = String(value ?? '').trim();
  const isGlobal = !current || current === GLOBAL_EXTMAP_PATH;
  let lastCustom = isGlobal ? '' : current;

  const inp = el('input', {
    type: 'text',
    class: 'settings-input',
    'data-key': key,
    value: isGlobal ? '' : current,
    placeholder: 'extmap.conf',
  }) as HTMLInputElement;
  inp.addEventListener('change', onChange);
  inp.addEventListener('blur', onChange);
  inp.addEventListener('input', onChange);

  const sel = el('select', { class: 'settings-select settings-select--wide' }) as HTMLSelectElement;
  sel.append(new Option('Use global mappings', 'global', isGlobal, isGlobal));
  sel.append(new Option('Custom file', 'custom', !isGlobal, !isGlobal));

  const browse = onBrowsePath ? btn('Browse…', '', () => onBrowsePath(inp)) : null;
  const pathRow = el('div', { class: 'settings-extmap__path row' }, browse ? [inp, browse] : [inp]);
  pathRow.hidden = isGlobal;

  const edit = ctx.onEditExtMap ? btn('Edit…', '', () => ctx.onEditExtMap?.()) : null;
  if (edit) edit.hidden = !isGlobal;

  sel.addEventListener('change', () => {
    const global = sel.value === 'global';
    if (global) {
      lastCustom = inp.value.trim();
      inp.value = '';
    } else {
      inp.value = lastCustom;
    }
    pathRow.hidden = global;
    if (edit) edit.hidden = !global;
    onChange();
  });

  return el('div', { class: 'settings-row settings-row--stack' }, [
    el('div', { class: 'settings-row__main' }, [el('div', { class: 'settings-row__label' }, [label]), hint ?? el('span')]),
    el('div', { class: 'settings-extmap' }, [
      el('div', { class: 'row' }, edit ? [sel, edit] : [sel]),
      pathRow,
    ]),
  ]);
}

function addMenuButton(options: CheckOption[], onPick: (value: string) => void): HTMLElement {
  const wrap = el('div', { class: 'settings-add-menu' });
  const trigger = btn('+ Add…', 'settings-grid-add', () => {
    wrap.classList.toggle('open');
  });
  const menu = el('div', { class: 'settings-add-menu__dropdown', hidden: '' });
  for (const opt of options) {
    const item = btn(opt.label, 'settings-add-menu__item', () => {
      wrap.classList.remove('open');
      menu.hidden = true;
      onPick(opt.value);
    });
    menu.append(item);
  }
  wrap.append(trigger, menu);
  trigger.addEventListener('click', () => {
    menu.hidden = !wrap.classList.contains('open');
  });
  document.addEventListener(
    'click',
    (e) => {
      if (!wrap.contains(e.target as Node)) {
        wrap.classList.remove('open');
        menu.hidden = true;
      }
    },
    { capture: true },
  );
  return wrap;
}

function row(label: string, hint: HTMLElement | null, control: HTMLElement, kind: string): HTMLElement {
  if (kind === 'toggle') {
    return el('div', { class: 'settings-row settings-row--toggle' }, [
      el('div', { class: 'settings-row__main' }, [el('div', { class: 'settings-row__label' }, [label]), hint ?? el('span')]),
      el('label', { class: 'settings-switch' }, [control, el('span', { class: 'settings-switch__track' })]),
    ]);
  }
  return el('div', { class: `settings-row settings-row--${kind}` }, [
    el('div', { class: 'settings-row__main' }, [el('div', { class: 'settings-row__label' }, [label]), hint ?? el('span')]),
    control,
  ]);
}

function arr(v: unknown): string[] {
  return Array.isArray(v) ? v.map(String).filter(Boolean) : [];
}

function knownStringOptions(_field: FieldInfo, _ctx: FormContext): CheckOption[] | null {
  return null;
}

function collectValues(
  fields: FieldInfo[],
  root: HTMLElement,
  orig: Record<string, unknown>,
  ctx: FormContext,
): Record<string, unknown> {
  const hidden = hiddenKeys(ctx);
  const out: Record<string, unknown> = { ...orig };
  for (const field of fields) {
    if (hidden.has(field.key) || SKIP_KEYS.has(field.key)) continue;
    const key = field.key;
    const checklist = root.querySelector<HTMLElement>(`.settings-checklist[data-key="${key}"]`);
    if (checklist) {
      out[key] = [...checklist.querySelectorAll<HTMLInputElement>('input[type=checkbox]:checked')]
        .map((c) => (c.dataset.value || '').trim())
        .filter(Boolean);
      continue;
    }
    if (key === 'Options') {
      const holder = root.querySelector<HTMLElement>('.settings-fs-options[data-key="Options"]');
      if (holder) {
        const extras: string[] = [];
        for (const inp of holder.querySelectorAll<HTMLInputElement>('[data-opt]')) {
          const k = inp.dataset.opt || '';
          const v = inp.value.trim();
          if (k && v) extras.push(`${k}=${v}`);
        }
        out[key] = extras;
        continue;
      }
    }
    const grid = root.querySelector<HTMLElement>(`.settings-option-grid[data-key="${key}"]`);
    if (grid?.dataset.values) {
      try {
        out[key] = JSON.parse(grid.dataset.values) as string[];
      } catch {
        out[key] = [];
      }
      continue;
    }
    const bindings = root.querySelector<HTMLElement>(`.nbp-bindings[data-key="${key}"]`);
    if (bindings?.dataset.values) {
      try {
        out[key] = JSON.parse(bindings.dataset.values) as string[];
      } catch {
        out[key] = [];
      }
      continue;
    }
    const input = root.querySelector<HTMLElement>(`[data-key="${key}"]`) as HTMLInputElement | HTMLSelectElement | null;
    if (!input) continue;
    if (input instanceof HTMLInputElement && input.type === 'checkbox') out[key] = input.checked;
    else if (field.type === 'int' || field.type === 'uint') out[key] = Number(input.value);
    else out[key] = input.value;
  }

  const ipx = root.querySelector<HTMLElement>('.settings-checklist[data-key="_ipx_framing"]');
  if (ipx) {
    const checked = [...ipx.querySelectorAll<HTMLInputElement>('input[type=checkbox]:checked')].map((c) => c.dataset.value || '');
    out.IPXFrameTypes = checked;
    out.IPXFrameType = checked[0] || 'ethernet_ii';
  }

  return out;
}

export async function loadFormContext(model?: ConfigModel | null): Promise<FormContext> {
  const [hostIfaces, serial, users, cfg, schemas, shareBackends, zones] = await Promise.all([
    api.listInterfaces().catch(() => []),
    api.serialPorts().catch(() => []),
    api.users().catch(() => ({ unavailable: true, list: [] })),
    model ? Promise.resolve(model) : api.config().catch(() => null),
    api.schemas().catch(() => null as Schemas | null),
    api.shareBackends().catch(() => null as ShareBackends | null),
    api.listZones().catch(() => [] as string[]),
  ]);

  const ifaceNames = Object.keys(cfg?.Interfaces || {});
  const hostDevices = hostIfaces.map((i) => ({
    value: i.Name,
    label: i.Description ? `${i.Name} — ${i.Description}` : i.Name,
  }));

  const defaultBridge =
    Object.entries(cfg?.Interfaces || {}).find(([, v]) => v?.Default)?.[1] ||
    Object.values(cfg?.Interfaces || {})[0];
  const bridgeMac = String(defaultBridge?.HWAddress || '');

  return {
    ifaceNames,
    hostDevices,
    serialPorts: serial.map((p) => ({ value: p.device, label: p.label || p.device })),
    userNames: users.list.map((u) => u.Name).filter(Boolean),
    bridgeMac: bridgeMac || undefined,
    portMembers: portMemberOptions(cfg, schemas),
    shareBackends: shareBackends || undefined,
    zones: zones.filter(Boolean),
  };
}

/** AppleTalk DDP ports that can join the DDP router. IPX and NetBEUI are peers with their own mini-routers, not members. */
const APPLETALK_PORT_KEYS = ['EtherTalk', 'LToUDP', 'TashTalk'];

/** Per-instance identity matching port.Base.InstanceName: empty Name → schema key. */
export function portInstanceName(inst: Record<string, unknown>, schemaKey: string): string {
  const named = String(inst.Name ?? inst.name ?? '').trim();
  if (named) return named;
  const skey = String(inst.SKey ?? inst.skey ?? '').trim();
  return skey || schemaKey;
}

export function portMemberOptions(model: ConfigModel | null | undefined, schemas?: Schemas | null): CheckOption[] {
  let keys = APPLETALK_PORT_KEYS;
  const seeded = schemas?.sections
    ?.filter((s) => s.repeated && s.capabilities?.includes('appletalk_seed'))
    .map((s) => s.key)
    .filter(Boolean);
  if (seeded?.length) keys = seeded;
  const out: CheckOption[] = [];
  for (const key of keys) {
    const list = model?.Lists?.[key] || [];
    if (!list.length) {
      out.push({ value: key, label: key });
      continue;
    }
    for (const inst of list) {
      const name = portInstanceName(inst, key);
      out.push({ value: name, label: name === key ? key : `${name} (${key})` });
    }
  }
  return out;
}

export type ModalMount = (overlay: HTMLElement, cleanup?: () => void) => () => void;

export function openPathBrowser(startDir: string, onPick: (p: string) => void, mount?: ModalMount): void {
  const overlay = el('div', { class: 'modal-overlay' });
  const listBox = el('div');
  const here = el('div', { class: 'muted path-here' });
  let dismiss: () => void;
  const close = () => dismiss();
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
  if (mount) dismiss = mount(overlay);
  else {
    document.body.append(overlay);
    dismiss = () => overlay.remove();
  }
  void go(startDir);
}

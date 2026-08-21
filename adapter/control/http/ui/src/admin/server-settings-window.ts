import { loadPrefs, savePrefs } from 'classicstack-web/util/prefs';
import {
  renderSettingsFrame,
  renderSettingsGroup,
  renderSettingsNav,
  renderSettingsPanelHeading,
  type SettingsNavItem,
  type SettingsRow,
} from 'classicstack-web/ui/settings-panel';
import { enableWindowResize } from 'classicstack-web/ui/window-resize';
import { isCompactUi } from 'classicstack-web/ui/layout-mode';
import type { FinderWindow } from 'classicstack-web/ui/finder-window';
import type { ExtensionEditorDialog } from 'classicstack-web/ui/extension-editor-dialog';
import { api, type AuthUser, type ConfigModel, type FieldInfo, type Schemas } from '../api';
import { btn, el } from './dom';
import { loadFormContext, openPathBrowser, renderLiveForm, type FormContext } from './settings/form';
import { settingsIcons } from './settings/icons';
import { INTERFACE_FIELDS, WELL_KNOWN } from './settings/well-known';

const MIN_W = 520;
const MIN_H = 400;
const DEF_W = 820;
const DEF_H = 580;

export type SettingsSection =
  | 'general'
  | 'bridge'
  | 'tashtalk'
  | 'ltoudp'
  | 'ethertalk'
  | 'ipx'
  | 'netbeui'
  | 'netbios'
  | 'router'
  | 'users'
  | 'afp'
  | 'smb'
  | 'ncp'
  | 'etherdfs'
  | 'netboot'
  | 'macip'
  | 'ipxgw'
  | 'web'
  | 'client'
  | 'fuse'
  | 'logging'
  | 'advanced';

const SECTION_TITLE: Record<SettingsSection, string> = {
  general: 'General',
  bridge: 'Bridge',
  tashtalk: 'TashTalk',
  ltoudp: 'LToUDP',
  ethertalk: 'EtherTalk (DDP)',
  ipx: 'IPX',
  netbeui: 'NetBEUI',
  netbios: 'NetBIOS',
  router: 'AppleTalk Router',
  users: 'Users & Groups',
  afp: 'AFP',
  smb: 'SMB',
  ncp: 'NCP',
  etherdfs: 'EtherDFS',
  netboot: 'Netboot',
  macip: 'MacIP Gateway',
  ipxgw: 'IPX Gateway',
  web: 'Web Interface',
  client: 'Client',
  fuse: 'FUSE',
  logging: 'Logging',
  advanced: 'Advanced',
};

const SECTION_DESC: Record<SettingsSection, string> = {
  general: 'Server identity and Finder preferences for this admin UI.',
  bridge: 'Uplink interface between ClassicStack and the host network.',
  tashtalk: 'Serial LocalTalk adapter connected through a TashTalk device.',
  ltoudp: 'LocalTalk encapsulated in UDP multicast on the LAN.',
  ethertalk: 'AppleTalk Phase 2 over Ethernet (DDP).',
  ipx: 'Novell IPX transport, network numbers, and frame type.',
  netbeui: 'NetBIOS Frames (NBF) over the LAN.',
  netbios: 'NetBIOS name service and transport selection.',
  router: 'AppleTalk zones and which ports join the router.',
  users: 'Local accounts used by AFP, SMB, and NCP.',
  afp: 'Apple Filing Protocol server and volume shares.',
  smb: 'SMB/CIFS server and share definitions.',
  ncp: 'NetWare Core Protocol volumes and bindery options.',
  etherdfs: 'EtherDFS DOS network drives over Ethernet.',
  netboot: 'AppleTalk Boot Protocol and ChainBoot for classic Macs.',
  macip: 'IP-over-AppleTalk gateway with NAT and DHCP relay.',
  ipxgw: 'IPX gateway for MacIPX clients on AppleTalk.',
  web: 'Management web UI listen address and enablement.',
  client: 'In-process LAN file client used by the Finder.',
  fuse: 'Host mounts of remote volumes via FUSE / WinFsp.',
  logging: 'Process-wide log verbosity.',
  advanced: 'Edit server.toml directly and inspect registered services.',
};

const NAV: SettingsNavItem[] = [
  { id: 'general', label: 'General', iconHtml: settingsIcons.general },
  { id: 'bridge', label: 'Bridge', iconHtml: settingsIcons.bridge },
  { id: 'tashtalk', label: 'TashTalk', iconHtml: settingsIcons.tashtalk },
  { id: 'ltoudp', label: 'LToUDP', iconHtml: settingsIcons.ltoudp },
  { id: 'ethertalk', label: 'EtherTalk', iconHtml: settingsIcons.ethertalk },
  { id: 'ipx', label: 'IPX', iconHtml: settingsIcons.ipx },
  { id: 'netbeui', label: 'NetBEUI', iconHtml: settingsIcons.netbeui },
  { id: 'netbios', label: 'NetBIOS', iconHtml: settingsIcons.netbios },
  { id: 'router', label: 'AppleTalk Router', iconHtml: settingsIcons.router },
  { id: 'users', label: 'Users & Groups', iconHtml: settingsIcons.users },
  { id: 'afp', label: 'AFP', iconHtml: settingsIcons.afp },
  { id: 'smb', label: 'SMB', iconHtml: settingsIcons.smb },
  { id: 'ncp', label: 'NCP', iconHtml: settingsIcons.ncp },
  { id: 'etherdfs', label: 'EtherDFS', iconHtml: settingsIcons.etherdfs },
  { id: 'netboot', label: 'Netboot', iconHtml: settingsIcons.netboot },
  { id: 'macip', label: 'MacIP Gateway', iconHtml: settingsIcons.macip },
  { id: 'ipxgw', label: 'IPX Gateway', iconHtml: settingsIcons.ipxgw },
  { id: 'web', label: 'Web Interface', iconHtml: settingsIcons.web },
  { id: 'client', label: 'Client', iconHtml: settingsIcons.client },
  { id: 'fuse', label: 'FUSE', iconHtml: settingsIcons.fuse },
  { id: 'logging', label: 'Logging', iconHtml: settingsIcons.logging },
  { id: 'advanced', label: 'Advanced', iconHtml: settingsIcons.advanced },
];

const SHARE_KEYS = {
  afp: { owner: 'AFP', key: 'AFPVolumes', add: 'volume', nameKey: 'VName', listTitle: 'Shares' },
  smb: { owner: 'SMB', key: 'SMBShares', add: 'share', nameKey: 'SName', listTitle: 'Shares' },
  ncp: { owner: 'NCP', key: 'NCPVolumes', add: 'volume', nameKey: 'VName', listTitle: 'Shares' },
  etherdfs: { owner: 'EtherDFS', key: 'EtherDFSDrives', add: 'drive', nameKey: 'DName', listTitle: 'Drives' },
  fuse: { owner: 'Client', key: 'FUSEVolumes', add: 'volume', nameKey: 'Mountpoint', listTitle: 'Auto-mounted volumes' },
} as const;

const PORT_KEYS: Partial<Record<SettingsSection, string>> = {
  ethertalk: 'EtherTalk',
  tashtalk: 'TashTalk',
  ltoudp: 'LToUDP',
  ipx: 'IPX',
  netbeui: 'NetBEUI',
};

const SINGLETON_KEYS: Partial<Record<SettingsSection, { schema: string; owner: string }>> = {
  afp: { schema: 'AFP', owner: 'AFP' },
  smb: { schema: 'SMB', owner: 'SMB' },
  ncp: { schema: 'NCP', owner: 'NCP' },
  etherdfs: { schema: 'EtherDFS', owner: 'EtherDFS' },
  netbios: { schema: 'NetBIOS', owner: 'NetBIOS' },
  netboot: { schema: 'Netboot', owner: 'Netboot' },
  macip: { schema: 'MacIP', owner: 'MacIP' },
  ipxgw: { schema: 'IPXGW', owner: 'IPXGW' },
};

export interface ServerSettingsHost {
  finder?: FinderWindow;
  extensionEditor?: ExtensionEditorDialog;
  leases?: { show: () => void };
}

/** macOS-style server settings (General, transports, services, Advanced). */
export class ServerSettingsWindow extends HTMLElement {
  private host: ServerSettingsHost | null = null;
  private section: SettingsSection = 'general';
  private shellMounted = false;
  private model: ConfigModel | null = null;
  private schemas: Schemas | null = null;
  private formCtx: FormContext | null = null;
  private statusEl: HTMLElement | null = null;
  private activeForm: { destroy: () => void } | null = null;
  private modalStack: Array<() => void> = [];
  /** After the next section sync, open the share editor for this instance name. */
  private pendingShareName: string | null = null;

  connectedCallback(): void {
    this.classList.add('settings-window');
    this.hidden = true;
    this.addEventListener('click', (e) => {
      const t = (e.target as HTMLElement).closest<HTMLElement>('[data-act="close"]');
      if (t && this.contains(t)) this.close();
    });
    this.addEventListener('keydown', (e) => {
      if (e.key !== 'Escape' || this.hidden) return;
      if (this.dismissTopModal()) return;
      this.close();
    });
  }

  bind(host: ServerSettingsHost): void {
    this.host = host;
  }

  /** Open Settings on a section; when shareName is set, open that volume/share editor. */
  open(section: SettingsSection = 'general', shareName?: string): void {
    [...this.modalStack].reverse().forEach((dismiss) => dismiss());
    this.section = section;
    this.pendingShareName = shareName?.trim() || null;
    this.hidden = false;
    this.ensureShell();
    void this.refreshAll();
  }

  close(): void {
    [...this.modalStack].reverse().forEach((dismiss) => dismiss());
    this.activeForm?.destroy();
    this.activeForm = null;
    this.pendingShareName = null;
    this.hidden = true;
    this.innerHTML = '';
    this.shellMounted = false;
    this.model = null;
    this.schemas = null;
  }

  /** Mount a modal above the settings panel; settings stays open underneath. */
  showModal(overlay: HTMLElement, cleanup?: () => void): () => void {
    overlay.classList.add('settings-modal-overlay');
    let layer = this.querySelector('.settings-modal-layer') as HTMLElement | null;
    if (!layer) {
      layer = el('div', { class: 'settings-modal-layer' });
      this.append(layer);
    }
    const dismiss = () => {
      cleanup?.();
      overlay.remove();
      const i = this.modalStack.indexOf(dismiss);
      if (i >= 0) this.modalStack.splice(i, 1);
    };
    this.modalStack.push(dismiss);
    layer.append(overlay);
    overlay.addEventListener('click', (e) => {
      if (e.target === overlay) dismiss();
    });
    return dismiss;
  }

  private dismissTopModal(): boolean {
    const top = this.modalStack[this.modalStack.length - 1];
    if (!top) return false;
    top();
    return true;
  }

  private browsePath = (inp: HTMLInputElement): void => {
    openPathBrowser(inp.value, (p) => { inp.value = p; }, (overlay) => this.showModal(overlay));
  };

  private ensureShell(): void {
    if (this.shellMounted) return;
    this.innerHTML = renderSettingsFrame('Settings', renderSettingsNav(NAV, this.section));
    this.shellMounted = true;
    this.statusEl = el('div', { class: 'settings-status muted' });
    const content = this.querySelector('.settings-panel__content');
    content?.append(this.statusEl);
    this.setupChrome();
    this.querySelector('.settings-nav')?.addEventListener('click', (e) => {
      const btn = (e.target as HTMLElement).closest<HTMLElement>('[data-nav]');
      if (!btn?.dataset.nav) return;
      const next = btn.dataset.nav as SettingsSection;
      if (next === this.section) return;
      this.section = next;
      void this.syncSection();
    });
  }

  private setupChrome(): void {
    const card = this.querySelector('.settings-shell__card') as HTMLElement | null;
    if (!card) return;
    if (!isCompactUi()) {
      card.style.width = `${Math.min(DEF_W, window.innerWidth - 32)}px`;
      card.style.height = `${Math.min(DEF_H, window.innerHeight - 48)}px`;
    }
    enableWindowResize(card, {
      minWidth: isCompactUi() ? 280 : MIN_W,
      minHeight: isCompactUi() ? 320 : MIN_H,
    });
  }

  private async refreshAll(): Promise<void> {
    try {
      const [model, schemas] = await Promise.all([api.config(), api.schemas()]);
      this.model = model;
      this.schemas = schemas;
      this.formCtx = await loadFormContext(model);
      this.setStatus('');
    } catch (e) {
      this.setStatus(e instanceof Error ? e.message : String(e), true);
    }
    await this.syncSection();
    const shareName = this.pendingShareName;
    this.pendingShareName = null;
    if (shareName) this.openShareByName(shareName);
  }

  private openShareByName(name: string): void {
    if (!(this.section in SHARE_KEYS)) return;
    const meta = SHARE_KEYS[this.section as keyof typeof SHARE_KEYS];
    const list = this.model?.Lists?.[meta.key] || [];
    const want = name.toLowerCase();
    const inst = list.find((i) => this.instName(i).toLowerCase() === want);
    if (!inst) {
      this.setStatus(`Share “${name}” not found in config.`, true);
      return;
    }
    void this.openShareEditor(meta, inst, this.schema(meta.key), false);
  }

  private async persist(apply: () => Promise<void>): Promise<void> {
    await apply();
    await api.save();
    const model = await api.config().catch(() => null);
    if (model) {
      this.model = model;
      this.formCtx = await loadFormContext(model);
    }
  }

  private mountLiveForm(
    fields: FieldInfo[],
    data: Record<string, unknown>,
    context: FormContext,
    apply: (section: Record<string, unknown>) => Promise<void>,
    onBrowsePath?: (input: HTMLInputElement) => void,
  ): HTMLElement {
    const form = renderLiveForm({
      fields,
      data,
      context,
      onBrowsePath,
      onApply: async (section) => this.persist(() => apply(section)),
    });
    this.activeForm = form;
    return form.root;
  }

  private async syncSection(): Promise<void> {
    this.activeForm?.destroy();
    this.activeForm = null;
    this.querySelectorAll<HTMLElement>('.settings-nav__item').forEach((b) => {
      const on = b.dataset.nav === this.section;
      b.classList.toggle('is-selected', on);
      b.setAttribute('aria-current', on ? 'page' : 'false');
    });
    const headingSlot = this.querySelector('.settings-panel__heading-slot');
    if (headingSlot) {
      const nav = NAV.find((item) => item.id === this.section);
      headingSlot.innerHTML = renderSettingsPanelHeading({
        title: SECTION_TITLE[this.section],
        description: SECTION_DESC[this.section],
        iconHtml: nav?.iconHtml,
      });
    }
    const content = this.querySelector('.settings-panel__content');
    if (!content || !this.statusEl) return;
    content.replaceChildren(this.statusEl);
    const panel = await this.renderSection(this.section);
    content.append(panel);
  }

  private setStatus(msg: string, err = false): void {
    if (!this.statusEl) return;
    this.statusEl.textContent = msg;
    this.statusEl.className = err ? 'settings-status err' : 'settings-status muted';
  }

  private schema(key: string): FieldInfo[] {
    return this.schemas?.sections.find((s) => s.key === key)?.fields || [];
  }

  private async renderSection(section: SettingsSection): Promise<HTMLElement> {
    if (!this.model) return el('p', { class: 'muted' }, ['Loading…']);
    switch (section) {
      case 'general':
        return this.renderGeneral();
      case 'bridge':
        return this.renderBridge();
      case 'router':
        return this.renderWellKnown('Router', this.model.Router || {});
      case 'web':
        return this.renderWellKnown('HTTP', this.model.HTTP || {});
      case 'client':
        return this.renderWellKnown('Client', this.model.Client || {});
      case 'fuse':
        return this.renderFuse();
      case 'logging':
        return this.renderWellKnown('Logging', this.model.Logging || {});
      case 'users':
        return this.renderUsers();
      case 'advanced':
        return this.renderAdvanced();
      default:
        if (section in PORT_KEYS) return this.renderPort(section);
        if (section in SINGLETON_KEYS) return this.renderService(section as keyof typeof SINGLETON_KEYS);
        return el('p', { class: 'muted' }, ['Section unavailable in this build.']);
    }
  }

  private renderGeneral(): HTMLElement {
    const prefs = loadPrefs();
    const finder = this.host?.finder;
    const wrap = el('div');

    const finderRows: SettingsRow[] = [
      {
        type: 'select',
        id: 'default-view',
        label: 'Default view',
        description: 'Finder view when no URL view parameter is set.',
        value: prefs.defaultView,
        options: [
          { value: 'icon', label: 'Icons' },
          { value: 'list', label: 'List' },
          { value: 'column', label: 'Columns' },
        ],
      },
      {
        type: 'toggle',
        id: 'show-hidden',
        label: 'Show hidden files',
        checked: finder?.getShowHiddenFiles?.() ?? prefs.showHiddenFiles,
      },
      {
        type: 'toggle',
        id: 'auto-expand',
        label: 'Auto-expand files',
        checked: finder?.getAutoExpandFiles?.() ?? prefs.autoExpandFiles,
      },
      {
        type: 'toggle',
        id: 'read-finder-icons',
        label: 'Load fork icons',
        checked: finder?.getReadFinderIcons?.() ?? prefs.readFinderIcons,
      },
      {
        type: 'button',
        id: 'extension-editor',
        label: 'File type mappings',
        buttonLabel: 'Edit mappings…',
      },
      {
        type: 'select',
        id: 'zip-export',
        label: 'Export format',
        value: prefs.zipExportStyle,
        options: [
          { value: 'appledouble', label: 'AppleDouble zip' },
          { value: 'macosx', label: 'Mac OS X zip' },
        ],
      },
    ];

    wrap.innerHTML = renderSettingsGroup('Finder', finderRows);
    const identityWrap = el('div');
    identityWrap.append(
      this.mountLiveForm(
        WELL_KNOWN.Identity,
        this.model?.Identity || {},
        { ...this.formCtx, schemaKey: 'Identity' },
        (section) => api.setWellKnown('Identity', section),
      ),
    );
    wrap.insertBefore(identityWrap, wrap.firstChild);

    wrap.addEventListener('change', (e) => {
      const t = e.target as HTMLInputElement | HTMLSelectElement;
      const id = t.dataset.id;
      if (!id) return;
      if (id === 'default-view') savePrefs({ defaultView: t.value as 'icon' | 'list' | 'column' });
      else if (id === 'show-hidden') this.host?.finder?.setShowHiddenFiles?.((t as HTMLInputElement).checked);
      else if (id === 'auto-expand') this.host?.finder?.setAutoExpandFiles?.((t as HTMLInputElement).checked);
      else if (id === 'read-finder-icons') this.host?.finder?.setReadFinderIcons?.((t as HTMLInputElement).checked);
      else if (id === 'zip-export') savePrefs({ zipExportStyle: t.value === 'macosx' ? 'macosx' : 'appledouble' });
    });
    wrap.addEventListener('click', (e) => {
      const t = (e.target as HTMLElement).closest('[data-field="button"]') as HTMLElement | null;
      if (t?.dataset.id === 'extension-editor') this.host?.extensionEditor?.open();
    });
    return wrap;
  }

  private renderBridge(): HTMLElement {
    const ifaces = this.model?.Interfaces || {};
    const names = Object.keys(ifaces);
    const name = names.find((n) => ifaces[n]?.Default) || names[0] || 'br-lan';
    const data = { Name: name, Kind: 'bridge', Backend: 'pcap', Default: true, ...ifaces[name] };
    return el('div', {}, [
      this.mountLiveForm(
        INTERFACE_FIELDS,
        data,
        { ...this.formCtx, schemaKey: 'Bridge', bridgeMac: String((data as Record<string, unknown>).HWAddress || this.formCtx?.bridgeMac || '') },
        async (section) => {
          section.Kind = 'bridge';
          await api.setInterface(section);
        },
      ),
    ]);
  }

  private renderFuse(): HTMLElement {
    const wrap = el('div');
    wrap.append(
      this.mountLiveForm(
        WELL_KNOWN.FUSE || [],
        this.model?.FUSE || {},
        { ...this.formCtx, schemaKey: 'FUSE' },
        (section) => api.setWellKnown('FUSE', section),
      ),
    );
    wrap.append(this.renderShareList('fuse'));
    return wrap;
  }

  private renderWellKnown(key: string, data: Record<string, unknown>): HTMLElement {
    return el('div', {}, [
      this.mountLiveForm(
        WELL_KNOWN[key] || [],
        data,
        { ...this.formCtx, schemaKey: key, portMembers: this.formCtx?.portMembers },
        (section) => api.setWellKnown(key, section),
      ),
    ]);
  }

  private portData(section: SettingsSection): Record<string, unknown> {
    const key = PORT_KEYS[section]!;
    const list = this.model?.Lists?.[key] || [];
    if (list[0]) return { ...list[0] };
    return { SKey: key, IsEnabled: false, Name: key };
  }

  private renderPort(section: SettingsSection): HTMLElement {
    const key = PORT_KEYS[section]!;
    const fields = this.schema(key);
    if (!fields.length) return el('p', { class: 'muted' }, [`${key} is not available in this build.`]);
    return el('div', {}, [
      this.mountLiveForm(
        fields,
        this.portData(section),
        { ...this.formCtx, schemaKey: key, bridgeMac: this.formCtx?.bridgeMac },
        (sectionData) => api.addInstance(key, key, sectionData),
        (inp) => this.browsePath(inp),
      ),
    ]);
  }

  private renderService(section: keyof typeof SINGLETON_KEYS): HTMLElement {
    const meta = SINGLETON_KEYS[section]!;
    const fields = this.schema(meta.schema);
    const data = this.model?.Sections?.[meta.schema] || {};
    const wrap = el('div');

    if (fields.length) {
      wrap.append(
        this.mountLiveForm(
          fields,
          data,
          {
            ...this.formCtx,
            schemaKey: meta.schema,
            bridgeMac: this.formCtx?.bridgeMac,
            onViewLeases: section === 'macip' ? () => this.host?.leases?.show() : undefined,
          },
          (sectionData) => api.reconfigure(meta.owner, sectionData),
          (inp) => this.browsePath(inp),
        ),
      );
    }

    if (section in SHARE_KEYS) {
      wrap.append(this.renderShareList(section as keyof typeof SHARE_KEYS));
    }
    return wrap;
  }

  private instName(inst: Record<string, unknown>): string {
    return String(inst.VName || inst.SName || inst.DName || inst.Name || inst.name || inst.Mountpoint || '');
  }

  private renderShareList(section: keyof typeof SHARE_KEYS): HTMLElement {
    const meta = SHARE_KEYS[section];
    const list = this.model?.Lists?.[meta.key] || [];
    const fields = this.schema(meta.key);
    const wrap = el('div', { class: 'settings-shares' });
    wrap.append(el('div', { class: 'settings-group__title' }, [meta.listTitle]));

    for (const inst of list) {
      const name = this.instName(inst);
      const row = el('button', { type: 'button', class: 'settings-user-row' }, [
        el('span', { class: 'settings-user-row__name' }, [name]),
        el('span', { class: 'settings-user-row__chev', 'aria-hidden': 'true' }, ['›']),
      ]);
      row.addEventListener('click', () => void this.openShareEditor(meta, inst, fields, false));
      wrap.append(row);
    }

    const add = el('button', { type: 'button', class: 'settings-user-row settings-user-row--add' }, ['Add ' + meta.add + '…']);
    add.addEventListener('click', () => void this.openShareEditor(meta, this.blankShare(meta, list), fields, true));
    wrap.append(add);
    return wrap;
  }

  private blankShare(meta: (typeof SHARE_KEYS)[keyof typeof SHARE_KEYS], list: Record<string, unknown>[]): Record<string, unknown> {
    if (meta.key === 'FUSEVolumes') {
      return { Remote: '', Mountpoint: '', ReadOnly: false };
    }
    if (list[0]) {
      const out: Record<string, unknown> = {};
      for (const [k, v] of Object.entries(list[0])) {
        out[k] = typeof v === 'boolean' ? false : Array.isArray(v) ? [] : typeof v === 'number' ? 0 : '';
      }
      return out;
    }
    return {
      [meta.nameKey]: '',
      FSType: 'local_fs',
      Path: '',
      ReadOnly: false,
      Options: [],
    };
  }

  private async openShareEditor(
    meta: (typeof SHARE_KEYS)[keyof typeof SHARE_KEYS],
    inst: Record<string, unknown>,
    fields: FieldInfo[],
    isNew: boolean,
  ): Promise<void> {
    let liveForm: ReturnType<typeof renderLiveForm> | null = null;
    const overlay = el('div', { class: 'modal-overlay' });
    const body = el('div', { class: 'modal-body' });
    const status = el('div', { class: 'err' });
    const dismiss = this.showModal(overlay, () => liveForm?.destroy());
    liveForm = renderLiveForm({
      fields,
      data: inst,
      context: {
        ...this.formCtx,
        schemaKey: meta.key,
        userNames: this.formCtx?.userNames,
        hideFields: meta.key === 'EtherDFSDrives' ? new Set(['AllowedUsers']) : undefined,
        onEditExtMap: meta.key === 'AFPVolumes' ? () => this.host?.extensionEditor?.open() : undefined,
      },
      onBrowsePath: this.browsePath,
      debounceMs: isNew ? 999999 : 450,
      onApply: isNew
        ? async () => undefined
        : async (section) => {
            const prev = this.instName(inst);
            const next = this.instName(section);
            if (prev && next && prev !== next) await api.removeInstance(meta.owner, meta.key, prev);
            await this.persist(() => api.addInstance(meta.owner, meta.key, section));
          },
    });
    body.append(liveForm.root);
    overlay.append(
      el('div', { class: 'modal settings-modal' }, [
        el('div', { class: 'modal-head' }, [
          el('h2', {}, [(isNew ? 'Add ' : 'Edit ') + meta.add]),
          btn('✕', '', dismiss),
        ]),
        body,
        status,
        ...(isNew
          ? [
              el('div', { class: 'modal-foot' }, [
                btn('Cancel', '', dismiss),
                btn('Create', 'primary', async () => {
                  status.textContent = '';
                  try {
                    await this.persist(() => api.addInstance(meta.owner, meta.key, liveForm!.collect()));
                    dismiss();
                    await this.refreshAll();
                  } catch (e) {
                    status.textContent = e instanceof Error ? e.message : String(e);
                  }
                }),
              ]),
            ]
          : meta.key === 'FUSEVolumes'
            ? [
                el('div', { class: 'modal-foot' }, [
                  btn('Remove', 'danger', async () => {
                    const name = this.instName(inst);
                    if (!name) return;
                    status.textContent = '';
                    try {
                      await this.persist(() => api.removeInstance(meta.owner, meta.key, name));
                      dismiss();
                      await this.refreshAll();
                    } catch (e) {
                      status.textContent = e instanceof Error ? e.message : String(e);
                    }
                  }),
                ]),
              ]
            : []),
      ]),
    );
  }

  private async renderUsers(): Promise<HTMLElement> {
    const wrap = el('div');
    let res: { unavailable: boolean; list: AuthUser[] };
    try {
      res = await api.users();
    } catch (e) {
      wrap.append(el('p', { class: 'err' }, [e instanceof Error ? e.message : String(e)]));
      return wrap;
    }
    if (res.unavailable) {
      wrap.append(el('p', { class: 'muted' }, ['No user store is configured in this build.']));
      return wrap;
    }

    const GUEST = 'Guest';
    const list = [...res.list];
    if (!list.some((u) => u.Name.toLowerCase() === GUEST.toLowerCase())) {
      list.unshift({ Name: GUEST, Disabled: false });
    }

    for (const u of list) {
      const isGuest = u.Name.toLowerCase() === GUEST.toLowerCase();
      const row = el('button', { type: 'button', class: 'settings-user-row' }, [
        el('span', { class: 'settings-user-row__avatar', 'aria-hidden': 'true' }, [isGuest ? '👤' : '🔑']),
        el('span', { class: 'settings-user-row__name' }, [isGuest ? 'Guest User' : u.Name]),
        el('span', { class: 'settings-user-row__meta' }, [u.Disabled ? 'Disabled' : '']),
        el('span', { class: 'settings-user-row__chev', 'aria-hidden': 'true' }, ['›']),
      ]);
      row.addEventListener('click', () => this.openUserDetail(u, isGuest, () => void this.refreshAll().then(() => void this.syncSection())));
      wrap.append(row);
    }

    const add = el('button', { type: 'button', class: 'settings-user-row settings-user-row--add' }, ['Add User…']);
    add.addEventListener('click', () => this.openAddUser(() => void this.refreshAll().then(() => void this.syncSection())));
    wrap.append(add);
    return wrap;
  }

  private openUserDetail(u: AuthUser, isGuest: boolean, onDone: () => void): void {
    const overlay = el('div', { class: 'modal-overlay' });
    const status = el('div', { class: 'err' });
    const dismiss = this.showModal(overlay);
    const actions: Node[] = [
      btn(u.Disabled ? 'Enable account' : 'Disable account', '', async () => {
        await api.setUserDisabled(u.Name, !u.Disabled);
        dismiss();
        onDone();
      }),
    ];
    if (!isGuest) {
      actions.push(btn('Reset password…', '', async () => {
        const pw = prompt(`New password for ${u.Name}:`);
        if (pw == null) return;
        await api.setUser(u.Name, pw);
        dismiss();
        onDone();
      }));
      actions.push(btn('Remove user', 'danger', async () => {
        if (!confirm(`Remove ${u.Name}?`)) return;
        await api.removeUser(u.Name);
        dismiss();
        onDone();
      }));
    }
    overlay.append(
      el('div', { class: 'modal' }, [
        el('div', { class: 'modal-head' }, [el('h2', {}, [u.Name]), btn('✕', '', dismiss)]),
        el('div', { class: 'modal-body' }, [
          el('p', { class: 'muted' }, [isGuest ? 'Controls anonymous AFP/SMB/NCP logins.' : 'Local file-service account.']),
          el('div', { class: 'row wrap' }, actions),
          status,
        ]),
      ]),
    );
  }

  private openAddUser(onDone: () => void): void {
    const overlay = el('div', { class: 'modal-overlay' });
    const nameIn = el('input', { type: 'text', placeholder: 'username' });
    const passIn = el('input', { type: 'password', placeholder: 'password' });
    const status = el('div', { class: 'err' });
    const dismiss = this.showModal(overlay);
    overlay.append(
      el('div', { class: 'modal' }, [
        el('div', { class: 'modal-head' }, [el('h2', {}, ['Add User']), btn('✕', '', dismiss)]),
        el('div', { class: 'modal-body' }, [
          el('label', {}, ['Username']),
          nameIn,
          el('label', {}, ['Password']),
          passIn,
          status,
        ]),
        el('div', { class: 'modal-foot' }, [
          btn('Cancel', '', dismiss),
          btn('Add', 'primary', async () => {
            status.textContent = '';
            try {
              await api.setUser(nameIn.value.trim(), passIn.value);
              dismiss();
              onDone();
            } catch (e) {
              status.textContent = e instanceof Error ? e.message : String(e);
            }
          }),
        ]),
      ]),
    );
  }

  private renderAdvanced(): HTMLElement {
    const wrap = el('div');
    const ta = el('textarea', { class: 'settings-toml', rows: '16', spellcheck: 'false' });
    const status = el('div', { class: 'err' });
    wrap.append(
      el('p', { class: 'settings-panel__lead muted' }, [
        'Edit server.toml directly. Validate before applying; a numbered backup is written on save.',
      ]),
      ta,
      status,
      el('div', { class: 'row wrap' }, [
        btn('Reload', '', async () => {
          status.textContent = '';
          try {
            ta.value = await api.configDownload();
          } catch (e) {
            status.textContent = e instanceof Error ? e.message : String(e);
          }
        }),
        btn('Validate', '', async () => {
          status.textContent = '';
          try {
            await api.configValidate(ta.value);
            status.textContent = 'Valid.';
            status.className = 'settings-status muted';
          } catch (e) {
            status.textContent = e instanceof Error ? e.message : String(e);
          }
        }),
        btn('Apply & save', 'primary', async () => {
          status.textContent = '';
          try {
            const res = await api.configApply(ta.value);
            status.textContent = `Applied (revision ${res.revision}). Reload recommended.`;
            status.className = 'settings-status muted';
            await this.refreshAll();
          } catch (e) {
            status.textContent = e instanceof Error ? e.message : String(e);
          }
        }),
        btn('Download', '', () => {
          const a = document.createElement('a');
          a.href = 'config_download';
          a.download = 'server.toml';
          a.click();
        }),
      ]),
    );
    void api.configDownload().then((t) => { ta.value = t; }).catch(() => undefined);

    if (this.schemas?.sections.length) {
      wrap.append(el('div', { class: 'settings-group__title' }, ['Registered services']));
      for (const sc of this.schemas.sections) {
        if (sc.repeated) continue;
        const row = el('button', { type: 'button', class: 'settings-user-row' }, [
          el('span', { class: 'settings-user-row__name' }, [sc.display_name || sc.key]),
          el('span', { class: 'settings-user-row__meta' }, [sc.key]),
          el('span', { class: 'settings-user-row__chev' }, ['›']),
        ]);
        row.addEventListener('click', () => {
          this.section = sc.key.toLowerCase() as SettingsSection;
          if (!NAV.some((n) => n.id === this.section)) {
            void this.openRawService(sc.key, sc.display_name || sc.key);
          } else {
            void this.syncSection();
          }
        });
        wrap.append(row);
      }
    }
    return wrap;
  }

  private openRawService(key: string, title: string): void {
    const data = this.model?.Sections?.[key] || {};
    const fields = this.schema(key);
    const overlay = el('div', { class: 'modal-overlay' });
    const body = el('div', { class: 'modal-body' });
    let liveForm: ReturnType<typeof renderLiveForm> | null = null;
    const dismiss = this.showModal(overlay, () => liveForm?.destroy());
    liveForm = renderLiveForm({
      fields,
      data,
      context: { ...this.formCtx, schemaKey: key },
      onApply: async (section) => {
        await this.persist(() => api.reconfigure(key, section));
      },
    });
    body.append(liveForm.root);
    overlay.append(
      el('div', { class: 'modal settings-modal' }, [
        el('div', { class: 'modal-head' }, [el('h2', {}, [title]), btn('✕', '', dismiss)]),
        body,
      ]),
    );
  }
}

customElements.define('server-settings-window', ServerSettingsWindow);

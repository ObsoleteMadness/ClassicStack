/** FinderHost over ClassicStack’s /finder HTTP API (no in-browser AFP/TashTalk). */

import type {
  Credentials,
  FinderHost,
  RemoteEndpoint,
  SessionInfo,
  ShareKind,
  SidebarAction,
  SidebarGroup,
} from 'classicstack-web/ui/finder-host';
import type { Catalog } from 'classicstack-web/fs/virtual-fs';
import type { NameConflictChoice } from 'classicstack-web/fs/name-conflict';
import type { LoginDialog } from 'classicstack-web/ui/login-dialog';
import type { AlertDialog } from 'classicstack-web/ui/alert-dialog';
import type { NameConflictDialog } from 'classicstack-web/ui/name-conflict-dialog';
import { api, ApiError, type FinderMountedVolume, type FinderSession, type FinderVolume } from '../api';
import { telemetry, type FinderEvent } from '../telemetry';
import { HttpFinderAPI } from './http-finder-api';
import { promptText } from '../admin/prompt';
import type { EndpointInfoModel, EndpointLocation } from '../admin/endpoint-info';

const DISCOVER_SCHEMES = ['afp', 'smb', 'ncp', 'etherdfs'] as const;
const AFP_UAMS = ['No User Authent', 'Cleartxt Passwrd'];
const SMB_AUTH_FALLBACK = ['Share-level security', 'Plaintext passwords'];
const NCP_AUTH_FALLBACK = ['Unencrypted'];

const GROUP_SHARES = 'shares';
const GROUP_MOUNTED = 'mounted';
const GROUP_APPLETALK = 'appletalk';
const GROUP_SMB = 'smb';
const GROUP_NETWARE = 'netware';
const GROUP_ETHERDFS = 'etherdfs';

const SHARE_BADGE: Record<string, string> = {
  afp: 'AFP',
  smb: 'SMB',
  ncp: 'NCP',
  etherdfs: 'EDFS',
};

const TRANSPORT_BADGE: Record<string, string> = {
  tcp: 'TCP',
  nbt: 'TCP',
  ddp: 'DDP',
  pcap: 'DDP',
  ltoudp: 'DDP',
  tashtalk: 'DDP',
  nbp: 'DDP',
  ipx: 'IPX',
  nbipx: 'IPX',
  nbf: 'NBF',
  netbeui: 'NBF',
  etherdfs: 'EDFS',
};

function fallbackAuth(kind: string): string[] {
  switch (kind) {
    case 'smb':
      return [...SMB_AUTH_FALLBACK];
    case 'ncp':
      return [...NCP_AUTH_FALLBACK];
    case 'afp':
      return [...AFP_UAMS];
    default:
      return [];
  }
}

function offersGuest(kind: string, allowGuest?: boolean): boolean {
  if (kind === 'smb' || kind === 'ncp') return true;
  if (allowGuest != null) return allowGuest;
  return kind === 'afp';
}

function asShareKind(kind: string): ShareKind {
  if (kind === 'local' || kind === 'afp' || kind === 'smb' || kind === 'ncp' || kind === 'etherdfs') return kind;
  return 'afp';
}

function defaultTransport(kind: string): string {
  switch (kind) {
    case 'smb':
      return 'tcp';
    case 'ncp':
      return 'ipx';
    case 'etherdfs':
      return 'etherdfs';
    case 'afp':
      return 'ddp';
    default:
      return '';
  }
}

function remoteGroup(kind: string): string {
  switch (kind) {
    case 'smb':
      return GROUP_SMB;
    case 'ncp':
      return GROUP_NETWARE;
    case 'etherdfs':
      return GROUP_ETHERDFS;
    default:
      return GROUP_APPLETALK;
  }
}

function schemeForGroup(group?: string): (typeof DISCOVER_SCHEMES)[number][] {
  switch (group) {
    case GROUP_APPLETALK:
      return ['afp'];
    case GROUP_SMB:
      return ['smb'];
    case GROUP_NETWARE:
      return ['ncp'];
    case GROUP_ETHERDFS:
      return ['etherdfs'];
    case GROUP_MOUNTED:
      return [];
    default:
      return [...DISCOVER_SCHEMES];
  }
}

/** Client scheme that gates a sidebar group's visibility (Shares/Mounted are ungated). */
const SCHEME_FOR_GROUP = new Map<string, string>([
  [GROUP_APPLETALK, 'afp'],
  [GROUP_SMB, 'smb'],
  [GROUP_NETWARE, 'ncp'],
  [GROUP_ETHERDFS, 'etherdfs'],
]);

function mountedEndpointId(sessionId: string): string {
  return `mounted:${sessionId}`;
}

function toMountedEndpoint(m: FinderMountedVolume): RemoteEndpoint & EndpointLocation {
  const kind = asShareKind(m.kind);
  const transport = (m.transport || defaultTransport(kind)).toLowerCase();
  const title = m.volume || m.serverName;
  const subtitle = m.mountpoint || (m.volume && m.serverName && m.volume !== m.serverName ? m.serverName : m.target);
  const target = (m.target || '').trim();
  return {
    id: mountedEndpointId(m.sessionId),
    kind,
    title,
    subtitle,
    group: GROUP_MOUNTED,
    badge: TRANSPORT_BADGE[transport] || 'MOUNTED',
    protocol: kind,
    transport,
    role: 'volume',
    uri: /^[a-z][a-z0-9+.-]*:\/\//i.test(target) ? target.replace(/\/+$/, '') : undefined,
  };
}

function toFinderSession(m: FinderMountedVolume): FinderSession {
  return {
    sessionId: m.sessionId,
    serverName: m.serverName,
    kind: m.kind,
    volumes: m.volume ? [m.volume] : [],
    allowGuest: true,
    uams: [],
    rootId: m.rootId,
    rootPath: m.rootPath,
    volume: m.volume,
    target: m.target,
    transport: m.transport,
    protocol: m.protocol,
    capabilities: m.capabilities,
  };
}

function isVolumeEndpoint(ep: RemoteEndpoint): boolean {
  return ep.kind === 'local' || ep.role === 'volume';
}

function catalogSessionId(cat: Catalog): string | undefined {
  if (cat && typeof cat === 'object' && 'sessionId' in cat) {
    const id = (cat as { sessionId?: string }).sessionId;
    return id || undefined;
  }
  return undefined;
}

/** Exact mount for a sidebar volume row. Must not match another server by display name. */
function lookupMounted(
  ep: RemoteEndpoint,
  mounted: Map<string, FinderMountedVolume>,
): FinderMountedVolume | undefined {
  const direct = mounted.get(ep.id);
  if (direct) return direct;
  for (const m of mounted.values()) {
    if (asShareKind(m.kind) !== ep.kind) continue;
    if (ep.id === mountedEndpointId(m.sessionId) || m.sessionId === ep.id) return m;
    if (m.target === ep.id) {
      if (ep.role === 'volume' && m.volume && ep.title && m.volume !== ep.title) continue;
      return m;
    }
  }
  return undefined;
}

function toEndpoint(v: FinderVolume): RemoteEndpoint & EndpointLocation {
  const isLocal = v.kind === 'local' || v.id.startsWith('local:');
  const protocol = (v.protocol || (isLocal ? '' : v.kind) || '').toLowerCase();
  if (isLocal) {
    return {
      id: v.id,
      kind: 'local',
      title: v.title,
      subtitle: v.subtitle,
      group: GROUP_SHARES,
      badge: SHARE_BADGE[protocol] || protocol.toUpperCase(),
      protocol,
    };
  }
  const kind = asShareKind(v.kind);
  const transport = (v.transport || defaultTransport(kind)).toLowerCase();
  return {
    id: v.id,
    kind,
    title: v.title,
    subtitle: v.subtitle,
    group: remoteGroup(kind),
    badge: TRANSPORT_BADGE[transport] || transport.toUpperCase(),
    protocol: protocol || kind,
    transport,
    address: v.address,
    uri: v.uri,
    os: v.os,
    version: v.version,
  };
}

function sanitizeMountName(name: string): string {
  const cleaned = name.replace(/[/\\:*?"<>|]/g, '_').trim();
  return cleaned || 'ClassicStack';
}

function isAuthError(message: string): boolean {
  const m = message.toLowerCase();
  return (
    m.includes('usernotauth') ||
    m.includes('kfpusernotauth') ||
    m.includes('-5023') ||
    (m.includes('fplogin') &&
      (m.includes('5023') || m.includes('auth') || m.includes('denied') || m.includes('password')))
  );
}

function applyMountCreds(body: Record<string, unknown>, creds: Credentials): void {
  if (creds.kind === 'guest') {
    body.guest = true;
    delete body.user;
    delete body.password;
  } else {
    body.user = creds.username;
    body.password = creds.password;
    delete body.guest;
  }
}

export type GoFinderHostOptions = {
  onConfigureShare?: (ep: RemoteEndpoint) => void;
  onEndpointInfo?: (model: EndpointInfoModel) => void;
};

export class GoFinderHost implements FinderHost {
  private finderAPI = new HttpFinderAPI();
  private login: LoginDialog;
  private alert: AlertDialog;
  private nameConflict: NameConflictDialog;
  private session: FinderSession | null = null;
  private pending: RemoteEndpoint | null = null;
  private lastRemote = new Map<string, FinderVolume[]>();
  private mounted = new Map<string, FinderMountedVolume>();
  private mountedInflight: Promise<FinderMountedVolume[]> | null = null;
  private mountedLoaded = false;
  /** Open Finder sessions keyed by sidebar endpoint id (share or FUSE mount). */
  private sessionsByEndpoint = new Map<string, FinderSession>();
  private catalogsByEndpoint = new Map<string, Catalog>();
  /** Path-opened (or otherwise connected) servers kept in the sidebar. */
  private pinned = new Map<string, RemoteEndpoint>();
  private mountAvailable = false;
  private capsLoaded = false;
  private mountHint = '';
  private defaultMountDir = '/Volumes';
  /** [Client] enablement, refreshed from GET /finder/state. Defaults open so the
   *  sidebar doesn’t flash groups away before the first state load resolves. */
  private clientEnabled = true;
  private enabledServices = new Set<string>(DISCOVER_SCHEMES);
  private clientStateRefreshTimer: ReturnType<typeof setTimeout> | null = null;
  private onConfigureShare?: (ep: RemoteEndpoint) => void;
  private onEndpointInfo?: (model: EndpointInfoModel) => void;
  private onNetworksChange = new Set<() => void>();

  constructor(login: LoginDialog, alert: AlertDialog, nameConflict: NameConflictDialog, opts?: GoFinderHostOptions) {
    this.login = login;
    this.alert = alert;
    this.nameConflict = nameConflict;
    this.onConfigureShare = opts?.onConfigureShare;
    this.onEndpointInfo = opts?.onEndpointInfo;
    telemetry.onFinder.add((ev) => this.onFinderEvent(ev));
    telemetry.onState.add((ev) => this.onStateEvent(ev));
    void this.loadMountCaps();
    void this.loadClientState();
    void this.refreshMounted();
  }

  /** Fires when SSE reports new last-seen servers for a scheme. */
  watchNetworks(cb: () => void): () => void {
    this.onNetworksChange.add(cb);
    return () => this.onNetworksChange.delete(cb);
  }

  private onFinderEvent(ev: FinderEvent): void {
    if (ev.Kind === 'scanning') return;
    if (ev.Kind !== 'networks' || !ev.Scheme) return;
    this.lastRemote.set(ev.Scheme, ev.Volumes ?? []);
    this.onNetworksChange.forEach((cb) => cb());
  }

  private async loadMountCaps(): Promise<void> {
    try {
      const st = await api.finderMountStatus();
      this.mountAvailable = !!st.mountAvailable;
      this.mountHint = st.hint || '';
      if (st.defaultMountDir) this.defaultMountDir = st.defaultMountDir;
    } catch {
      this.mountAvailable = false;
    } finally {
      this.capsLoaded = true;
    }
  }

  /** Re-reads [Client] state (debounced) after the Settings UI reconfigures it, so the
   *  sidebar reflects a services/enabled change without a page reload. Saving from
   *  Settings publishes a StateChanged{Component:"Client", To:"reconfigured"} on the
   *  SSE "state" topic (see compose/supervisor.SetWellKnown / reconfigureKnown). */
  private onStateEvent(ev: unknown): void {
    const component = (ev as { Component?: string } | null)?.Component;
    if (component !== 'Client') return;
    if (this.clientStateRefreshTimer != null) clearTimeout(this.clientStateRefreshTimer);
    this.clientStateRefreshTimer = setTimeout(() => {
      this.clientStateRefreshTimer = null;
      void this.loadClientState();
    }, 150);
  }

  /** Loads [Client] enablement so disabled schemes' sidebar sections stay hidden. */
  private async loadClientState(): Promise<void> {
    try {
      const st = await api.finderState();
      this.clientEnabled = !!st.enabled;
      this.enabledServices = new Set((st.services?.length ? st.services : [...DISCOVER_SCHEMES]).map((s) => s.toLowerCase()));
    } catch {
      /* keep defaults (everything shown) */
    }
    // Drop any last-seen servers cached from before a scheme was turned off, so a
    // re-enable later starts from a fresh scan instead of resurrecting stale ones,
    // and so composeSidebar's own enabled-scheme filter never even has stale rows
    // to filter in the meantime.
    for (const scheme of DISCOVER_SCHEMES) {
      if (!this.schemeEnabled(scheme)) this.lastRemote.delete(scheme);
    }
    this.onNetworksChange.forEach((cb) => cb());
  }

  /** True when scheme's sidebar group/discovery should be offered. */
  private schemeEnabled(scheme: string): boolean {
    return this.clientEnabled && this.enabledServices.has(scheme);
  }

  isConnected(): boolean {
    return true;
  }

  nodeLabel(): string {
    return this.session?.serverName || 'ClassicStack';
  }

  localCatalog(): Catalog | null {
    return null;
  }

  sidebarGroups(): SidebarGroup[] {
    const groups: SidebarGroup[] = [
      { id: GROUP_SHARES, title: 'Shares', hideWhenEmpty: true },
      { id: GROUP_MOUNTED, title: 'Mounted', hideWhenEmpty: true },
      { id: GROUP_APPLETALK, title: 'AppleTalk', refresh: true, empty: 'None' },
      { id: GROUP_SMB, title: 'SMB', refresh: true, empty: 'None' },
      { id: GROUP_NETWARE, title: 'NetWare', refresh: true, empty: 'None' },
      { id: GROUP_ETHERDFS, title: 'EtherDFS', refresh: true, empty: 'None' },
    ];
    return groups.filter((g) => {
      const scheme = SCHEME_FOR_GROUP.get(g.id);
      return !scheme || this.schemeEnabled(scheme);
    });
  }

  sidebarContextMenu(ep: RemoteEndpoint, volume?: string): SidebarAction[] {
    if (ep.kind === 'local') {
      return [{ id: 'configure', label: 'Configure Share…' }];
    }
    const actions: SidebarAction[] = [];
    if (volume || ep.role === 'volume') {
      actions.push({ id: 'info', label: 'Get Info…' });
      if (volume && (this.mountAvailable || !this.capsLoaded)) {
        actions.push({ id: 'mount', label: 'Mount…' });
      }
      return actions;
    }
    actions.push({ id: 'info', label: 'Get Info…' });
    if (ep.kind === 'afp' || ep.kind === 'smb') {
      actions.push({ id: 'message', label: 'Send Message…' });
    }
    if (this.sessionsByEndpoint.has(ep.id)) {
      actions.push({ id: 'disconnect', label: 'Disconnect' });
    }
    if (this.mountAvailable || !this.capsLoaded) {
      actions.push({ id: 'mount', label: 'Mount…' });
    }
    return actions;
  }

  async onSidebarAction(ep: RemoteEndpoint, action: string, volume?: string): Promise<void> {
    if (action === 'configure') {
      this.onConfigureShare?.(ep);
      return;
    }
    if (action === 'info' || action === 'share-info') {
      const sess = this.sessionsByEndpoint.get(ep.id) || (volume ? this.sessionsByEndpoint.get(ep.id) : undefined);
      const mounted = lookupMounted(ep, this.mounted);
      this.onEndpointInfo?.({
        kind: volume || ep.role === 'volume' ? 'share' : 'server',
        endpoint: ep,
        volume: volume || (ep.role === 'volume' ? ep.title : undefined),
        session: sess || (mounted ? toFinderSession(mounted) : null),
        mountpoint: mounted?.mountpoint,
      });
      return;
    }
    if (action === 'message') {
      await this.sendMessage(ep);
      return;
    }
    if (action === 'disconnect') {
      await this.disconnectEndpoint(ep);
      return;
    }
    if (action === 'mount') {
      await this.mountShare(ep, volume);
      return;
    }
    if (action === 'eject' || action === 'unmount') {
      const mounted = this.mounted.get(ep.id);
      if (mounted) {
        if (this.session?.sessionId === mounted.sessionId && this.pending?.role !== 'volume') {
          await this.closeVolume(mounted.volume);
          return;
        }
        await api.finderClose(mounted.sessionId).catch(() => undefined);
        this.mounted.delete(ep.id);
        if (this.session?.sessionId === mounted.sessionId) {
          this.session = null;
          this.pending = null;
        }
        return;
      }
      if (volume) await this.closeVolume(volume);
    }
  }

  async readyMounted(): Promise<void> {
    if (this.mountedLoaded && !this.mountedInflight) return;
    await this.refreshMounted();
  }

  async cachedNetwork(scope?: string): Promise<RemoteEndpoint[]> {
    await this.loadSeen(scope);
    return this.composeSidebar();
  }

  /** Discover schemes worth asking for: in scope and not turned off in [Client]. */
  private activeSchemes(scope?: string): (typeof DISCOVER_SCHEMES)[number][] {
    return schemeForGroup(scope).filter((scheme) => this.schemeEnabled(scheme));
  }

  async refreshNetwork(scope?: string): Promise<RemoteEndpoint[]> {
    const schemes = this.activeSchemes(scope);
    const disabled: string[] = [];
    await Promise.all(
      schemes.map(async (scheme) => {
        try {
          this.lastRemote.set(scheme, await api.finderDiscover(scheme));
        } catch (e) {
          if (e instanceof ApiError && e.status === 403) disabled.push(scheme);
          /* otherwise keep last-seen already in lastRemote */
        }
      }),
    );
    if (disabled.length) {
      const names = disabled.map((s) => SHARE_BADGE[s] || s.toUpperCase()).join(', ');
      this.showAlert(
        'Client disabled',
        `${names} discovery is turned off. Enable it (and the service) under Settings → Client.`,
      );
    }
    return this.composeSidebar();
  }

  private async loadSeen(scope?: string): Promise<void> {
    const schemes = this.activeSchemes(scope);
    await Promise.all(
      schemes.map(async (scheme) => {
        try {
          this.lastRemote.set(scheme, await api.finderSeen(scheme));
        } catch {
          /* keep whatever we already have */
        }
      }),
    );
  }

  private refreshMounted(): Promise<FinderMountedVolume[]> {
    if (this.mountedInflight) return this.mountedInflight;
    this.mountedInflight = (async () => {
      const mounted = (await api.finderMounted().catch(() => [] as FinderMountedVolume[])) ?? [];
      this.mounted.clear();
      for (const m of mounted) {
        this.mounted.set(toMountedEndpoint(m).id, m);
      }
      this.mountedLoaded = true;
      return mounted;
    })().finally(() => {
      this.mountedInflight = null;
    });
    return this.mountedInflight;
  }

  private async composeSidebar(): Promise<RemoteEndpoint[]> {
    const [local, mounted] = await Promise.all([
      api.finderLocal().then((v) => v ?? [] as FinderVolume[]).catch(() => [] as FinderVolume[]),
      this.refreshMounted(),
    ]);
    const remote: FinderVolume[] = [];
    for (const scheme of DISCOVER_SCHEMES) {
      // Skip a disabled scheme even if lastRemote still holds an entry for it (an
      // SSE push can race a settings change) — otherwise its servers would surface
      // miscategorized under whatever sidebar group is still visible, since their
      // own group id no longer matches a rendered group.
      if (!this.schemeEnabled(scheme)) continue;
      remote.push(...(this.lastRemote.get(scheme) ?? []));
    }
    const seen = new Set<string>();
    const out: RemoteEndpoint[] = [];
    for (const m of mounted) {
      if (!m.mountpoint) continue;
      const ep = toMountedEndpoint(m);
      if (seen.has(ep.id)) continue;
      seen.add(ep.id);
      out.push(ep);
    }
    for (const ep of this.pinned.values()) {
      if (seen.has(ep.id)) continue;
      seen.add(ep.id);
      out.push(ep);
    }
    for (const v of [...local, ...remote]) {
      if (seen.has(v.id)) continue;
      seen.add(v.id);
      out.push(toEndpoint(v));
    }
    return out;
  }

  private rememberEndpoint(ep: RemoteEndpoint, info: FinderSession): Catalog {
    this.sessionsByEndpoint.set(ep.id, info);
    const existing = this.catalogsByEndpoint.get(ep.id);
    if (existing && catalogSessionId(existing) === info.sessionId) return existing;
    const cat = this.finderAPI.openCatalog(info);
    this.catalogsByEndpoint.set(ep.id, cat);
    return cat;
  }

  /** Keep a path-opened / connected server in the sidebar after discover refreshes. */
  private pinRemote(ep: RemoteEndpoint, title?: string): void {
    if (ep.kind === 'local' || ep.role === 'volume') return;
    this.pinned.set(ep.id, { ...ep, title: title || ep.title });
  }

  private connectTarget(ep: RemoteEndpoint): string {
    if (/^[a-z][a-z0-9+.-]*:\/\//i.test(ep.id)) return ep.id;
    if (ep.uri) return ep.uri;
    return ep.id;
  }

  private applyLinkFields(body: Record<string, unknown>, ep: RemoteEndpoint): void {
    const t = (ep.transport || '').toLowerCase();
    if (!t) return;
    if (t === 'ltoudp' || t === 'tashtalk' || t === 'pcap' || t === 'tcp' || t === 'tap') {
      body.ifaceType = t;
    } else {
      body.transport = t;
    }
  }

  private sessionInfo(info: FinderSession, ep: RemoteEndpoint): SessionInfo {
    return {
      serverName: info.serverName || ep.title,
      volumes: info.volumes?.length ? info.volumes : info.volume ? [info.volume] : [],
      allowGuest: offersGuest(ep.kind, info.allowGuest),
      uams: info.uams?.length ? info.uams : [],
    };
  }

  async beginRemote(ep: RemoteEndpoint): Promise<SessionInfo> {
    this.pending = ep;
    this.pinRemote(ep);
    if (isVolumeEndpoint(ep)) {
      const mounted = lookupMounted(ep, this.mounted);
      if (mounted) {
        this.session = toFinderSession(mounted);
        this.sessionsByEndpoint.set(ep.id, this.session);
        return {
          serverName: mounted.serverName || ep.title,
          volumes: mounted.volume ? [mounted.volume] : [],
          allowGuest: true,
          uams: [],
        };
      }
    }
    const cached = this.sessionsByEndpoint.get(ep.id);
    if (cached?.sessionId && (cached.rootId || cached.volume || cached.volumes?.length)) {
      this.session = cached;
      return this.sessionInfo(cached, ep);
    }
    try {
      const info = await this.finderAPI.connect?.({
        kind: ep.kind,
        id: ep.id,
        target: this.connectTarget(ep),
        guest: true,
      });
      if (!info) {
        return {
          serverName: ep.title,
          volumes: [],
          allowGuest: offersGuest(ep.kind),
          uams: fallbackAuth(ep.kind),
        };
      }
      this.session = info as FinderSession;
      this.sessionsByEndpoint.set(ep.id, this.session);
      this.pinRemote(ep, this.session.serverName);
      return this.sessionInfo(this.session, ep);
    } catch {
      return {
        serverName: ep.title,
        volumes: [],
        allowGuest: offersGuest(ep.kind),
        uams: fallbackAuth(ep.kind),
      };
    }
  }

  async loginRemote(creds: Credentials): Promise<string[]> {
    const ep = this.pending;
    if (!ep || !this.finderAPI.connect) throw new Error('no remote selected');
    const bound = this.sessionsByEndpoint.get(ep.id);
    if (bound?.sessionId && bound.sessionId === this.session?.sessionId) {
      if (bound.rootId || bound.volume) {
        return bound.volumes?.length ? bound.volumes : bound.volume ? [bound.volume] : [];
      }
      if (creds.kind === 'guest' && bound.volumes?.length) {
        return bound.volumes;
      }
    }
    const body: Record<string, unknown> = {
      kind: ep.kind,
      id: ep.id,
      target: this.connectTarget(ep),
    };
    this.applyLinkFields(body, ep);
    if (creds.kind === 'guest') {
      body.guest = true;
    } else {
      body.user = creds.username;
      body.password = creds.password;
    }
    const info = await this.finderAPI.connect(body as Record<string, never> & { kind: string; id: string });
    this.session = info;
    this.sessionsByEndpoint.set(ep.id, info);
    this.pinRemote(ep, info.serverName);
    return info.volumes ?? [];
  }

  async openVolume(name: string): Promise<Catalog> {
    const ep = this.pending;
    const sess = (ep && this.sessionsByEndpoint.get(ep.id)) || this.session;
    if (!sess?.sessionId) throw new Error('not signed in');
    const info = (await this.finderAPI.openVolume?.(sess.sessionId, name)) as FinderSession;
    this.session = info;
    if (ep) this.sessionsByEndpoint.set(ep.id, info);
    if (ep && isVolumeEndpoint(ep)) {
      return this.rememberEndpoint(ep, info);
    }
    return this.finderAPI.openCatalog(info);
  }

  /**
   * Open a share or FUSE-mounted volume as a catalog without replacing the
   * Finder’s currently viewed session (drag onto another sidebar row).
   */
  async openEndpointCatalog(ep: RemoteEndpoint): Promise<Catalog> {
    const cached = this.catalogsByEndpoint.get(ep.id);
    if (cached) return cached;
    if (isVolumeEndpoint(ep)) {
      const mounted = lookupMounted(ep, this.mounted);
      if (mounted) {
        return this.rememberEndpoint(ep, toFinderSession(mounted));
      }
    }
    const sess = this.sessionsByEndpoint.get(ep.id);
    if (sess?.sessionId) {
      if (sess.rootId || sess.volume) {
        return this.rememberEndpoint(ep, sess);
      }
      if (sess.volumes?.length && this.finderAPI.openVolume) {
        const vol = sess.volume || sess.volumes[0] || ep.title;
        const opened = await this.finderAPI.openVolume(sess.sessionId, vol);
        return this.rememberEndpoint(ep, opened);
      }
    }
    if (!this.finderAPI.connect || !this.finderAPI.openVolume) {
      throw new Error('finder backend cannot open catalogs');
    }
    const info = await this.finderAPI.connect({
      kind: ep.kind,
      id: ep.id,
      target: this.connectTarget(ep),
      guest: true,
    });
    let opened = info;
    if (!info.rootId) {
      const vol = info.volume || info.volumes?.[0];
      if (!vol) {
        throw new Error(`no volumes on “${ep.title}”`);
      }
      opened = await this.finderAPI.openVolume(info.sessionId, vol);
    }
    return this.rememberEndpoint(ep, opened);
  }

  async closeRemote(): Promise<void> {
    const id = this.session?.sessionId;
    const epId = this.pending?.id;
    this.session = null;
    this.pending = null;
    if (epId) {
      this.catalogsByEndpoint.delete(epId);
      this.sessionsByEndpoint.delete(epId);
      this.pinned.delete(epId);
    }
    if (id) {
      await api.finderClose(id).catch(() => undefined);
      this.mounted.delete(mountedEndpointId(id));
    }
  }

  async closeVolume(name: string): Promise<void> {
    const id = this.session?.sessionId;
    if (id) {
      await api.finderCloseVolume(id, name).catch(() => undefined);
    }
    for (const [key, m] of [...this.mounted]) {
      if (m.volume !== name) continue;
      if (id && m.sessionId === id) {
        this.mounted.delete(key);
        continue;
      }
      if (this.session && m.serverName && m.serverName !== this.session.serverName) continue;
      await api.finderClose(m.sessionId).catch(() => undefined);
      this.mounted.delete(key);
    }
  }

  promptCredentials(opts: Parameters<FinderHost['promptCredentials']>[0]): Promise<Credentials | null> {
    return this.login.prompt(opts);
  }

  dismissLogin(): void {
    this.login.close();
  }

  showAlert(title: string, text: string): void {
    this.alert.show(title, text);
  }

  promptNameConflict(opts: { name: string; isDir: boolean; suggestedName: string }): Promise<NameConflictChoice> {
    return this.nameConflict.prompt(opts);
  }

  private async disconnectEndpoint(ep: RemoteEndpoint): Promise<void> {
    const sess = this.sessionsByEndpoint.get(ep.id);
    if (sess?.sessionId) {
      await api.finderClose(sess.sessionId).catch(() => undefined);
    }
    this.sessionsByEndpoint.delete(ep.id);
    this.catalogsByEndpoint.delete(ep.id);
    this.pinned.delete(ep.id);
    if (this.pending?.id === ep.id) {
      this.session = null;
      this.pending = null;
    }
  }

  private async sendMessage(ep: RemoteEndpoint): Promise<void> {
    const text = await promptText('Send Message', `Message to ${ep.title}`);
    if (!text) return;
    try {
      if (ep.kind === 'smb') {
        const to = ep.title.replace(/\s+/g, '').slice(0, 15) || ep.title;
        await api.netSend(to, text);
        this.showAlert('Message sent', `Sent to ${ep.title}.`);
        return;
      }
      if (ep.kind === 'afp') {
        await api.afpMessage(0, text);
        this.showAlert('Message sent', 'AFP clients of this ClassicStack will receive the message.');
        return;
      }
      this.showAlert('Messaging unavailable', `${ep.kind.toUpperCase()} does not support sending messages.`);
    } catch (e) {
      this.showAlert('Send failed', e instanceof Error ? e.message : String(e));
    }
  }

  /** Browse or FUSE session for this server, if the user already signed in. */
  private resolveSessionForMount(ep: RemoteEndpoint): FinderSession | null {
    if (isVolumeEndpoint(ep)) {
      const mounted = lookupMounted(ep, this.mounted);
      if (mounted?.sessionId) return toFinderSession(mounted);
    } else {
      for (const m of this.mounted.values()) {
        if (asShareKind(m.kind) !== ep.kind) continue;
        if (m.target === ep.id || m.sessionId === ep.id) return toFinderSession(m);
      }
    }
    const cached = this.sessionsByEndpoint.get(ep.id);
    if (cached?.sessionId) return cached;
    if (this.pending?.id === ep.id && this.session?.sessionId) return this.session;
    return null;
  }

  private async mountShare(ep: RemoteEndpoint, volume?: string): Promise<void> {
    if (!this.mountAvailable) {
      this.showAlert('Mount unavailable', this.mountHint || 'This binary has no FUSE/WinFsp host.');
      return;
    }
    let vol = (volume || '').trim();
    let existing = this.resolveSessionForMount(ep);
    if (!vol && existing?.volumes?.length === 1) vol = existing.volumes[0] || '';
    if (!vol && existing?.volumes?.length) {
      vol = window.prompt('Volume to mount:', existing.volumes[0] || '')?.trim() || '';
      if (!vol) return;
    }
    if (!vol) {
      vol = window.prompt('Volume to mount:', ep.title)?.trim() || '';
      if (!vol) return;
    }
    const suggested = `${this.defaultMountDir.replace(/[/\\]+$/, '')}/${sanitizeMountName(vol)}`;
    const mountpoint = window.prompt('Mount at:', suggested)?.trim();
    if (!mountpoint) return;

    // Open the volume on an existing browse session so FUSE mount can reuse its AFP
    // login instead of dialing FPLogin again (Mac Classic often rejects a second login).
    if (
      existing?.sessionId &&
      !existing.rootId &&
      !existing.volume &&
      this.finderAPI.openVolume
    ) {
      try {
        const opened = (await this.finderAPI.openVolume(existing.sessionId, vol)) as FinderSession;
        this.sessionsByEndpoint.set(ep.id, opened);
        if (this.session?.sessionId === existing.sessionId) this.session = opened;
        existing = opened;
      } catch (e) {
        const msg = e instanceof Error ? e.message : String(e);
        if (!isAuthError(msg)) {
          this.showAlert('Mount failed', msg);
          return;
        }
        // Fall through: mount without sessionId will prompt for credentials.
        existing = null;
      }
    }

    const body: Record<string, unknown> = {
      kind: ep.kind,
      id: ep.id,
      target: ep.id,
      volume: vol,
      mountpoint,
    };
    let prompted = false;
    if (existing?.sessionId) {
      body.sessionId = existing.sessionId;
    } else {
      const creds = await this.promptCredentials({
        serverName: ep.title,
        kind: ep.kind,
        uams: existing?.uams?.length ? existing.uams : fallbackAuth(ep.kind),
        allowGuest: offersGuest(ep.kind, existing?.allowGuest),
      });
      if (!creds) return;
      prompted = true;
      applyMountCreds(body, creds);
    }

    const runMount = async (authError?: string) => {
      if (authError) {
        delete body.sessionId;
        const creds = await this.promptCredentials({
          serverName: ep.title,
          kind: ep.kind,
          uams: existing?.uams?.length ? existing.uams : fallbackAuth(ep.kind),
          allowGuest: offersGuest(ep.kind, existing?.allowGuest),
          error: authError,
        });
        if (!creds) return null;
        applyMountCreds(body, creds);
      }
      return api.finderMount(body);
    };

    try {
      const info = await runMount();
      if (!info) return;
      await this.refreshMounted();
      this.showAlert('Mounted', `${info.volume} at ${info.mountpoint}`);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      if (!isAuthError(msg)) {
        this.showAlert('Mount failed', msg);
        return;
      }
      try {
        const info = await runMount(
          existing?.sessionId && !prompted
            ? `Login was rejected (${msg}). Enter credentials to try again.`
            : `Login failed (${msg}). Check the username and password.`,
        );
        if (!info) return;
        await this.refreshMounted();
        this.showAlert('Mounted', `${info.volume} at ${info.mountpoint}`);
      } catch (e2) {
        this.showAlert('Mount failed', e2 instanceof Error ? e2.message : String(e2));
      }
    }
  }
}

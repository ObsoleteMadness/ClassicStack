/** JSON/SSE helpers for the HTTP control adapter. */

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

async function errText(r: Response): Promise<string> {
  const j = (await r.json().catch(() => null)) as { error?: string } | null;
  if (j?.error) return j.error;
  return `HTTP ${r.status}`;
}

function refQs(ref: import('classicstack-web/finder').NodeRef): Record<string, string> {
  return typeof ref === 'string' ? { path: ref } : { id: String(ref) };
}

function parentQs(parent: import('classicstack-web/finder').NodeRef): Record<string, string> {
  return typeof parent === 'string' ? { parentPath: parent } : { parent: String(parent) };
}

function refBody(ref: import('classicstack-web/finder').NodeRef): { id?: number; path?: string } {
  return typeof ref === 'string' ? { path: ref } : { id: ref };
}

function parentBody(parent: import('classicstack-web/finder').NodeRef): { parentId?: number; parentPath?: string } {
  return typeof parent === 'string' ? { parentPath: parent } : { parentId: parent };
}

export async function apiJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const r = await fetch(path, {
    ...init,
    headers: {
      ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
      ...init?.headers,
    },
  });
  if (!r.ok) throw new ApiError(r.status, await errText(r));
  if (r.status === 204 || r.headers.get('content-length') === '0') return undefined as T;
  const text = await r.text();
  if (!text) return undefined as T;
  return JSON.parse(text) as T;
}

export async function apiSend(path: string, init?: RequestInit): Promise<void> {
  const r = await fetch(path, {
    ...init,
    headers: {
      ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
      ...init?.headers,
    },
  });
  if (!r.ok) throw new ApiError(r.status, await errText(r));
}

export async function apiProbe(path: string): Promise<{ code: number; body: unknown }> {
  const r = await fetch(path);
  const body = await r.json().catch(() => null);
  return { code: r.status, body };
}

export type Unit = {
  Name: string;
  Kind?: string;
  Enabled?: boolean;
  Running?: boolean;
  Binding?: string;
  DependsOn?: string[];
  Props?: Record<string, string>;
  Error?: string;
};

export type HostInfo = {
  boardName?: string;
  osName?: string;
  hostIp?: string;
  architecture?: string;
  goVersion?: string;
  version?: string;
  gitSha?: string;
  totalMemory?: number;
  freeMemory?: number;
};

export type AuthUser = {
  Name: string;
  Disabled?: boolean;
};

export type FieldInfo = {
  key: string;
  display_name?: string;
  description?: string;
  type: string;
  widget?: string;
  capability?: string;
  secret?: boolean;
};

export type FSParamInfo = {
  key: string;
  required: boolean;
  secret: boolean;
  doc: string;
};

export type ShareBackends = {
  fs_types: string[];
  fork_backends: string[];
  filename_codecs: string[];
  metastores: string[];
  meta_backends: string[];
  fs_params: Record<string, FSParamInfo[]>;
};

export type SectionInfo = {
  key: string;
  repeated?: boolean;
  display_name?: string;
  description?: string;
  capabilities?: string[];
  fields?: FieldInfo[];
};

export type Schemas = {
  singleton: string[];
  repeated: string[];
  sections: SectionInfo[];
};

export type ConfigModel = {
  Identity?: Record<string, unknown>;
  Logging?: Record<string, unknown>;
  HTTP?: Record<string, unknown>;
  Client?: Record<string, unknown>;
  FUSE?: Record<string, unknown>;
  Router?: Record<string, unknown>;
  Interfaces?: Record<string, Record<string, unknown>>;
  Lists?: Record<string, Record<string, unknown>[]>;
  Sections?: Record<string, Record<string, unknown>>;
};

export type InterfaceInfo = {
  Name: string;
  Description?: string;
  Addr?: string;
};

export type SerialPortInfo = {
  device: string;
  label: string;
};

export type AFPSessionInfo = {
  id: number;
  network: number;
  node: number;
  user: string;
  logged_in: boolean;
  last_seen: number;
};

export type SMBSessionInfo = {
  client: string;
  mac: string;
  netbios_name: string;
  user: string;
  dialect: string;
  negotiated_at: number;
  native_os: string;
  native_lanman: string;
  primary_domain: string;
  open_trees: number;
  open_files: number;
};

export type NCPSessionInfo = {
  number: number;
  endpoint: string;
  user: string;
  logged_in: boolean;
  open_files: number;
  last_seen: number;
};

export type EtherDFSSessionInfo = {
  mac: string;
  open_files: number;
  last_seen: number;
};

export type MacIPLeaseInfo = {
  ip: string;
  at_network: number;
  at_node: number;
  source: string;
};

export type BrowsePath = {
  path: string;
  parent: string;
  entries: { name: string; dir: boolean }[];
};

export type FinderVolume = {
  id: string;
  kind: string;
  title: string;
  subtitle?: string;
  protocol?: string;
  transport?: string;
  address?: string;
  uri?: string;
  os?: string;
  version?: string;
  readOnly?: boolean;
};

export type FinderSession = {
  sessionId: string;
  serverName: string;
  kind: string;
  volumes: string[];
  allowGuest: boolean;
  uams?: string[];
  rootId?: number;
  rootPath?: string;
  volume?: string;
  target?: string;
  transport?: string;
  protocol?: string;
  os?: string;
  dialect?: string;
  capabilities?: import('classicstack-web/finder').CatalogCapabilities;
};

export type FinderNode = import('classicstack-web/finder').FinderNodeDto;

export type FinderMountedVolume = {
  sessionId: string;
  kind: string;
  serverName: string;
  volume: string;
  target?: string;
  transport?: string;
  rootId?: number;
  rootPath?: string;
  mountpoint?: string;
  protocol?: string;
  capabilities?: import('classicstack-web/finder').CatalogCapabilities;
};

export type FinderMountInfo = {
  id: string;
  mountpoint: string;
  volume: string;
  kind: string;
  server?: string;
};

export type FinderMountStatus = {
  mountAvailable: boolean;
  defaultMountDir: string;
  hint?: string;
  mounts: FinderMountInfo[];
};

export type FinderClientState = {
  enabled: boolean;
  scanning: boolean;
  mountEnabled: boolean;
  iface?: string;
  services?: string[];
  networks: FinderVolume[];
  connections: FinderSession[];
  volumes: FinderMountedVolume[];
};

export type FinderOpProgress = {
  phase?: 'copying' | 'moving' | 'expanding' | 'listing';
  path?: string;
  bytesDone?: number;
  bytesTotal?: number;
  destName?: string;
  destParentId?: number | string;
  done?: boolean;
  error?: string;
};

export type LogRecord = {
  Time?: string;
  Level?: number;
  Component?: string;
  Msg?: string;
  Fields?: { Key: string; Kind?: number; Value?: unknown; Str?: string; Int?: number; Bool?: boolean }[];
};

export const api = {
  statusProbe: () => apiProbe('status'),
  status: () => apiJSON<Unit[]>('status'),
  hostInfo: () => apiJSON<HostInfo>('host_info'),
  setup: (user: string, password: string) =>
    apiJSON<{ revision: string }>('setup', {
      method: 'POST',
      body: JSON.stringify({ user, password }),
    }),
  action: (verb: 'start' | 'stop' | 'restart', name: string) =>
    apiSend(verb, { method: 'POST', body: JSON.stringify({ Name: name }) }),
  shutdown: () => apiSend('shutdown', { method: 'POST', body: '{}' }),
  stackRestart: () => apiSend('stack_restart', { method: 'POST', body: '{}' }),
  config: () => apiJSON<ConfigModel>('config'),
  reconfigure: (name: string, section: Record<string, unknown>) =>
    apiSend('reconfigure', { method: 'POST', body: JSON.stringify({ name, section }) }),
  setWellKnown: (key: string, section: Record<string, unknown>) =>
    apiSend('set_well_known', { method: 'POST', body: JSON.stringify({ key, section }) }),
  setInterface: (iface: Record<string, unknown>) =>
    apiSend('set_interface', { method: 'POST', body: JSON.stringify(iface) }),
  listInterfaces: () => apiJSON<InterfaceInfo[]>('list_interfaces'),
  serialPorts: () => apiJSON<SerialPortInfo[]>('list_serial_ports'),
  configDownload: () => fetch('config_download').then((r) => (r.ok ? r.text() : Promise.reject(new Error(`HTTP ${r.status}`)))),
  configValidate: (toml: string) =>
    apiJSON<{ ok: boolean }>('config_validate', { method: 'POST', body: toml, headers: { 'Content-Type': 'text/plain' } }),
  configApply: (toml: string) =>
    apiJSON<{ revision: string }>('config_apply', { method: 'POST', body: toml, headers: { 'Content-Type': 'text/plain' } }),
  save: () => apiJSON<{ revision: string }>('save', { method: 'POST', body: '{}' }),
  schemas: () => apiJSON<Schemas>('schemas'),
  fsTypes: () => apiJSON<string[]>('list_fs_types'),
  shareBackends: () => apiJSON<ShareBackends>('share_backends'),
  addInstance: (owner: string, key: string, section: Record<string, unknown>) =>
    apiSend('add_instance', { method: 'POST', body: JSON.stringify({ owner, key, section }) }),
  removeInstance: (owner: string, key: string, name: string) =>
    apiSend('remove_instance', { method: 'POST', body: JSON.stringify({ owner, key, name }) }),
  browsePath: (dir: string) =>
    apiJSON<BrowsePath>('browse_path?dir=' + encodeURIComponent(dir || '')),
  users: async (): Promise<{ unavailable: boolean; list: AuthUser[] }> => {
    const r = await fetch('users');
    if (r.status === 501) return { unavailable: true, list: [] };
    if (!r.ok) throw new ApiError(r.status, await errText(r));
    return { unavailable: false, list: (await r.json()) as AuthUser[] };
  },
  setUser: (name: string, password: string) =>
    apiSend('set_user', { method: 'POST', body: JSON.stringify({ name, password }) }),
  setUserDisabled: (name: string, disabled: boolean) =>
    apiSend('set_user_disabled', { method: 'POST', body: JSON.stringify({ name, disabled }) }),
  removeUser: (name: string) =>
    apiSend('remove_user', { method: 'POST', body: JSON.stringify({ name }) }),

  extMap: (path: string) =>
    apiJSON<{ path: string; content: string }>('extmap?path=' + encodeURIComponent(path)),
  saveExtMap: (path: string, content: string) =>
    apiJSON<{ saved: boolean; backup: string }>('extmap', {
      method: 'POST',
      body: JSON.stringify({ path, content }),
    }),

  finderLocal: () => apiJSON<FinderVolume[]>('finder/local'),
  finderSeen: (scheme?: string) =>
    apiJSON<FinderVolume[]>('finder/discover' + (scheme ? '?scheme=' + encodeURIComponent(scheme) : '')),
  finderState: () => apiJSON<FinderClientState>('finder/state'),
  finderDiscover: (scheme: string) =>
    apiJSON<FinderVolume[]>('finder/discover', {
      method: 'POST',
      body: JSON.stringify({ scheme }),
    }),
  finderConnect: (body: Record<string, unknown>) =>
    apiJSON<FinderSession>('finder/sessions', { method: 'POST', body: JSON.stringify(body) }),
  finderMounted: () => apiJSON<FinderMountedVolume[]>('finder/mounted'),
  finderClose: (id: string) =>
    apiSend('finder/sessions?id=' + encodeURIComponent(id), { method: 'DELETE' }),
  finderOpen: (sessionId: string, volume: string) =>
    apiJSON<FinderSession>('finder/open', {
      method: 'POST',
      body: JSON.stringify({ sessionId, volume }),
    }),
  finderNode: (session: string, ref: import('classicstack-web/finder').NodeRef) => {
    const q = new URLSearchParams({ session, ...refQs(ref) });
    return apiJSON<FinderNode>(`finder/node?${q}`);
  },
  finderChildren: (session: string, ref: import('classicstack-web/finder').NodeRef) => {
    const q = new URLSearchParams({ session, ...refQs(ref) });
    return apiJSON<FinderNode[]>(`finder/children?${q}`);
  },
  finderLookup: async (
    session: string,
    parent: import('classicstack-web/finder').NodeRef,
    name: string,
  ): Promise<FinderNode | null> => {
    const q = new URLSearchParams({ session, name, ...parentQs(parent) });
    const r = await fetch(`finder/lookup?${q}`);
    if (r.status === 404) return null;
    if (!r.ok) throw new ApiError(r.status, await errText(r));
    return (await r.json()) as FinderNode;
  },
  finderMkdir: (sessionId: string, parent: import('classicstack-web/finder').NodeRef, name: string) =>
    apiJSON<FinderNode>('finder/mkdir', {
      method: 'POST',
      body: JSON.stringify({ sessionId, name, ...parentBody(parent) }),
    }),
  finderCreate: (body: Record<string, unknown>) =>
    apiJSON<FinderNode>('finder/create', { method: 'POST', body: JSON.stringify(body) }),
  finderRename: (sessionId: string, ref: import('classicstack-web/finder').NodeRef, name: string) =>
    apiSend('finder/rename', { method: 'POST', body: JSON.stringify({ sessionId, name, ...refBody(ref) }) }),
  finderMove: (
    sessionId: string,
    ref: import('classicstack-web/finder').NodeRef,
    parent: import('classicstack-web/finder').NodeRef,
  ) =>
    apiSend('finder/move', {
      method: 'POST',
      body: JSON.stringify({ sessionId, ...refBody(ref), ...parentBody(parent) }),
    }),
  finderMoveAcross: (body: Record<string, unknown>) =>
    fetch('finder/move', { method: 'POST', body: JSON.stringify(body), headers: { 'Content-Type': 'application/json' } }),
  finderRemove: (sessionId: string, ref: import('classicstack-web/finder').NodeRef) =>
    apiSend('finder/remove', { method: 'POST', body: JSON.stringify({ sessionId, ...refBody(ref) }) }),
  finderFinderInfo: (sessionId: string, ref: import('classicstack-web/finder').NodeRef, finderInfo: string) =>
    apiSend('finder/finderinfo', {
      method: 'PUT',
      body: JSON.stringify({ sessionId, finderInfo, ...refBody(ref) }),
    }),
  finderAttrs: (sessionId: string, ref: import('classicstack-web/finder').NodeRef, attrs: Record<string, boolean>) =>
    apiSend('finder/attrs', {
      method: 'POST',
      body: JSON.stringify({ sessionId, attrs, ...refBody(ref) }),
    }),
  finderResolve: (session: string, path: string) =>
    apiJSON<FinderNode>(`finder/resolve?session=${encodeURIComponent(session)}&path=${encodeURIComponent(path)}`),
  finderPathOf: (session: string, ref: import('classicstack-web/finder').NodeRef) => {
    const q = new URLSearchParams({ session, ...refQs(ref) });
    return apiJSON<{ path: string }>(`finder/path?${q}`);
  },
  finderMountStatus: () => apiJSON<FinderMountStatus>('finder/mount'),
  finderMount: (body: Record<string, unknown>) =>
    apiJSON<FinderMountInfo>('finder/mount', { method: 'POST', body: JSON.stringify(body) }),
  finderCopy: (body: Record<string, unknown>) =>
    fetch('finder/copy', { method: 'POST', body: JSON.stringify(body), headers: { 'Content-Type': 'application/json' } }),
  finderExpand: (body: Record<string, unknown>) =>
    fetch('finder/expand', { method: 'POST', body: JSON.stringify(body), headers: { 'Content-Type': 'application/json' } }),
  finderUnmount: (id: string) =>
    apiSend('finder/mount?id=' + encodeURIComponent(id), { method: 'DELETE' }),
  finderCloseVolume: (sessionId: string, volume: string) =>
    apiSend(
      'finder/open?session=' + encodeURIComponent(sessionId) + '&volume=' + encodeURIComponent(volume),
      { method: 'DELETE' },
    ),

  listZones: () => apiJSON<string[]>('list_zones'),
  macipLeases: () => apiJSON<MacIPLeaseInfo[]>('macip_leases'),
  smbSessions: () => apiJSON<SMBSessionInfo[]>('smb_sessions'),
  afpSessions: () => apiJSON<AFPSessionInfo[]>('afp_sessions'),
  ncpSessions: () => apiJSON<NCPSessionInfo[]>('ncp_sessions'),
  etherdfsSessions: () => apiJSON<EtherDFSSessionInfo[]>('etherdfs_sessions'),
  afpMessage: (sessionId: number, text: string) =>
    apiSend('afp_message', { method: 'POST', body: JSON.stringify({ session_id: sessionId, text }) }),
  afpDisconnect: (sessionId: number, text: string, minutes = 0) =>
    apiSend('afp_disconnect', {
      method: 'POST',
      body: JSON.stringify({ session_id: sessionId, text, minutes }),
    }),
  netSend: (to: string, text: string) =>
    apiSend('netsend', { method: 'POST', body: JSON.stringify({ to, text }) }),
};

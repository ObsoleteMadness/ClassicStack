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
  secret?: boolean;
};

export type SectionInfo = {
  key: string;
  repeated?: boolean;
  display_name?: string;
  description?: string;
  fields?: FieldInfo[];
};

export type Schemas = {
  singleton: string[];
  repeated: string[];
  sections: SectionInfo[];
};

export type ConfigModel = {
  Lists?: Record<string, Record<string, unknown>[]>;
  Sections?: Record<string, Record<string, unknown>>;
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
};

export type FinderNode = {
  id: number;
  parentId: number;
  name: string;
  isDir: boolean;
  dataBytes?: number;
  resourceBytes?: number;
  finderInfo?: string;
  createDate?: number;
  modDate?: number;
};

export type LogRecord = {
  Time?: string;
  Level?: number;
  Component?: string;
  Msg?: string;
  Fields?: { Key: string; Value?: unknown; Str?: string; Int?: number }[];
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
  config: () => apiJSON<ConfigModel>('config'),
  save: () => apiJSON<{ revision: string }>('save', { method: 'POST', body: '{}' }),
  schemas: () => apiJSON<Schemas>('schemas'),
  fsTypes: () => apiJSON<string[]>('list_fs_types'),
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
  finderDiscover: (scheme: string) =>
    apiJSON<FinderVolume[]>('finder/discover', {
      method: 'POST',
      body: JSON.stringify({ scheme }),
    }),
  finderConnect: (body: Record<string, unknown>) =>
    apiJSON<FinderSession>('finder/sessions', { method: 'POST', body: JSON.stringify(body) }),
  finderClose: (id: string) =>
    apiSend('finder/sessions?id=' + encodeURIComponent(id), { method: 'DELETE' }),
  finderOpen: (sessionId: string, volume: string) =>
    apiJSON<FinderSession>('finder/open', {
      method: 'POST',
      body: JSON.stringify({ sessionId, volume }),
    }),
  finderNode: (session: string, id: number) =>
    apiJSON<FinderNode>(`finder/node?session=${encodeURIComponent(session)}&id=${id}`),
  finderChildren: (session: string, id: number) =>
    apiJSON<FinderNode[]>(`finder/children?session=${encodeURIComponent(session)}&id=${id}`),
  finderLookup: async (session: string, parent: number, name: string): Promise<FinderNode | null> => {
    const q = `session=${encodeURIComponent(session)}&parent=${parent}&name=${encodeURIComponent(name)}`;
    const r = await fetch(`finder/lookup?${q}`);
    if (r.status === 404) return null;
    if (!r.ok) throw new ApiError(r.status, await errText(r));
    return (await r.json()) as FinderNode;
  },
  finderMkdir: (sessionId: string, parentId: number, name: string) =>
    apiJSON<FinderNode>('finder/mkdir', {
      method: 'POST',
      body: JSON.stringify({ sessionId, parentId, name }),
    }),
  finderCreate: (body: Record<string, unknown>) =>
    apiJSON<FinderNode>('finder/create', { method: 'POST', body: JSON.stringify(body) }),
  finderRename: (sessionId: string, id: number, name: string) =>
    apiSend('finder/rename', { method: 'POST', body: JSON.stringify({ sessionId, id, name }) }),
  finderMove: (sessionId: string, id: number, parentId: number) =>
    apiSend('finder/move', { method: 'POST', body: JSON.stringify({ sessionId, id, parentId }) }),
  finderRemove: (sessionId: string, id: number) =>
    apiSend('finder/remove', { method: 'POST', body: JSON.stringify({ sessionId, id }) }),
  finderFinderInfo: (sessionId: string, id: number, finderInfo: string) =>
    apiSend('finder/finderinfo', {
      method: 'PUT',
      body: JSON.stringify({ sessionId, id, finderInfo }),
    }),
};

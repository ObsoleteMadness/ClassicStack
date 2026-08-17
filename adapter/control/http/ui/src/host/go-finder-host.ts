/** FinderHost over ClassicStack’s /finder HTTP API (no in-browser AFP/TashTalk). */

import type {
  Credentials,
  FinderHost,
  RemoteEndpoint,
  SessionInfo,
  ShareKind,
  SidebarGroup,
} from 'classicstack-web/ui/finder-host';
import type { Catalog } from 'classicstack-web/fs/virtual-fs';
import type { NameConflictChoice } from 'classicstack-web/fs/name-conflict';
import type { LoginDialog } from 'classicstack-web/ui/login-dialog';
import type { AlertDialog } from 'classicstack-web/ui/alert-dialog';
import type { NameConflictDialog } from 'classicstack-web/ui/name-conflict-dialog';
import { api, type FinderSession, type FinderVolume } from '../api';
import { HttpCatalog } from '../fs/http-catalog';

const DISCOVER_SCHEMES = ['afp', 'smb', 'ncp', 'etherdfs'] as const;
const REMOTE_UAMS = ['No User Authent', 'Cleartxt Passwrd'];

const GROUP_SHARES = 'shares';
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
  ddp: 'DDP',
  ipx: 'IPX',
  nbp: 'NBP',
  etherdfs: 'EDFS',
};

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
      return 'nbp';
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

function toEndpoint(v: FinderVolume): RemoteEndpoint {
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
  };
}

export class GoFinderHost implements FinderHost {
  private login: LoginDialog;
  private alert: AlertDialog;
  private nameConflict: NameConflictDialog;
  private session: FinderSession | null = null;
  private pending: RemoteEndpoint | null = null;

  constructor(login: LoginDialog, alert: AlertDialog, nameConflict: NameConflictDialog) {
    this.login = login;
    this.alert = alert;
    this.nameConflict = nameConflict;
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
    return [
      { id: GROUP_SHARES, title: 'Shares', hideWhenEmpty: true },
      { id: GROUP_APPLETALK, title: 'AppleTalk', refresh: true, empty: 'None' },
      { id: GROUP_SMB, title: 'SMB', empty: 'None' },
      { id: GROUP_NETWARE, title: 'NetWare', empty: 'None' },
      { id: GROUP_ETHERDFS, title: 'EtherDFS', empty: 'None' },
    ];
  }

  async refreshNetwork(): Promise<RemoteEndpoint[]> {
    const local = await api.finderLocal().catch(() => [] as FinderVolume[]);
    const remote: FinderVolume[] = [];
    await Promise.all(
      DISCOVER_SCHEMES.map(async (scheme) => {
        try {
          remote.push(...(await api.finderDiscover(scheme)));
        } catch {
          /* scheme unavailable on this host / empty LAN */
        }
      }),
    );
    const seen = new Set<string>();
    const out: RemoteEndpoint[] = [];
    for (const v of [...local, ...remote]) {
      if (seen.has(v.id)) continue;
      seen.add(v.id);
      out.push(toEndpoint(v));
    }
    return out;
  }

  async beginRemote(ep: RemoteEndpoint): Promise<SessionInfo> {
    await this.closeRemote().catch(() => undefined);
    this.pending = ep;
    if (ep.kind === 'local') {
      const info = await api.finderConnect({ kind: 'local', id: ep.id });
      this.session = info;
      this.pending = ep;
      return {
        serverName: info.serverName || ep.title,
        volumes: info.volumes ?? [],
        allowGuest: true,
        uams: [],
      };
    }
    return {
      serverName: ep.title,
      volumes: [],
      allowGuest: true,
      uams: [...REMOTE_UAMS],
    };
  }

  async loginRemote(creds: Credentials): Promise<string[]> {
    if (this.session?.kind === 'local') {
      return this.session.volumes ?? [];
    }
    const ep = this.pending;
    if (!ep) throw new Error('no remote selected');
    const body: Record<string, unknown> = {
      kind: ep.kind,
      id: ep.id,
      target: ep.id,
    };
    if (creds.kind === 'guest') {
      body.guest = true;
    } else {
      body.user = creds.username;
      body.password = creds.password;
    }
    const info = await api.finderConnect(body);
    this.session = info;
    return info.volumes ?? [];
  }

  async openVolume(name: string): Promise<Catalog> {
    if (!this.session) throw new Error('not signed in');
    const info = await api.finderOpen(this.session.sessionId, name);
    this.session = info;
    return new HttpCatalog(info.sessionId, info.rootId);
  }

  async closeRemote(): Promise<void> {
    const id = this.session?.sessionId;
    this.session = null;
    this.pending = null;
    if (id) await api.finderClose(id).catch(() => undefined);
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
}

/**
 * ClassicStack file-client URI grammar (same as csfs / csclient / csmount).
 *
 *   <scheme>://[[user][:pass]@]<server>[,<transport>]/<volume>[/<path>]
 *
 * Mirrors client/uri.Parse — keep this in lockstep with that package.
 */

import type { Credentials, RemoteEndpoint, ShareKind } from 'classicstack-web/ui/finder-host';

const SCHEMES = new Set<ShareKind>(['afp', 'smb', 'ncp', 'etherdfs']);

export type ClientTarget = {
  scheme: ShareKind;
  user: string;
  pass: string;
  hasCreds: boolean;
  server: string;
  transport: string;
  volume: string;
  path: string;
};

export class ClientURIError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ClientURIError';
  }
}

/** Parse a csclient / csfs URI. Throws ClientURIError on a malformed value. */
export function parseClientURI(raw: string): ClientTarget {
  const input = raw.trim();
  if (!input) throw new ClientURIError('URI is empty');

  const schemeSep = input.indexOf('://');
  if (schemeSep <= 0) throw new ClientURIError('URI must start with a scheme:// prefix (afp, smb, ncp, etherdfs)');
  const schemeRaw = input.slice(0, schemeSep).toLowerCase();
  if (!SCHEMES.has(schemeRaw as ShareKind)) {
    throw new ClientURIError(`Unknown scheme “${schemeRaw}”; use afp, smb, ncp, or etherdfs`);
  }
  const scheme = schemeRaw as ShareKind;
  const rest = input.slice(schemeSep + 3);

  const slash = rest.indexOf('/');
  const authority = slash >= 0 ? rest.slice(0, slash) : rest;
  const pathPart = slash >= 0 ? rest.slice(slash + 1) : '';

  let serverAndTransport = authority;
  let user = '';
  let pass = '';
  let hasCreds = false;
  const at = authority.lastIndexOf('@');
  if (at >= 0) {
    hasCreds = true;
    const creds = authority.slice(0, at);
    serverAndTransport = authority.slice(at + 1);
    const colon = creds.indexOf(':');
    if (colon >= 0) {
      user = creds.slice(0, colon);
      pass = creds.slice(colon + 1);
    } else {
      user = creds;
    }
  }

  let server = serverAndTransport;
  let transport = '';
  const comma = serverAndTransport.lastIndexOf(',');
  if (comma >= 0) {
    server = serverAndTransport.slice(0, comma);
    transport = serverAndTransport.slice(comma + 1).toLowerCase();
  }
  if (!server) throw new ClientURIError('URI has an empty server');

  let volume = '';
  let path = '';
  if (slash >= 0) {
    const cut = pathPart.indexOf('/');
    if (cut >= 0) {
      volume = pathPart.slice(0, cut);
      path = pathPart.slice(cut + 1).replace(/^\/+|\/+$/g, '');
    } else {
      volume = pathPart;
    }
  }

  return { scheme, user, pass, hasCreds, server, transport, volume, path };
}

/** Server URI with no credentials, volume, or trailing slash (Finder `uri` field). */
export function serverURI(t: ClientTarget): string {
  let s = `${t.scheme}://${t.server}`;
  if (t.transport) s += `,${t.transport}`;
  return s;
}

export function credentialsFromTarget(t: ClientTarget): Credentials | undefined {
  if (!t.hasCreds) return undefined;
  if (!t.user && !t.pass) return { kind: 'guest' };
  return { kind: 'password', username: t.user, password: t.pass };
}

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

function defaultTransport(kind: ShareKind): string {
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

function remoteGroup(kind: ShareKind): string {
  switch (kind) {
    case 'smb':
      return 'smb';
    case 'ncp':
      return 'netware';
    case 'etherdfs':
      return 'etherdfs';
    default:
      return 'appletalk';
  }
}

function afpTitle(server: string): { title: string; subtitle?: string } {
  const colon = server.lastIndexOf(':');
  if (colon <= 0) return { title: server };
  return { title: server.slice(0, colon), subtitle: server.slice(colon + 1) };
}

/** Sidebar endpoint for a URI that is not already in the discovered list. */
export function endpointFromTarget(t: ClientTarget): RemoteEndpoint {
  const transport = (t.transport || defaultTransport(t.scheme)).toLowerCase();
  const names = t.scheme === 'afp' ? afpTitle(t.server) : { title: t.server };
  const uri = serverURI(t);
  return {
    id: uri,
    kind: t.scheme,
    title: names.title,
    subtitle: names.subtitle,
    group: remoteGroup(t.scheme),
    badge: TRANSPORT_BADGE[transport] || transport.toUpperCase(),
    protocol: t.scheme,
    transport,
    uri,
  };
}

function stripSlash(s: string): string {
  return s.replace(/\/+$/, '').toLowerCase();
}

/** Prefer a discovered server over a synthetic URI row when they are the same host. */
export function matchEndpoint(list: readonly RemoteEndpoint[], t: ClientTarget): RemoteEndpoint | undefined {
  const want = stripSlash(serverURI(t));
  const wantNoTransport = stripSlash(`${t.scheme}://${t.server}`);
  const object = t.scheme === 'afp' ? afpTitle(t.server) : { title: t.server };
  const zone = (object.subtitle || '').toLowerCase();
  const name = object.title.toLowerCase();

  const byURI = list.find((ep) => {
    if (ep.kind !== t.scheme) return false;
    const id = stripSlash(ep.id);
    const uri = stripSlash(ep.uri || '');
    if (uri === want || id === want) return true;
    if (uri === wantNoTransport || id === wantNoTransport) return true;
    if (id.startsWith(`${wantNoTransport},`)) return true;
    return false;
  });
  if (byURI) return byURI;

  return list.find((ep) => {
    if (ep.kind !== t.scheme) return false;
    if ((ep.title || '').toLowerCase() !== name) return false;
    if (zone && (ep.subtitle || '').toLowerCase() !== zone) return false;
    return true;
  });
}

import { api, type ConfigModel, type Unit } from '../api';
import { btn, el } from './dom';
import { mountFloatingWindow, raise } from './floating-window';
import { telemetry } from '../telemetry';

import iconAFP from '../icons/AppleShare8.png';
import iconEtherDFS from '../icons/etherdfs.png';
import iconFileSharing from '../icons/filesharing8.png';
import iconLToUDP from '../icons/ltoudp1.png';
import iconMacs from '../icons/macs.png';
import iconNCP from '../icons/netware.png';
import iconNetwork from '../icons/network8.png';
import iconPC from '../icons/pc8.png';
import iconPCMono from '../icons/pc1.png';
import iconRouter from '../icons/router.png';
import iconSharing from '../icons/sharing.png';
import iconSMB from '../icons/smb.png';
import iconTashTalk from '../icons/tashtalk.png';
import iconUsers from '../icons/users8.png';

type NodeState = 'neutral' | 'enabled' | 'running' | 'disabled';

type DagNode = {
  id: string;
  title: string;
  subtitle: string;
  icon: string;
  x: number;
  y: number;
  state: NodeState;
  configKey: string;
  shareProtocol?: string;
  clickable: boolean;
};

type DagEdge = { from: string; to: string; bidirectional?: boolean };

type TopologyOptions = { openSharing: (protocol: string) => void };

type ServiceConnections = { afp: number | null; smb: number | null; ncp: number | null; etherdfs: number | null };

type RouterNetworks = { ethertalk: string | null; ltoudp: string | null; tashtalk: string | null };

type RenderModel = {
  nodes: DagNode[];
  edges: DagEdge[];
  networks: RouterNetworks;
  ifaceName: string;
  routerMembers: string[];
};

type LayoutStore = Record<string, { x: number; y: number }>;

const SVG_NS = 'http://www.w3.org/2000/svg';
const LAYOUT_KEY = 'classicstack.topology.layout.v2';
const REFRESH_MS = 3500;
const DRAG_THRESHOLD = 5;
const GRID = 8;

const ICON = 64;
const WELL_PAD = 8;
const WELL = ICON + WELL_PAD * 2;
const NODE_W = 112;
const NODE_H = WELL + 40;
const COL_GAP = 168;
const ROW_GAP = 140;
const ORIGIN_X = 36;
const ORIGIN_Y = 28;
const MIN_W = 880;
const MIN_H = 460;

const NODE_ICONS: Record<string, string> = {
  lan: iconNetwork,
  host: iconMacs,
  router: iconRouter,
  ethertalk: iconSharing,
  nbf: iconPC,
  ipxgw: iconFileSharing,
  ltoudp: iconLToUDP,
  tashtalk: iconTashTalk,
  mactcp: iconUsers,
  ipx: iconSharing,
  tcp: iconPCMono,
  afp: iconAFP,
  smb: iconSMB,
  ncp: iconNCP,
  etherdfs: iconEtherDFS,
};

const COLUMN: Record<string, number> = {
  lan: 0,
  host: 0,
  ethertalk: 1,
  router: 1,
  nbf: 1,
  tashtalk: 2,
  ltoudp: 2,
  mactcp: 2,
  ipxgw: 2,
  ipx: 3,
  tcp: 3,
  afp: 4,
  smb: 4,
  ncp: 4,
  etherdfs: 4,
};

const COLUMN_ORDER: Record<number, string[]> = {
  0: ['lan', 'host'],
  1: ['ethertalk', 'router', 'nbf'],
  2: ['tashtalk', 'ltoudp', 'mactcp', 'ipxgw'],
  3: ['ipx', 'tcp'],
  4: ['afp', 'smb', 'ncp', 'etherdfs'],
};

function lower(value: string): string {
  return value.toLowerCase();
}

function asObject(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null;
  return value as Record<string, unknown>;
}

function asString(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

function asStringList(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.map((v) => String(v).trim()).filter(Boolean);
}

function asBool(value: unknown, fallback = false): boolean {
  if (typeof value === 'boolean') return value;
  return fallback;
}

function asNumber(value: unknown): number | null {
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string') {
    const n = Number(value);
    return Number.isFinite(n) ? n : null;
  }
  return null;
}

function listByKey(model: ConfigModel, key: string): Record<string, unknown>[] {
  const lists = model.Lists || {};
  const wanted = lower(key);
  for (const [k, v] of Object.entries(lists)) {
    if (lower(k) === wanted && Array.isArray(v)) return v;
  }
  return [];
}

function sectionByKey(model: ConfigModel, key: string): Record<string, unknown> | null {
  const sections = model.Sections || {};
  const wanted = lower(key);
  for (const [k, v] of Object.entries(sections)) {
    if (lower(k) === wanted) return asObject(v);
  }
  const top = asObject((model as Record<string, unknown>)[key]);
  if (top) return top;
  for (const [k, v] of Object.entries(model as Record<string, unknown>)) {
    if (lower(k) === wanted) return asObject(v);
  }
  return null;
}

function pickIfaceName(model: ConfigModel): string {
  const client = sectionByKey(model, 'Client');
  const clientIface = asString(client?.Iface).trim();
  if (clientIface) return clientIface;
  const names = Object.entries(model.Interfaces || {})
    .filter(([, v]) => !!v)
    .map(([name]) => name);
  if (names.length === 1) return names[0];
  for (const [name, raw] of Object.entries(model.Interfaces || {})) {
    const iface = asObject(raw);
    if (asBool(iface?.Default)) return name;
  }
  return names[0] || 'lan';
}

function ifaceBackend(model: ConfigModel, ifaceName: string): string {
  const ifc = asObject((model.Interfaces || {})[ifaceName]);
  return asString(ifc?.Backend).trim() || 'pcap';
}

function unitMap(units: Unit[]): Map<string, Unit> {
  const map = new Map<string, Unit>();
  for (const u of units) map.set(lower(u.Name), u);
  return map;
}

function stateFromUnit(units: Map<string, Unit>, name: string): NodeState {
  const u = units.get(lower(name));
  if (!u) return 'neutral';
  if (u.Running) return 'running';
  if (u.Enabled) return 'enabled';
  return 'disabled';
}

function hasUnit(units: Map<string, Unit>, name: string): boolean {
  return units.has(lower(name));
}

function sectionEnabled(model: ConfigModel, key: string): boolean {
  return asBool(sectionByKey(model, key)?.Enabled, false);
}

function sectionStringList(model: ConfigModel, key: string, field: string): string[] {
  return asStringList(sectionByKey(model, key)?.[field]).map((s) => lower(s));
}

function bindsTransport(list: string[], token: string): boolean {
  return list.length === 0 || list.includes(token);
}

function networkRange(instances: Record<string, unknown>[]): string | null {
  if (!instances.length) return null;
  const parts: string[] = [];
  for (const inst of instances) {
    const start = asNumber(inst.SeedNetwork ?? inst.seed_network);
    const end = asNumber(inst.SeedNetworkEnd ?? inst.seed_network_end);
    if (start == null) continue;
    parts.push(end != null && end >= start ? `${start}-${end}` : String(start));
  }
  return parts.length ? parts.join(', ') : null;
}

function readRates(stats: Record<string, unknown>): Record<string, number> {
  const raw = (stats.Rates || stats.rates) as Record<string, unknown> | undefined;
  if (!raw) return {};
  const out: Record<string, number> = {};
  for (const [k, v] of Object.entries(raw)) {
    const n = asNumber(v);
    if (n != null) out[k] = n;
  }
  return out;
}

function rateForInterface(units: Unit[], ifaceName: string): number {
  let total = 0;
  for (const u of units) {
    if ((u.Binding || '').trim() !== ifaceName) continue;
    const st = telemetry.stats[u.Name] || telemetry.stats[lower(u.Name)] || {};
    const rates = readRates(st);
    total += (rates.bytes_rx || 0) + (rates.bytes_tx || 0);
  }
  return total;
}

function fmtRate(bytesPerSec: number): string {
  if (bytesPerSec <= 0) return '0 B/s';
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
  let n = bytesPerSec;
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  const d = n >= 100 ? 0 : n >= 10 ? 1 : 2;
  return `${n.toFixed(d)} ${units[i]}`;
}

function gauge(stats: Record<string, unknown>, key: string): number | null {
  const gauges = (stats.Gauges || stats.gauges) as Record<string, unknown> | undefined;
  if (!gauges) return null;
  return asNumber(gauges[key]);
}

function connLabel(n: number | null): string {
  return `connections: ${n == null ? '?' : n}`;
}

async function serviceConnections(): Promise<ServiceConnections> {
  const [afp, smb] = await Promise.all([
    api.afpSessions().then((rows) => rows.length).catch(() => null),
    api.smbSessions().then((rows) => rows.length).catch(() => null),
  ]);
  const ncpStats = telemetry.stats.NCP || telemetry.stats.ncp || {};
  const etherdfsStats = telemetry.stats.EtherDFS || telemetry.stats.etherdfs || {};
  const ncpGauge = gauge(ncpStats, 'connected_machines');
  const etherdfsGauge = gauge(etherdfsStats, 'sessions');
  return {
    afp,
    smb,
    ncp: ncpGauge == null ? null : Math.max(0, Math.floor(ncpGauge)),
    etherdfs: etherdfsGauge == null ? null : Math.max(0, Math.floor(etherdfsGauge)),
  };
}

function loadLayout(): LayoutStore {
  try {
    const raw = localStorage.getItem(LAYOUT_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as LayoutStore;
    if (!parsed || typeof parsed !== 'object') return {};
    return parsed;
  } catch {
    return {};
  }
}

function saveLayout(store: LayoutStore): void {
  try {
    localStorage.setItem(LAYOUT_KEY, JSON.stringify(store));
  } catch {
    /* quota / private mode */
  }
}

function autoLayout(ids: string[]): LayoutStore {
  const present = new Set(ids);
  const usedCols = [...new Set(ids.map((id) => COLUMN[id] ?? 4))].sort((a, b) => a - b);
  const colIndex = new Map<number, number>();
  usedCols.forEach((c, i) => colIndex.set(c, i));
  const out: LayoutStore = {};
  for (const col of usedCols) {
    const order = COLUMN_ORDER[col] || [];
    const members = order.filter((id) => present.has(id));
    members.forEach((id, row) => {
      out[id] = {
        x: ORIGIN_X + (colIndex.get(col) || 0) * COL_GAP,
        y: ORIGIN_Y + row * ROW_GAP,
      };
    });
  }
  for (const id of ids) {
    if (!out[id]) out[id] = { x: ORIGIN_X + 4 * COL_GAP, y: ORIGIN_Y };
  }
  return out;
}

function placeNodes(nodes: DagNode[], saved: LayoutStore): void {
  const auto = autoLayout(nodes.map((n) => n.id));
  for (const node of nodes) {
    const pos = saved[node.id] || auto[node.id];
    node.x = pos.x;
    node.y = pos.y;
  }
}

function addNode(nodes: DagNode[], node: Omit<DagNode, 'icon' | 'x' | 'y'> & { x?: number; y?: number }): void {
  nodes.push({
    ...node,
    icon: NODE_ICONS[node.id] || iconNetwork,
    x: node.x ?? 0,
    y: node.y ?? 0,
  });
}

function buildDag(model: ConfigModel, units: Unit[], counts: ServiceConnections): RenderModel {
  const unitsByName = unitMap(units);
  const smbTransports = sectionStringList(model, 'SMB', 'Transports');
  const afpTransports = sectionStringList(model, 'AFP', 'Transports');
  const clientServices = sectionStringList(model, 'Client', 'Services');
  const routerMembersRaw = asStringList(sectionByKey(model, 'Router')?.Members);
  const routerMembers = routerMembersRaw.length ? routerMembersRaw : ['EtherTalk', 'LToUDP', 'MacIP', 'IPXGW'];

  const ethTalkNet = networkRange(listByKey(model, 'EtherTalk'));
  const ltoUDPNet = networkRange(listByKey(model, 'LToUDP'));
  const tashNet = networkRange(listByKey(model, 'TashTalk'));

  const hasSMB = hasUnit(unitsByName, 'SMB');
  const hasNCP = hasUnit(unitsByName, 'NCP');
  const hasAFP = hasUnit(unitsByName, 'AFP');
  const hasEtherDFS = hasUnit(unitsByName, 'EtherDFS') || clientServices.includes('etherdfs');
  const hasNBF =
    hasUnit(unitsByName, 'NetBEUI') ||
    hasUnit(unitsByName, 'NetBIOS') ||
    smbTransports.includes('netbeui') ||
    sectionStringList(model, 'NetBIOS', 'Transports').includes('netbeui');
  const hasIPX = hasUnit(unitsByName, 'IPX') || hasUnit(unitsByName, 'IPXGW') || smbTransports.includes('ipx') || hasNCP;
  const hasIPXGW = hasUnit(unitsByName, 'IPXGW') || hasIPX;
  const hasMacIP = hasUnit(unitsByName, 'MacIP') || sectionEnabled(model, 'MacIP');
  const hasLToUDP = hasUnit(unitsByName, 'LToUDP') || listByKey(model, 'LToUDP').length > 0;
  const hasTashTalk = hasUnit(unitsByName, 'TashTalk') || listByKey(model, 'TashTalk').length > 0;
  const hasEtherTalk = hasUnit(unitsByName, 'EtherTalk') || listByKey(model, 'EtherTalk').length > 0;
  const afpTCP = hasAFP && bindsTransport(afpTransports, 'tcp');
  const afpDDP = hasAFP && bindsTransport(afpTransports, 'ddp');
  const smbTCP = hasSMB && (bindsTransport(smbTransports, 'tcp') || bindsTransport(smbTransports, 'nbt'));
  const hasTCP = hasUnit(unitsByName, 'SMB-TCP') || afpTCP || smbTCP;
  const hasRouter = hasUnit(unitsByName, 'Router') || hasEtherTalk || hasLToUDP || hasTashTalk || hasIPXGW || hasMacIP;

  const nodes: DagNode[] = [];
  const edges: DagEdge[] = [];
  const ifaceName = pickIfaceName(model);

  addNode(nodes, {
    id: 'lan',
    title: ifaceName,
    subtitle: `${ifaceBackend(model, ifaceName)} · ${fmtRate(rateForInterface(units, ifaceName))}`,
    state: 'neutral',
    configKey: 'Interface',
    clickable: true,
  });
  addNode(nodes, {
    id: 'host',
    title: 'Host',
    subtitle: 'client stack',
    state: 'neutral',
    configKey: 'Client',
    clickable: true,
  });

  if (hasRouter) {
    const netParts = [ethTalkNet, ltoUDPNet, tashNet].filter((v): v is string => !!v);
    addNode(nodes, {
      id: 'router',
      title: 'AppleTalk Router',
      subtitle: netParts.length ? `nets ${netParts.join(' | ')}` : 'router',
      state: stateFromUnit(unitsByName, 'Router'),
      configKey: 'Router',
      clickable: true,
    });
  }
  if (hasEtherTalk) {
    addNode(nodes, {
      id: 'ethertalk',
      title: 'EtherTalk',
      subtitle: ethTalkNet ? `net ${ethTalkNet}` : 'bridge segment',
      state: stateFromUnit(unitsByName, 'EtherTalk'),
      configKey: 'EtherTalk',
      clickable: true,
    });
  }
  if (hasNBF) {
    addNode(nodes, {
      id: 'nbf',
      title: 'NBF',
      subtitle: 'NetBEUI',
      state: stateFromUnit(unitsByName, 'NetBEUI'),
      configKey: 'NetBEUI',
      clickable: true,
    });
  }
  if (hasTashTalk) {
    addNode(nodes, {
      id: 'tashtalk',
      title: 'TashTalk',
      subtitle: tashNet ? `net ${tashNet}` : 'serial LocalTalk',
      state: stateFromUnit(unitsByName, 'TashTalk'),
      configKey: 'TashTalk',
      clickable: true,
    });
  }
  if (hasLToUDP) {
    addNode(nodes, {
      id: 'ltoudp',
      title: 'LToUDP',
      subtitle: ltoUDPNet ? `net ${ltoUDPNet}` : 'UDP LocalTalk',
      state: stateFromUnit(unitsByName, 'LToUDP'),
      configKey: 'LToUDP',
      clickable: true,
    });
  }
  if (hasMacIP) {
    addNode(nodes, {
      id: 'mactcp',
      title: 'MacIP',
      subtitle: 'IP gateway',
      state: stateFromUnit(unitsByName, 'MacIP'),
      configKey: 'MacIP',
      clickable: true,
    });
  }
  if (hasIPXGW) {
    addNode(nodes, {
      id: 'ipxgw',
      title: 'IPXGW',
      subtitle: 'MacIPX gateway',
      state: stateFromUnit(unitsByName, 'IPXGW'),
      configKey: 'IPXGW',
      clickable: true,
    });
  }
  if (hasIPX) {
    addNode(nodes, {
      id: 'ipx',
      title: 'IPX',
      subtitle: 'from IPXGW',
      state: stateFromUnit(unitsByName, 'IPX'),
      configKey: 'IPX',
      clickable: true,
    });
  }
  if (hasTCP) {
    addNode(nodes, {
      id: 'tcp',
      title: 'Host TCP',
      subtitle: 'IP transport',
      state: stateFromUnit(unitsByName, 'SMB-TCP'),
      configKey: 'Client',
      clickable: true,
    });
  }
  if (hasAFP) {
    addNode(nodes, {
      id: 'afp',
      title: 'AFP',
      subtitle: connLabel(counts.afp),
      state: stateFromUnit(unitsByName, 'AFP'),
      configKey: 'AFP',
      shareProtocol: 'afp',
      clickable: true,
    });
  }
  if (hasSMB) {
    addNode(nodes, {
      id: 'smb',
      title: 'SMB',
      subtitle: connLabel(counts.smb),
      state: stateFromUnit(unitsByName, 'SMB'),
      configKey: 'SMB',
      shareProtocol: 'smb',
      clickable: true,
    });
  }
  if (hasNCP) {
    addNode(nodes, {
      id: 'ncp',
      title: 'NCP',
      subtitle: connLabel(counts.ncp),
      state: stateFromUnit(unitsByName, 'NCP'),
      configKey: 'NCP',
      shareProtocol: 'ncp',
      clickable: true,
    });
  }
  if (hasEtherDFS) {
    addNode(nodes, {
      id: 'etherdfs',
      title: 'EtherDFS',
      subtitle: connLabel(counts.etherdfs),
      state: stateFromUnit(unitsByName, 'EtherDFS'),
      configKey: 'EtherDFS',
      shareProtocol: 'etherdfs',
      clickable: true,
    });
  }

  if (hasRouter) edges.push({ from: 'lan', to: 'router' });
  if (hasEtherTalk) edges.push({ from: 'lan', to: 'ethertalk' });
  if (hasEtherTalk && hasRouter) edges.push({ from: 'ethertalk', to: 'router', bidirectional: true });
  if (hasNBF) edges.push({ from: 'lan', to: 'nbf' });
  if (hasIPX) edges.push({ from: 'lan', to: 'ipx' });
  if (hasEtherDFS) edges.push({ from: 'lan', to: 'etherdfs' });

  if (hasRouter && hasLToUDP) edges.push({ from: 'router', to: 'ltoudp', bidirectional: true });
  if (hasRouter && hasTashTalk) edges.push({ from: 'router', to: 'tashtalk', bidirectional: true });
  if (hasRouter && hasMacIP) edges.push({ from: 'router', to: 'mactcp', bidirectional: true });
  if (hasRouter && hasIPXGW) edges.push({ from: 'router', to: 'ipxgw', bidirectional: true });
  if (hasRouter && afpDDP) edges.push({ from: 'router', to: 'afp' });

  if (hasLToUDP) edges.push({ from: 'host', to: 'ltoudp' });
  if (hasTashTalk) edges.push({ from: 'host', to: 'tashtalk' });
  if (hasMacIP) edges.push({ from: 'host', to: 'mactcp' });
  if (hasTCP) edges.push({ from: 'host', to: 'tcp' });
  if (hasIPXGW && hasIPX) edges.push({ from: 'ipxgw', to: 'ipx' });

  if (hasNBF && hasSMB && bindsTransport(smbTransports, 'netbeui')) edges.push({ from: 'nbf', to: 'smb' });
  if (hasIPX && hasSMB && bindsTransport(smbTransports, 'ipx')) edges.push({ from: 'ipx', to: 'smb' });
  if (hasIPX && hasNCP) edges.push({ from: 'ipx', to: 'ncp' });
  if (hasTCP && hasSMB && smbTCP) edges.push({ from: 'tcp', to: 'smb' });
  if (hasTCP && hasAFP && afpTCP) edges.push({ from: 'tcp', to: 'afp' });

  return { nodes, edges, networks: { ethertalk: ethTalkNet, ltoudp: ltoUDPNet, tashtalk: tashNet }, ifaceName, routerMembers };
}

function prettyJSON(v: unknown): string {
  return JSON.stringify(v, null, 2) || '{}';
}

function showConfigModal(root: HTMLElement, title: string, payload: unknown, openSharing?: () => void): void {
  const overlay = el('div', { class: 'modal-overlay' });
  const close = () => overlay.remove();
  const pre = el('pre', { class: 'topology-config' }, [prettyJSON(payload)]);
  const footKids = [btn('Close', '', close)];
  if (openSharing) footKids.unshift(btn('Open Sharing Editor', 'primary', openSharing));
  overlay.append(
    el('div', { class: 'modal topology-modal' }, [
      el('div', { class: 'modal-head' }, [el('h2', {}, [title]), btn('✕', '', close)]),
      el('div', { class: 'modal-body' }, [pre]),
      el('div', { class: 'modal-foot' }, footKids),
    ]),
  );
  overlay.addEventListener('click', (e) => {
    if (e.target === overlay) close();
  });
  document.body.append(overlay);
  const obs = new MutationObserver(() => {
    if (!root.contains(overlay)) obs.disconnect();
  });
  obs.observe(root, { childList: true, subtree: true });
}

function configForNode(model: ConfigModel, dag: RenderModel, node: DagNode): unknown {
  if (node.id === 'lan') {
    return {
      name: dag.ifaceName,
      interface: (model.Interfaces || {})[dag.ifaceName] || {},
      xfer_note: 'xfer rate comes from summed port bytes_rx/bytes_tx telemetry rates for this interface',
    };
  }
  if (node.id === 'host') return sectionByKey(model, 'Client') || model.Client || {};
  if (node.id === 'router') {
    return {
      section: sectionByKey(model, 'Router') || model.Router || {},
      members: dag.routerMembers,
      networks: dag.networks,
      ethertalk: listByKey(model, 'EtherTalk'),
      ltoudp: listByKey(model, 'LToUDP'),
      tashtalk: listByKey(model, 'TashTalk'),
    };
  }
  if (node.id === 'ethertalk') return listByKey(model, 'EtherTalk');
  if (node.id === 'nbf') return listByKey(model, 'NetBEUI');
  if (node.id === 'ltoudp') return listByKey(model, 'LToUDP');
  if (node.id === 'tashtalk') return listByKey(model, 'TashTalk');
  if (node.id === 'mactcp') return sectionByKey(model, 'MacIP') || {};
  if (node.id === 'ipxgw') return sectionByKey(model, 'IPXGW') || {};
  if (node.id === 'ipx') return listByKey(model, 'IPX');
  if (node.id === 'tcp') return { smb: sectionByKey(model, 'SMB') || {}, afp: sectionByKey(model, 'AFP') || {} };
  if (node.id === 'smb') return sectionByKey(model, 'SMB') || {};
  if (node.id === 'afp') return sectionByKey(model, 'AFP') || {};
  if (node.id === 'ncp') return sectionByKey(model, 'NCP') || {};
  if (node.id === 'etherdfs') return sectionByKey(model, 'EtherDFS') || {};
  return sectionByKey(model, node.configKey) || {};
}

function svgEl(name: string, attrs: Record<string, string> = {}): SVGElement {
  const node = document.createElementNS(SVG_NS, name);
  for (const [k, v] of Object.entries(attrs)) node.setAttribute(k, v);
  return node;
}

type Side = 'left' | 'right' | 'top' | 'bottom';

type Well = { x: number; y: number; w: number; h: number; cx: number; cy: number };

function wellOf(node: DagNode): Well {
  const x = node.x + (NODE_W - WELL) / 2;
  return { x, y: node.y, w: WELL, h: WELL, cx: x + WELL / 2, cy: node.y + WELL / 2 };
}

function sidePoint(box: Well, side: Side): { x: number; y: number } {
  if (side === 'right') return { x: box.x + box.w, y: box.cy };
  if (side === 'left') return { x: box.x, y: box.cy };
  if (side === 'bottom') return { x: box.cx, y: box.y + box.h };
  return { x: box.cx, y: box.y };
}

function pickSides(a: Well, b: Well): [Side, Side] {
  const dx = b.cx - a.cx;
  const dy = b.cy - a.cy;
  if (Math.abs(dx) >= Math.abs(dy)) return dx >= 0 ? ['right', 'left'] : ['left', 'right'];
  return dy >= 0 ? ['bottom', 'top'] : ['top', 'bottom'];
}

function edgePath(from: DagNode, to: DagNode): string {
  const a = wellOf(from);
  const b = wellOf(to);
  const [sa, sb] = pickSides(a, b);
  const p1 = sidePoint(a, sa);
  const p2 = sidePoint(b, sb);
  const pull = Math.max(36, Math.min(80, Math.hypot(p2.x - p1.x, p2.y - p1.y) * 0.35));
  const c1 = offset(p1, sa, pull);
  const c2 = offset(p2, sb, pull);
  return `M ${p1.x} ${p1.y} C ${c1.x} ${c1.y}, ${c2.x} ${c2.y}, ${p2.x} ${p2.y}`;
}

function offset(p: { x: number; y: number }, side: Side, d: number): { x: number; y: number } {
  if (side === 'right') return { x: p.x + d, y: p.y };
  if (side === 'left') return { x: p.x - d, y: p.y };
  if (side === 'bottom') return { x: p.x, y: p.y + d };
  return { x: p.x, y: p.y - d };
}

function clientToSvg(svg: SVGSVGElement, clientX: number, clientY: number): { x: number; y: number } {
  const ctm = svg.getScreenCTM();
  if (!ctm) return { x: 0, y: 0 };
  const pt = svg.createSVGPoint();
  pt.x = clientX;
  pt.y = clientY;
  const p = pt.matrixTransform(ctm.inverse());
  return { x: p.x, y: p.y };
}

function snap(n: number): number {
  return Math.round(n / GRID) * GRID;
}

function nodeClass(state: NodeState, dragging: boolean): string {
  return `dag-node ${state}${dragging ? ' is-dragging' : ''}`;
}

function createNodeGroup(node: DagNode): SVGGElement {
  const wellX = (NODE_W - WELL) / 2;
  const g = svgEl('g', {
    class: nodeClass(node.state, false),
    'data-node-id': node.id,
    transform: `translate(${node.x} ${node.y})`,
  }) as SVGGElement;
  if (node.clickable) {
    g.setAttribute('tabindex', '0');
    g.setAttribute('role', 'button');
    g.setAttribute('aria-label', `Open config for ${node.title}`);
  }
  g.append(
    svgEl('rect', { class: 'dag-node-hit', x: '0', y: '0', width: String(NODE_W), height: String(NODE_H) }),
    svgEl('rect', {
      class: 'dag-node-well',
      x: String(wellX),
      y: '0',
      width: String(WELL),
      height: String(WELL),
      rx: '10',
      ry: '10',
    }),
    svgEl('image', {
      href: node.icon,
      x: String(wellX + WELL_PAD),
      y: String(WELL_PAD),
      width: String(ICON),
      height: String(ICON),
    }),
    svgEl('circle', {
      class: 'dag-node-pip',
      cx: String(wellX + WELL - 6),
      cy: '6',
      r: '5',
    }),
    svgEl('text', {
      class: 'dag-node-title',
      x: String(NODE_W / 2),
      y: String(WELL + 16),
      'text-anchor': 'middle',
    }),
    svgEl('text', {
      class: 'dag-node-sub',
      x: String(NODE_W / 2),
      y: String(WELL + 30),
      'text-anchor': 'middle',
    }),
  );
  const title = g.querySelector('.dag-node-title');
  const sub = g.querySelector('.dag-node-sub');
  if (title) title.textContent = node.title;
  if (sub) sub.textContent = node.subtitle;
  return g;
}

function paintNode(g: SVGGElement, node: DagNode, dragging: boolean): void {
  g.setAttribute('class', nodeClass(node.state, dragging));
  g.setAttribute('transform', `translate(${node.x} ${node.y})`);
  const img = g.querySelector('image');
  if (img && img.getAttribute('href') !== node.icon) img.setAttribute('href', node.icon);
  const title = g.querySelector('.dag-node-title');
  const sub = g.querySelector('.dag-node-sub');
  if (title && title.textContent !== node.title) title.textContent = node.title;
  if (sub && sub.textContent !== node.subtitle) sub.textContent = node.subtitle;
}

function edgeKey(edge: DagEdge): string {
  return `${edge.from}\0${edge.to}\0${edge.bidirectional ? '1' : '0'}`;
}

function contentSize(nodes: DagNode[]): { w: number; h: number } {
  let w = MIN_W;
  let h = MIN_H;
  for (const n of nodes) {
    w = Math.max(w, n.x + NODE_W + 48);
    h = Math.max(h, n.y + NODE_H + 48);
  }
  return { w, h };
}

export type TopologyHandle = {
  start: () => void;
  stop: () => void;
  destroy: () => void;
};

export function mountTopology(root: HTMLElement, opts: TopologyOptions): TopologyHandle {
  const wrap = el('div', { class: 'topology-root' });
  root.replaceChildren(wrap);

  let alive = true;
  let model: ConfigModel | null = null;
  let units: Unit[] = [];
  let counts: ServiceConnections = { afp: null, smb: null, ncp: null, etherdfs: null };
  let dag: RenderModel | null = null;
  let zoom = 1;
  let tick = 0;
  let refreshInFlight = false;
  let loopID: number | null = null;
  let layout = loadLayout();

  const tools = el('div', { class: 'row topology-tools' });
  const viewport = el('div', { class: 'topology-viewport' });
  const canvas = el('div', { class: 'topology-canvas' });
  viewport.append(canvas);

  const svg = svgEl('svg', {
    class: 'dag-svg',
    role: 'img',
    'aria-label': 'ClassicStack topology diagram',
  }) as SVGSVGElement;
  const defs = svgEl('defs');
  defs.append(
    svgEl('marker', {
      id: 'dag-arrow',
      viewBox: '0 0 10 10',
      refX: '9',
      refY: '5',
      markerWidth: '7',
      markerHeight: '7',
      orient: 'auto',
    }),
    svgEl('marker', {
      id: 'dag-arrow-start',
      viewBox: '0 0 10 10',
      refX: '1',
      refY: '5',
      markerWidth: '7',
      markerHeight: '7',
      orient: 'auto',
    }),
  );
  defs.querySelector('#dag-arrow')?.appendChild(svgEl('path', { d: 'M 0 0 L 10 5 L 0 10 z' }));
  defs.querySelector('#dag-arrow-start')?.appendChild(svgEl('path', { d: 'M 10 0 L 0 5 L 10 10 z' }));
  const edgesLayer = svgEl('g', { class: 'dag-edges' });
  const nodesLayer = svgEl('g', { class: 'dag-nodes' });
  svg.append(defs, edgesLayer, nodesLayer);
  canvas.append(svg);

  const nodeEls = new Map<string, SVGGElement>();
  const edgeEls = new Map<string, SVGPathElement>();
  let draggingID: string | null = null;

  function applyZoom(): void {
    if (!dag) return;
    const { w, h } = contentSize(dag.nodes);
    svg.setAttribute('viewBox', `0 0 ${w} ${h}`);
    svg.style.width = `${Math.round(w * zoom)}px`;
    svg.style.height = `${Math.round(h * zoom)}px`;
  }

  function persist(): void {
    if (!dag) return;
    const next: LayoutStore = { ...layout };
    for (const n of dag.nodes) next[n.id] = { x: n.x, y: n.y };
    layout = next;
    saveLayout(layout);
  }

  function redrawEdges(): void {
    if (!dag) return;
    const byID = new Map(dag.nodes.map((n) => [n.id, n]));
    const seen = new Set<string>();
    for (const edge of dag.edges) {
      const from = byID.get(edge.from);
      const to = byID.get(edge.to);
      if (!from || !to) continue;
      const key = edgeKey(edge);
      seen.add(key);
      let path = edgeEls.get(key);
      if (!path) {
        path = svgEl('path', { class: edge.bidirectional ? 'dag-edge bidir' : 'dag-edge' }) as SVGPathElement;
        path.setAttribute('marker-end', 'url(#dag-arrow)');
        if (edge.bidirectional) path.setAttribute('marker-start', 'url(#dag-arrow-start)');
        edgeEls.set(key, path);
        edgesLayer.append(path);
      }
      path.setAttribute('d', edgePath(from, to));
    }
    for (const [key, path] of edgeEls) {
      if (seen.has(key)) continue;
      path.remove();
      edgeEls.delete(key);
    }
  }

  function syncChart(next: RenderModel, relayout: boolean): void {
    dag = next;
    if (relayout) {
      const auto = autoLayout(next.nodes.map((n) => n.id));
      layout = { ...auto };
      saveLayout(layout);
    }
    placeNodes(next.nodes, layout);
    const keep = new Set(next.nodes.map((n) => n.id));
    for (const [id, g] of nodeEls) {
      if (keep.has(id)) continue;
      g.remove();
      nodeEls.delete(id);
    }
    for (const node of next.nodes) {
      let g = nodeEls.get(node.id);
      if (!g) {
        g = createNodeGroup(node);
        nodeEls.set(node.id, g);
        nodesLayer.append(g);
        bindNode(g, node.id);
      } else {
        paintNode(g, node, draggingID === node.id);
      }
    }
    redrawEdges();
    applyZoom();
  }

  function nodeByID(id: string): DagNode | undefined {
    return dag?.nodes.find((n) => n.id === id);
  }

  function bindNode(g: SVGGElement, id: string): void {
    g.addEventListener('pointerdown', (ev) => {
      if (ev.button !== 0 || !dag) return;
      const node = nodeByID(id);
      if (!node) return;
      ev.preventDefault();
      ev.stopPropagation();
      g.setPointerCapture(ev.pointerId);
      const startClient = { x: ev.clientX, y: ev.clientY };
      const origin = { x: node.x, y: node.y };
      const grab = clientToSvg(svg, ev.clientX, ev.clientY);
      const grabOff = { x: grab.x - node.x, y: grab.y - node.y };
      let moved = false;
      draggingID = id;
      nodesLayer.append(g);

      const onMove = (e: PointerEvent) => {
        const dx = e.clientX - startClient.x;
        const dy = e.clientY - startClient.y;
        if (!moved && dx * dx + dy * dy < DRAG_THRESHOLD * DRAG_THRESHOLD) return;
        moved = true;
        const p = clientToSvg(svg, e.clientX, e.clientY);
        node.x = Math.max(8, p.x - grabOff.x);
        node.y = Math.max(8, p.y - grabOff.y);
        layout[node.id] = { x: node.x, y: node.y };
        paintNode(g, node, true);
        redrawEdges();
        applyZoom();
      };
      const onUp = (e: PointerEvent) => {
        g.releasePointerCapture(e.pointerId);
        g.removeEventListener('pointermove', onMove);
        g.removeEventListener('pointerup', onUp);
        g.removeEventListener('pointercancel', onUp);
        draggingID = null;
        if (moved) {
          node.x = Math.max(8, snap(node.x));
          node.y = Math.max(8, snap(node.y));
          paintNode(g, node, false);
          persist();
          redrawEdges();
          applyZoom();
        } else {
          paintNode(g, node, false);
          node.x = origin.x;
          node.y = origin.y;
          if (node.clickable) openNode(node);
        }
      };
      g.addEventListener('pointermove', onMove);
      g.addEventListener('pointerup', onUp);
      g.addEventListener('pointercancel', onUp);
    });
    g.addEventListener('keydown', (ev) => {
      const node = nodeByID(id);
      if (!node) return;
      if (ev.key === 'Enter' || ev.key === ' ') {
        ev.preventDefault();
        openNode(node);
        return;
      }
      const step = ev.shiftKey ? GRID * 4 : GRID;
      let dx = 0;
      let dy = 0;
      if (ev.key === 'ArrowLeft') dx = -step;
      else if (ev.key === 'ArrowRight') dx = step;
      else if (ev.key === 'ArrowUp') dy = -step;
      else if (ev.key === 'ArrowDown') dy = step;
      else return;
      ev.preventDefault();
      node.x = Math.max(8, node.x + dx);
      node.y = Math.max(8, node.y + dy);
      paintNode(g, node, false);
      persist();
      redrawEdges();
      applyZoom();
    });
  }

  function openNode(node: DagNode): void {
    if (!model || !dag) return;
    const payload = configForNode(model, dag, node);
    const jumpToSharing = node.shareProtocol ? () => opts.openSharing(node.shareProtocol || '') : undefined;
    showConfigModal(root, node.title || node.id, payload, jumpToSharing);
  }

  function fitZoom(): void {
    if (!dag) return;
    const { w } = contentSize(dag.nodes);
    const candidate = (viewport.clientWidth - 18) / w;
    zoom = Math.max(0.55, Math.min(1.5, candidate || 1));
    applyZoom();
  }

  tools.append(
    btn('Refresh now', '', () => void refresh(true)),
    btn('Zoom −', '', () => {
      zoom = Math.max(0.5, zoom - 0.1);
      applyZoom();
    }),
    btn('Zoom +', '', () => {
      zoom = Math.min(2.5, zoom + 0.1);
      applyZoom();
    }),
    btn('Fit', '', () => fitZoom()),
    btn('100%', '', () => {
      zoom = 1;
      applyZoom();
    }),
    btn('Reset layout', '', () => {
      if (!model) return;
      syncChart(buildDag(model, units, counts), true);
    }),
  );

  wrap.replaceChildren(tools, viewport);

  async function refresh(forceFull = false): Promise<void> {
    if (!alive || refreshInFlight) return;
    refreshInFlight = true;
    try {
      const wantFull = forceFull || !model || tick % 6 === 0;
      if (wantFull) {
        const res = await Promise.all([api.config(), api.status(), serviceConnections()]);
        [model, units, counts] = res;
      } else {
        const res = await Promise.all([api.status(), serviceConnections()]);
        [units, counts] = res;
      }
      if (!model) return;
      const next = buildDag(model, units, counts);
      if (draggingID) {
        const current = dag;
        if (current) {
          const pos = new Map(current.nodes.map((n) => [n.id, { x: n.x, y: n.y }]));
          for (const n of next.nodes) {
            const p = pos.get(n.id);
            if (p) {
              n.x = p.x;
              n.y = p.y;
            }
          }
        }
      }
      syncChart(next, false);
    } catch (e) {
      if (!model) {
        wrap.replaceChildren(el('div', { class: 'panel err' }, [e instanceof Error ? e.message : String(e)]));
      }
    } finally {
      tick++;
      refreshInFlight = false;
    }
  }

  const onStats = () => {
    if (!alive || !model || draggingID) return;
    syncChart(buildDag(model, units, counts), false);
  };
  function start(): void {
    if (!alive) return;
    if (loopID == null) {
      telemetry.onStats.add(onStats);
      loopID = window.setInterval(() => void refresh(false), REFRESH_MS);
    }
    void refresh(true).then(() => {
      requestAnimationFrame(() => fitZoom());
    });
  }

  function stop(): void {
    if (loopID != null) {
      clearInterval(loopID);
      loopID = null;
    }
    telemetry.onStats.delete(onStats);
  }

  function destroy(): void {
    alive = false;
    stop();
    wrap.remove();
  }

  return { start, stop, destroy };
}

/** Floating configuration DAG. */
export class TopologyWindow extends HTMLElement {
  openSharing: (protocol: string) => void = () => undefined;
  private chart: TopologyHandle | null = null;

  connectedCallback(): void {
    this.classList.add('activity-window', 'topology-window');
    this.hidden = true;
    this.style.left = '36px';
    this.style.top = '64px';
    this.innerHTML = `
      <div class="activity-window__chrome">
        <div class="activity-window__title">Topology</div>
        <span class="badge ok topology-live">live</span>
        <button type="button" class="btn log-panel__btn" data-act="close" aria-label="Close">✕</button>
      </div>
      <div class="activity-window__body topology-window__body"></div>
    `;
    mountFloatingWindow(this, { chromeClass: 'activity-window__chrome', minWidth: 560, minHeight: 320 });
    const body = this.querySelector<HTMLElement>('.topology-window__body');
    if (body) {
      this.chart = mountTopology(body, {
        openSharing: (protocol) => this.openSharing(protocol),
      });
    }
    this.addEventListener('click', (e) => {
      if ((e.target as HTMLElement).closest('[data-act="close"]')) this.hide();
    });
    window.addEventListener('keydown', this.onKey);
  }

  disconnectedCallback(): void {
    this.chart?.destroy();
    this.chart = null;
    window.removeEventListener('keydown', this.onKey);
  }

  show(): void {
    this.hidden = false;
    this.chart?.start();
    raise(this);
  }

  hide(): void {
    this.hidden = true;
    this.chart?.stop();
  }

  toggle(): void {
    if (this.hidden) this.show();
    else this.hide();
  }

  private onKey = (e: KeyboardEvent): void => {
    if (e.key !== 'Escape' || this.hidden) return;
    if (document.querySelector('.modal-overlay')) return;
    this.hide();
  };
}

customElements.define('cs-topology-window', TopologyWindow);

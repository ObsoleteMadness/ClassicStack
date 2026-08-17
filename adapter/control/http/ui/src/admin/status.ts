import { api, type HostInfo, type Unit } from '../api';
import { telemetry } from '../telemetry';
import { btn, el, formatBytes } from './dom';

const GROUPS = [
  {
    id: 'fileservices',
    label: 'File & print services',
    members: ['AFP', 'SMB', 'SMB-TCP', 'NCP', 'EtherDFS', 'NetBIOS', 'Browser', 'Messenger'],
  },
  {
    id: 'appletalk',
    label: 'AppleTalk router',
    members: ['Router', 'RTMP', 'ZIP', 'NBP', 'AEP', 'MacIP', 'IPXGW', 'IPXDiag'],
  },
  {
    id: 'transports',
    label: 'Transports',
    members: ['EtherTalk', 'LToUDP', 'TashTalk', 'IPX', 'NetBEUI'],
  },
  { id: 'other', label: 'Other', members: [] as string[] },
];

function groupOf(u: Unit): string {
  for (const g of GROUPS) {
    if (g.members.includes(u.Name)) return g.id;
    if (g.id === 'transports' && u.Kind === 'port') return g.id;
  }
  return 'other';
}

export function renderStatus(root: HTMLElement): void {
  const wrap = el('div');
  root.replaceChildren(wrap);
  void refresh();

  async function refresh() {
    let units: Unit[] = [];
    let host: HostInfo | null = null;
    try {
      units = await api.status();
    } catch {
      units = [];
    }
    try {
      host = await api.hostInfo();
    } catch {
      host = null;
    }
    paint(units, host);
  }

  function paint(units: Unit[], host: HostInfo | null) {
    const sections: Node[] = [];
    if (host) {
      sections.push(
        el('div', { class: 'panel' }, [
          el('h2', {}, ['Host']),
          el('div', { class: 'kv' }, [
            `${host.osName || ''} · ${host.architecture || ''} · ${host.hostIp || ''}`.trim(),
          ]),
          el('div', { class: 'kv' }, [
            `Memory ${formatBytes(host.freeMemory || 0)} free / ${formatBytes(host.totalMemory || 0)}`,
          ]),
          el('div', { class: 'kv' }, [`${host.version || ''} ${host.goVersion || ''}`.trim()]),
        ]),
      );
    }
    sections.push(
      el('div', { class: 'row spread' }, [el('h2', {}, ['Services']), btn('Refresh', '', () => void refresh())]),
    );
    const grouped = new Map<string, Unit[]>();
    for (const u of units) {
      const id = groupOf(u);
      const list = grouped.get(id) ?? [];
      list.push(u);
      grouped.set(id, list);
    }
    let any = false;
    for (const g of GROUPS) {
      const list = grouped.get(g.id);
      if (!list?.length) continue;
      any = true;
      sections.push(el('h3', { class: 'group-head' }, [g.label]));
      sections.push(el('div', { class: 'grid' }, list.map((u) => card(u))));
    }
    if (!any) sections.push(el('div', { class: 'panel muted' }, ['No components built.']));
    wrap.replaceChildren(...sections);
    paintMetrics();
  }

  function card(u: Unit): HTMLElement {
    const running = !!u.Running;
    const actions = el(
      'div',
      { class: 'card-actions' },
      running
        ? [
            btn('Stop', '', () => void act('stop', u.Name)),
            btn('Restart', '', () => void act('restart', u.Name)),
          ]
        : [btn('Start', 'primary', () => void act('start', u.Name))],
    );
    const metric = el('div', { class: 'kv metric' });
    metric.dataset.metricFor = u.Name;
    return el('div', { class: 'card' }, [
      el('h3', {}, [
        el('span', { class: 'dot ' + (running ? 'run' : 'stop') }),
        el('span', { class: 'card-title' }, [u.Name]),
      ]),
      el('div', { class: 'kv' }, [`${u.Enabled ? 'Enabled' : 'Disabled'} · ${running ? 'Running' : 'Stopped'}`]),
      u.Binding ? el('div', { class: 'kv' }, [`Binding ${u.Binding}`]) : el('span'),
      metric,
      actions,
    ]);
  }

  async function act(verb: 'start' | 'stop' | 'restart', name: string) {
    try {
      await api.action(verb, name);
    } catch (e) {
      alert(e instanceof Error ? e.message : String(e));
    }
    await refresh();
  }

  function paintMetrics() {
    wrap.querySelectorAll<HTMLElement>('[data-metric-for]').forEach((node) => {
      const name = node.dataset.metricFor || '';
      const s = telemetry.stats[name];
      if (!s) return;
      const counters = (s.Counters || s.counters || {}) as Record<string, number>;
      const gauges = (s.Gauges || s.gauges || {}) as Record<string, number>;
      const parts = [
        ...Object.entries(gauges).map(([k, v]) => `${k}=${v}`),
        ...Object.entries(counters).slice(0, 4).map(([k, v]) => `${k}=${v}`),
      ];
      node.textContent = parts.join(' · ');
    });
  }

  const onStats = () => paintMetrics();
  telemetry.onStats.add(onStats);
  const obs = new MutationObserver(() => {
    if (!root.contains(wrap)) telemetry.onStats.delete(onStats);
  });
  obs.observe(root, { childList: true });
}

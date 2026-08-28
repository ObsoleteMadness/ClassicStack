import { api, type HostInfo, type Unit } from '../api';
import { telemetry, type LiveConn } from '../telemetry';
import { btn, el, formatBytes } from './dom';
import { alertError } from './prompt';
import { kindLabel, type NotificationCentre } from './notifications';

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

const SLIDER_ICON = `<svg class="control-plane-toggle__glyph" xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><line x1="4" x2="4" y1="21" y2="14"/><line x1="4" x2="4" y1="10" y2="3"/><line x1="12" x2="12" y1="21" y2="12"/><line x1="12" x2="12" y1="8" y2="3"/><line x1="20" x2="20" y1="21" y2="16"/><line x1="20" x2="20" y1="12" y2="3"/><line x1="2" x2="6" y1="14" y2="14"/><line x1="10" x2="14" y1="8" y2="8"/><line x1="18" x2="22" y1="16" y2="16"/></svg>`;

function groupOf(u: Unit): string {
  for (const g of GROUPS) {
    if (g.members.includes(u.Name)) return g.id;
    if (g.id === 'transports' && u.Kind === 'port') return g.id;
  }
  return 'other';
}

export type ControlPlaneHandle = {
  readonly el: HTMLElement;
  readonly toggle: HTMLButtonElement;
  open: () => void;
  close: () => void;
  togglePanel: () => void;
};

/** Right-hand control plane: live connection, alerts, and service start/stop. */
export function mountControlPlane(
  header: HTMLElement,
  workspace: HTMLElement,
  notify: NotificationCentre,
): ControlPlaneHandle {
  const toggle = el('button', {
    type: 'button',
    class: 'control-plane-toggle',
    id: 'control-plane-toggle',
    'aria-pressed': 'false',
    'aria-label': 'Control panel',
  });
  toggle.innerHTML = `${SLIDER_ICON}<span id="conn" class="badge">connecting</span>`;
  header.append(toggle);

  const aside = el('aside', {
    class: 'control-plane',
    'aria-label': 'Control panel',
    'aria-hidden': 'true',
  });
  const body = el('div', { class: 'control-plane__body' });
  aside.append(body);
  workspace.append(aside);

  let units: Unit[] = [];
  let host: HostInfo | null = null;
  let apiReachable = true;
  let refreshTimer: ReturnType<typeof setTimeout> | null = null;

  const isOpen = (): boolean => aside.classList.contains('is-open');

  function paintConn(): void {
    const badge = toggle.querySelector('#conn');
    const sse = telemetry.conn;
    let text: string = sse;
    let cls = '';
    if (sse === 'connected' && apiReachable) {
      const enabled = units.filter((u) => u.Enabled);
      const running = enabled.filter((u) => u.Running);
      if (enabled.length && running.length === 0) {
        text = 'stopped';
        cls = 'bad';
      } else if (enabled.length && running.length < enabled.length) {
        text = 'degraded';
        cls = 'warn';
      } else {
        text = 'connected';
        cls = 'ok';
      }
    } else if (sse === 'connected' && !apiReachable) {
      text = 'offline';
      cls = 'bad';
    } else if (sse === 'connecting') {
      text = 'connecting';
    } else {
      text = 'offline';
      cls = 'bad';
    }
    if (badge) {
      badge.textContent = text;
      badge.className = 'badge' + (cls ? ' ' + cls : '');
    }
    toggle.setAttribute('aria-label', `Control panel (${text})`);
    toggle.classList.toggle('is-live', cls === 'ok');
    toggle.classList.toggle('is-down', cls === 'bad');
  }

  function paintAlerts(): HTMLElement {
    const notices = notify.list().slice(0, 12);
    const kids: Node[] = [el('h2', {}, ['Alerts'])];
    if (notices.length) kids.push(btn('Clear', '', () => notify.clearAll()));
    const head = el('div', { class: 'control-plane__section-head' }, kids);
    if (!notices.length) {
      return el('section', { class: 'control-plane__alerts' }, [
        head,
        el('p', { class: 'muted' }, ['No alerts or messages.']),
      ]);
    }
    const list = el(
      'div',
      { class: 'control-plane__alert-list' },
      notices.map((n) => {
        const t = new Date(n.time).toLocaleTimeString();
        return el('article', { class: `notify-item notify-item--${n.kind}${n.read ? '' : ' unread'}` }, [
          el('div', { class: 'notify-item__meta' }, [
            el('span', {}, [kindLabel(n.kind)]),
            el('time', {}, [t]),
          ]),
          el('h3', {}, [n.title]),
          el('p', {}, [n.text]),
        ]);
      }),
    );
    return el('section', { class: 'control-plane__alerts' }, [head, list]);
  }

  function card(u: Unit): HTMLElement {
    const running = !!u.Running;
    const actions = el(
      'div',
      { class: 'card-actions' },
      running
        ? [btn('Stop', '', () => void act('stop', u.Name)), btn('Restart', '', () => void act('restart', u.Name))]
        : [btn('Start', 'primary', () => void act('start', u.Name))],
    );
    const metric = el('div', { class: 'kv metric' });
    metric.dataset.metricFor = u.Name;
    return el('div', { class: 'card control-plane__unit' }, [
      el('h3', {}, [
        el('span', { class: 'dot ' + (running ? 'run' : 'stop') }),
        el('span', { class: 'card-title' }, [u.Name]),
      ]),
      el('div', { class: 'kv' }, [`${u.Enabled ? 'Enabled' : 'Disabled'} · ${running ? 'Running' : 'Stopped'}`]),
      u.Binding ? el('div', { class: 'kv' }, [u.Binding]) : el('span'),
      u.Error ? el('div', { class: 'kv err' }, [u.Error]) : el('span'),
      metric,
      actions,
    ]);
  }

  function paintMetrics(): void {
    body.querySelectorAll<HTMLElement>('[data-metric-for]').forEach((node) => {
      const name = node.dataset.metricFor || '';
      const s = telemetry.stats[name];
      if (!s) return;
      const counters = (s.Counters || s.counters || {}) as Record<string, number>;
      const gauges = (s.Gauges || s.gauges || {}) as Record<string, number>;
      const parts = [
        ...Object.entries(gauges).map(([k, v]) => `${k}=${v}`),
        ...Object.entries(counters)
          .slice(0, 4)
          .map(([k, v]) => `${k}=${v}`),
      ];
      node.textContent = parts.join(' · ');
    });
  }

  function paint(): void {
    paintConn();
    if (!isOpen()) return;
    const y = aside.scrollTop;
    const sections: Node[] = [paintAlerts()];
    if (host) {
      sections.push(
        el('section', { class: 'control-plane__host' }, [
          el('h2', {}, ['Host']),
          el('div', { class: 'kv' }, [`${host.osName || ''} · ${host.architecture || ''}`.trim()]),
          el('div', { class: 'kv' }, [host.hostIp || '']),
          el('div', { class: 'kv' }, [
            `Memory ${formatBytes(host.freeMemory || 0)} free / ${formatBytes(host.totalMemory || 0)}`,
          ]),
        ]),
      );
    }
    sections.push(
      el('div', { class: 'control-plane__section-head' }, [
        el('h2', {}, ['Services']),
        btn('Refresh', '', () => void refresh()),
      ]),
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
      sections.push(el('div', { class: 'control-plane__units' }, list.map((u) => card(u))));
    }
    if (!any) sections.push(el('p', { class: 'muted' }, ['No components built.']));
    body.replaceChildren(...sections);
    aside.scrollTop = y;
    paintMetrics();
  }

  async function refresh(): Promise<void> {
    try {
      units = await api.status();
      apiReachable = true;
    } catch {
      units = [];
      apiReachable = false;
    }
    try {
      host = await api.hostInfo();
    } catch {
      host = null;
    }
    paint();
  }

  // Present participles for the lifecycle verbs, so a failure names what was attempted.
  const ACTING: Record<'start' | 'stop' | 'restart', string> = {
    start: 'starting',
    stop: 'stopping',
    restart: 'restarting',
  };

  async function act(verb: 'start' | 'stop' | 'restart', name: string): Promise<void> {
    try {
      await api.action(verb, name);
    } catch (e) {
      // "Error starting NetBEUI — pcap: no such device en9". The reason the unit would
      // not come up is the answer the operator pressed the button for, so it gets a
      // dialog whatever the status code: they asked for this explicitly.
      alertError(`Error ${ACTING[verb]} ${name}`, e instanceof Error ? e.message : String(e));
    }
    await refresh();
  }

  function open(): void {
    aside.classList.add('is-open');
    aside.setAttribute('aria-hidden', 'false');
    workspace.classList.add('control-plane-open');
    toggle.setAttribute('aria-pressed', 'true');
    toggle.classList.add('is-open');
    void refresh();
  }

  function close(): void {
    aside.classList.remove('is-open');
    aside.setAttribute('aria-hidden', 'true');
    workspace.classList.remove('control-plane-open');
    toggle.setAttribute('aria-pressed', 'false');
    toggle.classList.remove('is-open');
  }

  function togglePanel(): void {
    if (isOpen()) close();
    else open();
  }

  toggle.addEventListener('click', (e) => {
    e.stopPropagation();
    togglePanel();
  });

  const onNotices = (): void => {
    if (isOpen()) paint();
  };
  notify.onChange.add(onNotices);

  const onState = (): void => {
    if (refreshTimer) clearTimeout(refreshTimer);
    refreshTimer = setTimeout(() => void refresh(), 150);
  };
  telemetry.onState.add(onState);
  // A reconnect (e.g. after a network blip) may have missed state transitions
  // that happened while the SSE stream was down, so treat "back online" as a
  // reason to re-fetch rather than trusting the stale cached units/host — this
  // is what previously relied on the 5s poll eventually catching up.
  const onConn = (s: LiveConn): void => {
    paintConn();
    if (s === 'connected') void refresh();
  };
  telemetry.onConn.add(onConn);
  telemetry.onStats.add(() => paintMetrics());

  void refresh();

  const obs = new MutationObserver(() => {
    if (document.body.contains(aside)) return;
    notify.onChange.delete(onNotices);
    telemetry.onState.delete(onState);
    telemetry.onConn.delete(onConn);
    obs.disconnect();
  });
  obs.observe(document.body, { childList: true, subtree: true });

  return { el: aside, toggle, open, close, togglePanel };
}

// ClassicStack web-admin SPA — native Web Components, no framework, no jQuery.
//
// It speaks the JSON/SSE control API the HTTP control adapter exposes
// (adapter/control/http):
//   GET  /status                       component list (409 first-run, 401 needs-auth)
//   POST /setup                        {user,password} → first-run admin
//   POST /start|/stop|/restart         {Name}
//   POST /reconfigure                  {name, section}  apply one section LIVE
//   GET  /config                       masked config.Model
//   POST /save                         persist the live model → {revision}
//   GET  /list_fs_types                fs-type names
//   GET  /params_for?fs_type=…         per-type param schema (secret → password field)
//   GET  /list_interfaces              enumerable host NICs (picker)
//   POST /set_interface|/remove_interface   interface NAMESPACE CRUD (Model.Interfaces)
//   GET  /list_zones                   AppleTalk zone probe (diagnostics)
//   GET  /registered_names             NBP name table (NBP stat drill-down)
//   GET  /macip_leases                 MacIP lease table (MacIP stat drill-down)
//   GET  /aarp_table                   AARP address mapping table (EtherTalk drill-down)
//   GET  /users                        stored identities
//   POST /set_user|/set_user_disabled|/remove_user
//   GET  /subscribe?topics=stats,state,log   Server-Sent Events telemetry
//
// Auth is HTTP Basic, handled by the browser once an admin exists. The first-run
// gate returns 409 {setup_required:true}; the SPA shows the setup form, the one
// unauthenticated POST the server allows.

const api = {
  async statusProbe() {
    const r = await fetch("status");
    return { code: r.status, body: r.ok ? await r.json() : null };
  },
  async status() {
    const r = await fetch("status");
    if (!r.ok) throw new Error(`status: HTTP ${r.status}`);
    return r.json();
  },
  async setup(user, password) {
    const r = await fetch("setup", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ user, password }),
    });
    if (!r.ok) throw new Error((await r.json().catch(() => ({}))).error || `HTTP ${r.status}`);
    return r.json();
  },
  async action(verb, name) {
    const r = await fetch(verb, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ Name: name }),
    });
    if (!r.ok) throw new Error(`${verb} ${name}: ${await errText(r)}`);
  },
  async config() {
    const r = await fetch("config");
    if (!r.ok) throw new Error(`config: HTTP ${r.status}`);
    return r.json();
  },
  async reconfigure(name, section) {
    const r = await fetch("reconfigure", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, section }),
    });
    if (!r.ok) throw new Error(await errText(r));
  },
  async save() {
    const r = await fetch("save", { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" });
    if (!r.ok) throw new Error(await errText(r));
    return r.json();
  },
  async fsTypes() {
    const r = await fetch("list_fs_types");
    return r.ok ? r.json() : [];
  },
  async paramsFor(t) {
    const r = await fetch("params_for?fs_type=" + encodeURIComponent(t));
    return r.ok ? r.json() : [];
  },
  async interfaces() {
    const r = await fetch("list_interfaces");
    if (!r.ok) throw new Error(await errText(r));
    return r.json();
  },
  async zones() {
    const r = await fetch("list_zones");
    if (r.status === 501) throw new Error("not available in this build");
    if (!r.ok) throw new Error(await errText(r));
    return r.json();
  },
  // registeredNames / macipLeases are the drill-down probes behind the NBP "registered
  // names" and MacIP "active leases" dashboard stats. 501 = service not in this build.
  async registeredNames() {
    const r = await fetch("registered_names");
    if (r.status === 501) throw new Error("NBP not available in this build");
    if (!r.ok) throw new Error(await errText(r));
    return r.json();
  },
  async macipLeases() {
    const r = await fetch("macip_leases");
    if (r.status === 501) throw new Error("MacIP gateway not available in this build");
    if (!r.ok) throw new Error(await errText(r));
    return r.json();
  },
  // aarpTable is the drill-down probe behind the EtherTalk port: the AARP Address Mapping
  // Table (resolved AppleTalk-node→MAC mappings) across the EtherTalk segments.
  async aarpTable() {
    const r = await fetch("aarp_table");
    if (r.status === 501) throw new Error("EtherTalk not available in this build");
    if (!r.ok) throw new Error(await errText(r));
    return r.json();
  },
  // setInterface / removeInterface edit the interface NAMESPACE (Model.Interfaces): the
  // named NIC/serial/bridge entries a port binds to (distinct from interfaces(), which
  // enumerates the host's physical NICs for a picker).
  async setInterface(iface) {
    const r = await fetch("set_interface", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(iface),
    });
    if (!r.ok) throw new Error(await errText(r));
  },
  async removeInterface(name) {
    const r = await fetch("remove_interface", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name }),
    });
    if (!r.ok) throw new Error(await errText(r));
  },
  async users() {
    const r = await fetch("users");
    if (r.status === 501) return { unavailable: true, list: [] };
    if (!r.ok) throw new Error(await errText(r));
    return { unavailable: false, list: await r.json() };
  },
  async setUser(name, password) {
    const r = await fetch("set_user", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, password }),
    });
    if (!r.ok) throw new Error(await errText(r));
  },
  async setUserDisabled(name, disabled) {
    const r = await fetch("set_user_disabled", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, disabled }),
    });
    if (!r.ok) throw new Error(await errText(r));
  },
  async removeUser(name) {
    const r = await fetch("remove_user", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name }),
    });
    if (!r.ok) throw new Error(await errText(r));
  },
  // addInstance/removeInstance are the create/delete half of repeated-section config
  // (AFP volumes, SMB shares): owner is the consuming component ("AFP"/"SMB"), key the
  // schema key ("AFPVolumes"/"SMBShares").
  async addInstance(owner, key, section) {
    const r = await fetch("add_instance", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ owner, key, section }),
    });
    if (!r.ok) throw new Error(await errText(r));
  },
  async removeInstance(owner, key, name) {
    const r = await fetch("remove_instance", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ owner, key, name }),
    });
    if (!r.ok) throw new Error(await errText(r));
  },
  async serialPorts() {
    const r = await fetch("list_serial_ports");
    return r.ok ? r.json() : [];
  },
  async browsePath(dir) {
    const r = await fetch("browse_path?dir=" + encodeURIComponent(dir || ""));
    if (!r.ok) throw new Error(await errText(r));
    return r.json();
  },
  async extMap(path) {
    const r = await fetch("extmap?path=" + encodeURIComponent(path));
    if (!r.ok) throw new Error(await errText(r));
    return r.json();
  },
  async saveExtMap(path, content) {
    const r = await fetch("extmap", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path, content }),
    });
    if (!r.ok) throw new Error(await errText(r));
    return r.json();
  },
  // downloadConfig triggers a server.toml download via a transient link.
  downloadConfig() {
    const a = el("a", { href: "config_download", download: "server.toml" });
    document.body.append(a); a.click(); a.remove();
  },
};

// errText extracts a {"error":…} body or falls back to the HTTP status line.
async function errText(r) {
  const j = await r.json().catch(() => null);
  if (j && j.error) return j.error;
  return `HTTP ${r.status}`;
}

// setConn updates the connection badge in the header.
function setConn(text, cls) {
  const el = document.getElementById("conn");
  if (!el) return;
  el.textContent = text;
  el.className = "badge" + (cls ? " " + cls : "");
}

// ---------------------------------------------------------------------------
// Telemetry: a single SSE stream feeds live stats, state changes and logs to
// whichever view is mounted. Views register callbacks; the bus dispatches by
// topic. The browser's EventSource auto-reconnects.
// ---------------------------------------------------------------------------
const telemetry = {
  source: null,
  // latest per-component stats sample: { Component: {Counters, Gauges} }
  stats: {},
  onStats: new Set(),
  onState: new Set(),
  onLog: new Set(),
  start() {
    if (this.source) return;
    const es = new EventSource("subscribe?topics=stats,state,log");
    es.addEventListener("stats", (e) => {
      try {
        const s = JSON.parse(e.data);
        this.stats[s.Component] = s.Stats || {};
        this.onStats.forEach((cb) => cb(s));
      } catch (_) {}
    });
    es.addEventListener("state", (e) => {
      try { this.onState.forEach((cb) => cb(JSON.parse(e.data))); } catch (_) {}
    });
    es.addEventListener("log", (e) => {
      try { this.onLog.forEach((cb) => cb(JSON.parse(e.data))); } catch (_) {}
    });
    this.source = es;
  },
};

// <cs-app> is the root: it probes the gate and swaps in the setup or admin shell.
class CsApp extends HTMLElement {
  connectedCallback() {
    this.route();
    document.getElementById("tabs").addEventListener("click", (e) => {
      const btn = e.target.closest(".tab");
      if (btn) this.selectTab(btn.dataset.tab);
    });
  }
  async route() {
    try {
      const { code, body } = await api.statusProbe();
      if (code === 409) {
        setConn("setup", "");
        this.show(new CsSetup());
        return;
      }
      if (code === 401) {
        setConn("locked", "bad");
        this.show(el("div", { class: "panel" }, [
          el("h2", {}, ["Authentication required"]),
          el("p", { class: "muted" }, ["Reload and enter the web-admin credentials."]),
          button("Reload", "primary", () => location.reload()),
        ]));
        return;
      }
      setConn("connected", "ok");
      document.getElementById("tabs").classList.remove("hidden");
      telemetry.start();
      this.units = body || [];
      this.views = {
        dashboard: new CsDashboard(),
        config: new CsConfig(),
        diagnostics: new CsDiagnostics(),
        users: new CsUsers(),
        logs: new CsLogs(),
      };
      this.selectTab("dashboard");
    } catch (e) {
      setConn("offline", "bad");
      this.show(el("div", { class: "panel err" }, ["Cannot reach the server: " + e.message]));
    }
  }
  selectTab(name) {
    if (!this.views) return;
    for (const b of document.querySelectorAll(".tab")) {
      b.classList.toggle("active", b.dataset.tab === name);
    }
    const v = this.views[name];
    if (!v) return;
    this.show(v);
    if (v.activate) v.activate();
  }
  show(view) {
    this.replaceChildren(view);
  }
}

// <cs-setup> is the first-run admin-creation form.
class CsSetup extends HTMLElement {
  connectedCallback() {
    const userIn = el("input", { type: "text", autocomplete: "username" });
    const passIn = el("input", { type: "password", autocomplete: "new-password" });
    const errBox = el("div", { class: "err" });
    const submit = button("Create admin", "primary", async () => {
      errBox.textContent = "";
      try {
        await api.setup(userIn.value.trim(), passIn.value);
        location.reload(); // gate flips to Basic auth; reload so the browser prompts
      } catch (e) {
        errBox.textContent = e.message;
      }
    });
    this.replaceChildren(el("div", { class: "panel" }, [
      el("h2", {}, ["First-run setup"]),
      el("p", { class: "muted" }, ["Create the web-admin account that gates this interface."]),
      el("label", {}, ["Username"]), userIn,
      el("label", {}, ["Password"]), passIn,
      errBox,
      el("div", { class: "row" }, [submit]),
    ]));
  }
}

// DASHBOARD_GROUPS are the functional buckets the dashboard sorts components into, so
// related services sit together (the user's "group services where it makes sense").
// `members` lists the component names that belong to a group; `match` is a fallback
// predicate (by name) for instance-named components (transport ports). Order here is
// the on-screen order; anything unmatched falls into "other".
const DASHBOARD_GROUPS = [
  { id: "appletalk", label: "AppleTalk router", desc: "Routing, zones, name binding, gateways",
    members: ["Router", "RTMP", "ZIP", "NBP", "AEP", "MacIP", "IPXGW", "IPXDiag"] },
  { id: "fileservices", label: "File & print services", desc: "AFP, SMB, NetBIOS, browsing",
    members: ["AFP", "SMB", "SMB-TCP", "NetBIOS", "Browser", "Messenger"] },
  { id: "transports", label: "Transports", desc: "Link-layer ports (zone / network seed)",
    members: ["EtherTalk", "LToUDP", "TashTalk", "IPX", "NetBEUI"],
    match: (u) => (u.Kind === "port") },
  { id: "other", label: "Other", desc: "" },
];

// groupUnits buckets units by DASHBOARD_GROUPS (explicit membership first, then the
// group's match predicate), with everything else falling into "other".
function groupUnits(units) {
  const out = {};
  const memberGroup = new Map();
  for (const g of DASHBOARD_GROUPS) for (const m of g.members || []) memberGroup.set(m, g.id);
  for (const u of units) {
    let id = memberGroup.get(u.Name);
    if (!id) {
      const g = DASHBOARD_GROUPS.find((g) => g.match && g.match(u));
      id = g ? g.id : "other";
    }
    (out[id] ||= []).push(u);
  }
  return out;
}

// ---------------------------------------------------------------------------
// Dashboard: a card per component with lifecycle controls, a live-stats line,
// and a cog that opens the per-component config modal.
// ---------------------------------------------------------------------------
class CsDashboard extends HTMLElement {
  units = [];
  pending = new Set();
  connectedCallback() {
    this.statsCb = () => this.renderMetrics();
    telemetry.onStats.add(this.statsCb);
  }
  disconnectedCallback() {
    telemetry.onStats.delete(this.statsCb);
  }
  async activate() {
    try {
      this.units = await api.status();
    } catch (e) {
      this.units = [];
    }
    this.render();
  }
  async act(verb, name) {
    if (this.pending.has(name)) return;
    this.pending.add(name);
    this.render();
    try {
      await api.action(verb, name);
    } catch (e) {
      alert(e.message);
    } finally {
      this.pending.delete(name);
      setTimeout(() => this.activate(), 300);
    }
  }
  render() {
    // Nest components that hard-depend on exactly one built parent UNDER that parent
    // (e.g. SMB-TCP → SMB), so a transport child renders inside its service's card
    // rather than as a peer. A component with no (built) parent is a top-level card.
    const byName = new Map(this.units.map((u) => [u.Name, u]));
    const childrenOf = new Map();
    const isChild = new Set();
    for (const u of this.units) {
      const parents = (u.DependsOn || []).filter((d) => byName.has(d));
      if (parents.length === 1) {
        const p = parents[0];
        if (!childrenOf.has(p)) childrenOf.set(p, []);
        childrenOf.get(p).push(u);
        isChild.add(u.Name);
      }
    }
    // Top-level units (not nested under a parent) are sorted into functional GROUPS so
    // related services sit together (File services; AppleTalk router; Transports) rather
    // than one flat grid. A unit nested as a child renders inside its parent's card and
    // is skipped here.
    const tops = this.units.filter((u) => !isChild.has(u.Name));
    const groups = groupUnits(tops);

    const sections = [
      el("div", { class: "row spread" }, [
        el("h2", {}, ["Components"]),
        button("Refresh", "", () => this.activate()),
      ]),
    ];
    let any = false;
    for (const g of DASHBOARD_GROUPS) {
      const list = groups[g.id];
      if (!list || !list.length) continue;
      any = true;
      sections.push(el("div", { class: "group-head" }, [
        el("h3", {}, [g.label]),
        g.desc ? el("span", { class: "group-desc" }, [g.desc]) : "",
      ]));
      sections.push(el("div", { class: "grid" },
        list.map((u) => this.card(u, childrenOf.get(u.Name) || []))));
    }
    if (!any) {
      sections.push(el("div", { class: "panel muted" }, ["No components built (empty config)."]));
    }
    this.replaceChildren(...sections);
    this.renderMetrics();
  }
  card(u, children = []) {
    const pending = this.pending.has(u.Name);
    const indicator = pending
      ? el("span", { class: "spinner" })
      : el("span", { class: "dot " + (u.Running ? "run" : "stop") });

    const cog = button("⚙", "cog", () => openConfigModal(u.Name), false);
    cog.title = "Configure " + u.Name;

    const detail = [];
    detail.push(el("div", { class: "kv" }, [pending ? "Working…" :
      `${u.Enabled ? "Enabled" : "Disabled"} · ${u.Running ? "Running" : "Stopped"}`]));
    if (u.Kind) detail.push(kv("Kind", u.Kind));
    if (u.Binding) detail.push(kv("Binding", u.Binding));
    if (u.Props) for (const [k, v] of Object.entries(u.Props)) detail.push(kv(k, v));

    const metric = el("div", { class: "kv metric" });
    metric.dataset.metricFor = u.Name;

    const actions = el("div", { class: "card-actions" }, u.Running
      ? [
          button("Stop", "", () => this.act("stop", u.Name), pending),
          button("Restart", "", () => this.act("restart", u.Name), pending),
        ]
      : [button("Start", "primary", () => this.act("start", u.Name), pending)]);

    // Drill-down: a service with a clickable gauge (NBP registered names, MacIP active
    // leases) gets a link that opens a detail modal over the matching probe.
    const drill = drillDownFor(u.Name);

    const kids = children.length
      ? [el("div", { class: "card-children" }, [
          el("div", { class: "card-children-label" }, ["Transports"]),
          ...children.map((c) => this.childRow(c)),
        ])]
      : [];

    return el("div", { class: "card" }, [
      el("h3", {}, [indicator, el("span", { class: "card-title" }, [u.Name]), cog]),
      ...detail,
      metric,
      ...(drill ? [drill] : []),
      actions,
      ...kids,
    ]);
  }
  // childRow renders a nested dependent (e.g. SMB-TCP under SMB) as a compact line with
  // its own state dot, binding, and lifecycle controls.
  childRow(u) {
    const pending = this.pending.has(u.Name);
    const indicator = pending
      ? el("span", { class: "spinner" })
      : el("span", { class: "dot " + (u.Running ? "run" : "stop") });
    const cog = button("⚙", "cog", () => openConfigModal(u.Name), false);
    cog.title = "Configure " + u.Name;
    const actions = u.Running
      ? [button("Stop", "", () => this.act("stop", u.Name), pending),
         button("Restart", "", () => this.act("restart", u.Name), pending)]
      : [button("Start", "primary", () => this.act("start", u.Name), pending)];
    const bind = u.Binding ? el("span", { class: "muted" }, [" " + u.Binding]) : "";
    return el("div", { class: "child-row" }, [
      indicator,
      el("span", { class: "child-name" }, [u.Name]),
      bind,
      el("div", { class: "row" }, [...actions, cog]),
    ]);
  }
  renderMetrics() {
    for (const el of this.querySelectorAll("[data-metric-for]")) {
      el.textContent = metricLine(telemetry.stats[el.dataset.metricFor]);
    }
  }
}

// drillDownFor returns a clickable "view detail" link for a component whose dashboard
// stat has a backing probe — NBP's registered-name table and MacIP's lease table — or
// null for components with no drill-down. The link opens a modal listing the live rows.
function drillDownFor(name) {
  if (name === "NBP") {
    return el("div", { class: "kv drill" }, [
      linkButton("▸ Registered names", () => openProbeModal("Registered NBP names",
        () => api.registeredNames(), nbpNameColumns)),
    ]);
  }
  if (name === "MacIP") {
    return el("div", { class: "kv drill" }, [
      linkButton("▸ Active leases", () => openProbeModal("MacIP active leases",
        () => api.macipLeases(), macipLeaseColumns)),
    ]);
  }
  if (name === "EtherTalk") {
    return el("div", { class: "kv drill" }, [
      linkButton("▸ AARP table", () => openProbeModal("AARP address mapping table",
        () => api.aarpTable(), aarpEntryColumns)),
    ]);
  }
  return null;
}

// nbpNameColumns / macipLeaseColumns describe the table columns for the probe modals.
const nbpNameColumns = [
  { label: "Object", get: (r) => r.object },
  { label: "Type", get: (r) => r.type },
  { label: "Zone", get: (r) => r.zone || "*" },
  { label: "Socket", get: (r) => r.socket },
];
const macipLeaseColumns = [
  { label: "IP", get: (r) => r.ip },
  { label: "AppleTalk", get: (r) => `${r.at_network}.${r.at_node}` },
  { label: "Source", get: (r) => r.source },
];
const aarpEntryColumns = [
  { label: "Port", get: (r) => r.port },
  { label: "AppleTalk", get: (r) => `${r.network}.${r.node}` },
  { label: "MAC", get: (r) => r.mac },
];

// openProbeModal opens a read-only modal that loads rows from a probe fn and renders
// them as a table per the column spec. An empty result shows a friendly note; an
// ErrUnavailable (501) shows the "not in this build" message the api wrapper threw.
async function openProbeModal(title, load, columns) {
  const overlay = el("div", { class: "modal-overlay" });
  const body = el("div", { class: "modal-body" }, [el("p", { class: "muted" }, ["loading…"])]);
  const close = () => overlay.remove();
  overlay.append(el("div", { class: "modal" }, [
    el("div", { class: "modal-head" }, [el("h2", {}, [title]), button("✕", "modal-close", close)]),
    body,
    el("div", { class: "modal-foot" }, [button("Refresh", "", () => run()), button("Close", "primary", close)]),
  ]));
  overlay.addEventListener("click", (e) => { if (e.target === overlay) close(); });
  document.body.append(overlay);

  async function run() {
    body.replaceChildren(el("p", { class: "muted" }, ["loading…"]));
    let rows;
    try { rows = await load(); }
    catch (e) { body.replaceChildren(el("p", { class: "err" }, [e.message])); return; }
    if (!rows || !rows.length) {
      body.replaceChildren(el("p", { class: "muted" }, ["No entries."]));
      return;
    }
    const head = el("tr", {}, columns.map((c) => th(c.label)));
    const trs = rows.map((r) => el("tr", {}, columns.map((c) => el("td", {}, [String(c.get(r) ?? "")]))));
    body.replaceChildren(el("table", {}, [el("thead", {}, [head]), el("tbody", {}, trs)]));
  }
  run();
}

// metricLine renders a compact one-liner from a component's stats sample: rx/tx frame
// counters (the keys the core ports publish via component.Statful — frames_rx/tx,
// bytes_rx/tx) and any gauges it reports (active_leases, sessions, registered_names…).
function metricLine(s) {
  if (!s) return "";
  const parts = [];
  const c = s.Counters || {};
  const rx = c.frames_rx, tx = c.frames_tx;
  if (rx !== undefined || tx !== undefined) {
    parts.push(`↓ ${fmtNum(rx || 0)} fr / ${fmtBytes(c.bytes_rx || 0)}`);
    parts.push(`↑ ${fmtNum(tx || 0)} fr / ${fmtBytes(c.bytes_tx || 0)}`);
  }
  for (const [k, v] of Object.entries(s.Gauges || {})) {
    parts.push(`${k}: ${fmtNum(v)}`);
  }
  return parts.join("  ·  ");
}
function fmtNum(n) { return Number(n).toLocaleString(); }
function fmtBytes(n) {
  n = Number(n);
  if (n < 1024) return `${n} B`;
  if (n < 1048576) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1048576).toFixed(1)} MB`;
}

// ---------------------------------------------------------------------------
// Per-component config modal. It loads the live (masked) model, finds this
// component's section, renders an editable form, and POSTs /reconfigure to
// apply it LIVE. Persisting to disk is the Configuration tab's Save.
// ---------------------------------------------------------------------------
async function openConfigModal(name) {
  const overlay = el("div", { class: "modal-overlay" });
  const body = el("div", { class: "modal-body" }, [el("p", { class: "muted" }, ["loading…"])]);
  const status = el("div", { class: "err" });
  const close = () => overlay.remove();

  const applyBtn = button("Apply (live)", "primary", () => {});
  const modal = el("div", { class: "modal" }, [
    el("div", { class: "modal-head" }, [
      el("h2", {}, ["Configure " + name]),
      button("✕", "modal-close", close),
    ]),
    el("div", { class: "banner" }, ["Changes apply live and restart this component. They are not saved to disk until you Save on the Configuration tab."]),
    body, status,
    el("div", { class: "modal-foot" }, [button("Cancel", "", close), applyBtn]),
  ]);
  overlay.append(modal);
  overlay.addEventListener("click", (e) => { if (e.target === overlay) close(); });
  document.body.append(overlay);

  let model, section, key;
  try {
    model = await api.config();
    [section, key] = findSection(model, name);
  } catch (e) {
    body.replaceChildren(el("p", { class: "err" }, [e.message]));
    return;
  }
  if (!section) {
    body.replaceChildren(el("p", { class: "muted" }, ["This component has no editable config section."]));
    applyBtn.disabled = true;
    return;
  }

  const editor = sectionEditor(key, section, model);
  body.replaceChildren(editor.node);
  applyBtn.addEventListener("click", async () => {
    status.textContent = "";
    status.className = "err";
    try {
      await editor.apply();
      status.className = "ok-msg";
      status.textContent = "Applied. Restarting component…";
      setTimeout(close, 700);
    } catch (e) {
      status.textContent = e.message;
    }
  });
}

// findSection locates the model section addressed by a dashboard component name.
// It checks the well-known typed fields, the singleton Sections map, and the
// repeated Lists (by instance name). Returns [sectionObject, reconfigureKey].
function findSection(model, name) {
  const wellKnown = {
    Logging: "Logging", Router: "Router", Bridge: "Bridge",
    Identity: "Identity", AdminAuth: "AdminAuth",
  };
  if (wellKnown[name] && model[name]) return [model[name], name];
  if (model.Sections && model.Sections[name]) return [model.Sections[name], name];
  // Repeated instance: search every list for a matching instance name.
  for (const [k, list] of Object.entries(model.Lists || {})) {
    for (const inst of list || []) {
      if (inst && (inst.Name === name || inst.name === name)) return [inst, k];
    }
  }
  return [null, null];
}

// sectionEditor is the SINGLE source of truth for editing one config section, shared
// by BOTH the dashboard cog modal (openConfigModal) and the Configuration page
// (CsConfig.sectionPanel) so the two render identical fields/widgets and apply the same
// way. It builds the model-aware form (formOptionsFor) and an apply() that POSTs
// /reconfigure for the section's key. Callers wrap node/apply in their own chrome (a
// modal vs. a collapsible panel) but never re-implement the form.
function sectionEditor(key, section, model) {
  const form = sectionForm(section, formOptionsFor(key, model));
  return {
    node: form.node,
    key,
    apply: () => api.reconfigure(key, form.collect()),
    collect: () => form.collect(),
  };
}

// FIELD_META maps a section field's JSON key (the Go field name, or its toml name
// where the codec uses that) to a human label + one-line description, so the forms
// read as proper labelled fields rather than raw struct names. Unknown keys fall back
// to a humanised version of the key (camelCase → "Camel Case"). Keep this the single
// place field copy lives, shared by every form (the modal and the config page).
const FIELD_META = {
  // Identity / server
  hostname: { label: "Host name", desc: "Server name on the network (NetBIOS/SMB use it; ≤15 chars when NetBIOS is on)." },
  Hostname: { label: "Host name", desc: "Server name on the network (NetBIOS/SMB use it; ≤15 chars when NetBIOS is on)." },
  workgroup: { label: "Workgroup", desc: "SMB/NetBIOS workgroup (domain) name." },
  Workgroup: { label: "Workgroup", desc: "SMB/NetBIOS workgroup (domain) name." },
  description: { label: "Description", desc: "Free-text server comment shown to clients." },
  Description: { label: "Description", desc: "Free-text server comment shown to clients." },
  // Logging
  Level: { label: "Log level", desc: "debug · info · warn · error." },
  level: { label: "Log level", desc: "debug · info · warn · error." },
  // Router
  DefaultZone: { label: "Default zone", desc: "The AppleTalk zone the router advertises when a port seeds none." },
  Members: { label: "Members", desc: "The components that participate." },
  // AFP server
  ServerName: { label: "Server name", desc: "AppleTalk/Chooser name. Empty falls back to the host name." },
  Zone: { label: "Zone", desc: "AppleTalk zone this service advertises into. Empty = the router's default zone." },
  Transports: { label: "Transports", desc: "Which transport stacks to bind. Empty = bind all built transports." },
  TCPAddr: { label: "TCP listen address", desc: "Explicit host:port for the TCP transport (never an implicit privileged port)." },
  NBTAddr: { label: "NBT listen address", desc: "Explicit host:port for NetBIOS-over-TCP (:139). Empty = not bound." },
  ScopeID: { label: "NetBIOS scope", desc: "NetBIOS scope identifier (rarely used; empty = default scope)." },
  // Port / transport instance
  Name: { label: "Instance name", desc: "Unique name for this instance (referenced by the router's Members list)." },
  Iface: { label: "Interface", desc: "The named interface this transport binds to. Empty inherits the bridge." },
  IsEnabled: { label: "Enabled", desc: "Whether this instance is configured on (≠ currently running)." },
  MAC: { label: "Station MAC", desc: "Ethernet source address for EtherTalk. Empty = use the NIC's own MAC." },
  SeedNetwork: { label: "Seed network start", desc: "First AppleTalk network number this port asserts. 0 = non-seed (learn from a peer router)." },
  SeedNetworkEnd: { label: "Seed network end", desc: "Last network number of the seed range. 0 = a single number (== start)." },
  SeedZone: { label: "Seed zone", desc: "Default zone name this port seeds. Empty = non-seed / inherit." },
  // Interface namespace
  Kind: { label: "Kind", desc: "nic · serial · bridge." },
  Addr: { label: "Address", desc: "Optional pinned address for a NIC interface." },
  Backend: { label: "Link backend", desc: "pcap (default) · tap · tun. Only meaningful for a NIC." },
  Device: { label: "Device", desc: "Serial device path (COM3 / /dev/ttyUSB0)." },
  Baud: { label: "Baud rate", desc: "Serial line speed. 0 = adapter default." },
  // AFP volume / SMB share
  VName: { label: "Volume name", desc: "Display name shown to AFP clients." },
  SName: { label: "Share name", desc: "Display name shown to SMB clients." },
  Path: { label: "Path", desc: "Host directory backing this share." },
  FSType: { label: "Filesystem type", desc: "Storage backend (local_fs, memfs, …)." },
  ReadOnly: { label: "Read-only", desc: "Export the whole share read-only." },
  Options: { label: "Options", desc: "Backend-specific key=value parameters." },
  AllowedUsers: { label: "Allowed users", desc: "Access allow-list. Empty = guest/world." },
  ExtMapPath: { label: "Extension map file", desc: "Netatalk-style type/creator map for files with no stored Finder info." },
  // MacIP / capture
  Mode: { label: "Mode", desc: "Gateway mode (bridge / nat)." },
  Snaplen: { label: "Snap length", desc: "Bytes captured per packet (0 = full frame)." },
};

// fieldMeta returns the label + description for a field key, humanising unknown keys.
function fieldMeta(key) {
  if (FIELD_META[key]) return FIELD_META[key];
  // Humanise: split camelCase / snake_case into Title-cased words.
  const label = String(key)
    .replace(/_/g, " ")
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/^./, (c) => c.toUpperCase());
  return { label, desc: "" };
}

// fieldRow stacks a label, input, and optional hint into a field group.
function fieldRow(label, input, hint) {
  const kids = [label, input];
  if (hint) kids.push(hint);
  return el("div", { class: "field-group" }, kids);
}

// sectionForm builds an editable form from a section object by reflecting its
// JSON fields. Each scalar becomes an input typed from its value; the form
// collects edits back into an object of the same shape for /reconfigure.
//
// opts.overrides maps a field key to a widget factory (value, section) → {node,
// collect} that REPLACES the generic rendering for that field — used for the
// model-aware widgets: Router members as a port checkbox list, an interface dropdown,
// transport-binding checkboxes. opts.hide is a Set of field keys to omit entirely (a
// bridge interface has no serial Baud/Device, so they are hidden for that section).
function sectionForm(section, opts = {}) {
  const overrides = opts.overrides || {};
  const hide = opts.hide || new Set();
  const inputs = {};
  const custom = {}; // key → {node, collect} from an override
  const nodes = [];
  for (const [k, v] of Object.entries(section)) {
    if (hide.has(k)) continue;
    if (overrides[k]) {
      const w = overrides[k](v, section);
      custom[k] = w;
      nodes.push(...(Array.isArray(w.nodes) ? w.nodes : [w.node]));
      continue;
    }
    const meta = fieldMeta(k);
    const label = el("label", {}, [meta.label]);
    const hint = meta.desc ? el("p", { class: "field-hint" }, [meta.desc]) : null;
    let input;
    if (typeof v === "boolean") {
      input = el("input", { type: "checkbox" });
      if (v) input.checked = true;
      const row = [el("label", { class: "inline" }, [input, meta.label])];
      if (hint) row.push(hint);
      nodes.push(el("div", { class: "field-group" }, row));
    } else if (typeof v === "number") {
      input = el("input", { type: "number", value: String(v) });
      nodes.push(fieldRow(label, input, hint));
    } else if (Array.isArray(v)) {
      input = el("textarea", {}, [v.join("\n")]);
      input.dataset.kind = "list";
      nodes.push(fieldRow(label, input, hint || el("p", { class: "field-hint" }, ["one entry per line"])));
    } else if (v !== null && typeof v === "object") {
      // Nested object (e.g. an Options map): edit as JSON.
      input = el("textarea", {}, [JSON.stringify(v, null, 2)]);
      input.dataset.kind = "json";
      nodes.push(fieldRow(label, input, hint));
    } else {
      input = el("input", { type: "text", value: v == null ? "" : String(v) });
      nodes.push(fieldRow(label, input, hint));
    }
    inputs[k] = { input, orig: v };
  }
  return {
    node: el("div", {}, nodes),
    // _fields / _nodes are exposed so instanceForm can reuse per-field rendering while
    // substituting its own widgets (a path picker, an fs_type dropdown) for some keys.
    _fields: inputs,
    _nodes: nodes,
    collect() {
      const out = {};
      // Hidden fields are preserved verbatim from the original section so a bridge's
      // unused serial keys (Baud/Device) round-trip unchanged rather than being dropped.
      for (const k of hide) if (k in section) out[k] = section[k];
      for (const [k, { input, orig }] of Object.entries(inputs)) {
        if (typeof orig === "boolean") out[k] = input.checked;
        else if (typeof orig === "number") out[k] = Number(input.value);
        else if (Array.isArray(orig)) {
          out[k] = input.value.split("\n").map((s) => s.trim()).filter(Boolean);
        } else if (input.dataset.kind === "json") {
          try { out[k] = JSON.parse(input.value); } catch (_) { out[k] = orig; }
        } else out[k] = input.value;
      }
      for (const [k, w] of Object.entries(custom)) out[k] = w.collect();
      return out;
    },
  };
}

// ---- model-aware form widgets -------------------------------------------------------

// checkboxList renders a labelled checkbox group bound to a list value (the selected
// subset of `choices`). collect() returns the checked tokens in `choices` order. Used
// for transport bindings (an empty result means "bind all" by the section's own rule,
// so the hint says so).
function checkboxList(label, choices, selected, hint) {
  const set = new Set(selected || []);
  const boxes = choices.map((c) => {
    const cb = el("input", { type: "checkbox", value: c });
    if (set.has(c)) cb.checked = true;
    return { c, cb };
  });
  const node = el("div", { class: "field-group" }, [
    el("label", {}, [label]),
    ...boxes.map(({ c, cb }) => el("label", { class: "inline" }, [cb, c])),
    ...(hint ? [el("p", { class: "field-hint" }, [hint])] : []),
  ]);
  return {
    node,
    collect: () => boxes.filter((b) => b.cb.checked).map((b) => b.c),
  };
}

// dropdown renders a labelled <select> over `choices` (each {value,label}) bound to a
// single string value. An empty option is prepended so "inherit / none" is selectable.
function dropdown(label, choices, value, hint) {
  const sel = el("select", {}, [
    el("option", value ? { value: "" } : { value: "", selected: "" }, ["(none / inherit)"]),
    ...choices.map((c) => {
      const v = typeof c === "string" ? c : c.value;
      const t = typeof c === "string" ? c : c.label;
      return el("option", v === value ? { value: v, selected: "" } : { value: v }, [t]);
    }),
  ]);
  const node = el("div", { class: "field-group" }, [
    el("label", {}, [label]), sel,
    ...(hint ? [el("p", { class: "field-hint" }, [hint])] : []),
  ]);
  return { node, collect: () => sel.value };
}

// portInstanceNames returns the names of the configured port instances in the model —
// the candidates for [Router].Members. Ports live in Model.Lists keyed by a transport
// schema (EtherTalk/TashTalk/LToUDP/IPX/NetBEUI); each instance's name is its join key.
function portInstanceNames(model) {
  const names = [];
  for (const [, list] of Object.entries(model.Lists || {})) {
    for (const inst of list || []) {
      const n = instName(inst);
      // Heuristic: a port instance carries an interface/transport shape, not a path —
      // exclude AFP volumes / SMB shares (which have a Path/FSType).
      if (n && !("Path" in inst) && !("path" in inst) && !("FSType" in inst) && !("fs_type" in inst)) {
        names.push(n);
      }
    }
  }
  return names;
}

// interfaceChoices returns the selectable interface names: the declared namespace
// entries (Model.Interfaces) plus the default Bridge, for a port/bridge interface
// dropdown. Host NICs are offered too via the live enumerator where relevant.
function interfaceChoices(model) {
  const names = new Set();
  for (const k of Object.keys(model.Interfaces || {})) names.add(k);
  if (model.Bridge && model.Bridge.Name) names.add(model.Bridge.Name);
  return [...names];
}

// APPLETALK_TRANSPORTS are the transport schema keys whose port instances seed the
// AppleTalk internet (zone + network range): the forms for these surface the seed
// fields and an interface picker. IPX/NetBEUI are transports too but carry no AppleTalk
// seed, so they only get the interface picker.
const APPLETALK_TRANSPORTS = ["EtherTalk", "LToUDP", "TashTalk"];
const NON_AT_TRANSPORTS = ["IPX", "NetBEUI"];

// formOptionsFor returns the sectionForm opts (overrides + hidden fields) for a given
// section/reconfigure key, making the generic form model-aware: Router gets a member
// checkbox list; SMB/NetBIOS/AFP get transport-binding checkboxes; Bridge gets a member
// multi-select with serial fields hidden; an AppleTalk transport port gets an interface
// dropdown plus its zone/network seed fields surfaced.
function formOptionsFor(name, model) {
  const opts = { overrides: {}, hide: new Set() };
  if (name === "Router") {
    opts.overrides.Members = (v) => checkboxList("Members — ports that join this router",
      portInstanceNames(model), v, "Checked ports forward/participate in RTMP+ZIP; unchecked ports run standalone on their own segment.");
  } else if (name === "SMB") {
    opts.overrides.Transports = (v) => checkboxList("Transports",
      ["netbeui", "ipx", "nbt", "tcp"], v, "Empty = bind all built transports. tcp/nbt also need an address below.");
  } else if (name === "NetBIOS") {
    opts.overrides.Transports = (v) => checkboxList("Transports",
      ["netbeui", "ipx", "nbt"], v, "Empty = bind all built transports.");
  } else if (name === "AFP") {
    opts.overrides.Transports = (v) => checkboxList("Transports",
      ["ddp", "tcp"], v, "ddp = classic (needs router membership); tcp = AFP-over-TCP (DSI). Empty = all.");
  } else if (name === "EtherDFS") {
    // EtherDFS binds ONE NIC for its raw-Ethernet (EtherType 0xEDF5) link, so its
    // Interface is a single-select dropdown like a transport port's.
    opts.overrides.Interface = (v) => dropdown("Interface — the NIC this EtherDFS server binds to",
      interfaceChoices(model), v, "Empty inherits the shared bridge interface.");
  } else if (name === "Bridge") {
    // A bridge aggregates member interfaces; it has no serial line speed or device.
    opts.hide.add("Baud").add("Device").add("Kind");
    opts.overrides.Members = (v) => checkboxList("Member interfaces",
      interfaceChoices(model).filter((n) => n !== model.Bridge?.Name), v,
      "The NICs this bridge aggregates.");
  } else if (APPLETALK_TRANSPORTS.includes(name) || NON_AT_TRANSPORTS.includes(name)) {
    // A transport port binds ONE interface from the namespace (plus the implicit
    // bridge inheritance), so Iface is a single-select dropdown rather than free text.
    opts.overrides.Iface = (v) => dropdown("Interface — the named interface this port binds to",
      interfaceChoices(model), v, "Empty inherits the shared bridge interface.");
    if (NON_AT_TRANSPORTS.includes(name)) {
      // IPX / NetBEUI carry no AppleTalk seed; hide the AppleTalk-only fields.
      opts.hide.add("SeedNetwork").add("SeedNetworkEnd").add("SeedZone").add("MAC");
    } else if (name !== "EtherTalk") {
      // Only EtherTalk uses a station MAC; LToUDP/TashTalk have none.
      opts.hide.add("MAC");
    }
  }
  return opts;
}

// ---------------------------------------------------------------------------
// Configuration tab: shows every editable section, drives Save (persist) and a
// client-side Download of the live model as a JSON backup.
// ---------------------------------------------------------------------------
class CsConfig extends HTMLElement {
  async activate() {
    this.render(el("p", { class: "muted" }, ["loading…"]));
    try {
      this.model = await api.config();
      // Status tells us which components were actually built, so the share/volume
      // editors and binding panels show even when their lists are still empty.
      this.units = await api.status().catch(() => []);
    } catch (e) {
      this.render(el("p", { class: "err" }, [e.message]));
      return;
    }
    this.renderModel();
  }
  hasComponent(name) {
    return (this.units || []).some((u) => u.Name === name);
  }
  render(child) {
    this.replaceChildren(el("div", { class: "panel" }, [el("h2", {}, ["Configuration"]), child]));
  }
  renderModel() {
    const status = el("div", { class: "err" });
    const save = button("Save to disk", "primary", async () => {
      status.textContent = ""; status.className = "err";
      try {
        const { revision } = await api.save();
        status.className = "ok-msg";
        status.textContent = "Saved. Backup revision: " + revision;
      } catch (e) { status.textContent = e.message; }
    });
    // Download the faithful on-disk server.toml (server-rendered through the codec),
    // not the JSON model shape — the operator wants a real backup file.
    const download = button("Download server.toml", "", () => api.downloadConfig());

    const singletons = this.editableSections().map(([name, key, sec]) =>
      this.sectionPanel(name, key, sec));

    this.replaceChildren(
      el("div", { class: "banner" }, ["Saving rewrites server.toml and drops any comments in the file. Apply changes a section live without persisting; Save writes them all to disk."]),
      el("div", { class: "row", style: "margin-bottom:14px" }, [save, download]),
      status,
      // Repeated-section editors (AFP volumes / SMB shares) with Add/Delete + a path picker.
      ...this.listEditors(),
      // Interface namespace editor (NIC / serial / bridge entries ports bind to).
      new CsInterfaces(this.model, () => this.activate()),
      // Extension-map grid editor (a file, not a model section).
      this.extMapPanel(),
      ...singletons,
    );
  }

  // sectionPanel renders one singleton section as a collapsible form with Apply (live).
  // It reuses the SAME sectionEditor as the dashboard cog modal, so the fields, widgets,
  // and apply behaviour are identical between the two surfaces.
  sectionPanel(name, key, sec) {
    const editor = sectionEditor(key, sec, this.model);
    const sstat = el("div", { class: "err" });
    const apply = button("Apply (live)", "", async () => {
      sstat.textContent = ""; sstat.className = "err";
      try {
        await editor.apply();
        sstat.className = "ok-msg"; sstat.textContent = "Applied.";
      } catch (e) { sstat.textContent = e.message; }
    });
    return el("details", { class: "panel" }, [
      el("summary", {}, [name]),
      editor.node,
      el("div", { class: "row" }, [apply]),
      sstat,
    ]);
  }

  // listEditors builds one panel per repeated-section key (AFPVolumes/SMBShares): a row
  // per instance with Edit/Delete and an Add form. The owner component (AFP/SMB) drives
  // the live reconcile. The well-known editors are shown whenever their owner component
  // was built (even with no instances yet, so "Add volume/share" is always reachable);
  // any other repeated key present in the model is shown too.
  listEditors() {
    const known = [
      { key: "AFPVolumes", owner: "AFP" },
      { key: "SMBShares", owner: "SMB" },
      { key: "EtherDFSDrives", owner: "EtherDFS" },
    ];
    const lists = this.model.Lists || {};
    const shown = new Set();
    const out = [];
    for (const { key, owner } of known) {
      if (this.hasComponent(owner) || lists[key]) {
        out.push(new CsInstanceEditor(key, owner, lists[key] || [], () => this.activate()));
        shown.add(key);
      }
    }
    for (const [key, list] of Object.entries(lists)) {
      if (shown.has(key)) continue;
      out.push(new CsInstanceEditor(key, key, list || [], () => this.activate()));
    }
    return out;
  }

  // extMapPanel is a collapsible 3-column (Extension/Creator/Type) grid editor over an
  // extension-map file. The file path is taken from the first AFP volume that names an
  // ExtMapPath; the operator can point it elsewhere.
  extMapPanel() {
    const ed = new CsExtMap();
    // Seed the path from an AFP volume's ExtMapPath, if any.
    for (const inst of (this.model.Lists && this.model.Lists.AFPVolumes) || []) {
      if (inst.ExtMapPath || inst.extmap_path) { ed.path = inst.ExtMapPath || inst.extmap_path; break; }
    }
    return ed;
  }
  // editableSections returns the SINGLETON sections as [displayName, reconfigureKey,
  // section] tuples: the well-known typed fields + the registered Sections map. Repeated
  // instances (Lists) are handled separately by listEditors (they need Add/Delete).
  //
  // Server-level singletons (AFP/SMB/NetBIOS) are SYNTHESISED with their defaults when
  // their owner component exists but the model carries no section yet — so an operator
  // can set the AFP name/zone or SMB/NetBIOS bindings on a fresh install without first
  // hand-editing server.toml. The synthesised default reconfigures live like a stored one.
  editableSections() {
    const m = this.model, out = [];
    for (const k of ["Identity", "Logging", "Router", "Bridge", "Capture"]) {
      if (m[k] && typeof m[k] === "object") out.push([k, k, m[k]]);
    }
    const sections = { ...(m.Sections || {}) };
    // Synthesise server singletons for built components missing a section.
    const serverDefaults = {
      AFP: { ServerName: "", Zone: "", Transports: [], TCPAddr: "" },
      SMB: { Transports: [], TCPAddr: "", NBTAddr: "" },
      NetBIOS: { Transports: [], ScopeID: "" },
      // EtherDFS is BOTH the wire endpoint and the file server, so its singleton
      // section carries the NIC binding (Interface/MAC/IsEnabled) plus the name
      // advertised in install checks.
      EtherDFS: { IsEnabled: false, Interface: "", MAC: "", ServerName: "" },
    };
    for (const [key, def] of Object.entries(serverDefaults)) {
      if (!sections[key] && this.hasComponent(key)) sections[key] = def;
    }
    for (const [k, sec] of Object.entries(sections)) out.push([k, k, sec]);
    return out;
  }
}

// ---------------------------------------------------------------------------
// <cs-instance-editor> — the AFP-volume / SMB-share table editor: a row per
// instance with Edit (opens the section form modal) and Delete, plus an Add
// form. It drives /add_instance, /remove_instance, /reconfigure; the owning
// service reconciles its live set. The name/path fields carry a Browse picker.
// ---------------------------------------------------------------------------
class CsInstanceEditor extends HTMLElement {
  constructor(key, owner, list, onChange) {
    super();
    this.key = key; this.owner = owner; this.list = list; this.onChange = onChange;
  }
  connectedCallback() { this.render(); }
  async add(section) {
    await api.addInstance(this.owner, this.key, section);
    this.onChange();
  }
  async remove(name) {
    if (!confirm(`Remove ${this.key} "${name}"?`)) return;
    try { await api.removeInstance(this.owner, this.key, name); this.onChange(); }
    catch (e) { alert(e.message); }
  }
  render() {
    const title = this.key === "AFPVolumes" ? "AFP Volumes"
      : this.key === "SMBShares" ? "SMB Shares"
      : this.key === "EtherDFSDrives" ? "EtherDFS Drives" : this.key;
    const rows = this.list.map((inst) => {
      const name = instName(inst);
      const path = inst.Path || inst.path || "";
      const ro = inst.ReadOnly || inst.read_only;
      const fst = inst.FSType || inst.fs_type || "";
      return el("tr", {}, [
        el("td", {}, [name]),
        el("td", { class: "muted" }, [path]),
        el("td", { class: "muted" }, [fst]),
        el("td", {}, [ro ? "ro" : "rw"]),
        el("td", {}, [el("div", { class: "row" }, [
          button("Edit", "", () => openInstanceModal(this.key, this.owner, inst, this.onChange)),
          button("Delete", "danger", () => this.remove(name)),
        ])]),
      ]);
    });
    const table = el("table", {}, [
      el("thead", {}, [el("tr", {}, [th("Name"), th("Path"), th("FS"), th("Mode"), th("")])]),
      el("tbody", {}, rows.length ? rows : [el("tr", {}, [el("td", { class: "muted", colspan: "5" }, ["No entries."])])]),
    ]);
    this.replaceChildren(el("details", { class: "panel", open: "" }, [
      el("summary", {}, [title]),
      table,
      el("div", { class: "row" }, [
        button("Add " + (this.key === "AFPVolumes" ? "volume" : this.key === "SMBShares" ? "share" : this.key === "EtherDFSDrives" ? "drive" : "entry"), "primary",
          () => openInstanceModal(this.key, this.owner, this.blankInstance(), this.onChange)),
      ]),
    ]));
  }
  // blankInstance seeds a new instance from an existing one's shape (so the form has the
  // right fields, keyed by the Go FIELD names the JSON uses — VName/FSType/Path, not the
  // toml names), with empty values. Falls back to the known volume/share field set when
  // the list is empty.
  blankInstance() {
    if (this.list.length) {
      const tmpl = this.list[0], out = {};
      for (const [k, v] of Object.entries(tmpl)) {
        out[k] = typeof v === "boolean" ? false : Array.isArray(v) ? [] : typeof v === "number" ? 0 : "";
      }
      return out;
    }
    const nameKey = this.key === "SMBShares" ? "SName" : this.key === "EtherDFSDrives" ? "DName" : "VName";
    const base = { [nameKey]: "", FSType: "local_fs", Path: "", ReadOnly: false, Options: [] };
    if (this.key === "SMBShares") base.Description = "";
    if (this.key === "AFPVolumes") base.ExtMapPath = "";
    // EtherDFS drives default to the "short" name engine so DOS sees 8.3 names.
    if (this.key === "EtherDFSDrives") base.NameEngine = "short";
    return base;
  }
}

// instName returns a repeated instance's name, trying the Go field names the JSON uses
// (VName for AFP volumes, SName for SMB shares) and the generic Name/name fallbacks.
function instName(inst) {
  return inst.VName || inst.SName || inst.Name || inst.name || "";
}

// openInstanceModal edits one repeated-section instance (a volume/share). isNew is
// inferred from whether the instance currently exists in the list; a create goes through
// /add_instance, an edit through /reconfigure. The path field gets a Browse picker and
// fs_type a dropdown from /list_fs_types.
async function openInstanceModal(key, owner, inst, onChange) {
  const isNew = !instName(inst);
  const overlay = el("div", { class: "modal-overlay" });
  const body = el("div", { class: "modal-body" }, [el("p", { class: "muted" }, ["loading…"])]);
  const status = el("div", { class: "err" });
  const close = () => overlay.remove();
  const saveBtn = button(isNew ? "Create" : "Save", "primary", () => {});
  overlay.append(el("div", { class: "modal" }, [
    el("div", { class: "modal-head" }, [el("h2", {}, [(isNew ? "Add to " : "Edit ") + key]), button("✕", "modal-close", close)]),
    body, status,
    el("div", { class: "modal-foot" }, [button("Cancel", "", close), saveBtn]),
  ]));
  overlay.addEventListener("click", (e) => { if (e.target === overlay) close(); });
  document.body.append(overlay);

  const fsTypes = await api.fsTypes().catch(() => []);
  const form = instanceForm(inst, fsTypes);
  body.replaceChildren(form.node);

  saveBtn.addEventListener("click", async () => {
    status.textContent = ""; status.className = "err";
    const section = form.collect();
    try {
      if (isNew) await api.addInstance(owner, key, section);
      else await api.reconfigure(key, section);
      status.className = "ok-msg"; status.textContent = "Saved.";
      setTimeout(() => { close(); onChange(); }, 500);
    } catch (e) { status.textContent = e.message; }
  });
}

// instanceForm renders a volume/share form: a Browse-able path field, an fs_type
// dropdown, and the remaining fields via the generic reflection. Returns {node, collect}.
function instanceForm(inst, fsTypes) {
  const base = sectionForm(inst);
  // Decorate: replace the plain path input with one carrying a Browse button, and
  // swap fs_type for a dropdown. We rebuild the node from inst to control field order.
  const nodes = [];
  const fields = {};

  const pathKey = "Path" in inst ? "Path" : "path" in inst ? "path" : null;
  const fsKey = "FSType" in inst ? "FSType" : "fs_type" in inst ? "fs_type" : null;

  for (const [k, v] of Object.entries(inst)) {
    if (k === pathKey) {
      const meta = fieldMeta(k);
      const inp = el("input", { type: "text", value: v || "" });
      fields[k] = { input: inp, orig: v };
      const kids = [el("label", {}, [meta.label]), pathPicker(inp)];
      if (meta.desc) kids.push(el("p", { class: "field-hint" }, [meta.desc]));
      nodes.push(el("div", { class: "field-group" }, kids));
    } else if (k === fsKey && fsTypes.length) {
      const meta = fieldMeta(k);
      const sel = el("select", {}, fsTypes.map((t) =>
        el("option", t === v ? { value: t, selected: "" } : { value: t }, [t])));
      fields[k] = { input: sel, orig: v };
      const kids = [el("label", {}, [meta.label]), sel];
      if (meta.desc) kids.push(el("p", { class: "field-hint" }, [meta.desc]));
      nodes.push(el("div", { class: "field-group" }, kids));
    } else {
      // fall back to the generic single-field rendering
      const one = sectionForm({ [k]: v });
      fields[k] = one._fields[k];
      nodes.push(...one._nodes);
    }
  }
  return {
    node: el("div", {}, nodes),
    collect() {
      const out = {};
      for (const [k, { input, orig }] of Object.entries(fields)) {
        if (typeof orig === "boolean") out[k] = input.checked;
        else if (typeof orig === "number") out[k] = Number(input.value);
        else if (Array.isArray(orig)) out[k] = input.value.split("\n").map((s) => s.trim()).filter(Boolean);
        else if (input.dataset && input.dataset.kind === "json") { try { out[k] = JSON.parse(input.value); } catch (_) { out[k] = orig; } }
        else out[k] = input.value;
      }
      return out;
    },
  };
}

// pathPicker wraps a text input with a Browse… button that opens a directory browser
// modal (over /browse_path) and writes the chosen directory back into the input.
function pathPicker(input) {
  const browse = button("Browse…", "", () => openPathBrowser(input.value, (p) => { input.value = p; }));
  return el("div", { class: "row" }, [input, browse]);
}

// openPathBrowser is a modal directory navigator over /browse_path: it lists the
// subdirectories of the current dir, lets the operator descend (or go up via ‹parent›),
// and Select chooses the current directory.
async function openPathBrowser(startDir, onPick) {
  const overlay = el("div", { class: "modal-overlay" });
  const listBox = el("div", {});
  const here = el("div", { class: "muted", style: "margin-bottom:8px;word-break:break-all" });
  const close = () => overlay.remove();
  let cur = startDir || "";

  async function go(dir) {
    try {
      const res = await api.browsePath(dir);
      cur = res.path;
      here.textContent = cur;
      const items = [el("button", { class: "" }, ["‹ parent"])];
      items[0].addEventListener("click", () => go(res.parent));
      for (const e of res.entries) {
        const b = el("button", {}, ["📁 " + e.name]);
        b.addEventListener("click", () => go(cur.replace(/[\\/]+$/, "") + "/" + e.name));
        items.push(b);
      }
      listBox.replaceChildren(el("div", { class: "row", style: "flex-wrap:wrap;gap:6px" }, items));
    } catch (err) {
      listBox.replaceChildren(el("p", { class: "err" }, [err.message]));
    }
  }

  overlay.append(el("div", { class: "modal" }, [
    el("div", { class: "modal-head" }, [el("h2", {}, ["Choose a directory"]), button("✕", "modal-close", close)]),
    el("div", { class: "modal-body" }, [here, listBox]),
    el("div", { class: "modal-foot" }, [
      button("Cancel", "", close),
      button("Select this folder", "primary", () => { onPick(cur); close(); }),
    ]),
  ]));
  overlay.addEventListener("click", (e) => { if (e.target === overlay) close(); });
  document.body.append(overlay);
  go(cur);
}

// ---------------------------------------------------------------------------
// <cs-interfaces> — the interface-namespace editor (Model.Interfaces): a row per
// named NIC/serial/bridge entry with Edit/Delete and an Add form, over
// /set_interface and /remove_interface. Kind-aware: a bridge picks member
// interfaces (no baud); a serial picks a device + baud; a nic picks an address +
// backend (pcap/tap/tun). The NIC device dropdown is populated from /list_interfaces.
// ---------------------------------------------------------------------------
class CsInterfaces extends HTMLElement {
  constructor(model, onChange) {
    super();
    this.model = model;
    this.onChange = onChange;
    this.ifaces = model.Interfaces || {};
  }
  connectedCallback() { this.render(); }
  async remove(name) {
    if (!confirm(`Remove interface "${name}"?`)) return;
    try { await api.removeInterface(name); this.onChange(); }
    catch (e) { alert(e.message); }
  }
  render() {
    const entries = Object.entries(this.ifaces);
    const rows = entries.map(([name, iface]) => el("tr", {}, [
      el("td", {}, [name]),
      el("td", { class: "muted" }, [iface.Kind || "nic"]),
      el("td", { class: "muted" }, [ifaceSummary(iface)]),
      el("td", {}, [el("div", { class: "row" }, [
        button("Edit", "", () => openInterfaceModal({ Name: name, ...iface }, this.model, this.onChange)),
        button("Delete", "danger", () => this.remove(name)),
      ])]),
    ]));
    const table = el("table", {}, [
      el("thead", {}, [el("tr", {}, [th("Name"), th("Kind"), th("Detail"), th("")])]),
      el("tbody", {}, rows.length ? rows : [el("tr", {}, [el("td", { class: "muted", colspan: "4" }, ["No interfaces declared. Ports fall back to a bare NIC by name."])])]),
    ]);
    this.replaceChildren(el("details", { class: "panel" }, [
      el("summary", {}, ["Interfaces"]),
      el("p", { class: "field-hint" }, ["Named NIC / serial / bridge entries a transport binds to. A bridge aggregates NICs and has no baud rate."]),
      table,
      el("div", { class: "row" }, [
        button("Add interface", "primary", () => openInterfaceModal(blankInterface(), this.model, this.onChange)),
      ]),
    ]));
  }
}

// ifaceSummary renders a one-line detail for an interface row by kind.
function ifaceSummary(iface) {
  const kind = iface.Kind || "nic";
  if (kind === "bridge") return "members: " + ((iface.Members || []).join(", ") || "—");
  if (kind === "serial") return `${iface.Device || "—"} @ ${iface.Baud || "default"}`;
  return [iface.Addr, iface.Backend].filter(Boolean).join(" · ") || "pcap";
}

// blankInterface seeds a new nic-kind entry.
function blankInterface() {
  return { Name: "", Kind: "nic", Addr: "", Backend: "pcap", Device: "", Baud: 0, Members: [] };
}

// openInterfaceModal edits one interface-namespace entry with kind-aware fields. The
// Kind selector swaps the field set live: nic → address + backend (+ device dropdown
// from /list_interfaces); serial → device + baud; bridge → member checkboxes (NO baud).
async function openInterfaceModal(iface, model, onChange) {
  const overlay = el("div", { class: "modal-overlay" });
  const body = el("div", { class: "modal-body" });
  const status = el("div", { class: "err" });
  const close = () => overlay.remove();
  const saveBtn = button("Save", "primary", () => {});
  overlay.append(el("div", { class: "modal" }, [
    el("div", { class: "modal-head" }, [el("h2", {}, ["Interface"]), button("✕", "modal-close", close)]),
    body, status,
    el("div", { class: "modal-foot" }, [button("Cancel", "", close), saveBtn]),
  ]));
  overlay.addEventListener("click", (e) => { if (e.target === overlay) close(); });
  document.body.append(overlay);

  const hostNics = await api.interfaces().catch(() => []);
  const cur = { ...iface };
  const nameIn = el("input", { type: "text", value: cur.Name || "", placeholder: "e.g. eth0, br-lan, ttyUSB-attic" });
  const kindSel = el("select", {}, ["nic", "serial", "bridge"].map((k) =>
    el("option", k === (cur.Kind || "nic") ? { value: k, selected: "" } : { value: k }, [k])));
  const fields = el("div", {});

  let collectKind = () => ({});
  function renderFields() {
    const kind = kindSel.value;
    if (kind === "nic") {
      const nicNames = hostNics.map((n) => n.Name);
      const dev = dropdown("Device (host NIC)", nicNames, cur.Name || "", "Pick a detected NIC, or type the name above.");
      const addr = el("input", { type: "text", value: cur.Addr || "", placeholder: "optional pinned address" });
      const backend = dropdown("Link backend", ["pcap", "tap", "tun"], cur.Backend || "pcap", "pcap is the default and only backend wired today.");
      fields.replaceChildren(dev.node, el("label", {}, ["Address"]), addr, backend.node);
      collectKind = () => ({ Kind: "nic", Addr: addr.value, Backend: backend.collect(), Device: "", Baud: 0, Members: [] });
    } else if (kind === "serial") {
      const device = el("input", { type: "text", value: cur.Device || "", placeholder: "COM3 / /dev/ttyUSB0" });
      const baud = el("input", { type: "number", value: String(cur.Baud || 0) });
      fields.replaceChildren(el("label", {}, ["Device"]), device, el("label", {}, ["Baud (0 = default)"]), baud);
      collectKind = () => ({ Kind: "serial", Device: device.value, Baud: Number(baud.value), Addr: "", Backend: "", Members: [] });
    } else { // bridge — member NICs, NO baud
      const choices = [...new Set([...hostNics.map((n) => n.Name), ...Object.keys(model.Interfaces || {})])]
        .filter((n) => n && n !== cur.Name);
      const members = checkboxList("Member interfaces", choices, cur.Members || [], "The NICs this bridge aggregates. A bridge has no baud rate.");
      fields.replaceChildren(members.node);
      collectKind = () => ({ Kind: "bridge", Members: members.collect(), Addr: "", Backend: "", Device: "", Baud: 0 });
    }
  }
  kindSel.addEventListener("change", renderFields);
  body.replaceChildren(
    el("label", {}, ["Name"]), nameIn,
    el("label", {}, ["Kind"]), kindSel,
    fields,
  );
  renderFields();

  saveBtn.addEventListener("click", async () => {
    status.textContent = ""; status.className = "err";
    const name = nameIn.value.trim();
    if (!name) { status.textContent = "Name is required."; return; }
    try {
      await api.setInterface({ Name: name, ...collectKind() });
      status.className = "ok-msg"; status.textContent = "Saved.";
      setTimeout(() => { close(); onChange(); }, 400);
    } catch (e) { status.textContent = e.message; }
  });
}

// ---------------------------------------------------------------------------
// <cs-extmap> — the AFP extension-map editor: a 3-column grid (Extension /
// Creator / Type) loaded from /extmap and serialised back to the Netatalk
// `.ext "TYPE" "CRTR"` format on save. Add/delete rows; the path is editable.
// ---------------------------------------------------------------------------
class CsExtMap extends HTMLElement {
  path = "";
  rows = []; // {ext, creator, type}
  connectedCallback() { this.render(); }
  async load() {
    if (!this.path) { this.rows = []; this.render(); return; }
    try {
      const { content } = await api.extMap(this.path);
      this.rows = parseExtMap(content || "");
    } catch (e) { this.rows = []; }
    this.render();
  }
  render() {
    const pathIn = el("input", { type: "text", value: this.path, placeholder: "path to extmap file (e.g. extmap.conf)" });
    const status = el("div", { class: "err" });

    const body = el("tbody", {}, this.rows.map((r, i) => el("tr", {}, [
      el("td", {}, [extCell(r, "ext", 0)]),
      el("td", {}, [extCell(r, "creator", 4)]),
      el("td", {}, [extCell(r, "type", 4)]),
      el("td", {}, [button("✕", "danger", () => { this.rows.splice(i, 1); this.render(); })]),
    ])));

    const grid = el("table", {}, [
      el("thead", {}, [el("tr", {}, [th("Extension"), th("Creator"), th("Type"), th("")])]),
      body,
    ]);

    this.replaceChildren(el("details", { class: "panel" }, [
      el("summary", {}, ["Extension map (type/creator)"]),
      el("p", { class: "field-hint" }, ["Defaults a file's classic Mac type/creator from its extension when none is stored (e.g. .txt → TEXT/ttxt)."]),
      el("label", {}, ["File path"]),
      el("div", { class: "row" }, [pathIn, button("Load", "", () => { this.path = pathIn.value.trim(); this.load(); })]),
      grid,
      el("div", { class: "row" }, [
        button("Add row", "", () => { this.rows.push({ ext: "", creator: "", type: "" }); this.render(); }),
        button("Save extension map", "primary", async () => {
          status.textContent = ""; status.className = "err";
          this.path = pathIn.value.trim();
          if (!this.path) { status.textContent = "Set a file path first."; return; }
          try {
            const { backup } = await api.saveExtMap(this.path, serializeExtMap(this.rows));
            status.className = "ok-msg";
            status.textContent = "Saved." + (backup ? " Backup: " + backup : "");
          } catch (e) { status.textContent = e.message; }
        }),
      ]),
      status,
    ]));
  }
}
// extCell builds a fixed-width text cell bound to row[field]; maxLen caps OSType width.
function extCell(row, field, maxLen) {
  const inp = el("input", { type: "text", value: row[field] || "" });
  if (maxLen) inp.maxLength = maxLen;
  inp.addEventListener("input", () => { row[field] = inp.value; });
  return inp;
}
// parseExtMap reads Netatalk `.ext "TYPE" "CRTR"` lines into {ext,creator,type} rows.
function parseExtMap(text) {
  const rows = [];
  for (const line of text.split("\n")) {
    const m = line.trim().match(/^(\S+)\s+"([^"]*)"\s+"([^"]*)"/);
    if (m) rows.push({ ext: m[1].replace(/^\./, ""), type: m[2], creator: m[3] });
  }
  return rows;
}
// serializeExtMap writes rows back to the Netatalk format (extension, type, creator).
function serializeExtMap(rows) {
  return rows
    .filter((r) => r.ext && r.type && r.creator)
    .map((r) => `.${r.ext} "${r.type}" "${r.creator}"`)
    .join("\n") + "\n";
}

// ---------------------------------------------------------------------------
// Diagnostics tab: read-only probes over the control plane.
// ---------------------------------------------------------------------------
class CsDiagnostics extends HTMLElement {
  activate() { this.render(); }
  render() {
    this.out = el("pre", {}, ["Pick a probe above."]);
    this.replaceChildren(el("div", { class: "panel" }, [
      el("h2", {}, ["Diagnostics"]),
      el("div", { class: "tools" }, [
        button("List Zones", "", () => this.run("Zones", api.zones())),
        button("List Interfaces", "", () => this.run("Interfaces", api.interfaces())),
        button("List Serial Ports", "", () => this.run("Serial ports", api.serialPorts())),
        button("Component Status", "", () => this.run("Status", api.status())),
      ]),
      this.out,
    ]));
  }
  async run(label, promise) {
    this.out.textContent = label + ": loading…";
    try {
      const data = await promise;
      this.out.textContent = label + ":\n" + JSON.stringify(data, null, 2);
    } catch (e) {
      this.out.textContent = label + " failed: " + e.message;
    }
  }
}

// ---------------------------------------------------------------------------
// Users tab: CRUD over the auth store (when a user store is wired).
// ---------------------------------------------------------------------------
class CsUsers extends HTMLElement {
  async activate() {
    this.replaceChildren(el("div", { class: "panel" }, [el("h2", {}, ["Users"]), el("p", { class: "muted" }, ["loading…"])]));
    let res;
    try { res = await api.users(); }
    catch (e) {
      this.replaceChildren(el("div", { class: "panel err" }, [e.message]));
      return;
    }
    this.unavailable = res.unavailable;
    this.list = res.list || [];
    this.render();
  }
  async refresh() {
    const res = await api.users();
    this.list = res.list || [];
    this.render();
  }
  render() {
    if (this.unavailable) {
      this.replaceChildren(el("div", { class: "panel" }, [
        el("h2", {}, ["Users"]),
        el("p", { class: "muted" }, ["No user store is wired in this build / configuration."]),
      ]));
      return;
    }
    const rows = this.list.map((u) => el("tr", {}, [
      el("td", {}, [u.Name]),
      el("td", {}, [u.Disabled ? el("span", { class: "muted" }, ["disabled"]) : "active"]),
      el("td", {}, [el("div", { class: "row" }, [
        button(u.Disabled ? "Enable" : "Disable", "", () => this.toggle(u)),
        button("Reset password", "", () => this.setPassword(u.Name)),
        button("Remove", "danger", () => this.remove(u.Name)),
      ])]),
    ]));
    const table = el("table", {}, [
      el("thead", {}, [el("tr", {}, [th("User"), th("State"), th("Actions")])]),
      el("tbody", {}, rows.length ? rows : [el("tr", {}, [el("td", { class: "muted", colspan: "3" }, ["No users."])])]),
    ]);

    const nameIn = el("input", { type: "text", placeholder: "username" });
    const passIn = el("input", { type: "password", placeholder: "password" });
    const addStat = el("div", { class: "err" });
    const add = button("Add user", "primary", async () => {
      addStat.textContent = ""; addStat.className = "err";
      try {
        await api.setUser(nameIn.value.trim(), passIn.value);
        nameIn.value = ""; passIn.value = "";
        await this.refresh();
      } catch (e) { addStat.textContent = e.message; }
    });

    this.replaceChildren(
      el("div", { class: "panel" }, [el("h2", {}, ["Users"]), table]),
      el("div", { class: "panel" }, [
        el("h2", {}, ["Add / reset user"]),
        el("div", { class: "row" }, [nameIn, passIn, add]),
        addStat,
      ]),
    );
  }
  async toggle(u) {
    try { await api.setUserDisabled(u.Name, !u.Disabled); await this.refresh(); }
    catch (e) { alert(e.message); }
  }
  async setPassword(name) {
    const pw = prompt(`New password for ${name}:`);
    if (pw == null) return;
    try { await api.setUser(name, pw); await this.refresh(); }
    catch (e) { alert(e.message); }
  }
  async remove(name) {
    if (!confirm(`Remove user ${name}?`)) return;
    try { await api.removeUser(name); await this.refresh(); }
    catch (e) { alert(e.message); }
  }
}

// ---------------------------------------------------------------------------
// Logs tab: streams the live log over SSE (the shared telemetry bus), with a
// level filter and follow toggle. History is whatever arrives after subscribe.
// ---------------------------------------------------------------------------
const LOG_LEVELS = ["TRACE", "DEBUG", "INFO", "WARN", "ERROR"];
class CsLogs extends HTMLElement {
  lines = [];
  minLevel = 1; // DEBUG
  follow = true;
  connectedCallback() {
    this.logCb = (rec) => this.append(rec);
    telemetry.onLog.add(this.logCb);
  }
  disconnectedCallback() {
    telemetry.onLog.delete(this.logCb);
  }
  activate() { this.render(); }
  render() {
    const sel = el("select", {}, LOG_LEVELS.map((lv, i) =>
      el("option", i === this.minLevel ? { value: String(i), selected: "" } : { value: String(i) }, [lv])));
    sel.addEventListener("change", () => { this.minLevel = Number(sel.value); this.repaint(); });

    const follow = el("input", { type: "checkbox" });
    if (this.follow) follow.checked = true;
    follow.addEventListener("change", () => { this.follow = follow.checked; });

    this.output = el("pre", { class: "log-output" });
    this.replaceChildren(el("div", { class: "panel" }, [
      el("div", { class: "log-controls" }, [
        el("label", { class: "inline" }, ["Level", sel]),
        el("label", { class: "inline" }, [follow, "Follow"]),
        button("Clear", "", () => { this.lines = []; this.repaint(); }),
        button("Download", "", () => this.download()),
      ]),
      this.output,
    ]));
    this.repaint();
  }
  append(rec) {
    this.lines.push(rec);
    if (this.lines.length > 2000) this.lines.shift();
    if (this.output) this.repaint();
  }
  repaint() {
    if (!this.output) return;
    const frag = document.createDocumentFragment();
    for (const r of this.lines) {
      const lvl = r.Level == null ? 2 : r.Level;
      if (lvl < this.minLevel) continue;
      const name = LOG_LEVELS[lvl] || "INFO";
      const ts = r.Time ? new Date(r.Time).toLocaleTimeString() : "";
      const extra = (r.Fields || []).map((f) => `${f.Key}=${fieldVal(f)}`).join(" ");
      const line = el("div", { class: "log-line log-" + name.toLowerCase() },
        [`${ts}  ${name.padEnd(5)}  ${r.Component ? "[" + r.Component + "] " : ""}${r.Msg || ""}${extra ? "  " + extra : ""}`]);
      frag.append(line);
    }
    this.output.replaceChildren(frag);
    if (this.follow) this.output.scrollTop = this.output.scrollHeight;
  }
  download() {
    const text = this.lines.map((r) => {
      const name = LOG_LEVELS[r.Level == null ? 2 : r.Level] || "INFO";
      const ts = r.Time || "";
      return `${ts} ${name} ${r.Component || ""} ${r.Msg || ""}`;
    }).join("\n");
    const blob = new Blob([text], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const a = el("a", { href: url, download: "classicstack.log" });
    document.body.append(a); a.click(); a.remove();
    URL.revokeObjectURL(url);
  }
}
function fieldVal(f) {
  switch (f.Kind) {
    case 1: return String(f.Int);
    case 2: return String(f.Bool);
    default: return f.Str;
  }
}

// --- tiny DOM helpers (the whole "framework") -------------------------------

// el builds an element with attributes and children (strings or nodes).
function el(tag, attrs = {}, children = []) {
  const n = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) n.setAttribute(k, v);
  for (const c of children) n.append(c);
  return n;
}
function th(label) { return el("th", {}, [label]); }
function kv(k, v) {
  return el("div", { class: "kv" }, [el("b", {}, [k + ": "]), String(v)]);
}
function button(label, cls, onClick, disabled = false) {
  const b = el("button", cls ? { class: cls } : {}, [label]);
  if (disabled) b.disabled = true;
  if (onClick) b.addEventListener("click", onClick);
  return b;
}
// linkButton is a button styled as an inline link (the drill-down affordance).
function linkButton(label, onClick) {
  const b = el("button", { class: "link" }, [label]);
  b.addEventListener("click", onClick);
  return b;
}

customElements.define("cs-app", CsApp);
customElements.define("cs-setup", CsSetup);
customElements.define("cs-dashboard", CsDashboard);
customElements.define("cs-config", CsConfig);
customElements.define("cs-instance-editor", CsInstanceEditor);
customElements.define("cs-interfaces", CsInterfaces);
customElements.define("cs-extmap", CsExtMap);
customElements.define("cs-diagnostics", CsDiagnostics);
customElements.define("cs-users", CsUsers);
customElements.define("cs-logs", CsLogs);

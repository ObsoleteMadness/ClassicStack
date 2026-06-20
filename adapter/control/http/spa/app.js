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
//   GET  /list_interfaces              enumerable NICs
//   GET  /list_zones                   AppleTalk zone probe (diagnostics)
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
    const cards = this.units.map((u) => this.card(u));
    this.replaceChildren(
      el("div", { class: "row spread" }, [
        el("h2", {}, ["Components"]),
        button("Refresh", "", () => this.activate()),
      ]),
      cards.length
        ? el("div", { class: "grid" }, cards)
        : el("div", { class: "panel muted" }, ["No components built (empty config)."]),
    );
    this.renderMetrics();
  }
  card(u) {
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

    return el("div", { class: "card" }, [
      el("h3", {}, [indicator, el("span", { class: "card-title" }, [u.Name]), cog]),
      ...detail,
      metric,
      actions,
    ]);
  }
  renderMetrics() {
    for (const el of this.querySelectorAll("[data-metric-for]")) {
      el.textContent = metricLine(telemetry.stats[el.dataset.metricFor]);
    }
  }
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

  const form = sectionForm(section);
  body.replaceChildren(form.node);
  applyBtn.addEventListener("click", async () => {
    status.textContent = "";
    status.className = "err";
    try {
      await api.reconfigure(key, form.collect());
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

// sectionForm builds an editable form from a section object by reflecting its
// JSON fields. Each scalar becomes an input typed from its value; the form
// collects edits back into an object of the same shape for /reconfigure.
function sectionForm(section) {
  const inputs = {};
  const nodes = [];
  for (const [k, v] of Object.entries(section)) {
    const label = el("label", {}, [k]);
    let input;
    if (typeof v === "boolean") {
      input = el("input", { type: "checkbox" });
      if (v) input.checked = true;
      nodes.push(el("label", { class: "inline" }, [input, k]));
    } else if (typeof v === "number") {
      input = el("input", { type: "number", value: String(v) });
      nodes.push(label, input);
    } else if (Array.isArray(v)) {
      input = el("textarea", {}, [v.join("\n")]);
      input.dataset.kind = "list";
      nodes.push(label, input, el("p", { class: "field-hint" }, ["one entry per line"]));
    } else if (v !== null && typeof v === "object") {
      // Nested object (e.g. an Options map): edit as JSON.
      input = el("textarea", {}, [JSON.stringify(v, null, 2)]);
      input.dataset.kind = "json";
      nodes.push(label, input);
    } else {
      input = el("input", { type: "text", value: v == null ? "" : String(v) });
      nodes.push(label, input);
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
      for (const [k, { input, orig }] of Object.entries(inputs)) {
        if (typeof orig === "boolean") out[k] = input.checked;
        else if (typeof orig === "number") out[k] = Number(input.value);
        else if (Array.isArray(orig)) {
          out[k] = input.value.split("\n").map((s) => s.trim()).filter(Boolean);
        } else if (input.dataset.kind === "json") {
          try { out[k] = JSON.parse(input.value); } catch (_) { out[k] = orig; }
        } else out[k] = input.value;
      }
      return out;
    },
  };
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
    } catch (e) {
      this.render(el("p", { class: "err" }, [e.message]));
      return;
    }
    this.renderModel();
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
      // Extension-map grid editor (a file, not a model section).
      this.extMapPanel(),
      ...singletons,
    );
  }

  // sectionPanel renders one singleton section as a collapsible form with Apply (live).
  sectionPanel(name, key, sec) {
    const form = sectionForm(sec);
    const sstat = el("div", { class: "err" });
    const apply = button("Apply (live)", "", async () => {
      sstat.textContent = ""; sstat.className = "err";
      try {
        await api.reconfigure(key, form.collect());
        sstat.className = "ok-msg"; sstat.textContent = "Applied.";
      } catch (e) { sstat.textContent = e.message; }
    });
    return el("details", { class: "panel" }, [
      el("summary", {}, [name]),
      form.node,
      el("div", { class: "row" }, [apply]),
      sstat,
    ]);
  }

  // listEditors builds one panel per repeated-section key (AFPVolumes/SMBShares): a row
  // per instance with Edit/Delete and an Add form. The owner component (AFP/SMB) drives
  // the live reconcile. Keys map to their owner by convention.
  listEditors() {
    const ownerFor = { AFPVolumes: "AFP", SMBShares: "SMB" };
    const out = [];
    for (const [key, list] of Object.entries(this.model.Lists || {})) {
      const owner = ownerFor[key] || key;
      out.push(new CsInstanceEditor(key, owner, list || [], () => this.activate()));
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
  editableSections() {
    const m = this.model, out = [];
    for (const k of ["Identity", "Logging", "Router", "Bridge", "Capture"]) {
      if (m[k] && typeof m[k] === "object") out.push([k, k, m[k]]);
    }
    for (const [k, sec] of Object.entries(m.Sections || {})) out.push([k, k, sec]);
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
      : this.key === "SMBShares" ? "SMB Shares" : this.key;
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
        button("Add " + (this.key === "AFPVolumes" ? "volume" : this.key === "SMBShares" ? "share" : "entry"), "primary",
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
    const nameKey = this.key === "SMBShares" ? "SName" : "VName";
    const base = { [nameKey]: "", FSType: "local_fs", Path: "", ReadOnly: false, Options: [] };
    if (this.key === "SMBShares") base.Description = "";
    if (this.key === "AFPVolumes") base.ExtMapPath = "";
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
      const inp = el("input", { type: "text", value: v || "" });
      fields[k] = { input: inp, orig: v };
      nodes.push(el("label", {}, [k]), pathPicker(inp));
    } else if (k === fsKey && fsTypes.length) {
      const sel = el("select", {}, fsTypes.map((t) =>
        el("option", t === v ? { value: t, selected: "" } : { value: t }, [t])));
      fields[k] = { input: sel, orig: v };
      nodes.push(el("label", {}, [k]), sel);
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

customElements.define("cs-app", CsApp);
customElements.define("cs-setup", CsSetup);
customElements.define("cs-dashboard", CsDashboard);
customElements.define("cs-config", CsConfig);
customElements.define("cs-instance-editor", CsInstanceEditor);
customElements.define("cs-extmap", CsExtMap);
customElements.define("cs-diagnostics", CsDiagnostics);
customElements.define("cs-users", CsUsers);
customElements.define("cs-logs", CsLogs);

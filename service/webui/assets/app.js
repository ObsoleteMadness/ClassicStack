"use strict";

// ClassicStack management SPA. A deliberately dependency-free vanilla-JS
// app: it talks to the control-plane JSON API and the SSE stats stream.
// The HTTP layer in service/webui owns no logic; everything here maps UI
// actions onto control-plane endpoints.

const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => Array.from(document.querySelectorAll(sel));

let currentConfig = null; // last-loaded config model (edited in place)
let latestRates = {}; // metric name -> per-second rate from SSE (counters)
let latestTotals = {}; // metric name -> cumulative total from SSE (counters)
let latestGauges = {}; // metric name -> latest absolute value from SSE (gauges)
// pendingServices holds the names of services with an in-flight start/stop/
// restart action. While pending, the card shows a spinner and its action
// buttons are disabled so the operator can't double-fire a transition.
const pendingServices = new Set();
let lastUnits = []; // last status payload, for immediate re-render on pending change

// ---- tab switching ----
$$(".tab").forEach((btn) => {
  btn.addEventListener("click", () => {
    $$(".tab").forEach((b) => b.classList.remove("active"));
    $$(".panel-view").forEach((v) => v.classList.remove("active"));
    btn.classList.add("active");
    $("#" + btn.dataset.tab).classList.add("active");
    if (btn.dataset.tab === "config") loadConfig();
    if (btn.dataset.tab === "logs") startLogs();
    else stopLogs();
  });
});

// ---- dashboard ----
async function loadStatus() {
  try {
    const units = await fetchJSON("/api/status");
    renderStatus(units);
  } catch (e) {
    $("#service-grid").textContent = "Failed to load status: " + e.message;
  }
}

function renderStatus(units) {
  lastUnits = units; // cache for immediate re-render (e.g. pending-state change)
  const grid = $("#service-grid");
  grid.innerHTML = "";
  units.forEach((u) => {
    const card = document.createElement("div");
    card.className = "card";
    const props = u.properties || {};
    let detail = "";
    if (u.binding) detail += kv("Binding", u.binding);
    Object.keys(props).forEach((k) => (detail += kv(k, props[k])));
    if (u.zones && u.zones.length) detail += kv("Zones", u.zones.join(", "));
    if (u.hostnames && u.hostnames.length) detail += kv("Hostnames", u.hostnames.join(", "));
    if (u.shares && u.shares.length)
      detail += kv("Shares", u.shares.map((s) => s.name).join(", "));

    // Every unit the supervisor drives as a hook is individually
    // start/stoppable: the ports/transports (LToUDP/TashTalk/EtherTalk), the
    // AppleTalk router, the DDP subsystems (AFP/MacIP/IPXGW), the
    // NetBIOS-family hooks (IPX/NetBEUI/NetBIOS/SMB), and the Web UI. Ports run
    // independently of the router; the DDP subsystems depend on it.
    const controllable = u.kind === "hook";
    const pending = pendingServices.has(u.name);
    const dis = pending ? " disabled" : "";
    let controls = "";
    if (controllable) {
      controls = u.running
        ? `<button data-action="stop" data-svc="${esc(u.name)}"${dis}>Stop</button>
           <button data-action="restart" data-svc="${esc(u.name)}"${dis}>Restart</button>`
        : `<button data-action="start" data-svc="${esc(u.name)}"${dis}>Start</button>`;
    }

    // While an action is in flight show a spinner instead of the status dot,
    // and a "Working…" state line, so the transition is visible.
    const indicator = pending
      ? `<span class="spinner" aria-label="working"></span>`
      : `<span class="dot ${u.running ? "running" : ""}"></span>`;
    const stateLine = pending
      ? "Working…"
      : `${u.enabled ? "Enabled" : "Disabled"} · ${u.running ? "Running" : "Stopped"}`;

    // A cog opens this unit's config modal — shown only for units that have at
    // least one config panel mapped to them.
    const hasConfig = panelsForUnit(u.name).length > 0;
    const cog = hasConfig
      ? `<button class="cog" data-config="${esc(u.name)}" title="Configure ${esc(u.name)}" aria-label="Configure ${esc(u.name)}">⚙</button>`
      : "";

    card.innerHTML = `
      <h3>${indicator}<span class="card-title">${esc(u.name)}</span>${cog}</h3>
      <div class="kv">${stateLine}</div>
      ${detail}
      <div class="kv metric" data-metric-for="${esc(u.name)}"></div>
      <div class="card-actions">${controls}</div>
    `;
    card.querySelectorAll("[data-action]").forEach((btn) =>
      btn.addEventListener("click", () => serviceAction(btn.dataset.svc, btn.dataset.action))
    );
    const cogBtn = card.querySelector("[data-config]");
    if (cogBtn) cogBtn.addEventListener("click", () => openServiceConfig(cogBtn.dataset.config));
    grid.appendChild(card);
  });
  renderMetrics(); // populate the just-built cards from the last SSE frame
}

function kv(k, v) {
  return `<div class="kv"><b>${esc(k)}:</b> ${esc(String(v))}</div>`;
}

async function serviceAction(name, action) {
  if (pendingServices.has(name)) return; // already transitioning
  pendingServices.add(name);
  renderStatus(lastUnits); // immediately reflect the spinner/disabled state
  try {
    await postJSON(`/api/services/${encodeURIComponent(name)}/${action}`, null);
  } catch (e) {
    alert(`${action} failed: ` + e.message);
  } finally {
    // Clear pending and refresh once the action has settled. The brief delay
    // lets the supervisor finish the (possibly multi-step) transition before
    // we re-read status.
    pendingServices.delete(name);
    setTimeout(loadStatus, 300);
  }
}

// ---- live stats via SSE ----
function startStats() {
  const es = new EventSource("/api/stats/stream");
  es.onmessage = (ev) => {
    try {
      const frame = JSON.parse(ev.data);
      latestRates = frame.rates || {};
      latestTotals = frame.totals || {};
      latestGauges = frame.gauges || {};
      renderMetrics();
    } catch (_) {}
  };
  es.onerror = () => {
    /* browser auto-reconnects */
  };
}

// renderMetrics writes each card's live-stats line from the latest SSE frame.
// Called on every frame and on each status re-render so a freshly built card
// shows the last-known stats immediately rather than waiting for the next tick.
function renderMetrics() {
  $$("[data-metric-for]").forEach((el) => {
    el.innerHTML = metricsForUnit(el.getAttribute("data-metric-for"));
  });
}

// Producers publish samples named "unit:<UnitName>:<metric>" so each sample
// attributes to exactly one dashboard card. These read the per-second rate,
// the cumulative total (counters) or the latest value (gauges) for one such
// metric.
function unitRate(unit, metric) {
  return latestRates[`unit:${unit}:${metric}`] || 0;
}
function unitTotal(unit, metric) {
  return latestTotals[`unit:${unit}:${metric}`] || 0;
}
function unitGauge(unit, metric) {
  return latestGauges[`unit:${unit}:${metric}`];
}

// metricsForUnit renders the live summary for a card: cumulative rx/tx packet
// totals plus current throughput for ports, and any gauge value the unit
// publishes (e.g. active sessions). The traffic line is always shown for units
// that report traffic counters (even when idle, so the totals stay visible);
// returns "" only for units that publish no metrics at all.
function metricsForUnit(unit) {
  const parts = [];
  const hasTraffic =
    `unit:${unit}:rx.packets` in latestTotals || `unit:${unit}:tx.packets` in latestTotals;
  if (hasTraffic) {
    const rxt = unitTotal(unit, "rx.packets");
    const txt = unitTotal(unit, "tx.packets");
    const rxp = unitRate(unit, "rx.packets");
    const txp = unitRate(unit, "tx.packets");
    const rxb = unitRate(unit, "rx.bytes");
    const txb = unitRate(unit, "tx.bytes");
    parts.push(
      `↓ ${fmtCount(rxt)} pkt (${rxp}/s, ${fmtBytes(rxb)}/s)`,
      `↑ ${fmtCount(txt)} pkt (${txp}/s, ${fmtBytes(txb)}/s)`,
    );
  }
  const sessions = unitGauge(unit, "sessions");
  if (sessions !== undefined) parts.push(`${sessions} session${sessions === 1 ? "" : "s"}`);
  return parts.map(esc).join(" · ");
}

// fmtCount renders a packet count with thousands separators for readability.
function fmtCount(n) {
  return Number(n).toLocaleString();
}

// fmtBytes renders a byte count as B/KB/MB with one decimal for the larger
// units, matching the compact per-second throughput display.
function fmtBytes(n) {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

// ---- logs ----
// The log viewer opens an SSE stream when its tab is active and closes it on
// leave. The server replays recent history first, then streams live lines.
// Rendering is capped to keep the DOM bounded; level filtering is client-side.
const LOG_MAX_LINES = 1000;
const LOG_LEVELS = { DEBUG: 0, INFO: 1, WARN: 2, ERROR: 3 };
let logSource = null; // active EventSource, or null when the tab is inactive

function startLogs() {
  if (logSource) return; // already streaming
  const out = $("#log-output");
  out.textContent = "";
  setLogStatus("connecting…");
  logSource = new EventSource("/api/logs/stream");
  logSource.onopen = () => setLogStatus("streaming");
  logSource.onmessage = (ev) => {
    try {
      appendLogEntry(JSON.parse(ev.data));
    } catch (_) {}
  };
  logSource.onerror = () => setLogStatus("reconnecting…");
}

function stopLogs() {
  if (!logSource) return;
  logSource.close();
  logSource = null;
  setLogStatus("disconnected");
}

function setLogStatus(s) {
  $("#log-status").textContent = s;
}

function appendLogEntry(entry) {
  const out = $("#log-output");
  const minLevel = LOG_LEVELS[$("#log-level-filter").value] ?? 0;
  const level = (entry.level || "INFO").toUpperCase();
  const line = document.createElement("div");
  line.className = "log-line log-" + level.toLowerCase();
  line.dataset.level = level;
  const ts = entry.t ? new Date(entry.t).toLocaleTimeString() : "";
  line.textContent = `${ts}  ${level.padEnd(5)}  ${entry.msg || ""}`;
  if ((LOG_LEVELS[level] ?? 1) < minLevel) line.classList.add("hidden");
  out.appendChild(line);

  while (out.childElementCount > LOG_MAX_LINES) out.removeChild(out.firstChild);

  if ($("#log-follow").checked) out.scrollTop = out.scrollHeight;
}

// Re-apply the level filter to already-rendered lines.
$("#log-level-filter").addEventListener("change", () => {
  const minLevel = LOG_LEVELS[$("#log-level-filter").value] ?? 0;
  $$("#log-output .log-line").forEach((el) => {
    const lvl = LOG_LEVELS[el.dataset.level] ?? 1;
    el.classList.toggle("hidden", lvl < minLevel);
  });
});

$("#btn-log-clear").addEventListener("click", () => {
  $("#log-output").textContent = "";
});

$("#btn-log-download").addEventListener("click", () => {
  window.location.href = "/api/logs/download";
});

// ---- configuration editor ----
async function loadConfig() {
  try {
    const resp = await fetchJSON("/api/config");
    currentConfig = resp.config;
    setDirty(resp.dirty);
    renderConfig(currentConfig);
  } catch (e) {
    $("#config-panels").textContent = "Failed to load config: " + e.message;
  }
}

// Dropdown option sets shared by the config panels.
const IFACE_MODES = ["pcap", "tap", "tun"]; // link backend
const BRIDGE_MODES = ["auto", "ethernet", "wifi"]; // pcap bridge mode
const IPX_FRAMINGS = ["ethernet_ii", "raw_802_3", "llc", "snap"];

// Panels mirror the classic control-panel layout. Each field binds to a
// dotted path in the config model.
const CONFIG_PANELS = [
  {
    title: "LocalTalk over UDP",
    units: ["LToUDP"],
    fields: [
      { label: "Enabled", path: "LToUdp.enabled", type: "bool" },
      { label: "Interface", path: "LToUdp.interface", type: "text" },
      { label: "Zone Name", path: "LToUdp.seed_zone", type: "text" },
      { label: "Seed Network", path: "LToUdp.seed_network", type: "number" },
      { label: "Attach to AppleTalk router", path: "LToUdp", type: "router-port", port: "LToUdp" },
    ],
  },
  {
    title: "TashTalk (LocalTalk)",
    units: ["TashTalk"],
    fields: [
      { label: "Serial Port", path: "TashTalk.port", type: "serial" },
      { label: "Zone Name", path: "TashTalk.seed_zone", type: "text" },
      { label: "Seed Network", path: "TashTalk.seed_network", type: "number" },
      { label: "Attach to AppleTalk router", path: "TashTalk", type: "router-port", port: "TashTalk" },
    ],
  },
  {
    // The shared virtual interface protocols inherit unless they go Custom.
    title: "Bridge (shared interface)",
    // EtherTalk (and other bridge consumers) edit the shared Bridge too.
    units: ["EtherTalk"],
    fields: [
      { label: "Mode", path: "Bridge.mode", type: "select", options: IFACE_MODES },
      { label: "Device", path: "Bridge.device", type: "iface" },
      { label: "HW Address", path: "Bridge.hw_address", type: "text" },
      { label: "Bridge Mode", path: "Bridge.bridge_mode", type: "select", options: BRIDGE_MODES },
    ],
  },
  {
    title: "EtherTalk",
    units: ["EtherTalk"],
    interfaceFor: "EtherTalk",
    fields: [
      { label: "Zone Name", path: "EtherTalk.seed_zone", type: "text" },
      { label: "Seed Net Min", path: "EtherTalk.seed_network_min", type: "number" },
      { label: "Seed Net Max", path: "EtherTalk.seed_network_max", type: "number" },
      { label: "Attach to AppleTalk router", path: "EtherTalk", type: "router-port", port: "EtherTalk" },
    ],
  },
  {
    title: "NetBEUI (NBF)",
    units: ["NetBEUI"],
    interfaceFor: "NetBEUI",
    fields: [{ label: "Enabled", path: "NetBEUI.enabled", type: "bool" }],
  },
  {
    title: "IPX",
    units: ["IPX"],
    interfaceFor: "IPX",
    fields: [
      { label: "Enabled", path: "IPX.enabled", type: "bool" },
      { label: "Framing", path: "IPX.framing", type: "select", options: IPX_FRAMINGS },
      { label: "Network", path: "IPX.internal_network", type: "text" },
    ],
  },
  {
    title: "IPX Gateway (MacIPX)",
    units: ["IPXGW"],
    fields: [
      { label: "Enabled", path: "IPXGW.enabled", type: "bool", hint: "Register an 'IPX Gateway' NBP name so MacIPX clients can discover us." },
      {
        label: "Zone Bindings",
        path: "IPXGW.bindings",
        type: "stringlist",
        placeholder: "Object:Zone",
        hint: "Optional 'Object:Zone' pairs. Leave empty to register one binding per zone the router knows.",
      },
    ],
  },
  {
    title: "MacIP Gateway",
    units: ["MacIP"],
    interfaceFor: "MacIP",
    fields: [
      { label: "Enabled", path: "MacIP.enabled", type: "bool" },
      { label: "Gateway Mode", path: "MacIP.mode", type: "select", options: ["pcap", "nat"] },
      { label: "Zone", path: "MacIP.zone", type: "text", hint: "MacIP gateway zone; defaults to the EtherTalk zone." },
      { label: "NAT Subnet", path: "MacIP.nat_subnet", type: "text", hint: "NAT mode: subnet to hand out, e.g. 192.168.100.0/24." },
      { label: "NAT Gateway IP", path: "MacIP.nat_gw", type: "text", hint: "NAT mode: the gateway's own IP on the NAT subnet." },
      { label: "Lease File", path: "MacIP.lease_file", type: "text", hint: "NAT mode: file to persist DHCP leases across restarts." },
      { label: "IP Gateway", path: "MacIP.ip_gateway", type: "text", hint: "Upstream/default gateway on the IP-side network." },
      { label: "DHCP Relay", path: "MacIP.dhcp_relay", type: "bool", hint: "Convert MacTCP auto-config to DHCP requests." },
      { label: "Nameserver", path: "MacIP.nameserver", type: "text", hint: "DNS server advertised to MacIP clients, e.g. 1.1.1.1." },
      { label: "BPF Filter", path: "MacIP.filter", type: "text", hint: "Optional pcap BPF filter override (advanced)." },
    ],
  },
  {
    title: "AFP File Server",
    units: ["AFP"],
    editor: {
      title: "AFP Volumes",
      section: "AFP",
      columns: [
        { key: "name", label: "Name", type: "text" },
        { key: "path", label: "Path", type: "text" },
        { key: "fs_type", label: "FS Type", type: "select", options: "fsTypes", default: "local_fs" },
        { key: "read_only", label: "Read-only", type: "bool" },
      ],
    },
    fields: [
      { label: "Enabled", path: "AFP.enabled", type: "bool" },
      { label: "Server Name", path: "AFP.name", type: "text" },
      { label: "Zone", path: "AFP.zone", type: "text" },
      { label: "Binding", path: "AFP.binding", type: "text" },
    ],
  },
  {
    title: "NetBIOS",
    units: ["NetBIOS"],
    fields: [
      { label: "Enabled", path: "NetBIOS.enabled", type: "bool" },
      {
        label: "Transports",
        path: "NetBIOS.transports",
        type: "stringlist",
        placeholder: "ipx | netbeui",
        hint: "Transports NetBIOS binds (e.g. ipx, netbeui). Leave empty for the defaults.",
      },
      { label: "Scope ID", path: "NetBIOS.scope_id", type: "text" },
    ],
  },
  {
    title: "SMB Server",
    units: ["SMB"],
    editor: {
      title: "SMB Shares",
      section: "SMB",
      columns: [
        { key: "name", label: "Name", type: "text" },
        { key: "path", label: "Path", type: "text" },
        { key: "fs_type", label: "FS Type", type: "select", options: "fsTypes", default: "local_fs" },
        { key: "read_only", label: "Read-only", type: "bool" },
      ],
    },
    fields: [
      { label: "Enabled", path: "SMB.enabled", type: "bool" },
      { label: "Server Name", path: "SMB.server_name", type: "text" },
      { label: "Workgroup", path: "SMB.workgroup", type: "text" },
      { label: "NBT Binding", path: "SMB.nbt_binding", type: "text" },
    ],
  },
  {
    title: "Packet Dump & Capture",
    fields: [
      { label: "Parse packets", path: "Logging.parse_packets", type: "bool" },
      { label: "Log traffic", path: "Logging.log_traffic", type: "bool" },
      { label: "Parse output file", path: "Logging.parse_output", type: "text" },
      { label: "LocalTalk pcap", path: "Capture.localtalk", type: "text" },
      { label: "EtherTalk pcap", path: "Capture.ethertalk", type: "text" },
      { label: "IPX pcap", path: "Capture.ipx", type: "text" },
      { label: "NetBEUI pcap", path: "Capture.netbeui", type: "text" },
      { label: "Snap length", path: "Capture.snaplen", type: "number" },
    ],
  },
  {
    title: "Web UI",
    units: ["WebUI"],
    fields: [
      { label: "Enabled", path: "WebUI.enabled", type: "bool" },
      { label: "Bind", path: "WebUI.bind", type: "text" },
      { label: "TLS", path: "WebUI.tls", type: "bool" },
    ],
  },
  {
    // The AppleTalk router has no parameters of its own beyond which transports
    // it binds; surface those toggles here so the Router card's cog is useful.
    title: "AppleTalk Router",
    units: ["Router"],
    fields: [
      { label: "Bind LToUDP", path: "LToUdp", type: "router-port", port: "LToUdp" },
      { label: "Bind TashTalk", path: "TashTalk", type: "router-port", port: "TashTalk" },
      { label: "Bind EtherTalk", path: "EtherTalk", type: "router-port", port: "EtherTalk" },
    ],
  },
];

let interfaceList = []; // [{name, description, addresses}]
let serialList = [];
let fsTypeList = []; // registered AFP fs_type names

// ifaceLabel builds a friendly dropdown label for an interface: the pcap
// Description (or the device name on the rare host without one) plus any IPs.
// On Windows the device name is a GUID, so the description is what's legible.
function ifaceLabel(i) {
  let label = i.description || i.name;
  if (i.addresses && i.addresses.length) label += " (" + i.addresses.join(", ") + ")";
  return label;
}

// loadConfigLists fetches the dropdown option sets (interfaces, serial ports,
// fs-types) the config fields need. Shared by the full editor and the
// per-service modal so both render the same friendly selectors.
async function loadConfigLists() {
  [interfaceList, serialList, fsTypeList] = await Promise.all([
    fetchJSON("/api/interfaces").catch(() => []),
    fetchJSON("/api/serial-ports").catch(() => []),
    fetchJSON("/api/fs-types").catch(() => ["local_fs"]),
  ]);
  if (!fsTypeList || !fsTypeList.length) fsTypeList = ["local_fs"];
}

// renderPanel builds one config panel (a <fieldset>) bound to cfg, including
// its fields, optional interface chooser, and optional share/volume editor. It
// is the unit of reuse shared by the full Configuration tab and the per-service
// modal opened from a dashboard card's cog.
function renderPanel(cfg, panel) {
  const fs = document.createElement("fieldset");
  fs.className = "config-panel";
  const legend = document.createElement("legend");
  legend.textContent = panel.title;
  fs.appendChild(legend);
  panel.fields.forEach((f) => fs.appendChild(renderField(cfg, f)));
  // A per-service Bridge/Custom interface chooser, when the panel declares one.
  if (panel.interfaceFor) fs.appendChild(renderInterfaceChooser(cfg, panel.interfaceFor));
  // A grouped volume/share editor, when the panel declares one.
  if (panel.editor) fs.appendChild(renderShareEditor(cfg, panel.editor.title, panel.editor.section, panel.editor.columns));
  return fs;
}

async function renderConfig(cfg) {
  await loadConfigLists();
  const root = $("#config-panels");
  root.innerHTML = "";
  CONFIG_PANELS.forEach((panel) => root.appendChild(renderPanel(cfg, panel)));
}

// panelsForUnit returns the config panels that edit the given dashboard unit,
// matched by the panel's `units` tag (a unit may span several panels, e.g.
// EtherTalk edits both its own panel and the shared Bridge panel).
function panelsForUnit(unit) {
  return CONFIG_PANELS.filter((p) => Array.isArray(p.units) && p.units.includes(unit));
}

// renderInterfaceChooser renders the per-service interface selector: a
// "Bridge" / "Custom" radio. Bridge means the service inherits the shared
// [Bridge] interface (no <Section>.Custom). Custom reveals a sub-form
// (Mode, Device, HW Address, and — for pcap — Bridge Mode) bound to
// cfg[section].Custom. EtherTalk is the bridge consumer itself, so it only
// shows an informational note.
function renderInterfaceChooser(cfg, section) {
  const wrap = document.createElement("div");
  wrap.className = "iface-chooser";
  const heading = document.createElement("div");
  heading.className = "iface-heading";
  heading.textContent = "Interface";
  wrap.appendChild(heading);

  if (section === "EtherTalk") {
    const note = document.createElement("div");
    note.className = "kv muted";
    note.textContent = "Uses the shared Bridge interface (configure it in the Bridge panel).";
    wrap.appendChild(note);
    return wrap;
  }

  if (!cfg[section]) cfg[section] = {};
  const isCustom = () => !!cfg[section].Custom;

  const radioRow = document.createElement("div");
  radioRow.className = "iface-radio";
  const sub = document.createElement("div");
  sub.className = "iface-subform";

  function rebuildSub() {
    sub.innerHTML = "";
    if (!isCustom()) {
      const bridgeDev = (cfg.Bridge && cfg.Bridge.device) || "(none)";
      const note = document.createElement("div");
      note.className = "kv muted";
      note.textContent = "Inherits the shared Bridge (" + bridgeDev + ").";
      sub.appendChild(note);
      return;
    }
    const c = cfg[section].Custom;
    const subFields = [
      { label: "Mode", path: "mode", type: "select", options: IFACE_MODES },
      { label: "Device", path: "device", type: "iface" },
      { label: "HW Address", path: "hw_address", type: "text" },
    ];
    if ((c.mode || "pcap") === "pcap") {
      subFields.push({ label: "Bridge Mode", path: "bridge_mode", type: "select", options: BRIDGE_MODES });
    }
    subFields.forEach((f) => {
      const row = document.createElement("div");
      row.className = "field";
      const label = document.createElement("label");
      label.textContent = f.label;
      row.appendChild(label);
      let input;
      if (f.type === "iface") {
        input = buildInterfaceSelect(c[f.path] || "", (v) => { c[f.path] = v; setDirty(true); });
      } else if (f.type === "select") {
        input = buildSelect(f.options, c[f.path] || "", (v) => {
          c[f.path] = v;
          setDirty(true);
          if (f.path === "mode") rebuildSub(); // toggling pcap shows/hides bridge mode
        });
      } else {
        input = document.createElement("input");
        input.type = "text";
        input.value = c[f.path] == null ? "" : c[f.path];
        input.addEventListener("input", () => { c[f.path] = input.value; setDirty(true); });
      }
      row.appendChild(input);
      sub.appendChild(row);
    });
  }

  [["bridge", "Bridge"], ["custom", "Custom"]].forEach(([val, lbl]) => {
    const id = "iface-" + section + "-" + val;
    const label = document.createElement("label");
    label.className = "radio";
    const radio = document.createElement("input");
    radio.type = "radio";
    radio.name = "iface-" + section;
    radio.id = id;
    radio.checked = val === "custom" ? isCustom() : !isCustom();
    radio.addEventListener("change", () => {
      if (!radio.checked) return;
      if (val === "custom") {
        if (!cfg[section].Custom) cfg[section].Custom = { mode: "pcap" };
      } else {
        delete cfg[section].Custom;
      }
      setDirty(true);
      rebuildSub();
    });
    label.appendChild(radio);
    label.appendChild(document.createTextNode(" " + lbl));
    radioRow.appendChild(label);
  });

  wrap.appendChild(radioRow);
  wrap.appendChild(sub);
  rebuildSub();
  return wrap;
}

// renderShareEditor builds a table editor over cfg[section].Volumes (a
// name-keyed map of share/volume objects) with add and remove controls. It
// renders as a nested group so it can sit inside its parent service panel
// (AFP volumes under AFP, SMB shares under SMB).
function renderShareEditor(cfg, title, section, columns) {
  const fs = document.createElement("fieldset");
  fs.className = "config-panel nested";
  const legend = document.createElement("legend");
  legend.textContent = title;
  fs.appendChild(legend);

  if (!cfg[section]) cfg[section] = {};
  if (!cfg[section].Volumes) cfg[section].Volumes = {};
  const volumes = cfg[section].Volumes;

  const table = document.createElement("table");
  table.className = "share-table";
  const head = document.createElement("tr");
  columns.forEach((c) => {
    const th = document.createElement("th");
    th.textContent = c.label;
    head.appendChild(th);
  });
  head.appendChild(document.createElement("th")); // remove column
  table.appendChild(head);

  function addRow(mapKey, entry) {
    const tr = document.createElement("tr");
    columns.forEach((c) => {
      const td = document.createElement("td");
      let input;
      if (c.type === "bool") {
        input = document.createElement("input");
        input.type = "checkbox";
        input.checked = !!entry[c.key];
        input.addEventListener("change", () => {
          entry[c.key] = input.checked;
          setDirty(true);
        });
      } else if (c.type === "select") {
        const opts = c.options === "fsTypes" ? fsTypeList : c.options || [];
        input = buildSelect(opts, entry[c.key] || c.default || "", (v) => {
          entry[c.key] = v;
          setDirty(true);
        });
      } else {
        input = document.createElement("input");
        input.type = "text";
        input.value = entry[c.key] == null ? "" : entry[c.key];
        input.addEventListener("input", () => {
          entry[c.key] = input.value;
          // Keep the map key in sync with the Name field so the TOML
          // table key matches what the operator typed.
          if (c.key === "name") rekey(input.value, entry, tr);
          setDirty(true);
        });
      }
      td.appendChild(input);
      tr.appendChild(td);
    });
    const rmTd = document.createElement("td");
    const rm = document.createElement("button");
    rm.textContent = "Remove";
    rm.addEventListener("click", () => {
      delete volumes[tr.dataset.key];
      tr.remove();
      setDirty(true);
    });
    rmTd.appendChild(rm);
    tr.appendChild(rmTd);
    tr.dataset.key = mapKey;
    table.appendChild(tr);
  }

  function rekey(newName, entry, tr) {
    const key = newName.trim();
    if (!key || key === tr.dataset.key) return;
    delete volumes[tr.dataset.key];
    volumes[key] = entry;
    tr.dataset.key = key;
  }

  Object.keys(volumes).forEach((k) => {
    const entry = volumes[k];
    if (!entry.name) entry.name = k;
    addRow(k, entry);
  });

  const add = document.createElement("button");
  add.textContent = "Add " + (section === "AFP" ? "volume" : "share");
  add.addEventListener("click", () => {
    let key = "New" + (Object.keys(volumes).length + 1);
    while (volumes[key]) key += "_";
    const entry = { name: key };
    columns.forEach((c) => {
      if (c.default !== undefined) entry[c.key] = c.default;
    });
    volumes[key] = entry;
    addRow(key, entry);
    setDirty(true);
  });

  fs.appendChild(table);
  fs.appendChild(add);
  return fs;
}

// buildSelect creates a <select> over options (array of strings or {value,
// label}), preselecting current. If current is not among the options it is
// added so a stored value is never silently dropped. onChange(value) fires on
// selection.
// buildStringList renders an editable list of free-text rows (one per array
// element) with per-row remove buttons and an Add button. onChange receives
// the current array (trimmed of empty entries) whenever it mutates.
function buildStringList(values, placeholder, onChange) {
  const wrap = document.createElement("div");
  wrap.className = "stringlist";
  const rows = document.createElement("div");
  rows.className = "stringlist-rows";
  wrap.appendChild(rows);

  const items = values.slice();

  function emit() {
    onChange(items.map((s) => s.trim()).filter((s) => s !== ""));
  }
  function render() {
    rows.innerHTML = "";
    items.forEach((val, i) => {
      const row = document.createElement("div");
      row.className = "stringlist-row";
      const input = document.createElement("input");
      input.type = "text";
      input.value = val;
      input.placeholder = placeholder;
      input.addEventListener("input", () => {
        items[i] = input.value;
        emit();
      });
      const del = document.createElement("button");
      del.type = "button";
      del.className = "stringlist-del";
      del.textContent = "✕";
      del.title = "Remove";
      del.addEventListener("click", () => {
        items.splice(i, 1);
        render();
        emit();
      });
      row.appendChild(input);
      row.appendChild(del);
      rows.appendChild(row);
    });
  }

  const add = document.createElement("button");
  add.type = "button";
  add.className = "stringlist-add";
  add.textContent = "Add";
  add.addEventListener("click", () => {
    items.push("");
    render();
  });
  wrap.appendChild(add);

  render();
  return wrap;
}

function buildSelect(options, current, onChange) {
  const sel = document.createElement("select");
  const norm = options.map((o) => (typeof o === "string" ? { value: o, label: o } : o));
  if (current && !norm.some((o) => o.value === current)) {
    norm.unshift({ value: current, label: current });
  }
  norm.forEach((o) => {
    const opt = document.createElement("option");
    opt.value = o.value;
    opt.textContent = o.label;
    if (o.value === current) opt.selected = true;
    sel.appendChild(opt);
  });
  sel.addEventListener("change", () => onChange(sel.value));
  return sel;
}

// buildInterfaceSelect creates an interface <select> with friendly labels. The
// stored value is the device name; a "(none)" blank is offered, and a stored
// device not present in the enumerated list (e.g. saved on another host) is
// preserved as its own option.
function buildInterfaceSelect(current, onChange) {
  const sel = document.createElement("select");
  const blank = document.createElement("option");
  blank.value = "";
  blank.textContent = "(none)";
  sel.appendChild(blank);
  let matched = !current;
  interfaceList.forEach((i) => {
    const o = document.createElement("option");
    o.value = i.name;
    o.textContent = ifaceLabel(i);
    if (i.name === current) {
      o.selected = true;
      matched = true;
    }
    sel.appendChild(o);
  });
  if (!matched) {
    const o = document.createElement("option");
    o.value = current;
    o.textContent = current + " (saved)";
    o.selected = true;
    sel.appendChild(o);
  }
  sel.addEventListener("change", () => onChange(sel.value));
  return sel;
}

function renderField(cfg, f) {
  const row = document.createElement("div");
  row.className = "field";
  if (f.hint) row.title = f.hint;
  const label = document.createElement("label");
  label.textContent = f.label;
  row.appendChild(label);

  const val = getPath(cfg, f.path);
  let input;
  if (f.type === "bool") {
    input = document.createElement("input");
    input.type = "checkbox";
    input.checked = !!val;
    input.addEventListener("change", () => {
      setPath(cfg, f.path, input.checked);
      setDirty(true);
    });
  } else if (f.type === "router-port") {
    // Router attachment lives in the [Router].ports allow-list, not on the
    // transport. The checkbox reflects/edits membership: an empty/absent list
    // means "bind every transport" (the default), so an unset list shows
    // checked. Toggling off switches the list to an explicit allow-list of the
    // other transports; toggling the last one back on clears it to empty again.
    input = document.createElement("input");
    input.type = "checkbox";
    input.checked = routerBindsPort(cfg, f.port);
    input.addEventListener("change", () => {
      setRouterPort(cfg, f.port, input.checked);
      setDirty(true);
    });
  } else if (f.type === "iface") {
    input = buildInterfaceSelect(val, (v) => {
      setPath(cfg, f.path, v);
      setDirty(true);
    });
  } else if (f.type === "serial") {
    input = document.createElement("select");
    const blank = document.createElement("option");
    blank.value = "";
    blank.textContent = "(none)";
    input.appendChild(blank);
    serialList.forEach((s) => {
      const o = document.createElement("option");
      o.value = s.name;
      o.textContent = s.description || s.name;
      if (s.name === val) o.selected = true;
      input.appendChild(o);
    });
    input.addEventListener("change", () => {
      setPath(cfg, f.path, input.value);
      setDirty(true);
    });
  } else if (f.type === "select") {
    input = buildSelect(f.options || [], val, (v) => {
      setPath(cfg, f.path, v);
      setDirty(true);
    });
  } else if (f.type === "stringlist") {
    input = buildStringList(Array.isArray(val) ? val : [], f.placeholder || "", (list) => {
      // Store undefined for an empty list so the omitempty field drops out of
      // the TOML entirely rather than serialising an empty array.
      setPath(cfg, f.path, list.length ? list : undefined);
      setDirty(true);
    });
  } else {
    input = document.createElement("input");
    input.type = f.type === "number" ? "number" : "text";
    input.value = val == null ? "" : val;
    input.addEventListener("input", () => {
      setPath(cfg, f.path, f.type === "number" ? Number(input.value) : input.value);
      setDirty(true);
    });
  }
  row.appendChild(input);
  return row;
}

// The transports the [Router].ports allow-list can name. Mirrors the Go
// RouterPort* constants (config/model.go) and the TOML section names.
const ROUTER_PORTS = ["LToUdp", "TashTalk", "EtherTalk"];

// routerBindsPort mirrors config.RouterModel.BindsPort: an empty/absent list
// binds every transport; otherwise only listed ones (case-insensitive).
function routerBindsPort(cfg, name) {
  const ports = (cfg.Router && cfg.Router.ports) || [];
  if (ports.length === 0) return true;
  return ports.some((p) => String(p).trim().toLowerCase() === name.toLowerCase());
}

// setRouterPort toggles a transport's membership in [Router].ports while
// preserving the "empty = all" convention: the list is only made explicit when
// some transport is detached, and collapses back to empty once all are
// attached again.
function setRouterPort(cfg, name, attached) {
  if (!cfg.Router) cfg.Router = {};
  // Start from the effective attached set (empty list ⇒ everything).
  let set = routerBindsPort(cfg, "")
    ? new Set(ROUTER_PORTS)
    : new Set(ROUTER_PORTS.filter((p) => routerBindsPort(cfg, p)));
  if (attached) set.add(name);
  else set.delete(name);
  // All attached ⇒ collapse to empty (the clean default); otherwise emit the
  // explicit allow-list in canonical order.
  if (ROUTER_PORTS.every((p) => set.has(p))) {
    delete cfg.Router.ports;
  } else {
    cfg.Router.ports = ROUTER_PORTS.filter((p) => set.has(p));
  }
}

function getPath(obj, path) {
  return path.split(".").reduce((o, k) => (o == null ? undefined : o[k]), obj);
}
function setPath(obj, path, value) {
  const keys = path.split(".");
  const last = keys.pop();
  let o = obj;
  keys.forEach((k) => {
    if (o[k] == null) o[k] = {};
    o = o[k];
  });
  o[last] = value;
}

function setDirty(d) {
  $("#dirty-indicator").classList.toggle("hidden", !d);
}

// ---- config actions ----
$("#btn-download").addEventListener("click", () => {
  window.location.href = "/api/config/download";
});

$("#btn-apply").addEventListener("click", async () => {
  try {
    await putJSON("/api/config", currentConfig);
    await postJSON("/api/config/apply", null);
    setConfigStatus("Applied live. Changes are running but not yet saved to disk.");
    loadStatus();
  } catch (e) {
    setConfigStatus("Apply failed: " + e.message);
  }
});

$("#btn-save").addEventListener("click", async () => {
  if (!confirm("Saving rewrites server.toml and removes comments. Continue?")) return;
  try {
    await putJSON("/api/config", currentConfig);
    const r = await postJSON("/api/config/save", null);
    setDirty(false);
    setConfigStatus("Saved. Backup written to " + (r.backup || "(no previous file)") + ".");
  } catch (e) {
    setConfigStatus("Save failed: " + e.message);
  }
});

function setConfigStatus(msg) {
  $("#config-status").textContent = msg;
}

// ---- per-service config modal ----
// A dashboard card's cog opens a modal showing just that service's config
// panels (the same fields as the Configuration tab). Apply stages the edited
// model and runs a live Apply, which the supervisor handles as an atomic
// whole-stack rebuild — so the edited service restarts with the new config.
// Edits are NOT written to disk; the modal makes that explicit.
let modalConfig = null; // deep clone of the config edited inside the modal
let modalUnit = null; // unit name the modal is currently editing

async function openServiceConfig(unit) {
  const panels = panelsForUnit(unit);
  if (!panels.length) return;
  try {
    await loadConfigLists();
    const resp = await fetchJSON("/api/config");
    // Edit a deep clone so closing without Apply discards the changes and the
    // dashboard's own config view is untouched.
    modalConfig = JSON.parse(JSON.stringify(resp.config || {}));
    modalUnit = unit;
  } catch (e) {
    alert("Could not load config: " + e.message);
    return;
  }

  $("#modal-title").textContent = "Configure " + unit;
  const body = $("#modal-body");
  body.innerHTML = "";
  panels.forEach((p) => body.appendChild(renderPanel(modalConfig, p)));
  setModalStatus("");
  $("#service-modal").classList.remove("hidden");
}

function closeServiceConfig() {
  $("#service-modal").classList.add("hidden");
  modalConfig = null;
  modalUnit = null;
}

function setModalStatus(msg) {
  $("#modal-status").textContent = msg;
}

// Wire the modal's static controls once at load.
(function initServiceModal() {
  const modal = $("#service-modal");
  if (!modal) return;
  $("#modal-close").addEventListener("click", closeServiceConfig);
  $("#modal-cancel").addEventListener("click", closeServiceConfig);
  // Click on the dimmed backdrop (outside the dialog) closes the modal.
  modal.addEventListener("click", (e) => {
    if (e.target === modal) closeServiceConfig();
  });
  // Escape closes it too.
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && !modal.classList.contains("hidden")) closeServiceConfig();
  });
  $("#modal-apply").addEventListener("click", applyServiceConfig);
})();

async function applyServiceConfig() {
  if (!modalConfig || !modalUnit) return;
  const applyBtn = $("#modal-apply");
  applyBtn.disabled = true;
  setModalStatus("Applying…");
  try {
    await putJSON("/api/config", modalConfig);
    await postJSON("/api/config/apply", null);
    // The whole-stack Apply restarts the affected service; reflect it on the
    // dashboard and mark the live config dirty (applied but not saved).
    setDirty(true);
    closeServiceConfig();
    loadStatus();
  } catch (e) {
    setModalStatus("Apply failed: " + e.message);
  } finally {
    applyBtn.disabled = false;
  }
}

// ---- extension-map editor ----
// A raw text editor for the Netatalk-style type/creator file. We edit the
// file verbatim (preserving comments/order) rather than parsing it into a
// grid; the server validates on save and reports the offending line.
let extMapLoaded = false;

async function loadExtMap() {
  try {
    const r = await fetchJSON("/api/extmap");
    $("#extmap-path").textContent = r.path || "(unset)";
    $("#extmap-text").value = r.content || "";
    setExtMapStatus("");
    extMapLoaded = true;
  } catch (e) {
    $("#extmap-path").textContent = "(unavailable)";
    $("#extmap-text").value = "";
    setExtMapStatus("Could not load extension map: " + e.message);
  }
}

function setExtMapStatus(msg) {
  $("#extmap-status").textContent = msg;
}

const extMapEditor = $("#extmap-editor");
if (extMapEditor) {
  // Lazily load the file the first time the section is expanded.
  extMapEditor.addEventListener("toggle", () => {
    if (extMapEditor.open && !extMapLoaded) loadExtMap();
  });
  $("#btn-extmap-reload").addEventListener("click", loadExtMap);
  $("#btn-extmap-save").addEventListener("click", async () => {
    try {
      const r = await putJSON("/api/extmap", { content: $("#extmap-text").value });
      setExtMapStatus(
        "Saved. Backup written to " +
          (r.backup || "(no previous file)") +
          ". Applies on next Apply.",
      );
    } catch (e) {
      setExtMapStatus("Save failed: " + e.message);
    }
  });
}

// ---- diagnostics ----
$$("[data-diag]").forEach((btn) => {
  btn.addEventListener("click", async () => {
    const kind = btn.dataset.diag;
    const out = $("#diag-output");
    out.textContent = "Running " + kind + "…";
    try {
      let url = "/api/diag/" + kind;
      if (kind === "aep-echo") {
        url += `?network=${$("#aep-net").value}&node=${$("#aep-node").value}`;
      }
      const data = kind === "aep-echo" ? await fetchJSON(url) : await fetchJSON(url);
      out.textContent = JSON.stringify(data, null, 2);
    } catch (e) {
      out.textContent = kind + " failed: " + e.message;
    }
  });
});

// Restart the whole stack (all ports, the router, and every hook). The Web UI
// server is preserved across the rebuild, so this connection survives.
const restartAllBtn = $("#btn-restart-all");
if (restartAllBtn) {
  restartAllBtn.addEventListener("click", async () => {
    if (!confirm("Restart the whole stack? Active sessions will be dropped.")) return;
    const out = $("#diag-output");
    restartAllBtn.disabled = true;
    out.textContent = "Restarting stack…";
    try {
      await postJSON("/api/restart-all", null);
      out.textContent = "Stack restarted.";
      loadStatus();
    } catch (e) {
      out.textContent = "Restart failed: " + e.message;
    } finally {
      restartAllBtn.disabled = false;
    }
  });
}

// ---- fetch helpers ----
async function fetchJSON(url) {
  const r = await fetch(url);
  if (!r.ok) throw new Error((await safeErr(r)) || r.statusText);
  return r.json();
}
async function postJSON(url, body) {
  const r = await fetch(url, {
    method: "POST",
    headers: body ? { "Content-Type": "application/json" } : {},
    body: body ? JSON.stringify(body) : null,
  });
  if (!r.ok) throw new Error((await safeErr(r)) || r.statusText);
  return r.json();
}
async function putJSON(url, body) {
  const r = await fetch(url, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!r.ok) throw new Error((await safeErr(r)) || r.statusText);
  return r.json();
}
async function safeErr(r) {
  try {
    const j = await r.json();
    return j.error;
  } catch (_) {
    return null;
  }
}

function esc(s) {
  return String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
}

// ---- boot ----
loadStatus();
startStats();
setInterval(loadStatus, 5000);

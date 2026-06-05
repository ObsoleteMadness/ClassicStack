"use strict";

// ClassicStack management SPA. A deliberately dependency-free vanilla-JS
// app: it talks to the control-plane JSON API and the SSE stats stream.
// The HTTP layer in service/webui owns no logic; everything here maps UI
// actions onto control-plane endpoints.

const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => Array.from(document.querySelectorAll(sel));

let currentConfig = null; // last-loaded config model (edited in place)
let latestRates = {}; // metric name -> per-second rate from SSE
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

    // Only standalone hooks (IPX/NetBEUI/NetBIOS/SMB/WebUI) are individually
    // start/stoppable; ports and the router-set share the stack lifecycle.
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

    card.innerHTML = `
      <h3>${indicator}${esc(u.name)}</h3>
      <div class="kv">${stateLine}</div>
      ${detail}
      <div class="kv metric" data-metric-for="${esc(u.name)}"></div>
      <div class="card-actions">${controls}</div>
    `;
    card.querySelectorAll("[data-action]").forEach((btn) =>
      btn.addEventListener("click", () => serviceAction(btn.dataset.svc, btn.dataset.action))
    );
    grid.appendChild(card);
  });
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
      $$("[data-metric-for]").forEach((el) => {
        const name = el.getAttribute("data-metric-for");
        const total = sumRatesForUnit(name);
        if (total !== null) el.textContent = `${total}/s`;
      });
    } catch (_) {}
  };
  es.onerror = () => {
    /* browser auto-reconnects */
  };
}

// sumRatesForUnit matches metric names that embed the unit name. Naming is
// best-effort until services publish per-unit metric labels.
function sumRatesForUnit(unit) {
  const key = unit.toLowerCase().replace(/[^a-z0-9]/g, "");
  let sum = 0;
  let found = false;
  Object.keys(latestRates).forEach((m) => {
    if (m.toLowerCase().replace(/[^a-z0-9]/g, "").includes(key)) {
      sum += latestRates[m];
      found = true;
    }
  });
  return found ? sum : null;
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
    fields: [
      { label: "Enabled", path: "LToUdp.enabled", type: "bool" },
      { label: "Interface", path: "LToUdp.interface", type: "text" },
      { label: "Zone Name", path: "LToUdp.seed_zone", type: "text" },
      { label: "Seed Network", path: "LToUdp.seed_network", type: "number" },
    ],
  },
  {
    title: "TashTalk (LocalTalk)",
    fields: [
      { label: "Serial Port", path: "TashTalk.port", type: "serial" },
      { label: "Zone Name", path: "TashTalk.seed_zone", type: "text" },
      { label: "Seed Network", path: "TashTalk.seed_network", type: "number" },
    ],
  },
  {
    // The shared virtual interface protocols inherit unless they go Custom.
    title: "Bridge (shared interface)",
    fields: [
      { label: "Mode", path: "Bridge.mode", type: "select", options: IFACE_MODES },
      { label: "Device", path: "Bridge.device", type: "iface" },
      { label: "HW Address", path: "Bridge.hw_address", type: "text" },
      { label: "Bridge Mode", path: "Bridge.bridge_mode", type: "select", options: BRIDGE_MODES },
    ],
  },
  {
    title: "EtherTalk",
    interfaceFor: "EtherTalk",
    fields: [
      { label: "Zone Name", path: "EtherTalk.seed_zone", type: "text" },
      { label: "Seed Net Min", path: "EtherTalk.seed_network_min", type: "number" },
      { label: "Seed Net Max", path: "EtherTalk.seed_network_max", type: "number" },
    ],
  },
  {
    title: "NetBEUI (NBF)",
    interfaceFor: "NetBEUI",
    fields: [{ label: "Enabled", path: "NetBEUI.enabled", type: "bool" }],
  },
  {
    title: "IPX",
    interfaceFor: "IPX",
    fields: [
      { label: "Enabled", path: "IPX.enabled", type: "bool" },
      { label: "Framing", path: "IPX.framing", type: "select", options: IPX_FRAMINGS },
      { label: "Network", path: "IPX.internal_network", type: "text" },
    ],
  },
  {
    title: "MacIP Gateway",
    interfaceFor: "MacIP",
    fields: [
      { label: "Enabled", path: "MacIP.enabled", type: "bool" },
      { label: "Gateway Mode", path: "MacIP.mode", type: "select", options: ["pcap", "nat"] },
      { label: "Zone", path: "MacIP.zone", type: "text" },
      { label: "NAT Subnet", path: "MacIP.nat_subnet", type: "text" },
      { label: "IP Gateway", path: "MacIP.ip_gateway", type: "text" },
      { label: "DHCP Relay", path: "MacIP.dhcp_relay", type: "bool" },
    ],
  },
  {
    title: "AFP File Server",
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
    title: "SMB Server",
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
    fields: [
      { label: "Enabled", path: "WebUI.enabled", type: "bool" },
      { label: "Bind", path: "WebUI.bind", type: "text" },
      { label: "TLS", path: "WebUI.tls", type: "bool" },
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

async function renderConfig(cfg) {
  [interfaceList, serialList, fsTypeList] = await Promise.all([
    fetchJSON("/api/interfaces").catch(() => []),
    fetchJSON("/api/serial-ports").catch(() => []),
    fetchJSON("/api/fs-types").catch(() => ["local_fs"]),
  ]);
  if (!fsTypeList || !fsTypeList.length) fsTypeList = ["local_fs"];

  const root = $("#config-panels");
  root.innerHTML = "";
  CONFIG_PANELS.forEach((panel) => {
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
    root.appendChild(fs);
  });
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

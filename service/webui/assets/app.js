"use strict";

// ClassicStack management SPA. A deliberately dependency-free vanilla-JS
// app: it talks to the control-plane JSON API and the SSE stats stream.
// The HTTP layer in service/webui owns no logic; everything here maps UI
// actions onto control-plane endpoints.

const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => Array.from(document.querySelectorAll(sel));

let currentConfig = null; // last-loaded config model (edited in place)
let latestRates = {}; // metric name -> per-second rate from SSE

// ---- tab switching ----
$$(".tab").forEach((btn) => {
  btn.addEventListener("click", () => {
    $$(".tab").forEach((b) => b.classList.remove("active"));
    $$(".panel-view").forEach((v) => v.classList.remove("active"));
    btn.classList.add("active");
    $("#" + btn.dataset.tab).classList.add("active");
    if (btn.dataset.tab === "config") loadConfig();
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

    card.innerHTML = `
      <h3><span class="dot ${u.running ? "running" : ""}"></span>${esc(u.name)}</h3>
      <div class="kv">${u.enabled ? "Enabled" : "Disabled"} · ${u.running ? "Running" : "Stopped"}</div>
      ${detail}
      <div class="kv metric" data-metric-for="${esc(u.name)}"></div>
      <button data-restart="${esc(u.name)}">Restart</button>
    `;
    card.querySelector("[data-restart]").addEventListener("click", () => restart(u.name));
    grid.appendChild(card);
  });
}

function kv(k, v) {
  return `<div class="kv"><b>${esc(k)}:</b> ${esc(String(v))}</div>`;
}

async function restart(name) {
  try {
    await postJSON(`/api/services/${encodeURIComponent(name)}/restart`, null);
    setTimeout(loadStatus, 300);
  } catch (e) {
    alert("Restart failed: " + e.message);
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
    title: "EtherTalk",
    fields: [
      { label: "Interface", path: "Bridge.device", type: "iface" },
      { label: "Bridge Mode", path: "Bridge.mode", type: "text" },
      { label: "Zone Name", path: "EtherTalk.seed_zone", type: "text" },
      { label: "Seed Net Min", path: "EtherTalk.seed_network_min", type: "number" },
      { label: "Seed Net Max", path: "EtherTalk.seed_network_max", type: "number" },
    ],
  },
  {
    title: "NetBEUI (NBF)",
    fields: [
      { label: "Enabled", path: "NetBEUI.enabled", type: "bool" },
      { label: "Interface", path: "NetBEUI.interface", type: "iface" },
    ],
  },
  {
    title: "IPX",
    fields: [
      { label: "Enabled", path: "IPX.enabled", type: "bool" },
      { label: "Interface", path: "IPX.interface", type: "iface" },
      { label: "Framing", path: "IPX.framing", type: "text" },
      { label: "Network", path: "IPX.internal_network", type: "text" },
    ],
  },
  {
    title: "AFP File Server",
    fields: [
      { label: "Enabled", path: "AFP.enabled", type: "bool" },
      { label: "Server Name", path: "AFP.name", type: "text" },
      { label: "Zone", path: "AFP.zone", type: "text" },
      { label: "Binding", path: "AFP.binding", type: "text" },
    ],
  },
  {
    title: "SMB Server",
    fields: [
      { label: "Enabled", path: "SMB.enabled", type: "bool" },
      { label: "Server Name", path: "SMB.server_name", type: "text" },
      { label: "Workgroup", path: "SMB.workgroup", type: "text" },
      { label: "NBT Binding", path: "SMB.nbt_binding", type: "text" },
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

let interfaceList = [];
let serialList = [];

async function renderConfig(cfg) {
  [interfaceList, serialList] = await Promise.all([
    fetchJSON("/api/interfaces").catch(() => []),
    fetchJSON("/api/serial-ports").catch(() => []),
  ]);

  const root = $("#config-panels");
  root.innerHTML = "";
  CONFIG_PANELS.forEach((panel) => {
    const fs = document.createElement("fieldset");
    fs.className = "config-panel";
    const legend = document.createElement("legend");
    legend.textContent = panel.title;
    fs.appendChild(legend);
    panel.fields.forEach((f) => fs.appendChild(renderField(cfg, f)));
    root.appendChild(fs);
  });
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
  } else if (f.type === "iface" || f.type === "serial") {
    input = document.createElement("select");
    const options = f.type === "iface" ? interfaceList : serialList.map((s) => s.name);
    const blank = document.createElement("option");
    blank.value = "";
    blank.textContent = "(none)";
    input.appendChild(blank);
    options.forEach((opt) => {
      const o = document.createElement("option");
      o.value = opt;
      o.textContent = opt;
      if (opt === val) o.selected = true;
      input.appendChild(o);
    });
    input.addEventListener("change", () => {
      setPath(cfg, f.path, input.value);
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
    status("Applied live. Changes are running but not yet saved to disk.");
    loadStatus();
  } catch (e) {
    status("Apply failed: " + e.message);
  }
});

$("#btn-save").addEventListener("click", async () => {
  if (!confirm("Saving rewrites server.toml and removes comments. Continue?")) return;
  try {
    await putJSON("/api/config", currentConfig);
    const r = await postJSON("/api/config/save", null);
    setDirty(false);
    status("Saved. Backup written to " + (r.backup || "(no previous file)") + ".");
  } catch (e) {
    status("Save failed: " + e.message);
  }
});

function status(msg) {
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

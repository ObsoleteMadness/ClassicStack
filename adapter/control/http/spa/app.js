// ClassicStack web-admin SPA — native Web Components, no framework, no jQuery.
//
// It speaks the same JSON control API every front-end shares (adapter/control/http):
//   GET  /status            component list (409 first-run, 401 needs-auth, 200 ok)
//   POST /setup             {user,password} → first-run admin
//   POST /start|/stop|/restart  {name}
//   GET  /config            masked config model
//   GET  /list_fs_types     fs-type names
//   GET  /params_for?fs_type=…  per-type param schema (Secret → password field)
//
// Auth is HTTP Basic, handled by the browser once an admin exists. The first-run
// gate returns 409 {setup_required:true}; the SPA shows the setup form, which is the
// one unauthenticated POST the server allows.

const api = {
  async status() {
    const r = await fetch("status");
    return { code: r.status, body: r.ok ? await r.json() : null };
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
    if (!r.ok) throw new Error(`${verb} ${name}: HTTP ${r.status}`);
  },
  async config() {
    const r = await fetch("config");
    if (!r.ok) throw new Error(`config: HTTP ${r.status}`);
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
};

// setConn updates the connection badge in the header.
function setConn(text, cls) {
  const el = document.getElementById("conn");
  if (!el) return;
  el.textContent = text;
  el.className = "badge" + (cls ? " " + cls : "");
}

// <cs-app> is the root: it probes the gate and swaps in the setup or dashboard view.
class CsApp extends HTMLElement {
  connectedCallback() {
    this.route();
  }
  async route() {
    try {
      const { code, body } = await api.status();
      if (code === 409) {
        setConn("setup", "");
        this.show(new CsSetup());
        return;
      }
      if (code === 401) {
        // The browser will have prompted for Basic auth; a 401 here means the user
        // dismissed it or failed. Offer a retry.
        setConn("locked", "bad");
        this.show(el("div", { class: "panel" }, [
          el("h2", {}, ["Authentication required"]),
          el("p", { class: "muted" }, ["Reload and enter the web-admin credentials."]),
          button("Reload", "primary", () => location.reload()),
        ]));
        return;
      }
      setConn("connected", "ok");
      const dash = new CsDashboard();
      dash.units = body || [];
      this.show(dash);
    } catch (e) {
      setConn("offline", "bad");
      this.show(el("div", { class: "panel err" }, ["Cannot reach the server: " + e.message]));
    }
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
        // After setup the gate flips to Basic auth; reload so the browser prompts.
        location.reload();
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

// <cs-dashboard> shows the component status table with lifecycle actions, plus the
// config panel.
class CsDashboard extends HTMLElement {
  units = [];
  connectedCallback() {
    this.render();
  }
  async refresh() {
    const { body } = await api.status();
    this.units = body || [];
    this.render();
  }
  async act(verb, name) {
    try {
      await api.action(verb, name);
      await this.refresh();
    } catch (e) {
      alert(e.message);
    }
  }
  render() {
    const rows = this.units.map((u) => {
      const dot = el("span", { class: "dot " + (u.Running ? "run" : "stop") });
      const actions = el("div", { class: "row" }, [
        button("Start", "", () => this.act("start", u.Name), u.Running),
        button("Stop", "", () => this.act("stop", u.Name), !u.Running),
        button("Restart", "", () => this.act("restart", u.Name), !u.Running),
      ]);
      return el("tr", {}, [
        el("td", {}, [dot, u.Name]),
        el("td", { class: "muted" }, [u.Kind || ""]),
        el("td", { class: "muted" }, [u.Binding || ""]),
        el("td", {}, [u.Enabled ? "yes" : "no"]),
        el("td", {}, [actions]),
      ]);
    });

    const table = el("table", {}, [
      el("thead", {}, [el("tr", {}, [
        th("Component"), th("Kind"), th("Binding"), th("Enabled"), th("Actions"),
      ])]),
      el("tbody", {}, rows.length ? rows : [
        el("tr", {}, [el("td", { class: "muted", colspan: "5" }, ["No components built (empty config)."])]),
      ]),
    ]);

    const cfg = new CsConfig();

    this.replaceChildren(
      el("div", { class: "panel" }, [
        el("div", { class: "row", style: "justify-content:space-between" }, [
          el("h2", {}, ["Components"]),
          button("Refresh", "", () => this.refresh()),
        ]),
        table,
      ]),
      cfg,
    );
  }
}

// <cs-config> shows the live (masked) config model as formatted JSON.
class CsConfig extends HTMLElement {
  async connectedCallback() {
    const pre = el("pre", {}, ["loading…"]);
    this.replaceChildren(el("div", { class: "panel" }, [el("h2", {}, ["Configuration (masked)"]), pre]));
    try {
      const m = await api.config();
      pre.textContent = JSON.stringify(m, null, 2);
    } catch (e) {
      pre.textContent = "error: " + e.message;
    }
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
function button(label, cls, onClick, disabled = false) {
  const b = el("button", cls ? { class: cls } : {}, [label]);
  if (disabled) b.disabled = true;
  b.addEventListener("click", onClick);
  return b;
}

customElements.define("cs-app", CsApp);
customElements.define("cs-setup", CsSetup);
customElements.define("cs-dashboard", CsDashboard);
customElements.define("cs-config", CsConfig);

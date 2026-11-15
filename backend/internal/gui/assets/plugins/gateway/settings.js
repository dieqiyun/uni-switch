// Settings > Network: local gateway port, model ids and permitted web
// page origins. Uses the app's shared helpers before start.js draws it.

// renderPort: the gateway's address on this computer (Magic_zero on Discord:
// 3425 taken by another program, or one easier to tell apart). Only its
// port, 1024 up, is typed here; uni-switch moves its gateway there and every
// agent it connected with it, or says why it can't (another program has
// it). UNI_SWITCH_ADDR, a whole host:port, comes first and the field shows it,
// off (Chicring on Discord: the row was titled a port while it showed one).
let portDraft = { value: null, err: "" };
// portSays is the gateway's word on a port it couldn't take, in the page's
// language: it says the port, so it is matched rather than looked up
function portSays(m) {
  let x;
  if ((x = /^port (\d+) is in use by another program/.exec(m))) return t("Port {p} is in use by another program: pick another one", { p: x[1] });
  if ((x = /^another uni-switch serves the gateway at (\S+):/.exec(m))) return t("Another uni-switch serves the gateway at {url}: set its port there, or quit it first", { url: x[1] });
  if (/from 1024 to 65535/.test(m)) return t("A port is a number from 1024 to 65535");
  return t(m);
}
function renderPort(s) {
  const box = $("#portList");
  const r = el("div", "row pref port-row");
  const who = el("div", "who");
  who.append(el("div", "name", t("Gateway address")));
  const sub = el("div", "sub");
  const d = portDraft;
  const now = s.addrEnv ? "" : String(s.port || 3425);
  const field = input(d.value ?? now, "3425");
  field.className = "words gateway-port";
  field.inputMode = "numeric";
  field.setAttribute("aria-label", t("Gateway address"));
  const save = el("button", "text", t("Apply"));
  if (s.addrEnv) {
    field.value = s.addrEnv;
    field.disabled = save.disabled = true;
    sub.textContent = t("UNI_SWITCH_ADDR={addr} sets the gateway’s whole address, host:port: unset it to set the port here", { addr: s.addrEnv });
  } else if (d.err) {
    sub.textContent = d.err;
    sub.classList.add("err");
  } else sub.textContent = t("Choose a local port from 1024 to 65535. Connected agents move with it. Now {url}", { url: s.gateway || "" });
  who.append(sub);
  save.onclick = () => {
    const p = Number(field.value.trim());
    if (!Number.isInteger(p) || p < 1024 || p > 65535) {
      d.value = field.value; d.err = t("A port is a number from 1024 to 65535");
      return renderPort(s);
    }
    if (p === (s.port || 3425)) { d.value = null; d.err = ""; return renderPort(s); }
    save.disabled = true;
    writingPrefs(api("settings/port", { port: p }).then((res) => res.settings && (prefs = res.settings, res)))
      .then((res) => {
        portDraft = { value: null, err: "" };
        const n = res.port?.moved?.length || 0;
        if (res.port?.error) status(res.port.error, "err");
        else status(n ? t("Gateway on port {p}; {n} agents moved with it", { p, n }) : t("Gateway on port {p}", { p }), "ok", 2500);
        // the sidebar's gateway line, and the list's address the pages
        // copy, say where it is now, not the port before (noahhrreion on
        // Discord: it kept the old one until Providers was opened)
        if (res.settings?.gateway) renderSideGw({ ...sideGwAt, url: res.settings.gateway, running: true });
        if (providers) loadProviders().catch(() => {});
        renderSettings();
      })
      .catch((e) => { d.value = field.value; d.err = portSays(e.message); status(d.err, "err"); renderSettings(); });
  };
  field.oninput = () => { d.value = field.value; };
  field.onkeydown = (e) => { e.stopPropagation(); if (e.key === "Enter") save.onclick(); if (e.key === "Escape") { portDraft = { value: null, err: "" }; renderPort(s); } };
  const val = el("div", "val");
  val.append(field, save);
  r.append(who, val);
  box.replaceChildren(r); // swapped whole: the list is never laid out empty
}

// renderBareIds: Model ids without the provider (settings.bareModelIDs,
// love1sbug on X, moving from CLIProxyAPI): the gateway's /v1/models
// lists each model by its own id as well, gpt-5.6-sol beside
// codex/gpt-5.6-sol. Posted on its own; off by default.
function renderBareIds(s) {
  $("#bareIdsSegs").replaceChildren(segs([[false, t("Off")], [true, t("On")]], !!s.bareModelIDs, (on) =>
    writingPrefs(api("settings/bare-ids", { on })).then((ns) => { prefs = state.settings = ns; renderSettings(); })
      .catch((e) => { status(t(e.message), "err"); renderSettings(); })));
}

// renderBusyLoad: the load past which a model its plugin says is busy
// (uni-switch.load) goes behind the others (settings.BusyLoad, axelburks on
// Discord). Custom… opens a field for any other number, 1 to 9999, which
// is then shown as its own choice; Enter or Apply saves it, Escape closes

// renderCORS: the web pages whose scripts may call the gateway from a
// browser (#1051), by origin — a row to add one, and one row each to take
// it away. A page listed calls with a gateway key; none listed, no page
// can read what the gateway answers, as before.
let corsDraft = { value: "", err: "" };
// corsSays is the gateway's word on an origin it didn't take, in the
// page's language
function corsSays(m) {
  if (/wildcard/.test(m)) return t("Name each origin: a wildcard would let every web page use uni-switch");
  if (/is not an (extension's )?origin/.test(m)) return t("Write an origin as http://localhost:3000 or https://app.example.com, or a browser extension's as chrome-extension://, moz-extension:// or safari-web-extension:// followed by its ID");
  return t(m);
}
function renderCORS(s) {
  renderWebPage(); // Browser access, the section under this one
  const box = $("#corsList");
  box.replaceChildren();
  const origins = s.corsOrigins || [];
  const d = corsDraft;
  const set = (next, done) => writingPrefs(api("settings/cors", { origins: next }))
    .then((ns) => { prefs = ns; d.err = ""; done?.(); renderSettings(); status(t("Saved"), "ok", 1500); })
    .catch((e) => { d.err = corsSays(e.message); status(d.err, "err"); renderSettings(); });
  const field = input(d.value, "http://localhost:3000");
  field.className = "words rule-match";
  field.setAttribute("aria-label", t("Web page origin"));
  field.autocomplete = "off";
  field.spellcheck = false;
  const add = el("button", "text", t("Add"));
  add.onclick = () => {
    d.value = field.value.trim();
    if (!d.value) return field.focus();
    set([...origins, d.value], () => { corsDraft = { value: "", err: "" }; });
  };
  field.oninput = () => { d.value = field.value; };
  field.onkeydown = (e) => { e.stopPropagation(); if (e.key === "Enter") add.onclick(); };
  const r = el("div", "row pref rule-row");
  const who = el("div", "who");
  who.append(el("div", "name", t("Web pages that may call uni-switch")),
    el("div", "sub" + (d.err ? " err" : ""), d.err || t("Scripts on these origins can call the gateway from a browser, each request with a gateway key from Gateway. A page from any other site is refused; pages on this computer are not")));
  const val = el("div", "val rule-add");
  val.append(field, add);
  r.append(who, val);
  box.append(r);
  for (const o of origins) {
    const row = el("div", "row pref cors-origin"); // an extension's id runs long: wrapped, never cut
    const w = el("div", "who");
    w.append(el("div", "name", o));
    const x = el("button", "text", t("Remove"));
    x.onclick = () => set(origins.filter((v) => v !== o));
    const v = el("div", "val");
    v.append(x);
    row.append(w, v);
    box.append(row);
  }
}

// renderWebPage serves the app page to a browser on this computer only. The page always asks for its key: the link carries it, and a
// browser without it gets nothing. The app's windows alone draw it; a
// browser on the page can't turn it off under itself.
let webPageState = null, webPagePort = null;
// Each draw asks again, as a port another program held can come free; the
// answer draws without asking.
function renderWebPage(asked) {
  const head = $("#webPageHead"), box = $("#webPageList");
  if (!box) return;
  head.hidden = box.hidden = web;
  if (web) return;
  if (!asked) api("web-page").then((r) => { webPageState = r; renderWebPage(true); }).catch(() => {});
  const s = webPageState;
  if (!s) return;
  const set = (body) => writingPrefs(api("web-page", body))
    .then((r) => { webPageState = r; webPagePort = null; renderWebPage(true); })
    .catch((e) => { status(t(e.message), "err"); webPageState = null; renderWebPage(); });
  const rows = [];
  const row = (name, sub, ...tools) => {
    const r = el("div", "row pref");
    const who = el("div", "who");
    who.append(el("div", "name", name));
    if (sub) who.append(sub instanceof Node ? sub : el("div", "sub", sub));
    const val = el("div", "val");
    val.append(...tools);
    r.append(who, val);
    rows.push(r);
    return r;
  };
  row(t("Open uni-switch in a browser"),
    t("Only browsers on this computer can open the page. Address: http://127.0.0.1:{port}", { port: s.port }),
    segs([["", t("Off")], ["local", t("This computer")]], s.mode || "", (mode) => set({ mode }))).classList.add("web-page-mode");
  if (s.mode) {
    const why = s.error ? el("div", "sub err", s.error.startsWith("port ") ? t("Port {p} is taken by another program, uni-switch web perhaps: uni-switch tries again every few seconds", { p: s.port }) : t(s.error))
      : el("div", "sub say", s.serving ? t("Served; anyone with this link can change every setting") : t("Starting…"));
    const links = [s.link].filter(Boolean);
    const box2 = el("div", "web-page-links");
    for (const l of links) {
      const line = el("span", "web-page-link");
      line.append(el("code", "", l.replace(/\?k=.*/, "?k=…")), copyBtn(l, t("Link"), t("Copied")));
      box2.append(line);
    }
    const rekey = el("button", "text", t("New key"));
    rekey.onclick = () => set({ newKey: true });
    row(t("Link"), why, box2, rekey).classList.add("web-page-link-row");
    const field = input(webPagePort ?? String(s.port), "3430");
    field.className = "words gateway-port";
    field.inputMode = "numeric";
    field.setAttribute("aria-label", t("Port"));
    const save = el("button", "text", t("Apply"));
    save.onclick = () => {
      const p = Number(field.value.trim());
      if (!Number.isInteger(p) || p < 1024 || p > 65535) return status(t("A port is a number from 1024 to 65535"), "err");
      set({ port: p });
    };
    field.oninput = () => { webPagePort = field.value; };
    field.onkeydown = (e) => { e.stopPropagation(); if (e.key === "Enter") save.onclick(); };
    row(t("Port"), "", field, save);
  }
  box.replaceChildren(...rows);
}

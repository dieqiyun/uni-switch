// Shared request diagnostics, conversation rendering and account allowance helpers.
// The Usage page and its reporting API have been removed.

// What was read back from the archive, by "<date>/<id>": the archive's
// copy, or {busy} while it is read, or {error}.
const archivedCalls = new Map();


function headersPanel(label, first, headers) {
  const panel = el("section", "call-body");
  const head = el("div", "call-body-head");
  const lines = [first, ...Object.keys(headers || {}).sort().flatMap((k) => headers[k].map((v) => `${k}: ${v}`))].filter(Boolean);
  head.append(el("span", "call-body-label", label), el("span", "grow"), copyBtn(lines.join("\n"), label));
  const pre = el("pre");
  pre.append(el("code", "", lines.join("\n")));
  panel.append(head, pre);
  return panel;
}


// A call's HTTP headers both ways (usrname1833 on Discord: 查看原始请求和
// 原始响应的http头, to debug what a capture would show), as the gateway
// read them out, secrets taken out: the client's, then each request uni-switch
// sent upstream for it and the head of its answer. Folded under the bodies
// in a diagnostic details panel; open when open is.
// h is /api/gateway/call's headers, or the archive's upstream ones.
function wireHeadersView(h, open, toggle) {
  const box = el("section", "call-wire" + (open ? " open" : ""));
  const b = el("button", "text call-wire-toggle");
  b.setAttribute("aria-expanded", String(open));
  const chev = el("span", "call-chev");
  chev.append(svg(CHEV_R, 11, 1.6));
  b.append(chev, el("span", "", t("HTTP headers")));
  b.onclick = (ev) => { ev.stopPropagation(); toggle(); };
  box.append(b);
  if (!open) return box;
  box.append(wireHeadersGrid(h, true));
  return box;
}


// the panels: the client's request (when withClient), and each upstream
// request with its answer
function wireHeadersGrid(h, withClient) {
  const grid = el("div", "call-details call-wire-grid");
  if (withClient) {
    const client = headersPanel(t("From the client"), "", h.request);
    client.classList.add("call-wire-wide");
    grid.append(client);
  }
  const ups = h.upstream || [];
  if (!ups.length) grid.append(el("p", "call-wire-note", t("No request went upstream.")));
  let plugin = false;
  for (const u of ups) {
    plugin ||= !!u.plugin;
    grid.append(
      headersPanel(u.plugin ? t("Handed to plugin {name}", { name: u.plugin }) : t("Sent upstream"), `${u.method} ${u.url}`, u.request),
      headersPanel(t("Upstream response"), u.error ? u.error : `${u.proto || "HTTP"} ${u.status} · ${ledTook(u.ms)}`, u.response),
    );
  }
  if (h.more) grid.append(el("p", "call-wire-note", t("{n} more requests upstream, not kept", { n: h.more })));
  if (plugin) grid.append(el("p", "call-wire-note", t("A plugin sends a request on to its vendor with headers of its own, which uni-switch doesn't see.")));
  return grid;
}


// The archived file, whole, however large: to the browser in uni-switch web,
// to Downloads in the app (#447).
async function downloadArchive(name, b) {
  const [date, aid] = name.split("/");
  const q = `date=${encodeURIComponent(date)}&id=${encodeURIComponent(aid)}`;
  if (web) {
    const a = el("a");
    a.href = "/api/archive/file?" + q;
    a.download = "";
    a.click();
    return;
  }
  b.classList.add("busy");
  b.disabled = true;
  try {
    const r = await api("archive/export?" + q, {});
    status(t("Saved to {path}", { path: r.path }), "ok");
  } catch (e) {
    status(t(e.message), "err");
  } finally {
    b.classList.remove("busy");
    b.disabled = false;
  }
}


// one body read back from the archive: shown, or, past 256 KB, only its
// size — the server leaves it out — for the file to be downloaded whole
function archiveBodyPanel(label, part, id, key = id) {
  if (part.omitted) {
    const panel = el("section", "call-body");
    const head = el("div", "call-body-head");
    head.append(el("span", "call-body-label", t(label)), el("span", "call-body-count", fmtBytes(part.size)));
    panel.append(head, el("p", "call-archive-big", t("Too long to show here. Download the archive to read it whole.")));
    return panel;
  }
  // cut where the archive stops: its limit, or 256 KB in one from before #447
  const cut = part.truncated && (part.size ? t("first {n} of {size}", { n: fmtBytes(new Blob([part.body]).size), size: fmtBytes(part.size) }) : true);
  return callBodyPanel(label, part.body, cut, id, key);
}


// A kept call, read by archive id and redrawn once its copy arrives.
function archivePanel(name, id, redraw) {
  const box = el("section", "call-archive");
  const [date, aid] = name.split("/");
  const got = archivedCalls.get(name);
  const head = el("div", "call-body-head");
  head.append(el("span", "call-body-label", t("Request archive")), el("code", "call-archive-id", name), el("span", "grow"));
  box.append(head);
  const dl = el("button", "text call-archive-dl", t("Download"));
  dl.onclick = (ev) => { ev.stopPropagation(); downloadArchive(name, dl); };
  if (!got || got.busy || got.error) {
    const b = el("button", "text", t(got?.busy ? "Fetching…" : "Fetch from archive"));
    b.disabled = !!got?.busy;
    b.onclick = (ev) => {
      ev.stopPropagation();
      archivedCalls.set(name, { busy: true });
      redraw();
      api(`archive?date=${encodeURIComponent(date)}&id=${encodeURIComponent(aid)}`)
        .then((a) => archivedCalls.set(name, a))
        .catch((e) => archivedCalls.set(name, { error: t(e.message) }))
        .finally(redraw);
    };
    head.append(b);
    if (got?.error) box.append(el("div", "call-archive-err", got.error));
    else head.append(dl); // one not in the bucket has nothing to download
    return box;
  }
  head.append(el("span", "call-body-truncated call-archive-note", t("Secrets taken out")), dl);
  if (got.large) {
    box.append(el("p", "call-archive-big", t("This archive is {size}, too large to show here. Download it to read it.", { size: got.bytes > 0 ? fmtBytes(got.bytes) : t("very large") })));
    return box;
  }
  const grid = el("div", "call-details");
  grid.append(
    headersPanel(t("Request Headers"), `${got.request.method || ""} ${got.request.path || ""}`.trim(), got.request.headers),
    headersPanel(t("Response Headers"), got.response.status ? `HTTP ${got.response.status}` : "", got.response.headers),
    archiveBodyPanel("Request Body", got.request, undefined, id + "|archive|req"),
    archiveBodyPanel("Response Body", got.response, id + "|archive"),
  );
  box.append(grid);
  // what uni-switch sent upstream for it, and the heads that came back
  if (got.upstream?.length) box.append(wireHeadersGrid({ upstream: got.upstream, more: got.upstreamMore }, false));
  return box;
}

// sharedBalances (keyCards): the keys uni-switch found to read one account's
// balance (sharedBalance, the first one's name) as one card, the first's,
// with the keys it stands for (sharedKeys): mimi on Discord, a site's three
// keys each showing the account's $8.39 and its curve. Keys that read
// their own, or another account's, stay each its own.
function sharedBalances(list) {
  if (!list.some((q) => q.sharedBalance)) return list;
  const same = (a, b) => a.provider === b.provider && (a.from || "") === (b.from || "") && a.sharedBalance === b.sharedBalance;
  const out = [];
  for (const q of list) {
    if (!q.sharedBalance) { out.push(q); continue; }
    const of = list.filter((x) => x.sharedBalance && same(x, q));
    if (of[0] !== q) continue;
    out.push(of.length > 1 ? { ...q, sharedKeys: of.map((x) => x.user) } : q);
  }
  return out;
}

// quotaReadings: a line's last eight readings, newest first, each what was
// left and when it was read (#802), a line each, ↻ where the window
// started again; for its legend key's title
function quotaReadings(l, now) {
  const pts = l.points.slice(-8);
  return pts.map((p, i) => (i > 0 && quotaCycleBreak(pts[i - 1], p) ? "↻ " : "") + Math.round(p.left) + "% · " + quotaTimeText(p.at, now)).reverse();
}

const cacheOf = (x) => (x.cache_read || 0) + (x.cache_write || 0);

// a count said short (fmtN: 1.2M, 35K, 12.3 万) with its exact value for a
// tooltip, nothing when the two read the same (skyhuang1994 on Discord:
// "看这么长串的数字有点懵逼")
const exactN = (n) => fmtN(n ||= 0) == String(n) ? "" : ledNum(n);


// ---------- sessions ----------
//
// Shared session identity, models and resume metadata.

// Used by the independent Sessions manager.

// sessModelNames lists models for a cost note, one an agent recorded no
// name for told as such: joined bare, the note read "Not counted: , with
// no known price" (tony, Discord)
const sessModelNames = (models) => [...new Set(models.map((m) => m || t("a model with no name")))].join(", ");


// wslBadge marks a session an agent ran in a WSL distro, read through
// \\wsl.localhost; its resume command runs it there through wsl.exe
function wslBadge(s) {
  const b = el("span", "badge wsl", "WSL " + s.wsl);
  b.title = t("In WSL {distro}: resumed there through wsl.exe", { distro: s.wsl });
  return b;
}


// sessModelsText: a session's models, the first two and how many more, and
// where uni-switch's gateway sent its calls, the most first: a routing group's
// member and the reasoning it was asked for
function sessModelsText(s) {
  const via = s.via?.length ? "→ " + [s.via[0].model, s.via[0].effort].filter(Boolean).join(" · ") + (s.via.length > 1 ? " +" + (s.via.length - 1) : "") : "";
  return sessModelNames(s.models.slice(0, 2).map((m) => m.model)) + (s.models.length > 2 ? " +" + (s.models.length - 2) : "") + (via ? " " + via : "");
}


// one place a session's calls went through uni-switch, in words
function viaText(v) {
  return `${v.provider}/${v.model}` + (v.effort ? " · " + v.effort : "") + " · " + t("{n} calls", { n: v.calls });
}


const ledExpanded = new WeakSet();

const LED_KINDS = { tool_use: "Tool call", tool_result: "Tool result", thinking: "Thinking", context: "Context", image: "Image" };

const LED_ROLES = { user: "You", assistant: "Assistant", tool: "Tool" };


// one thing said: who said it, and its words in a box; long ones show a part and
// unroll, and reasoning and what the agent put in itself are folded; model,
// when given, is said muted after it (a session's reader names the model
// of each reply, #2253)
function ledSaid(p, model) {
  const head = el("div", "cx-h");
  head.append(el("span", "cx-role " + p.role, t(LED_KINDS[p.kind] || LED_ROLES[p.role] || p.role)));
  if (p.name) head.append(el("code", "cx-name", p.name));
  if (model) {
    const m = el("span", "cx-model", model);
    m.title = t("Model") + ": " + model;
    head.append(m);
  }
  const words = el("pre", "cx-t", p.text);
  if (p.cut) words.append(el("em", "cx-cut", "\n" + t("… {n} more characters not shown", { n: ledNum(p.cut) })));
  if (p.kind === "thinking" || p.kind === "context") {
    const d = el("details", "cx-part fold");
    const sum = el("summary");
    sum.append(head);
    d.append(sum, words);
    return d;
  }
  const part = el("div", "cx-part");
  part.append(head, words);
  if (p.text.length > 700 || p.text.split("\n").length > 9) {
    part.classList.toggle("clamp", !ledExpanded.has(p));
    const more = el("button", "text cx-more", t(ledExpanded.has(p) ? "Collapse content" : "Show full content"));
    more.type = "button";
    more.onclick = () => {
      const clamped = part.classList.toggle("clamp");
      if (clamped) ledExpanded.delete(p); else ledExpanded.add(p);
      more.textContent = t(clamped ? "Show full content" : "Collapse content");
    };
    part.append(more);
  }
  return part;
}

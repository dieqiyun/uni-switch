// Drive the shipped callbacks with held API replies. A background read and
// two different card refreshes may finish in any order (#1630).
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const { test } = require("node:test");

const source = fs.readFileSync(process.env.UNI_SWITCH_APP_JS || path.resolve(__dirname, "../assets/app.js"), "utf8").replace(/\r\n/g, "\n");
function section(start, end) {
  const a = source.indexOf(start), b = source.indexOf(end, a);
  assert(a >= 0 && b > a, `missing ${start}`);
  return source.slice(a, b);
}
const plain = (x) => JSON.parse(JSON.stringify(x));
const card = (user, used) => ({ provider: "claude", user, windows: [{ name: "7 days", used }] });

function page(m = "window") {
  const context = vm.createContext({ Date, performance, setTimeout });
  vm.runInContext(`
    let quotas = [], quotasAt = 0, quotasKept = false, quotaHist = [], quotasAskedAt = 0, view = "usage";
    const mode = ${JSON.stringify(m)}; // the tray panel ("panel") also keeps each reading (#3160)
    const saved = [], localStorage = { setItem: (k, v) => saved.push([k, v]), getItem: () => null };
    const requests = [], errors = [];
    const usageAcctKey = q => q.provider + "\\n" + (q.user || "").toLowerCase();
    const quotaCycleHolds = new Map(); // past cycles' estimates (#2009), cleared by a refresh
    const t = x => x;
    const quotaError = err => err;
    const calm = () => true;
    const document = { querySelectorAll: () => [] };
    const el = () => ({ dataset: {}, classList: { add() {}, remove() {}, toggle() {} }, setAttribute() {} });
    function renderQuotas() {}
    function loadUpstream() {}
    function status(x) { errors.push(x); }
    function statusErr(x) { status(x.message, "err"); }
    const NO_SCROLL = { preventScroll: true };
    function api(path) {
      if (path.includes("history")) return Promise.resolve([]);
      return new Promise((resolve, reject) => requests.push({ path, resolve, reject }));
    }
    ${section("let quotasLoading =", "// A period of picked days is")}
    ${section("const quotaRefreshing =", "// familyQuota:")}
    globalThis.p = {
      requests, errors,
      seed: x => { quotas = x; },
      cards: () => quotas,
      load: () => loadQuotas(),
      ask: () => loadQuotas(true), // Refresh now
      refresh: q => quotaRefresh(q).onclick({ stopPropagation() {} }),
      answer: (i, x) => requests[i].resolve(x),
      fail: i => requests[i].reject(new Error("offline")),
      saved: () => saved.map(([k, v]) => [k, JSON.parse(v)]),
    };
  `, context);
  return context.p;
}
const tick = () => new Promise(setImmediate);
// the whole-page reads sent, by query ("" or "?asked=1")
const gets = (p, q) => p.requests.flatMap((r, i) => r.path === "usage/quotas" + q ? [i] : []);

for (const when of ["before", "during"]) {
  test(`a background read started ${when} a card refresh cannot undo its result`, async () => {
    const p = page(), old = card("a@x.com", 0), fresh = card("a@x.com", 32);
    p.seed([old]);
    let loading, refreshing;
    if (when === "before") {
      loading = p.load(); await tick(); refreshing = p.refresh(old);
    } else {
      refreshing = p.refresh(old); loading = p.load(); await tick();
    }
    const post = p.requests.findIndex(r => r.path.includes("/refresh?"));
    const get = p.requests.findIndex(r => r.path === "usage/quotas");
    assert(post >= 0 && get >= 0);
    p.answer(post, [fresh]); await refreshing;
    assert.equal(p.cards()[0].windows[0].used, 32);
    p.answer(get, [old]); await loading;
    assert.equal(p.cards()[0].windows[0].used, 32, "an older whole-page reply overwrote the refreshed card");
    // A later whole-page reading remains authoritative, including removals.
    const later = p.load(); await tick();
    p.answer(p.requests.length - 1, []); await later;
    assert.deepEqual(plain(p.cards()), []);
  });
}

test("refreshing two cards keeps both results when their full-list replies cross", async () => {
  const p = page(), a = card("a@x.com", 0), b = card("b@x.com", 0);
  p.seed([a, b]);
  const first = p.refresh(a), second = p.refresh(b);
  p.answer(1, [a, card("b@x.com", 61)]); await second;
  p.answer(0, [card("a@x.com", 32), b]); await first;
  assert.deepEqual(plain(p.cards()).map(q => q.windows[0].used), [32, 61], "one card's reply undid another card's completed refresh");
});

test("a failed card refresh leaves background readings usable", async () => {
  const p = page(), old = card("a@x.com", 0);
  p.seed([old]);
  const refresh = p.refresh(old), loading = p.load(); await tick();
  p.fail(0); await refresh;
  p.answer(1, [card("a@x.com", 32)]); await loading;
  assert.equal(p.cards()[0].windows[0].used, 32);
  assert.deepEqual(plain(p.errors), ["offline"]);
});

test("a card that comes back under another key still shows the reply", async () => {
  // A card read before its account was known has no user; its refresh
  // answers under the account's name.
  const p = page(), unnamed = card("", 0);
  p.seed([unnamed]);
  const refresh = p.refresh(unnamed);
  p.answer(0, [card("a@x.com", 32)]); await refresh;
  assert.deepEqual(plain(p.cards()).map(q => [q.user, q.windows[0].used]), [["a@x.com", 32]], "the refreshed card's reply was dropped");
});

test("refresh errors: a newly named account reports the reading's failure", async () => {
  const p = page(), unnamed = card("", 0), other = { ...card("b@x.com", 0), provider: "codex" };
  p.seed([unnamed, other]);
  const refresh = p.refresh(unnamed);
  const failed = { ...card("a@x.com", 0), error: "this Claude account needs setup", windows: [] };
  p.answer(0, [failed, { ...other, error: "another provider failed" }]); await refresh;
  assert.deepEqual(plain(p.errors), [failed.error]);
  assert.equal(p.cards()[0].error, failed.error);
});

test("refresh errors: a remote summary reports its failed child, not a local card", async () => {
  const p = page(), summary = { ...card("", 0), provider: "home", from: "Home" };
  const local = { ...card("b@x.com", 0), provider: "codex" };
  p.seed([summary, local]);
  const refresh = p.refresh(summary);
  const failed = { ...card("a@x.com", 0), provider: "home/claude", from: "Home", error: "remote account needs setup", windows: [] };
  p.answer(0, [{ ...local, error: "local account failed" }, failed]); await refresh;
  assert.deepEqual(plain(p.errors), [failed.error]);
  assert.equal(p.cards().find(q => q.provider === "home/claude").error, failed.error);
});

test("refresh errors: a missing named account doesn't report another account's error", async () => {
  const p = page(), a = card("a@x.com", 0);
  p.seed([a]);
  const refresh = p.refresh(a);
  p.answer(0, [{ ...card("b@x.com", 0), error: "another account failed", windows: [] }]); await refresh;
  assert.deepEqual(plain(p.errors), []);
});

for (const newerFailed of [false, true]) {
  test(`refresh errors: a renamed reply cannot report an older error over a newer ${newerFailed ? "failure" : "reading"}`, async () => {
    const p = page(), unnamed = card("", 0), b = card("b@x.com", 0);
    p.seed([unnamed, b]);
    const first = p.refresh(unnamed), second = p.refresh(b);
    const newer = newerFailed ? { ...card("b@x.com", 0), error: "newer failure", windows: [] } : card("b@x.com", 61);
    p.answer(1, [card("", 0), newer]); await second;
    // Distinct objects, as separate JSON replies are. The first read's
    // old error is discarded by keepRefreshedQuotas, not shown anew.
    p.answer(0, [card("a@x.com", 32), { ...card("b@x.com", 0), error: "older failure", windows: [] }]); await first;
    assert.deepEqual(plain(p.errors), newerFailed ? [newer.error] : []);
    assert.deepEqual(plain(p.cards().find(q => q.user === b.user)), newer);
  });
}

test("an account first named by refresh survives an older whole-page reply", async () => {
  const p = page(), unnamed = card("", 0);
  p.seed([unnamed]);
  const loading = p.load(); await tick();
  const refresh = p.refresh(unnamed);
  p.answer(1, [card("a@x.com", 32)]); await refresh;
  p.answer(0, [unnamed]); await loading;
  assert.deepEqual(plain(p.cards()).map(q => [q.user, q.windows[0].used]), [["a@x.com", 32]], "an old reply restored the unnamed card over its refresh");
});

test("an account first named by refresh keeps another card's completed refresh", async () => {
  const p = page(), unnamed = card("", 0), b = card("b@x.com", 0);
  p.seed([unnamed,b]);
  const first = p.refresh(unnamed), second = p.refresh(b);
  p.answer(1,[unnamed,card("b@x.com",61)]); await second;
  p.answer(0,[card("a@x.com",32),b]); await first;
  assert.deepEqual(plain(p.cards()).map(q => [q.user,q.windows[0].used]), [["a@x.com",32],["b@x.com",61]], "the renamed card's reply undid another card's completed refresh");
});

test("a renamed card cannot retire another provider's protection against a pending GET", async () => {
  const p = page(), unnamed = card("", 0), b = { ...card("b@x.com", 0), provider: "codex" };
  const freshB = { ...card("b@x.com", 61), provider: "codex" };
  p.seed([unnamed, b]);
  const loading = p.load(); await tick();
  const second = p.refresh(b);
  p.answer(1, [unnamed, freshB]); await second;
  const first = p.refresh(unnamed);
  p.answer(2, [card("a@x.com", 32), b]); await first;
  const readings = () => Object.fromEntries(plain(p.cards()).map(q => [q.user, q.windows[0].used]));
  assert.deepEqual(readings(), { "a@x.com": 32, "b@x.com": 61 }, "the provider refresh replaced another provider's newer card");
  p.answer(0, [unnamed, b]); await loading;
  assert.deepEqual(readings(), { "a@x.com": 32, "b@x.com": 61 }, "the card reply retired protection needed by the older GET");
});

test("a remote summary becomes its child cards without undoing a local refresh", async () => {
  const p = page(), summary = { ...card("", 0), provider: "home", from: "Home" };
  const child = { ...card("a@x.com", 32), provider: "home/claude", from: "Home" };
  const local = { ...card("b@x.com", 0), provider: "codex" };
  const freshLocal = { ...local, windows: [{ name: "7 days", used: 61 }] };
  p.seed([summary, local]);
  const loading = p.load(); await tick();
  const remote = p.refresh(summary), own = p.refresh(local);
  p.answer(2, [summary, freshLocal]); await own;
  p.answer(1, [child, local]); await remote;
  const readings = () => Object.fromEntries(plain(p.cards()).map(q => [q.provider, q.windows[0].used]));
  assert.deepEqual(readings(), { "home/claude": 32, codex: 61 });
  p.answer(0, [summary, local]); await loading;
  assert.deepEqual(readings(), { "home/claude": 32, codex: 61 }, "the late GET restored the remote summary or an old local reading");
});

test("the tray panel keeps the reading it shows, not the older reply a refresh crossed", async () => {
  // the panel opens next on what it kept (#3160). The read brings another
  // card's change, so what it shows is neither the reply nor the list
  // shown before it.
  const p = page("panel"), old = card("a@x.com", 0), b = card("b@x.com", 0);
  p.seed([old, b]);
  const loading = p.load(); await tick();
  const refreshing = p.refresh(old);
  const post = p.requests.findIndex(r => r.path.includes("/refresh?"));
  const get = p.requests.findIndex(r => r.path === "usage/quotas");
  p.answer(post, [card("a@x.com", 32), b]); await refreshing;
  p.answer(get, [old, card("b@x.com", 7)]); await loading;
  assert.deepEqual(plain(p.cards()).map(q => q.windows[0].used), [32, 7], "an older whole-page reply overwrote the refreshed card");
  const kept = p.saved().filter(([k]) => k === "uni-switch.panelQuotas").at(-1);
  assert(kept, "the panel kept nothing");
  assert.deepEqual(plain(kept[1]).map(q => q.windows[0].used), [32, 7], "the panel kept the older reply, or the list from before the read, for its next open");
});

test("Refresh now asked behind a background read is newer than a card refresh landing while it waits", async () => {
  const p = page(), old = card("a@x.com", 0);
  p.seed([old]);
  const first = p.load(); await tick();
  const asked = p.ask(); await tick();
  const refresh = p.refresh(old);
  const post = p.requests.findIndex(r => r.path.includes("/refresh?"));
  p.answer(post, [card("a@x.com", 32)]); await refresh;
  p.answer(0, [old]); await first; await tick();
  const g = gets(p, "?asked=1");
  assert.equal(g.length, 1, "Refresh now never went out");
  p.answer(g[0], [card("a@x.com", 40)]); await asked;
  assert.equal(p.cards()[0].windows[0].used, 40, "a read that went out after the card refresh lost to it");
});

test("an older whole-page read never lands over a newer asked one", async () => {
  // reads go out one at a time, so the asked one answers last
  const p = page(), old = card("a@x.com", 0);
  p.seed([old]);
  const first = p.load(); await tick();
  const refresh = p.refresh(old);
  const post = p.requests.findIndex(r => r.path.includes("/refresh?"));
  p.answer(post, [card("a@x.com", 32)]); await refresh;
  const asked = p.ask(); await tick();
  for (const i of gets(p, "?asked=1")) p.answer(i, [card("a@x.com", 40)]); // answered first, if it is out already
  await tick();
  p.answer(0, [old]); await first; await tick();
  for (const i of gets(p, "?asked=1")) p.answer(i, [card("a@x.com", 40)]);
  await asked;
  assert.equal(p.cards()[0].windows[0].used, 40, "an older read landed over a newer one");
});

test("a whole-page read landing between leaves a newer card protected from a renamed reply", async () => {
  const p = page(), unnamed = card("", 0), b = card("b@x.com", 0);
  p.seed([unnamed, b]);
  const first = p.refresh(unnamed);
  const loading = p.load(); await tick();
  const second = p.refresh(b);
  p.answer(2, [unnamed, card("b@x.com", 61)]); await second;
  p.answer(1, [unnamed, b]); await loading;
  p.answer(0, [card("a@x.com", 32), b]); await first;
  assert.deepEqual(plain(p.cards()).map(q => [q.user, q.windows[0].used]), [["a@x.com", 32], ["b@x.com", 61]], "the renamed reply undid b's refresh once a read had landed");
});

test("a card a renamed reply leaves out stays out against an older whole-page reply", async () => {
  const p = page(), unnamed = card("", 0), gone = card("c@x.com", 5);
  p.seed([unnamed, gone]);
  const loading = p.load(); await tick();
  const refresh = p.refresh(unnamed);
  p.answer(1, [card("a@x.com", 32)]); await refresh;
  p.answer(0, [unnamed, gone]); await loading;
  assert.deepEqual(plain(p.cards()).map(q => q.user), ["a@x.com"], "an older read restored a card the refresh had replaced");
});

test("a background read started while Refresh now is out takes it, so the asked reply can't undo a card refresh", async () => {
  // reads go out one at a time, so the later read is the asked one
  const p = page(), old = card("a@x.com", 0);
  p.seed([old]);
  const first = p.load(); await tick();
  const asked = p.ask(); await tick();
  p.answer(0, [old]); await first; await tick();
  const refresh = p.refresh(old);
  const post = p.requests.findIndex(r => r.path.includes("/refresh?"));
  p.answer(post, [card("a@x.com", 32)]); await refresh;
  const later = p.load(); await tick();
  for (const i of gets(p, "").slice(1)) p.answer(i, [card("a@x.com", 32)]); // answered first, if it went out beside the asked one
  await tick();
  const g = gets(p, "?asked=1");
  assert.equal(g.length, 1, "Refresh now never went out");
  p.answer(g[0], [old]); await asked; await later;
  assert.equal(p.cards()[0].windows[0].used, 32, "the asked reply, sent before the card refresh, undid it");
});

test("a card's refresh cannot retire another card's protection against a pending GET", async () => {
  const p = page(), a = card("a@x.com", 0), b = card("b@x.com", 0);
  p.seed([a, b]);
  const loading = p.load(); await tick();
  const first = p.refresh(a);
  p.answer(1, [card("a@x.com", 32), b]); await first;
  const second = p.refresh(b);
  p.answer(2, [a, card("b@x.com", 61)]); await second;
  p.answer(0, [a, b]); await loading;
  assert.deepEqual(plain(p.cards()).map(q => q.windows[0].used), [32, 61], "b's reply retired the marker a's refresh needed against the older GET");
});

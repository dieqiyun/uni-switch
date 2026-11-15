// Run with Node's test runner and Playwright on the module path; see README.md.
// Two things the Routing page did over and over while it sat in sight,
// each making garbage WebKit let pile up (#1481):
//
// - the arc a flight takes through uni-switch (via's j) was laid again every
//   frame of the flight, a new path string and its control points sixty
//   times a second, though its ends had not moved. It is laid again only
//   when an end moves.
// - noteAccounts, which takes the account names out of every answer of
//   the API (the trace's once a second), walked each object with
//   Object.entries: an array for the object and one for each key in it.
//   It walks them with for…in.
//
// The test counts both while requests come in and fly, and checks the
// arc is still drawn and the names still hidden. UNI_SWITCH_ROUTING_ASSETS
// points the page at an older asset tree, to watch this go red there.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const engines = process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"];

const assets = path.resolve(process.env.UNI_SWITCH_ROUTING_ASSETS || path.join(__dirname, "../assets"));
const iso = (ms) => new Date(ms).toISOString();
const acct = (n) => ({ id: `acct-${n}`, provider: `prov${n}`, name: `Provider ${n}`, who: `user${n}@example.com`, kind: "account", model: `model-${n}`, known: true, used: 10 * n, plan: "Pro" });
const agentsOf = ["codex", "claude", "opencode", "kimi"];

function req(id) {
  const t0 = Date.now() - 500;
  const order = [acct(id % 5), acct((id + 1) % 5)];
  const tries = order.slice(0, 1).map((w) => ({ id: w.id, model: w.model, start: iso(t0), done: true, ms: 300, status: 200 }));
  return { id, seq: id, time: iso(t0), agent: agentsOf[id % agentsOf.length], model: order[0].model, provider: order[0].provider, order, tries, done: true, status: 200, ms: 400, tokens: 900 };
}
function live(id) {
  const r = req(id);
  r.done = false;
  r.status = 0;
  r.tries.forEach((tr) => { tr.done = false; tr.status = 0; });
  return r;
}

function serve(lang, feed, next) {
  const state = { agents: agentsOf.map((id) => ({ id, name: id[0].toUpperCase() + id.slice(1), path: `/test/${id}`, fields: [] })), profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data }).catch(() => {});
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/gateway/trace") {
      const wait = url.searchParams.get("wait");
      if (wait && !next.length) await new Promise((r) => setTimeout(r, 50));
      const routes = wait ? next.splice(0, 1) : feed;
      const seq = routes.length ? Math.max(...routes.map((r) => r.seq)) : +(url.searchParams.get("after") || 0);
      return json({ mine: true, now: new Date().toISOString(), seq, totals: { requests: 0, rerouted: 0, errors: 0 }, routes });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ models: [], groups: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// counts the arcs laid (a path.via given its d) and the frames drawn
const init = () => {
  window.__arcs = 0;
  window.__frames = 0;
  const set = Element.prototype.setAttribute;
  Element.prototype.setAttribute = function (k, v) {
    if (k === "d" && this.classList?.contains("via")) window.__arcs++;
    return set.call(this, k, v);
  };
  const raf = window.requestAnimationFrame.bind(window);
  window.requestAnimationFrame = (cb) => raf((ts) => { window.__frames++; cb(ts); });
};

for (const engine of engines) for (const lang of ["en", "zh"]) {
  test(`${engine} ${lang}: a flight's arc is laid again only when its ends move, and the trace's account names are read without an array for each object`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const context = await browser.newContext({ viewport: { width: 1100, height: 900 } });
    await context.addInitScript(init);
    const page = await context.newPage();
    page.setDefaultTimeout(8000);
    page.setDefaultNavigationTimeout(60000);
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    const next = [];
    await page.route("**/*", serve(lang, [req(1)], next));
    await page.goto("http://uni-switch.test/?view=routing");
    await page.locator(".rt-req").first().waitFor();
    await page.waitForTimeout(800);

    // a request under way flies through uni-switch for as long as it is
    // answered: its arc is drawn, and laid once while nothing moves
    next.push(live(2));
    await page.waitForFunction(() => [...document.querySelectorAll("path.via")].some((p) => p.getAttribute("d")));
    await page.waitForTimeout(400); // the stage settles round the new request
    await page.evaluate(() => { window.__arcs = 0; window.__frames = 0; });
    await page.waitForTimeout(800);
    const { arcs, frames } = await page.evaluate(() => ({ arcs: window.__arcs, frames: window.__frames }));
    assert(frames > 10, `the flight drew only ${frames} frames in 800ms: nothing to count arcs over`);
    // laid every frame, it is about as many as the frames; the stage still
    // settling (a column fitted, a row come in) lays it a few times
    assert(arcs * 4 <= frames, `the arc through uni-switch was laid ${arcs} times over ${frames} frames, about once a frame`);

    // the trace's answers: noteAccounts walks each without Object.entries
    const entries = await page.evaluate(async () => {
      const e = Object.entries;
      let n = 0;
      Object.entries = function (x) { n++; return e(x); };
      const before = accountNames.size;
      noteAccounts({ routes: Array.from({ length: 50 }, (_, i) => ({ id: i, order: [{ kind: "account", who: `walk${i}@example.com`, name: "P" }], tries: [{ id: "a", status: 200 }], usage: [{ in: 1 }] })) });
      Object.entries = e;
      return { n, grown: accountNames.size - before, has: accountNames.has("walk7@example.com") };
    });
    assert.equal(entries.has, true, "noteAccounts no longer finds a route's account");
    assert.equal(entries.grown, 50, "noteAccounts missed some of the routes' accounts");
    assert(entries.n <= 2, `noteAccounts made Object.entries ${entries.n} times over one answer of 50 routes`);
    assert.equal(errors.length, 0, errors.join("\n"));
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// What a request's row in Routing says is worked out once for each route
// the gateway's trace sends, not again on every update (#1481). A trace
// update replaces only the routes that changed, yet each one redrew every
// row in sight from scratch: its outcome, its metrics with their token
// breakdown, its title and a JSON signature of all of it, ~150 MB of
// garbage a minute at one request a second. WebKit's WebContent let that
// pile up to a gigabyte while the window sat on Routing.
//
// The test counts the token breakdowns the page works out (each row's
// Tokens metric carries one in its title) while new requests come in one
// at a time over forty listed: worked out again for every row, that is
// forty and more an update; kept, it is one for the new row. A request
// that changed (answered after it was under way) and a new language are
// still drawn anew. UNI_SWITCH_ROUTING_ASSETS points the page at an older
// asset tree, to watch this go red there.
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
  const t0 = Date.now() - 60e3 + id * 100;
  const order = [acct(id % 5), acct((id + 1) % 5)];
  const tries = order.slice(0, 1).map((w) => ({ id: w.id, model: w.model, start: iso(t0), done: true, ms: 300, ttft: 120, status: 200 }));
  return { id, seq: id, time: iso(t0), agent: agentsOf[id % agentsOf.length], model: order[0].model, provider: order[0].provider, order, tries, done: true, status: 200, ms: 400, ttft: 150, tokens: 900, usage: [{ in: 600, out: 300, cache_read: 0, cache_write: 0 }] };
}
function live(id) {
  const r = req(id);
  r.done = false;
  r.status = 0;
  delete r.usage;
  r.tries.forEach((tr) => { tr.done = false; tr.status = 0; });
  return r;
}

// next: what the long poll answers next, one request at a time
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

// the token breakdowns worked out: t("Token breakdown") is asked once for
// each, wherever the page asks it. t is i18n.js's global, which the
// Routing plugin calls by name, so wrapping it counts every call.
const countBreakdowns = () => {
  window.__breakdowns = 0;
  const t0 = window.t;
  window.t = function (s, vars) { if (s === "Token breakdown") window.__breakdowns++; return t0(s, vars); };
};

for (const engine of engines) for (const lang of ["en", "zh"]) {
  test(`${engine} ${lang}: a trace update works out only the rows of the requests it brings`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const context = await browser.newContext({ viewport: { width: 1100, height: 900 } });
    const page = await context.newPage();
    page.setDefaultTimeout(8000);
    page.setDefaultNavigationTimeout(60000);
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    const feed = Array.from({ length: 40 }, (_, i) => req(i + 1)), next = [];
    await page.route("**/*", serve(lang, feed, next));
    await page.goto("http://uni-switch.test/?view=routing");
    await page.waitForFunction(() => document.querySelectorAll(".rt-req").length >= 40);
    // what the first draw settles into: the days, the agents' names
    await page.waitForTimeout(800);
    await page.evaluate(countBreakdowns);

    // three requests, one update each
    for (const id of [41, 42, 43]) {
      next.push(req(id));
      await page.waitForFunction((n) => document.querySelectorAll(".rt-req").length >= n, id);
    }
    await page.waitForTimeout(300);
    const worked = await page.evaluate(() => window.__breakdowns);
    assert(worked <= 6, `three updates of one request each worked out ${worked} token breakdowns over 43 listed rows: every row in sight was worked out again`);

    // a request under way, then answered: its row says so, the same
    // route id sent again
    next.push(live(44));
    await page.waitForFunction(() => document.querySelectorAll(".rt-req").length >= 44);
    const said = () => page.evaluate(() => document.querySelector(".rt-req .said")?.textContent || "");
    assert.match(await said(), /user4@example\.com · model-4/);
    const before = await said();
    const done = req(44);
    done.seq = 45;
    next.push(done);
    await page.waitForFunction((was) => (document.querySelector(".rt-req .said")?.textContent || "") !== was, before);
    assert.equal(await page.evaluate(() => document.querySelector(".rt-req").className.includes("wait")), false, "the answered request's row still reads as under way");

    // a new language: every row is worked out again, in it
    const title = () => page.evaluate(() => document.querySelector(".rt-req .rt-metric.tokens")?.title || "");
    const was = await title();
    assert(was, "the answered request's row has no Tokens metric to read its breakdown from");
    await page.evaluate((l) => setLocale(l === "en" ? "zh" : "en"), lang);
    await page.waitForFunction((w) => (document.querySelector(".rt-req .rt-metric.tokens")?.title || "") !== w, was);
    assert.equal(errors.length, 0, errors.join("\n"));
  });
}

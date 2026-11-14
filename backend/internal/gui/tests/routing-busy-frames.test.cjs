// Run with Node's test runner and Playwright on the module path; see README.md.
// The Routing page stays light while requests fly (yg6888 on X: "打开页面很
// 卡", three agents sending at once through a group). Every frame poses
// every flight, and each pose measured every path it flies along and the
// wire ends it meets anew — getTotalLength and getPointAtLength took ~10 s
// of a 40 s look at a busy page — and each render formatted every row's
// token counts with a new formatter. A path is measured again only when it
// moves, its points are read from its d, and the numbers still read in
// the page's language.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const iso = (ms) => new Date(ms).toISOString();
const acct = (n) => ({ id: `acct-${n}`, provider: `prov${n}`, name: `Provider ${n}`, who: `user${n}@example.com`, kind: "account", model: `model-${n}`, known: true, used: 10 * n, plan: "Pro" });
const agentsOf = ["codex", "claude", "opencode"];

// a request answered by its account, or still under way at it
function req(id, live) {
  const t0 = Date.now() - 500;
  const order = [acct(id % 4), acct((id + 1) % 4)];
  const tries = [{ id: order[0].id, model: order[0].model, start: iso(t0), done: !live, ms: 300, ...(live ? {} : { status: 200 }) }];
  return {
    id, seq: id, time: iso(t0), agent: agentsOf[id % agentsOf.length], model: order[0].model, provider: order[0].provider, order, tries,
    done: !live, status: live ? 0 : 200, ms: live ? 0 : 400, tokens: live ? 0 : 123456,
    usage: live ? [] : [{ in: 120000, out: 3456, cache_read: 98765, cache_write: 1234 }],
  };
}

function serve(lang, feed) {
  const state = { agents: agentsOf.map((id) => ({ id, name: id[0].toUpperCase() + id.slice(1), path: `/test/${id}`, fields: [] })), profiles: [], settings: { lang, theme: "light" } };
  const first = Array.from({ length: 40 }, (_, i) => req(1 + i, false));
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/gateway/trace") {
      const trace = (routes) => json({ mine: true, now: new Date().toISOString(), seq: routes.length ? routes[routes.length - 1].seq : Number(url.searchParams.get("after")), totals: { requests: 0, rerouted: 0, errors: 0 }, routes });
      if (!url.searchParams.get("wait")) return trace(first);
      while (!feed.q.length && !feed.closed) await new Promise((r) => setTimeout(r, 20));
      return trace(feed.q.length ? [feed.q.shift()] : []).catch(() => {});
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

// counts of what the page measures, and of the frames it draws
const init = () => {
  const n = window.__n = { len: 0, pt: 0, lenAgain: 0, ptAgain: 0, fmt: 0, frames: 0 };
  const P = SVGGeometryElement.prototype, len = P.getTotalLength, pt = P.getPointAtLength, fmt = Number.prototype.toLocaleString;
  // what each path was measured at last, by its d: a length measured again
  // for the d it had then, or a point again along the same d, is one the
  // page measured before and asked for anew
  const lenAt = new WeakMap(), ptAt = new WeakMap();
  P.getTotalLength = function () {
    n.len++;
    const d = this.getAttribute("d");
    if (d && lenAt.get(this) === d) n.lenAgain++;
    lenAt.set(this, d);
    return len.call(this);
  };
  P.getPointAtLength = function (s) {
    n.pt++;
    const d = this.getAttribute("d");
    let m = ptAt.get(this);
    if (!m || m.d !== d) ptAt.set(this, m = { d, at: new Set() });
    if (d && m.at.has(s)) n.ptAgain++;
    m.at.add(s);
    return pt.call(this, s);
  };
  Number.prototype.toLocaleString = function (...a) { n.fmt++; return fmt.apply(this, a); };
  const raf = window.requestAnimationFrame.bind(window);
  window.requestAnimationFrame = (cb) => raf((ts) => { n.frames++; cb(ts); });
};

const grouped = { en: "120,000", de: "120.000", zh: "120,000" };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "de"]) {
    test(`${engine} ${lang}: flights in the air don't measure their paths every frame`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const context = await browser.newContext({ viewport: { width: 1200, height: 800 } });
      await context.addInitScript(init);
      const page = await context.newPage();
      page.setDefaultTimeout(8000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const feed = { q: [], closed: false };
      await page.route("**/*", serve(lang, feed));
      t.after(async () => { feed.closed = true; await browser.close(); });
      await page.goto("http://uni-switch.test/?view=routing");
      await page.locator(".rt-req").first().waitFor();
      await page.waitForTimeout(2500); // the first play ends

      // three agents' requests under way at once, then answered, over and over
      let id = 100;
      const pump = setInterval(() => {
        for (let i = 0; i < 3; i++) feed.q.push(req(id++, true));
        for (let i = 6; i > 3; i--) if (id - i >= 100) feed.q.push(req(id - i, false)); // the ones before, answered
      }, 400);
      t.after(() => clearInterval(pump));
      await page.waitForFunction(() => document.querySelectorAll(".rt-sky .rt-flier").length > 0);
      const before = await page.evaluate(() => ({ ...window.__n }));
      await page.waitForTimeout(3000);
      const after = await page.evaluate(() => ({ ...window.__n }));
      clearInterval(pump);
      const [frames, len, pt, lenAgain, ptAgain, fmt] = ["frames", "len", "pt", "lenAgain", "ptAgain", "fmt"].map((k) => after[k] - before[k]);
      t.diagnostic(`${frames} frames, ${len} lengths (${lenAgain} again), ${pt} points (${ptAgain} again), ${fmt} formats`);
      assert(frames > 20, `only ${frames} frames drawn`);
      // a path is measured when it is new or moved, not again for every
      // flight on it every frame (the wires the stage moves — rows sliding
      // to their new places, an agent ticking — are measured again as they
      // move, so they are new d's). Counted against the d's measured, not
      // the frames: a busy machine draws fewer frames while the flights'
      // paths stay as many, and the bar by frames went red there (#3557).
      assert(lenAgain <= len / 20, `${lenAgain} of ${len} path lengths measured again for a d already measured`);
      // a wire's two ends are asked for once by its tips and once among
      // the points along it: two a d at most, never one a frame
      assert(ptAgain <= 2 * len, `${ptAgain} of ${pt} points measured again along the ${len} d's measured`);
      // and the points along it are read from its d by the page: the
      // browser walks a path from its start for each point, and along the
      // wires to keys hundreds of rows down that was a sixth of WebKit's
      // time (#3164). The browser is asked for the ends (~1.2 a d), not
      // for points along the way (~3.3 a d here, ~20 on long wires)
      assert(pt <= 2 * len, `${pt} points asked of the browser along the ${len} d's measured`);
      // the rows' numbers come from one formatter, not one per number
      assert(fmt < 200, `${fmt} numbers formatted one formatter each over ${frames} frames`);
      // and they still read in the page's language
      const tip = await page.locator(".rt-req .rt-metric.tokens").first().getAttribute("title");
      assert(tip.includes(grouped[lang]), `token breakdown reads ${JSON.stringify(tip)}`);
      assert.deepEqual(errors, []);
    });
  }
}

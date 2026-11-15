// Run with Node's test runner and Playwright on the module path; see README.md.
// Coming back to the Routing page after uni-switch ran out of sight doesn't let
// loose a flock of uni-switchs (#302, JoeyMa-zh: after uni-switch had been in the
// background a while, opening Routing again showed a flood of birds flying
// at once). Out of sight — the window hidden, a covered window drawing no
// frames without saying it is hidden, or another tab of uni-switch's open —
// many requests come and are answered; then the page is seen again. What
// was answered meanwhile is listed, not flown: at most the few requests
// still under way fly, and the flights of the ones past never play.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const iso = (ms) => new Date(ms).toISOString();
const acct = (n) => ({ id: `acct-${n}`, provider: `prov${n}`, name: `Provider ${n}`, who: `user${n}@example.com`, kind: "account", model: `model-${n}`, known: true, used: 10 * n, plan: "Pro" });
const agentsOf = ["codex", "claude", "opencode", "kimi"];

// a request answered by its first account (after a 429 from one before it,
// now and then), or, live, still waiting on it
function req(id, live) {
  const t0 = Date.now() - 500;
  const order = [acct(id % 5), acct((id + 1) % 5)];
  const fails = !live && id % 3 === 0 ? 1 : 0;
  const tries = order.slice(0, fails + 1).map((w, i) => ({
    id: w.id, model: w.model, start: iso(t0 + i * 100), done: !live, ms: 300,
    ...(live ? {} : i < fails ? { status: 429, fail: "rate", again: true } : { status: 200 }),
  }));
  return { id, seq: id, time: iso(t0), agent: agentsOf[id % agentsOf.length], model: order[0].model, provider: order[0].provider, order, tries, done: !live, status: live ? 0 : 200, ms: live ? 0 : 400, tokens: live ? 0 : 900 };
}
const first = [req(1, false)];

function serve(lang, feed) {
  const state = { agents: agentsOf.map((id) => ({ id, name: id[0].toUpperCase() + id.slice(1), path: `/test/${id}`, fields: [] })), profiles: [], settings: { lang, theme: "light" } };
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
      // the long poll: answered with the next request the test feeds
      while (!feed.q.length && !feed.closed) await new Promise((r) => setTimeout(r, 20));
      return trace(feed.q.splice(0)).catch(() => {}); // every change since, as the gateway answers
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

// the page's frames held back while it is out of sight, as a hidden or
// covered window draws none (and a covered one may not say it is hidden),
// and document.hidden as the test says
const init = () => {
  const raf = window.requestAnimationFrame.bind(window);
  let held = [], n = 0;
  window.__paused = false;
  window.__hide = false;
  window.requestAnimationFrame = (cb) => { if (!window.__paused) return raf(cb); held.push(cb); return 1e9 + ++n; };
  window.__resume = () => { window.__paused = false; const q = held; held = []; for (const cb of q) raf(cb); };
  Object.defineProperty(Document.prototype, "hidden", { configurable: true, get: () => window.__hide });
  Object.defineProperty(Document.prototype, "visibilityState", { configurable: true, get: () => (window.__hide ? "hidden" : "visible") });
};

// how the page goes out of sight, and comes back
const ways = {
  "the window hidden": {
    hide: (page) => page.evaluate(() => { window.__paused = true; window.__hide = true; document.dispatchEvent(new Event("visibilitychange")); }),
    show: (page) => page.evaluate(() => { window.__hide = false; document.dispatchEvent(new Event("visibilitychange")); window.__resume(); }),
  },
  "the window covered, drawing no frames": {
    hide: (page) => page.evaluate(() => { window.__paused = true; }),
    show: (page) => page.evaluate(() => window.__resume()),
  },
  "another tab open": {
    hide: (page) => page.locator('#nav button[data-view="agents"]').click(),
    show: (page) => page.locator('#nav button[data-view="routing"]').click(),
  },
};

const words = { en: "Replay them all", zh: "全部重放" };
const DONE = 30, LIVE = 2;

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    for (const [way, how] of Object.entries(ways)) {
      test(`${engine} ${lang}: back in sight after ${way}, the requests of meanwhile don't all fly at once`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const context = await browser.newContext({ viewport: { width: 1100, height: 760 } });
        await context.addInitScript(init);
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const feed = { q: [], closed: false };
        await page.route("**/*", serve(lang, feed));
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-flood-${way.split(" ")[1]}.png`) });
          }
          feed.closed = true;
          await browser.close();
        });
        await page.goto("http://uni-switch.test/?view=routing");
        await page.locator(".rt-req").first().waitFor();
        await page.waitForTimeout(2500); // the first request's play ends

        // out of sight: many requests come and are answered, one update
        // each as the gateway sends them, and a couple still under way
        await how.hide(page);
        for (let i = 0; i < DONE; i++) feed.q.push(req(2 + i, false));
        for (let i = 0; i < LIVE; i++) feed.q.push(req(2 + DONE + i, true));
        for (let i = 0; i < 100 && feed.q.length; i++) await page.waitForTimeout(50);
        assert.equal(feed.q.length, 0, "the page must have taken every update");
        await page.waitForTimeout(1500);

        // back in sight: the birds in the sky, looked at every 40ms
        await page.evaluate(() => {
          window.__most = 0;
          window.__look = setInterval(() => { window.__most = Math.max(window.__most, document.querySelectorAll(".rt-sky .rt-flier").length); }, 40);
        });
        await how.show(page);
        await page.waitForTimeout(3000);
        const most = await page.evaluate(() => { clearInterval(window.__look); return window.__most; });
        // at most the ones under way: a carrier each, and one let go
        assert(most <= 2 * LIVE, `${most} uni-switchs flew at once after coming back (at most ${2 * LIVE})`);
        assert(most >= 1, "the requests still under way must fly");
        // and every request is listed
        await page.waitForFunction((n) => document.querySelectorAll(".rt-req").length === n, 1 + DONE + LIVE);
        assert.equal(await page.getByRole("button", { name: words[lang] }).count(), 1);
        assert.deepEqual(errors, []);
      });
    }
  }
}

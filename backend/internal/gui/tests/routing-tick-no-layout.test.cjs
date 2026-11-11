// Run with Node's test runner and Playwright on the module path; see README.md.
// Routing: the stage's nodes pulse as a request passes them with no layout
// of the page (gpboyer2, #3731). Each pulse took its class off, read
// offsetWidth to start the animation again, and put the class back, which
// laid the whole page out three or four times a request under steady
// traffic. With requests streaming in, nothing reads an animated node's
// offsetWidth, and the pulse still plays. In Chromium and WebKit.
const assert = require("node:assert/strict");
const { test } = require("node:test");
const fs = require("node:fs/promises");
const path = require("node:path");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = Date.now(), iso = (ms) => new Date(ms).toISOString();
const seat = { id: "codex@me", provider: "codex", name: "ChatGPT", who: "me@example.com", kind: "account", model: "gpt-6.1-sol", known: true, plan: "Pro" };
const prompt = (n) => ({ window: 272000, tokens: 90000 + n * 8000, counted: true, turns: n, parts: [
  { kind: "system", tokens: 9000, items: [{ name: "prompt", tokens: 9000 }] },
  { kind: "files", tokens: 30000 + n * 4000, items: Array.from({length: 1 + (n % 5)}, (_, i) => ({ name: "/work/f" + i + ".js", tag: "read", tokens: 3000 })) },
  { kind: "chat", tokens: 19000 + n * 2000, items: [{ name: "turn", tag: "turns", n, tokens: 19000 + n * 2000 }] } ] });
function req(id, step) {
  const t0 = now - (400 - id) * 1000, done = step === "done";
  return { id, seq: id * 10 + ({ start: 0, read: 1, done: 2 })[step], time: iso(t0), agent: "codex", session: "019a-session", model: "gpt-6.1-sol", provider: "codex",
    order: [seat], tries: [{ id: seat.id, model: seat.model, start: iso(t0), done, ...(done ? { status: 200, ms: 4000 } : {}) }],
    done, ...(done ? { status: 200, ms: 4000, tokens: 2000, cost: 0.05, priced: true, usage: [{ in: 3000, cache_read: 87000, out: 400 }] } : {}),
    ...(step === "start" ? {} : { prompt: prompt(id - 100) }) };
}
const first = [101, 102, 103].map((i) => req(i, "done"));
const models = [{ id: "a/sol", name: "sol", providerName: "A", icon: "generic" }, { id: "b/flash", name: "flash", providerName: "B", icon: "generic" }];
const base = { members: ["a/sol", "b/flash"], ready: true, memberInfo: [{ id: "a/sol", ready: true }, { id: "b/flash", ready: true }], offers: [], shared: [] };
const groups = { models, pools: [], groups: [{ ...base, id: "hand", name: "Hand", routing: "manual", pick: "a/sol", picked: "a/sol" }, { ...base, id: "auto", name: "Auto", routing: "order" }] };
function serve(feed) {
  const state = { agents: [{ id: "codex", name: "Codex", path: "/test/codex", fields: [] }], profiles: [], settings: { lang: "en", theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url()); const json = (d) => route.fulfill({ json: d });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: 'window.bootPrefs = {lang:"en",theme:"light",web:true};' });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/gateway/trace") {
      const trace = (routes) => json({ mine: true, now: new Date().toISOString(), seq: routes.length ? routes[routes.length - 1].seq : Number(url.searchParams.get("after")), totals: { requests: 0, rerouted: 0, errors: 0 }, routes });
      if (!url.searchParams.get("wait")) return trace(first);
      return trace([await new Promise((res) => { feed.next = res; })]);
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json(groups);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const ct = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file).catch(() => ""), contentType: ct });
  };
}
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: the stage pulses with no layout as requests come in`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const context = await browser.newContext({ viewport: { width: 1100, height: 900 } });
    const page = await context.newPage();
    page.setDefaultTimeout(5000);
    page.setDefaultNavigationTimeout(60000);
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    // every read of an animated node's offsetWidth counted, and every pulse
    // started or rewound
    await page.addInitScript(() => {
      window.reads = 0; window.pulses = 0;
      const d = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetWidth");
      Object.defineProperty(HTMLElement.prototype, "offsetWidth", { configurable: true, get() {
        if (this.matches?.(".rt-hub, .rt-src, .rt-cap, .rt-accts li")) window.reads++;
        return d.get.call(this);
      } });
      document.addEventListener("animationstart", (e) => { if (e.animationName === "rttick") window.pulses++; });
      setInterval(() => {
        for (const e of document.querySelectorAll(".rt-hub, .rt-src"))
          if (e.getAnimations().some((a) => a.animationName === "rttick" && a.playState === "running")) window.pulses += 0.001;
      }, 50);
    });
    const feed = {};
    await page.route("**/*", serve(feed));
    await page.goto("http://uni-switch.test/?view=routing");
    await page.locator(".rt-hub").waitFor();
    await page.waitForTimeout(800);
    const was = await page.evaluate(() => window.reads);
    for (let id = 104; id < 112; id++) for (const s of ["start", "read", "done"]) {
      for (let i = 0; i < 60 && !feed.next; i++) await page.waitForTimeout(50);
      const next = feed.next;
      assert(next, "the page asks for the next trace");
      feed.next = null;
      next(req(id, s));
      await page.waitForTimeout(250);
    }
    await page.waitForTimeout(1500);
    const [reads, pulses] = await page.evaluate(() => [window.reads, window.pulses]);
    assert.equal(reads - was, 0, "no animated node's offsetWidth read as requests came in");
    assert(pulses >= 1, `the stage still pulses: ${pulses}`);
    assert.deepEqual(errors, []);
    feed.next?.(req(999, "done"));
    await context.close();
  });
}

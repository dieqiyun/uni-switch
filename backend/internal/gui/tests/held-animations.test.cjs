// Run with Node's test runner and Playwright on the module path; see README.md.
// An entrance animation lets go of its element once it has played (#1481,
// hoffman24: the macOS app's WebKit process held over 1GB, 866MB of it
// graphics buffers for ~1,600 layers). The context window's 400 cells each
// played ctx-cell with fill "both", so all 400 stayed in effect for good,
// each one a compositing layer: 412 animations and 421 layers on Routing
// with one card. Played "backwards" (their keyframes give only where they
// start), nothing is held once they end and the card looks the same: each
// cell at full opacity and size, the cache bar grown. Chromium and WebKit,
// a phone's width and a desk's.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(process.env.ASSET_DIR || path.resolve(__dirname, "../assets"));
const now = Date.now();
const iso = (ms) => new Date(ms).toISOString();
const seat = { id: "codex@me", provider: "codex", name: "ChatGPT", who: "me@example.com", kind: "account", model: "gpt-6.1-sol", known: true, plan: "Pro" };
const prompt = (n) => ({
  window: 272000, tokens: 90000 + n * 8000, counted: true, turns: n,
  parts: [
    { kind: "system", tokens: 9000, items: [{ name: "prompt", tokens: 9000 }] },
    { kind: "files", tokens: 30000, items: [{ name: "/work/app/routing.js", tag: "read", tokens: 30000 }] },
    { kind: "chat", tokens: 19000, items: [{ name: "turn", tag: "turns", n, tokens: 19000 }] },
  ],
});
const routes = [101, 102, 103].map((id) => {
  const t0 = now - (200 - id) * 1000;
  return {
    id, seq: id * 10, time: iso(t0), agent: "codex", session: "019a-session", model: "gpt-6.1-sol", provider: "codex",
    order: [seat], tries: [{ id: seat.id, model: seat.model, start: iso(t0), done: true, status: 200, ms: 4000 }],
    done: true, status: 200, ms: 4000, tokens: 2000, cost: 0.05, priced: true, usage: [{ in: 3000, cache_read: 87000, out: 400 }], prompt: prompt(id - 100),
  };
});

async function serve(route) {
  const url = new URL(route.request().url());
  const json = (data) => route.fulfill({ json: data });
  if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"en",theme:"light",web:true};` });
  if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
  if (url.pathname === "/api/state") return json({ agents: [{ id: "codex", name: "Codex", path: "/test/codex", fields: [] }], profiles: [], settings: { lang: "en", theme: "light" } });
  if (url.pathname === "/api/gateway/trace") {
    if (url.searchParams.get("wait")) return new Promise(() => {});
    return json({ mine: true, now: new Date().toISOString(), seq: routes[routes.length - 1].seq, totals: { requests: 0, rerouted: 0, errors: 0 }, routes });
  }
  if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
  if (url.pathname === "/api/groups") return json({ groups: [], models: [], pools: [] });
  if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
  if (url.pathname.startsWith("/api/")) return json({});
  const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
  const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
  await route.fulfill({ body: await fs.readFile(file).catch(() => ""), contentType });
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const width of [420, 1100]) {
    test(`${engine} ${width}px: the context window's entrance lets go once played`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width, height: 900 } })).newPage();
      // the block grid, the look these cells are drawn in (the map is the default)
      await page.addInitScript(() => { try { localStorage.setItem("uni-switch.ctxLook", "grid"); } catch {} });
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve);
      await page.goto("http://uni-switch.test/?view=routing");
      await page.locator(".rt-ctx .ctx-waffle > i").first().waitFor();
      assert.equal(await page.locator(".rt-ctx .ctx-waffle > i").count(), 400);
      // the cells come in, staggered: the last has begun within 1.5s
      await page.waitForFunction(() => !document.getAnimations().some((a) => a.animationName === "ctx-cell" && a.playState === "running"), null, { timeout: 4000 });
      const held = await page.evaluate(() => {
        const by = {};
        for (const a of document.getAnimations()) {
          if (a.playState !== "finished") continue;
          const k = a.animationName || a.transitionProperty || "?";
          by[k] = (by[k] || 0) + 1;
        }
        return by;
      });
      assert.deepEqual(held, {}, "animations still in effect after they played");
      // and it looks as it did: every cell whole, the cache bar grown
      const look = await page.evaluate(() => {
        const cells = [...document.querySelectorAll(".rt-ctx .ctx-waffle > i")].map(getComputedStyle);
        const bar = document.querySelector(".rt-ctx .ctx-cache .ctx-bar i");
        return {
          faded: cells.filter((s) => s.opacity !== "1").length,
          scaled: cells.filter((s) => s.transform !== "none").length,
          bar: bar ? getComputedStyle(bar).transform : "none",
        };
      });
      assert.deepEqual(look, { faded: 0, scaled: 0, bar: "none" });
      assert.deepEqual(errors, []);
    });
  }
}

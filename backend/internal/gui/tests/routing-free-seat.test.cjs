// Run with Node's test runner and Playwright on the module path; see README.md.
// #3763 (MYCF711): a seat whose model its vendor serves at no cost
// (Weighed.free, which the gateway fills under a routing that weighs by
// it, Free first) said "100% used" with a full bar on the Routing page:
// the account's windows are what the rest of it costs, not this model, so
// the one seat that always can answer read as spent. Its row now says it
// costs nothing and draws no bar. A paid seat beside it still says its
// share, and a free seat that is resting still says its rest. In every
// language, at a window's width and at 420px, in both engines.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = Date.now();
const iso = (ms) => new Date(ms).toISOString();
// as the gateway sends them under Free first: the provider's routing is
// "free" on each, and only the free model's seats say free
const seat = (p, who, model, extra) => ({ id: `${p}@${who}`, provider: p, name: p, who, kind: "account", model, routing: "free", plan: "Free plan", known: true, used: 100, ...extra });
const free = seat("zen", "a@example.com", "deepseek-v4-flash-free", { free: true });
const resting = seat("zen", "b@example.com", "deepseek-v4-flash-free", { free: true, rest: { why: "rate", status: 429, until: iso(now + 20 * 60e3), for: 3600e9 } });
// the one that answered says so, as any seat does
const answered = seat("zen", "c@example.com", "deepseek-v4-flash-free", { free: true });
const paid = seat("codearts", "hw@example.com", "deepseek-v4-flash", { used: 95 });
const route = {
  id: 7, seq: 7, time: iso(now - 2000), agent: "claude", model: "deepseek-v4-flash-free", provider: "zen", done: true, status: 200, ms: 900, tokens: 1500,
  order: [answered, free, resting, paid], tries: [{ id: answered.id, model: answered.model, start: iso(now - 2000), done: true, ms: 800, status: 200 }],
};

function serve(lang) {
  const state = { agents: [{ id: "claude", name: "Claude Code", path: "/test/claude", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) return new Promise(() => {}); // nothing more comes
      return json({ mine: true, now: new Date().toISOString(), seq: 7, totals: { requests: 1, rerouted: 0, errors: 0 }, routes: [route] });
    }
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/usage/quotas/history") return json([]);
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ models: [], groups: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await r.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: ["free · costs nothing", "95% used"],
  zh: ["免费 · 不花额度", "已用 95%"],
  "zh-TW": ["免費 · 不花額度", "已用 95%"],
  ja: ["無料 · 枠を消費しない", "使用済み 95%"],
  de: ["kostenlos · zählt nicht zum Kontingent", "95% verbraucht"],
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: a seat whose model costs nothing says so, not that it is used up`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of Object.keys(words)) {
      for (const width of [1100, 420]) {
        await t.test(`${lang} at ${width}px`, async () => {
          const ctx = await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" });
          try {
            const page = await ctx.newPage();
            page.setDefaultTimeout(5000);
            page.setDefaultNavigationTimeout(60000);
            const errors = [];
            page.on("pageerror", (e) => errors.push(e.message));
            await page.route("**/*", serve(lang));
            await page.goto("http://uni-switch.test/?view=routing");
            const row = (w) => page.locator(`.rt-accts > li[title^="${w.id}"]`).first();
            await row(paid).locator("em").filter({ hasText: /\S/ }).waitFor();
            const look = (w) => row(w).evaluate((li) => ({ st: li.querySelector("em").textContent, bar: getComputedStyle(li.querySelector(".bar")).display !== "none", low: li.classList.contains("low") }));
            const [costsNothing, share] = words[lang];
            const f = await look(free);
            assert.equal(f.st, costsNothing, "the free seat says it costs nothing");
            assert.equal(f.bar, false, "and draws no bar of the account's allowance");
            assert.equal(f.low, false);
            // a paid seat beside it still says its share, with its bar
            const p = await look(paid);
            assert.equal(p.st, share);
            assert.equal(p.bar, true);
            // a free seat that is resting says its rest, not that it is free
            const r = await look(resting);
            assert.notEqual(r.st, costsNothing);
            assert.ok(await row(resting).evaluate((li) => li.classList.contains("rest")), "the resting free seat rests");
            const over = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
            assert.ok(over <= 1, `${lang} at ${width}: ${over}px past`);
            assert.deepEqual(errors, []);
          } finally {
            await ctx.close();
          }
        });
      }
    }
  });
}

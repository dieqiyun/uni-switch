// Run with Node's test runner and Playwright on the module path; see README.md.
// #3351: the Routing page's Accounts and keys counted each account's tried
// and answered over the last few requests the gateway keeps in memory,
// while the day tabs right above counted the day's whole history — the two
// could disagree by orders of magnitude. Live now, the day's counts come
// with the history read the page already polls (its `tallies`), counted
// from the same files the Usage page reads: an account tried all day but
// not in the kept window still shows its day, and the note says the counts
// are today's. What the tallies cannot say the kept requests still supply.
// In every language, in both engines.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = Date.now();
const iso = (ms) => new Date(ms).toISOString();
// the gateway keeps ten requests, all on the quiet account; the day's
// history says the busy one answered 88 of 90 tries, and the quiet one 12
const quiet = { id: "prov/quiet", provider: "prov", name: "Prov", who: "Quiet@Example.com", kind: "account", agent: "prov", model: "m" };
const routes = Array.from({ length: 10 }, (_, i) => ({
  id: 100 + i, seq: 100 + i, time: iso(now - (60 - i) * 1000), agent: "prov", model: "m", provider: "prov", done: true, status: 200, ms: 300,
  order: [quiet], tries: [{ id: "prov/quiet", model: "m", start: iso(now - (60 - i) * 1000), done: true, ms: 250, status: 200 }],
}));
const tallies = {
  '["prov","busy@example.com"]': { id: "prov/busy", provider: "prov", name: "Prov", who: "busy@example.com", kind: "account", agent: "prov", model: "m", tried: 90, ok: 88, fails: { rate: 2 } },
  '["prov","quiet@example.com"]': { id: "prov/quiet", provider: "prov", name: "Prov", who: "quiet@example.com", kind: "account", agent: "prov", model: "m", tried: 12, ok: 12 },
};
const words = {
  en: ["tried 90", "answered 88", "2 rate limited", "over today's requests"],
  zh: ["尝试 90", "应答 88", "2 限流", "统计自今天的请求"],
  "zh-TW": ["嘗試 90", "應答 88", "2 限流", "統計自今天的請求"],
  ja: ["試行 90", "応答 88", "2 レート制限", "今日のリクエストの集計"],
  de: ["90 versucht", "88 beantwortet", "2 Rate-Limit", "über die heutigen Anfragen"],
};

function serve(lang) {
  const state = { agents: [{ id: "prov", name: "Prov", path: "/test/prov", fields: [] }], profiles: [], settings: { lang, theme: "light", quotaLeft: false } };
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) return new Promise(() => {}); // nothing more comes
      return json({ mine: true, now: new Date().toISOString(), seq: 110, totals: { requests: 102, rerouted: 0, errors: 2 }, routes });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [{ day: new Date(now).toISOString().slice(0, 10), requests: 102 }], routes: [], tallies });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/usage/quotas/history") return json([]);
    if (url.pathname === "/api/groups") return json({ models: [], groups: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await r.fulfill({ body: await fs.readFile(file), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: Accounts and keys says an account's day, not the kept window's`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of Object.keys(words)) {
      await t.test(lang, async () => {
        const ctx = await browser.newContext({ viewport: { width: 1100, height: 760 }, reducedMotion: "reduce" });
        try {
          const page = await ctx.newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          const errors = [];
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", serve(lang));
          await page.goto("http://uni-switch.test/?view=routing");
          // the busy account is in no request the gateway keeps, and is
          // on the page all the same, with its day and its failures
          const busy = page.locator(".rt-act", { hasText: "busy@example.com" }).first();
          await busy.waitFor();
          const tally = busy.locator(".tally span");
          assert.deepEqual(await tally.allTextContents(), words[lang].slice(0, 3));
          // the quiet account says its day's twelve, not the kept ten
          const said = await page.locator(".rt-act", { hasText: "quiet@example.com" }).first().locator(".tally span").allTextContents();
          assert.deepEqual(said.slice(0, 2), [words[lang][0].replace("90", "12"), words[lang][1].replace("88", "12")]);
          // the note no longer points at the kept requests alone
          assert.equal(await page.locator(".row-head:has(+ .rt-acts) .note").textContent(), words[lang][3]);
          assert.deepEqual(errors, []);
        } finally {
          await ctx.close();
        }
      });
    }
  });
}

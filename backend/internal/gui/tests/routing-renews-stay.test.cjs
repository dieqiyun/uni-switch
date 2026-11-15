// Run with Node's test runner and Playwright on the module path; see README.md.
// dzxx0496 on Discord: "额度恢复时间能不能常驻". An account's row on the
// Routing page said when its allowance renews only while idle; once it was
// answering, kept for last or all but used up, the time went to its tooltip
// only. It now stays on the row whatever the row says, once. A resting row
// still says when it is back. In every language, at a window's width and at
// 420px, in both engines.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = Date.now();
const iso = (ms) => new Date(ms).toISOString();
const H = 3600e3;
const M = "deepseek-v4.1-flash";

// a request in flight on the first account (renews in 5h), the second all
// but used up (renews in 20h), the third kept for last (renews in 30h)
const acct = (i, used, h) => ({ id: `wb#a${i}`, provider: "wb", name: "WorkBuddy", who: `wb-${i}@example.com`, kind: "account", model: M,
  known: true, used, renews: [iso(now + h * H + 5 * 60e3)], usedRenews: iso(now + h * H + 5 * 60e3) });
const route = {
  id: 3, seq: 3, time: iso(now - 2000), agent: "claude", model: "group/flash", provider: "wb", done: false, status: 0, ms: 0, tokens: 0,
  group: { id: "flash", name: "Flash", routing: "", members: ["wb/" + M] },
  order: [acct(1, 40, 12), acct(2, 99, 20), acct(3, 93, 30)],
  tries: [{ id: "wb#a1", model: M, start: iso(now - 1500), done: false }],
};

function serve(lang) {
  const state = { agents: [{ id: "claude", name: "Claude Code", path: "/test/claude", fields: [] }], profiles: [], settings: { lang, theme: "light", quotaLeft: true } };
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) return new Promise(() => {}); // nothing more comes
      return json({ mine: true, now: new Date().toISOString(), seq: 3, totals: { requests: 1, rerouted: 0, errors: 0 }, routes: [route] });
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

// each row's time, as it reads in that language
const renews = {
  en: ["renews in 12 h", "renews in 20 h", "renews in 30 h"],
  zh: ["12 小时后重置", "20 小时后重置", "30 小时后重置"],
  "zh-TW": ["12 小時後重置", "20 小時後重置", "30 小時後重置"],
  ja: ["12 時間 後に更新", "20 時間 後に更新", "30 時間 後に更新"],
  de: ["erneuert sich in 12 h", "erneuert sich in 20 h", "erneuert sich in 30 h"],
};

const rowText = (page, who) => page.locator(".rt-accts > li", { hasText: who }).first().locator(".st, em").first();

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: an account's row says when it renews whatever it is doing`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of Object.keys(renews)) {
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
            await rowText(page, "wb-3@example.com").waitFor();
            for (const [i, want] of renews[lang].entries()) {
              const who = `wb-${i + 1}@example.com`;
              await page.waitForFunction(([who, w]) => [...document.querySelectorAll(".rt-accts > li")].some((l) => l.textContent.includes(who) && l.textContent.includes(w)), [who, want], { timeout: 4000 }).catch(() => {});
              const text = (await rowText(page, who).textContent()).trim();
              assert.ok(text.includes(want), `${lang} ${width}: ${who} says "${text}", not "${want}"`);
              assert.equal(text.split(want).length, 2, `${lang} ${width}: ${who} says its time once: "${text}"`);
            }
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

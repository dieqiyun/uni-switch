// Run with Node's test runner and Playwright on the module path; see README.md.
// A card whose window is used but names no reset doesn't say "Not used yet"
// (windy on Discord: WorkBuddy's 46% credits ring beside "还没用过"); a card
// with nothing used still does. APIs are mocked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const quotas = [
  { provider: "workbuddy-ai", name: "WorkBuddy AI", icon: "workbuddy", user: "a@b.c", plan: "Pro", windows: [{ name: "Credits", used: 46, count: 920, limit: 2000 }] },
  { provider: "codex", name: "Codex", icon: "codex-color", user: "x@y.z", plan: "plus", windows: [{ name: "5 hours", used: 0 }, { name: "7 days", used: 0 }] },
];

function serve(lang) {
  const settings = { theme: "light", lang, tray: "panel", quotaLeft: true, currency: "usd", usageOrder: [], panelUsageHidden: [] };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") return json(quotas);
    if (url.pathname === "/api/usage") return json({ days: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${engine} ${lang}: a used window with no reset is not "Not used yet"`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(async () => { await browser.close(); });
      const errors = [];
      const context = await browser.newContext({ viewport: { width: 380, height: 640 }, reducedMotion: "reduce" });
      await context.addInitScript(() => { try { localStorage.setItem("uni-switch.panelTab", "usage"); } catch {} });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      await page.goto("http://uni-switch.test/?mode=panel");
      await page.waitForFunction(() => document.querySelectorAll("#panelQuota .pq-card[data-card]").length >= 2);
      const say = await page.evaluate(() => {
        const out = {};
        for (const c of document.querySelectorAll("#panelQuota .pq-card[data-card]")) out[c.dataset.card.split("|")[0]] = c.querySelector(".pq-say")?.textContent ?? null;
        return { out, unused: t("Not used yet") };
      });
      assert.notEqual(say.unused, "", "the phrase is translated");
      assert.notEqual(say.out["workbuddy-ai"], say.unused, "46% used is not unused");
      assert.equal(say.out.codex, say.unused, "nothing used still says so");
      assert.deepEqual(errors, []);
    });
  }
}

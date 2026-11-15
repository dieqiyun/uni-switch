// Run with Node's test runner and Playwright on the module path; see README.md.
// A provider's Fallback picker draws the first dozen models that match; when
// more match, it says how many more there are and that typing narrows them
// (01huadalang on Discord: "怎么只有这么几个" — the rest looked missing), and a
// typed filter finds one past the dozen. In English and Chinese.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const base = { chat: "", responses: "", anthropic: "", catalog: "", agents: [], fallback: [], headers: {}, keyList: [], key: { set: true }, ready: true };
const acme = { ...base, id: "acme", name: "Acme", icon: "generic", models: [{ id: "a1", name: "A1", on: true }] };
const wb = { ...base, id: "wb", name: "WorkBuddy", icon: "generic", models: Array.from({ length: 20 }, (_, i) => ({ id: "m" + (i + 1), name: "WB Model " + (i + 1), on: true })) };

function serve(lang) {
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json({ providers: [acme, wb], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = { en: { more: "8 more — type to narrow" }, zh: { more: "还有 8 项 — 输入以筛选" } };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: the Fallback picker says how many more models match`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 900, height: 760 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator(".row.provider", { hasText: "Acme" }).first().click();
      // Fallback is under Advanced
      { const adv = page.locator(".editor details.advanced"); await adv.waitFor({ state: "attached" }); if (!(await adv.evaluate((d) => d.open))) await adv.locator(":scope > summary").click(); }
      const box = page.locator(".editor .fallback");
      await box.locator("input").focus();
      const chips = box.locator(".mchips .mchip");
      await assert.doesNotReject(chips.nth(11).waitFor());
      assert.equal(await chips.count(), 12);
      assert.equal((await box.locator(".mchips .hint").textContent()).trim(), words[lang].more);
      await box.locator("input").fill("model 17");
      assert.equal(await chips.count(), 1);
      assert.equal(await chips.first().getAttribute("title"), "wb/m17");
      assert.equal(await box.locator(".mchips .hint").count(), 0);
      assert.deepEqual(errors, []);
    });
  }
}

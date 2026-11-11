// Run with Node's test runner and Playwright on the module path; see README.md.
// A Claude account on overages (#4532) is read with its windows and an
// "Extra usage" row of text, used 100 (claudeOveragesWindow): it reads
// "In use" in the reader's language, with no meter, ring or share, and
// the windows beside it keep theirs. No backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function serve(lang) {
  const settings = { theme: "light", lang, tray: "panel" };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const IN_USE = { en: "In use", zh: "使用中", "zh-TW": "使用中", ja: "使用中", de: "In Verwendung" };
const EXTRA = { en: "Extra usage", zh: "额外用量", "zh-TW": "額外用量", ja: "追加使用量", de: "Zusätzliche Nutzung" };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${engine} ${lang}: Claude's overages row reads in use`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 440, height: 700 } })).newPage();
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      await page.goto("http://uni-switch.test/");
      await page.waitForFunction(() => typeof panelQuotaCard === "function");
      const got = await page.evaluate(() => {
        const extra = { name: "Extra usage", used: 100, display: "In use", text: true };
        const q = { windows: [{ name: "5 hours", used: 100 }, { name: "7 days", used: 40 }, extra], name: "Claude", user: "test" };
        const row = accountQuota({ test: q }, "test"), panel = panelQuotaCard(q), usage = quotaWindows(q);
        return {
          text: quotaText(extra), row: row.textContent, panel: panel.textContent, usage: usage.textContent,
          tracks: usage.querySelectorAll(".quota-track").length, rings: panel.querySelectorAll(".pq-ring").length,
          // a plugin's text row with its display keeps it as it came
          plugin: quotaText({ name: "积分余额", used: 0, display: "1,234 积分", text: true }),
        };
      });
      assert.equal(got.text, IN_USE[lang]);
      assert.equal(got.plugin, "1,234 积分");
      assert.match(got.usage, new RegExp(EXTRA[lang]));
      assert.match(got.usage, new RegExp(IN_USE[lang]));
      if (lang !== "en") assert.doesNotMatch(got.usage, /In use/);
      // the two windows keep their meters and shares; the row has none
      assert.equal(got.tracks, 2, got.usage);
      assert.equal((got.usage.match(/%/g) || []).length, 2, got.usage);
      assert.deepEqual(errors, []);
    });
  }
}

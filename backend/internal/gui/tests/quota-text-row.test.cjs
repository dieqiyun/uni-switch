// Run with Node's test runner and Playwright on the module path; see README.md.
// A plugin's usage row that is its text alone (the server's text: an
// aside with a display and nothing counted, as AutoClaw's 积分余额, Cline's
// balance or Cursor's Auto + Composer note) reads as that text: no meter,
// no ring and no "100% left" or "0% used" after it. Before, its used 0 drew
// an empty meter and "· 100% left". A counted window beside it is drawn
// as before. English and Chinese; no backend, the API is faked here.
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

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a usage row of text alone shows its text only`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 900, height: 700 } })).newPage();
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      await page.goto("http://uni-switch.test/");
      await page.waitForFunction(() => typeof panelQuotaCard === "function");
      const got = await page.evaluate(() => {
        const read = (windows) => {
          const q = { windows, name: "AutoClaw", user: "test" };
          const row = accountQuota({ test: q }, "test"), panel = panelQuotaCard(q), usage = quotaWindows(q);
          return {
            row: row.textContent, meters: row.querySelectorAll(".aq-track").length,
            panel: panel.textContent, rings: panel.querySelectorAll(".pq-ring").length,
            usage: usage.textContent, tracks: usage.querySelectorAll(".quota-track").length,
            text: quotaText(windows[windows.length - 1]),
          };
        };
        const balance = { name: "积分余额", used: 0, display: "1,234 积分", text: true };
        return { alone: read([balance]), beside: read([{ name: "5 hours", used: 40 }, balance]) };
      });
      const { alone, beside } = got;
      assert.equal(alone.text, "1,234 积分");
      for (const [where, s] of Object.entries({ row: alone.row, panel: alone.panel, usage: alone.usage })) {
        assert.match(s, /1,234 积分/, where);
        assert.doesNotMatch(s, /%/, where);
      }
      assert.equal(alone.meters, 0);
      assert.equal(alone.rings, 0);
      assert.equal(alone.tracks, 0);
      // the counted window keeps its meter, ring and share; the text row none
      assert.equal(beside.text, "1,234 积分");
      assert.equal(beside.meters, 1);
      assert.equal(beside.rings, 1);
      assert.equal(beside.tracks, 1);
      assert.match(beside.usage, /1,234 积分/);
      assert.equal((beside.usage.match(/%/g) || []).length, 1, beside.usage);
      assert.deepEqual(errors, []);
    });
  }
}

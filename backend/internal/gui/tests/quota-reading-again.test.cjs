// Run with Node's test runner and Playwright on the module path; see README.md.
// #959: the tray panel, opened, showed an account's old usage while the
// window had the new, until refreshed by hand. uni-switch answers usage/quotas
// at once with what it kept and reads the accounts again behind it; such an
// answer says so (X-Uni-Switch-Reading: 1), and the page asks again until the
// new reading has landed. The panel and the window both show it without a
// click; the follow-up asks stop once the read is done, and a run that never
// lands stops asking after half a minute's worth. English and Chinese,
// Chromium and WebKit; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

// the read behind: the first `stale` answers are the kept copy, said to be
// read while a new one is on its way; then the new one, read
function serve(lang, read) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data, headers) => route.fulfill({ json: data, headers });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/usage/quotas") {
      read.asks++;
      const stale = read.asks <= read.stale;
      const used = stale ? 40 : 100;
      return json([{ provider: "codex", name: "Codex", user: "a@example.com", plan: "plus",
        windows: [{ name: "5 hours", used, resetsAt: new Date(Date.now() + 36e5).toISOString() }] }],
      stale ? { "X-Uni-Switch-Reading": "1" } : {});
    }
    if (url.pathname === "/api/usage/quotas/history") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    for (const surface of ["panel", "window"]) {
      test(`${engine} ${lang} ${surface}: an answer read while the accounts are read again is followed by the new one`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await browser.newPage({ viewport: { width: surface === "panel" ? 440 : 900, height: 760 }, reducedMotion: "reduce" });
        page.setDefaultTimeout(8000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const read = { asks: 0, stale: 2 };
        await page.route("**/*", serve(lang, read));
        await page.goto(surface === "panel" ? "http://uni-switch.test/?mode=panel" : "http://uni-switch.test/?view=usage");
        if (surface === "panel") await page.locator('button[data-ptab="usage"]').click();
        const card = surface === "panel" ? ".pq-card" : ".subscription-card";
        await page.waitForFunction((s) => document.querySelector(s)?.textContent.includes("40%"), card);
        // no click, no focus: the new reading comes by itself
        await page.waitForFunction((s) => document.querySelector(s)?.textContent.includes("100%"), card);
        const after = read.asks;
        await page.waitForTimeout(2500);
        assert.equal(read.asks, after, "once the new reading is in, nothing more is asked");
        assert.deepEqual(errors, []);
      });
    }
    test(`${engine} ${lang}: a read that never lands is asked after for half a minute at most`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await browser.newPage({ viewport: { width: 440, height: 760 }, reducedMotion: "reduce" });
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.clock.install();
      const read = { asks: 0, stale: Infinity };
      await page.route("**/*", serve(lang, read));
      await page.goto("http://uni-switch.test/?mode=panel");
      await page.locator('button[data-ptab="usage"]').click();
      await page.waitForFunction(() => document.querySelector(".pq-card")?.textContent.includes("40%"));
      const first = read.asks;
      for (let i = 0; i < 40; i++) { await page.clock.runFor(1500); await page.waitForTimeout(30); }
      const asked = read.asks - first;
      assert(asked >= 15 && asked <= 21, `about 20 follow-ups, not more: ${asked}`);
      const n = read.asks;
      for (let i = 0; i < 10; i++) { await page.clock.runFor(1500); await page.waitForTimeout(30); }
      assert.equal(read.asks, n, "then it stops");
      assert.deepEqual(errors, []);
    });
  }
}

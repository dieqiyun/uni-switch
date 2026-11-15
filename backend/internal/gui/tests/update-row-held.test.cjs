// Run with Node's test runner and Playwright on the module path; see README.md.
// Settings › About, gone back to from another page: the Version row keeps
// its line under the name ("Up to date") and its Check button while the
// update read is still out, so the rows under it don't jump up 12px and
// back down (#3469, xisdumpling). English and Chinese; no backend, the API
// is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const words = { en: { version: "Version", latest: "Up to date", check: "Check" }, zh: { version: "版本", latest: "已是最新", check: "检查" } };

function server(lang, ctl) {
  const settings = {
    theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd", proxy: "", redact: false, redactWords: [],
    version: "0.1.400", dir: "~/.config/uni-switch", gateway: "http://127.0.0.1:3425", proxyNow: "none", proxySource: "none",
    visionModels: [], imageGenModels: [], workbuddyCheckins: [], lanURLs: [], fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
  };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" }, fx: settings.fx });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/update") {
      if (ctl.hold) await ctl.hold;
      return json({ state: "latest", current: "0.1.400" });
    }
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": the version row holds its height while its read is out", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh"]) {
      const w = words[lang], ctl = {}, errors = [];
      const page = await (await browser.newContext({ viewport: { width: 900, height: 600 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      page.on("pageerror", (e) => { if (!/^ResizeObserver loop completed with undelivered notifications/.test(e.message)) errors.push(e.message); });
      await page.route("**/*", server(lang, ctl));
      await watchSettingsReads(page);
      await page.goto("http://uni-switch.test/?view=settings&tab=about");
      await settingsDrawn(page);
      const row = page.locator("#about .row.pref", { has: page.locator(".name", { hasText: w.version }) }).first();
      await row.locator(".sub", { hasText: w.latest }).waitFor();
      const tall = (await row.boundingBox()).height;

      await page.evaluate(() => show("agents"));
      let release;
      ctl.hold = new Promise((go) => (release = go));
      // every frame from the return to Settings until the read is let go:
      // the row's height, its line under the name, and its button
      await page.evaluate((w) => {
        window.__rows = [];
        const look = () => {
          const r = [...document.querySelectorAll("#about .row.pref")].find((r) => r.querySelector(".name")?.textContent === w.version);
          if (r && r.offsetParent) window.__rows.push({ h: r.getBoundingClientRect().height, sub: r.querySelector(".who .sub")?.textContent || "", btn: !!r.querySelector(".val button") });
          if (!window.__stop) requestAnimationFrame(look);
        };
        look();
        show("settings");
      }, w);
      // the settings read and its redraw are done, the update read still held
      await page.waitForFunction(() => window.__rows.length > 20);
      await page.waitForTimeout(300);
      const held = await page.evaluate(() => window.__rows.slice());
      release();
      await row.locator(".sub", { hasText: w.latest }).waitFor();
      await page.evaluate(() => (window.__stop = true));

      const off = held.filter((s) => Math.abs(s.h - tall) > 0.5 || s.sub !== w.latest || !s.btn);
      assert.deepEqual(off.slice(0, 3), [], `${lang}: the row was ${tall}px with "${w.latest}" and ${w.check}; ${off.length} of ${held.length} frames drew it otherwise`);
      assert.equal((await row.boundingBox()).height, tall, `${lang}: the answer draws the same row`);
      assert.equal(await row.locator(".val button", { hasText: w.check }).count(), 1, `${lang}: one Check button`);
      assert.deepEqual(errors, [], `${lang}: page errors`);
      await page.context().close();
    }
  });
}

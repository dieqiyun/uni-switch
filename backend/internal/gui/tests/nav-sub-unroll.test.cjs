// Run with Node's test runner and Playwright on the module path; see README.md.
// The sidebar's list of a page's tabs (app.js navSubs) unrolls as the page
// opens. A list drawn again while it unrolls (Plugins' Installed tab gets
// its count once the plugins are read) put the selection where the rows
// were squeezed into the unrolling list, and it stayed there: on Installed
// it sat a third of a row up, over Discover's text, and shorter than the
// row (#1787). The rows keep their size while the list unrolls, so the
// selection is on its row however early the page draws its strip again.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

// the plugins' read is held until the list is unrolling, then answered
function serve(lang, held) {
  const installed = [{ spec: "@x/opencode-a-auth", providers: ["A"], version: "1.0.0", moved: [] }, { spec: "@x/opencode-b-auth", providers: ["B"], version: "1.0.0", moved: [] }];
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { url: "http://127.0.0.1:3425", running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/plugins") { await held; return json({ bun: true, bunVersion: "1.3.0", plugins: installed, movable: [] }); }
    if (url.pathname === "/api/plugins/listings") return json({ listings: [] });
    if (url.pathname === "/api/plugins/npm") return json({ npm: {} });
    if (url.pathname === "/api/plugins/github") return json({ repos: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    if (!/^\/[\w./-]*$/.test(url.pathname) || url.host !== "uni-switch.test") return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const installedWord = { en: "Installed", zh: "已安装", "zh-TW": "已安裝", ja: "インストール済み", de: "Installiert" };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: the sidebar's selection sits on its row when the list is drawn again as it unrolls`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of Object.keys(installedWord)) for (const width of [1024, 1440]) await t.test(`${lang} ${width}`, async () => {
      let open;
      const held = new Promise((r) => { open = r; });
      const context = await browser.newContext({ viewport: { width, height: 760 } });
      // the reader left Plugins on Installed
      await context.addInitScript(() => { try { localStorage.setItem("uni-switch.pluginTab", "installed"); } catch {} });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, held));
      await page.goto("http://uni-switch.test/");
      await page.waitForSelector("#nav button.on");
      assert.ok(await page.evaluate(() => document.body.classList.contains("side-nav")), "a sidebar at " + width);

      await page.locator('#nav button[data-view="plugins"]').click();
      const box = page.locator("#side .nav-sub:not(.leaving)");
      await box.waitFor();
      // the plugins come in while the list is still unrolling
      await page.waitForFunction(() => document.querySelector(".nav-sub:not(.leaving)")?.getAnimations().some((a) => a.playState === "running"));
      open();
      await page.waitForFunction((w) => document.querySelector(".nav-s.cur")?.textContent.startsWith(w) && /\d/.test(document.querySelector(".nav-s.cur").textContent), installedWord[lang]);
      // every row, the list and its selection, done moving
      await page.waitForFunction(() => {
        const b = document.querySelector(".nav-sub:not(.leaving)");
        return b && [b, ...b.querySelectorAll("*")].every((e) => e.getAnimations().every((a) => a.playState !== "running"));
      });
      await page.waitForTimeout(350); // the selection's own transition (.3s)

      const [rows, hl] = await page.evaluate(() => {
        const r = (e) => { const b = e.getBoundingClientRect(); return { top: b.top, bottom: b.bottom, text: e.textContent }; };
        return [[...document.querySelectorAll(".nav-sub:not(.leaving) .nav-s")].map(r), r(document.querySelector(".nav-sub:not(.leaving) .nav-hl"))];
      });
      // Built-in (P0 of the plugin runtime), Discover, Installed
      assert.equal(rows.length, 3);
      for (let i = 1; i < rows.length; i++) assert.ok(rows[i].top >= rows[i - 1].bottom, `the rows don't overlap: ${JSON.stringify(rows)}`);
      const at = rows.findIndex((r) => r.text.startsWith(installedWord[lang]));
      assert.ok(at > 0, `Installed is a row after another: ${JSON.stringify(rows)}`);
      assert.ok(Math.abs(hl.top - rows[at].top) < 1 && Math.abs(hl.bottom - rows[at].bottom) < 1,
        `the selection is on Installed (${rows[at].top}–${rows[at].bottom}), not ${hl.top}–${hl.bottom}`);
      assert.ok(hl.top >= rows[at - 1].bottom, "and nothing of it over the row above");
      assert.deepEqual(errors, []);
      await context.close();
    });
  });
}

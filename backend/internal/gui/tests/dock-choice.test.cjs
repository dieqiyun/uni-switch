// Run with Node's test runner and Playwright on the module path; see README.md.
// ShouguangCao on X: with Settings › Show in Dock left as it came, uni-switch's
// window was out of Command-Tab, so going back to it to paste a key copied
// in another app took the menu bar. The choice now reads With window unless
// Hide or Show was chosen (inDock in app.go), on a Mac, in both engines and
// every language, with the API faked:
// - settings that chose nothing show With window;
// - an older file's dockWindow shows With window, its dock Show;
// - Hide saves dockNever and shows Hide after; With window and Show clear it.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const choices = {
  en: ["Hide", "With window", "Show"],
  zh: ["隐藏", "跟随窗口", "显示"],
  "zh-TW": ["隱藏", "跟隨視窗", "顯示"],
  ja: ["非表示", "ウィンドウと連動", "表示"],
  de: ["Ausblenden", "Mit Fenster", "Anzeigen"],
};

function serve(lang, posted, st) {
  const settings = () => ({ lang, theme: "light", searchVendors: [], searchAPIs: [], ...st });
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data, status = 200) => r.fulfill({ status, json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: settings() });
    if (url.pathname === "/api/settings") {
      if (r.request().method() === "POST") {
        const b = JSON.parse(r.request().postData());
        posted.push(b);
        Object.assign(st, b);
      }
      return json(settings());
    }
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname === "/api/groups") return json({ models: [], groups: [], pools: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await r.fulfill({ body: await fs.readFile(file), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(choices)) {
    test(`${engine} ${lang}: Show in Dock reads With window unless Hide or Show was chosen`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const open = async (st) => {
        const page = await (await browser.newContext({ viewport: { width: 1000, height: 760 } })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.addInitScript(() => Object.defineProperty(navigator, "platform", { get: () => "MacIntel" }));
        const posted = [];
        await page.route("**/*", serve(lang, posted, st));
        await watchSettingsReads(page);
        await page.goto("http://uni-switch.test/?view=settings&tab=general");
        await settingsDrawn(page);
        await page.locator("#dockSegs .opt").first().waitFor();
        return { page, posted, errors };
      };
      // which of Hide, With window and Show is on, by place
      const on = (page) => page.locator("#dockSegs .opt").evaluateAll((os) => os.findIndex((o) => o.classList.contains("on")));
      const names = (page) => page.locator("#dockSegs .opt").evaluateAll((os) => os.map((o) => o.textContent.trim()));

      // nothing chosen: With window
      const { page, posted, errors } = await open({});
      assert.equal(await page.locator("#dockRow").isHidden(), false, "the Dock row shows on a Mac");
      assert.deepEqual(await names(page), choices[lang]);
      assert.equal(await on(page), 1, "settings that chose nothing read Hide");

      // Hide: saved as dockNever, and shown chosen
      const pick = async (i, key, want) => {
        await page.waitForFunction(() => prefsBusy === 0);
        // the page comes in animated: a place read mid-way isn't where it rests
        await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => {}))));
        await page.locator("#dockSegs .opt").nth(i).click();
        for (let n = 0; n < 50 && posted.at(-1)?.[key] !== want; n++) await page.waitForTimeout(50);
        assert.equal(posted.at(-1)?.[key], want, `posted ${JSON.stringify(posted.at(-1))}`);
      };
      await pick(0, "dockNever", true);
      assert.equal(posted.at(-1).dock, false);
      assert.equal(posted.at(-1).dockWindow, false);
      for (let n = 0; n < 50 && (await on(page)) !== 0; n++) await page.waitForTimeout(50);
      assert.equal(await on(page), 0, "Hide isn't shown chosen");
      // another setting saved keeps Hide
      await page.waitForFunction(() => prefsBusy === 0);
      const n = posted.length;
      await page.locator("#traySegs .opt").last().click();
      for (let i = 0; i < 50 && posted.length === n; i++) await page.waitForTimeout(50);
      if (posted.length > n) assert.equal(posted.at(-1).dockNever, true, "another setting's save cleared Hide");
      // With window and Show each clear it
      await pick(1, "dockWindow", true);
      assert.equal(posted.at(-1).dockNever, false);
      await pick(2, "dock", true);
      assert.equal(posted.at(-1).dockNever, false);
      assert.deepEqual(errors, []);

      // an older uni-switch's file
      for (const [st, want] of [[{ dockWindow: true }, 1], [{ dock: true }, 2], [{ dockNever: true }, 0]]) {
        const o = await open(st);
        assert.equal(await on(o.page), want, `${JSON.stringify(st)} reads ${await on(o.page)}`);
        assert.deepEqual(o.errors, []);
      }
    });
  }
}

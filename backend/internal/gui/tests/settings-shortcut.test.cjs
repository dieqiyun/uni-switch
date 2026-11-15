// Run with Node's test runner and Playwright on the module path; see README.md.
// Cmd+, (Ctrl+, off the Mac) opens Settings (#670: no shortcut for it; a
// Mac app's is ⌘,): from the Agents page, from a text box, and in the tray
// panel by asking for the window on its Settings page. Another modifier
// (Ctrl on the Mac, Alt) doesn't, a browser tab (uni-switch web) leaves the
// keys to the browser, and the gear's tooltip says the shortcut. English and
// Chinese; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function serve(lang, web, asked) {
  const settings = {
    theme: "light", lang, tray: "panel", currency: "usd", textSize: 100, version: "0.1.700", dir: "~/.config/uni-switch",
    gateway: "http://127.0.0.1:3999", proxy: "", proxyNow: "none", proxySource: "none", redactWords: [], visionModels: [], imageGenModels: [],
    workbuddyCheckins: [], lanURLs: [], fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
  };
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:${web}};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname.startsWith("/api/window/")) { if (url.pathname === "/api/window/main") asked.push(url.pathname + url.search); return json({}); }
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const words = { en: "Settings", zh: "设置" };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: Cmd/Ctrl+, opens Settings`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const open = async (url, web, asked) => {
        const page = await (await browser.newContext({ viewport: { width: 900, height: 700 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, web, asked));
        await page.goto("http://uni-switch.test" + url);
        await page.locator("#prefs").waitFor();
        return { page, errors };
      };
      const shown = (page) => page.evaluate(() => !document.querySelector("#view-settings").hidden && document.querySelector("#prefs").classList.contains("on"));

      const asked = [];
      const { page, errors } = await open("/", false, asked);
      const mod = await page.evaluate(() => /^Mac/.test(navigator.platform) ? "Meta" : "Control");
      assert.equal(await shown(page), false);
      // the gear says its shortcut
      assert.equal(await page.locator("#prefs").getAttribute("title"), words[lang] + (mod === "Meta" ? " (⌘,)" : " (Ctrl+,)"));
      // another modifier isn't it
      await page.keyboard.press((mod === "Meta" ? "Control" : "Alt") + "+Comma");
      await page.keyboard.press(mod + "+Alt+Comma");
      assert.equal(await shown(page), false);
      await page.keyboard.press(mod + "+Comma");
      await page.waitForFunction(() => !document.querySelector("#view-settings").hidden);
      assert.equal(await shown(page), true);
      assert.ok(new URL(page.url()).searchParams.get("view") === "settings", page.url());
      // from a text box on another page too
      await page.locator('#nav button[data-view="providers"]').click();
      assert.equal(await shown(page), false);
      const box = page.locator("#view-providers input:visible").first();
      if (await box.count()) { await box.focus(); }
      await page.keyboard.press(mod + "+Comma");
      await page.waitForFunction(() => !document.querySelector("#view-settings").hidden);
      assert.deepEqual(asked, []);
      assert.deepEqual(errors, []);

      // the panel asks for the window, on its Settings page
      const panelAsked = [];
      const panel = await open("/?mode=panel", false, panelAsked);
      await panel.page.keyboard.press(mod + "+Comma");
      for (let i = 0; i < 20 && !panelAsked.length; i++) await panel.page.waitForTimeout(50);
      assert.deepEqual(panelAsked, ["/api/window/main?view=settings"]);
      assert.deepEqual(panel.errors, []);

      // a browser tab leaves the keys to the browser, and says no shortcut
      const webAsked = [];
      const tab = await open("/", true, webAsked);
      assert.equal(await tab.page.locator("#prefs").getAttribute("title"), words[lang]);
      await tab.page.keyboard.press(mod + "+Comma");
      await tab.page.waitForTimeout(150);
      assert.equal(await shown(tab.page), false);
      assert.deepEqual(webAsked, []);
      assert.deepEqual(tab.errors, []);
    });
  }
}

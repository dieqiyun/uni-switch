// Run with Node's test runner and Playwright on the module path; see README.md.
// Anoninz on #2120: uni-switch's Open at login said On while the Mac's own
// switch for it, in System Settings › General › Login Items, was off, so
// uni-switch never opened at login. Settings now says whether the system will
// open it: in every language, at 1000px and 440px, in both engines, on a Mac,
// with the API faked:
// - a record the system holds switched off shows On with one line under it
//   saying so, and a button that asks uni-switch to open Login Items (the exact
//   x-apple.systempreferences URL, through /api/open);
// - the window coming back to the front reads the state again: switched
//   back on there, the line goes;
// - a page served to another computer (web) has no System Settings to
//   open, and no Open at login row at all.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn, reach } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const loginItems = "x-apple.systempreferences:com.apple.LoginItems-Settings.extension";
const line = {
  en: "Switched off in System Settings’ Login Items, so uni-switch won’t open at login",
  zh: "已在系统设置的登录项里关闭，登录时不会打开 uni-switch",
  "zh-TW": "已在系統設定的登入項目裡關閉，登入時不會開啟 uni-switch",
  ja: "システム設定のログイン項目でオフになっているため、ログイン時に uni-switch は開きません",
  de: "In den Systemeinstellungen unter „Anmeldeobjekte“ ausgeschaltet, daher öffnet sich uni-switch bei der Anmeldung nicht",
};
const button = { en: "Open Login Items", zh: "打开登录项", "zh-TW": "打開登入項目", ja: "ログイン項目を開く", de: "Anmeldeobjekte öffnen" };

function serve(lang, web, st, opened) {
  const settings = () => ({ lang, theme: "light", searchVendors: [], searchAPIs: [], ...st });
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data, status = 200) => r.fulfill({ status, json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:${web}};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: settings() });
    if (url.pathname === "/api/settings") return json(settings());
    if (url.pathname === "/api/open") {
      opened.push(JSON.parse(r.request().postData()).url);
      return json({});
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
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) for (const width of [1000, 440]) for (const web of [false, true]) {
    test(`${engine} ${lang} ${width}px${web ? " web" : ""}: Open at login says when the Mac's own switch for it is off`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width, height: 760 } })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.addInitScript(() => Object.defineProperty(navigator, "platform", { get: () => "MacIntel" }));
      const st = { login: true, loginAllow: true }, opened = [];
      await page.route("**/*", serve(lang, web, st, opened));
      await watchSettingsReads(page);
      await page.goto("http://uni-switch.test/?view=settings&tab=general");
      await settingsDrawn(page);
      const sub = page.locator("#loginRow .login-allow");
      if (web) {
        await page.locator("#loginRow").waitFor({ state: "hidden" });
        await page.locator("#langPick span").waitFor();
        assert.equal(await page.locator("#loginRow").isHidden(), true, "a page served to another computer offers Open at login");
        assert.deepEqual(errors, []);
        return;
      }
      await sub.waitFor();

      const seen = await page.evaluate(() => {
        const row = document.querySelector("#loginRow"), s = row.querySelector(".login-allow"), r = row.getBoundingClientRect();
        return {
          text: s.firstChild.textContent,
          inside: [...s.getClientRects()].every((x) => x.left >= r.left - 0.5 && x.right <= r.right + 0.5),
          stripe: parseFloat(getComputedStyle(row).borderLeftWidth) > 1,
          sideways: document.documentElement.scrollWidth > innerWidth,
          selects: document.querySelectorAll("#setPage-general select").length,
        };
      });
      assert.equal(seen.text, line[lang]);
      assert.ok(seen.inside, "the line runs out of its row");
      assert.equal(seen.stripe, false, "the row has a stripe down its left");
      assert.equal(seen.sideways, false, "the page scrolls sideways");
      assert.equal(seen.selects, 0, "no native <select>");

      const open = page.locator("#loginRow .login-allow-open");
      assert.equal(await open.innerText(), button[lang]);
      assert.equal(await open.evaluate((b) => b.tagName), "BUTTON");
      // scrolled to as a reader would, with the wheel, once the page has come
      // in (it comes in animated: a place read mid-way isn't where it rests)
      await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => {}))));
      // (reach waits for each wheel's scroll to land and for the button to be
      // the thing under the pointer: a fixed pause read a stale place, and
      // the click landed beside it)
      await reach(page, open);
      const b = await open.boundingBox();
      await page.mouse.click(b.x + b.width / 2, b.y + b.height / 2);
      for (let i = 0; i < 50 && !opened.length; i++) await page.waitForTimeout(50);
      assert.deepEqual(opened, [loginItems]);

      // switched back on in System Settings, then back to uni-switch: the line goes
      st.loginAllow = false;
      await page.evaluate(() => window.dispatchEvent(new Event("focus")));
      await sub.waitFor({ state: "detached" });
      assert.equal(await page.locator("#loginRow .sub").filter({ visible: true }).count(), 0, "Open at login says its name alone again");
      assert.deepEqual(errors, []);
    });
  }
}

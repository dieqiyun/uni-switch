// Run with Node's test runner and Playwright on the module path; see README.md.
// Settings › About: a download the network broke off (AIMetaPhD on X: an
// EOF from GitHub's asset host on Windows, 0.1.1615 to 0.1.1618) says what
// happened and what to do in the app's language: Check tries again, or the
// release page has it. The raw error, and never a signed address, stays out
// of the row. Check asks again; the release page is offered. Every
// language, both engines, at 1100 and 390 wide; no backend, the API is
// faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn, openPart } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");

const words = {
  en: { said: "0.1.1618's download broke off, a network problem. Click Check to try again, or download it from the release page", check: "Check", page: "Open the release page" },
  zh: { said: "0.1.1618 下载中断，是网络问题。点「检查」重试，或从发布页面手动下载", check: "检查", page: "打开发布页面" },
  "zh-TW": { said: "0.1.1618 下載中斷，是網路問題。點「檢查」重試，或從發布頁面手動下載", check: "檢查", page: "打開發布頁面" },
  ja: { said: "0.1.1618 のダウンロードが途中で切れました（ネットワークの問題）。「確認」で再試行するか、リリースページからダウンロードしてください", check: "確認", page: "リリースページを開く" },
  de: { said: "Der Download von 0.1.1618 brach ab, ein Netzwerkproblem. Klicke auf „Prüfen“, um es erneut zu versuchen, oder lade es von der Release-Seite herunter", check: "Prüfen", page: "Release-Seite öffnen" },
};

// what the backend says after the tries: the cause, the file, no address
const failed = {
  state: "error", current: "0.1.1615", latest: "0.1.1618", retry: true, network: true,
  url: "https://github.com/dieqiyun/uni-switch-releases/releases/tag/v0.1.1618",
  error: "the download of uni-switch.exe.new broke off (EOF)",
};

function server(lang, ctl) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    const settings = { theme: "light", lang, version: "0.1.1615", dir: "~/.config/uni-switch", gateway: "http://127.0.0.1:3425", lanURLs: [], fx: { rate: 7.2 } };
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/update/check") {
      ctl.checks++;
      return json(failed);
    }
    if (url.pathname === "/api/update") return json(failed);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a download the network broke off says so, and what to do", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const width of [1100, 390]) {
      for (const lang of Object.keys(words)) {
        const w = words[lang];
        await t.test(`${lang} at ${width}`, async () => {
          const ctl = { checks: 0 };
          const errors = [];
          const page = await (await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" })).newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          page.on("pageerror", (e) => { if (!/^ResizeObserver loop completed with undelivered notifications/.test(e.message)) errors.push(e.message); });
          await page.route("**/*", server(lang, ctl));
          await watchSettingsReads(page);
          await page.goto("http://uni-switch.test/?view=settings");
          await settingsDrawn(page);
          await openPart(page, "about");
          const row = page.locator("#about .row.pref", { has: page.locator(".sub", { hasText: w.said }) });
          await row.waitFor();
          const sub = (await row.locator(".sub").innerText()).trim();
          assert.equal(sub, w.said);
          assert.ok(!/https?:|sig=|jwt=|EOF/.test(sub), "no address and no raw error in the row: " + sub);
          await row.locator("button", { hasText: w.page }).waitFor();
          const check = row.locator("button", { hasText: w.check });
          await check.waitFor();
          const over = await page.evaluate(() => document.scrollingElement.scrollWidth - document.scrollingElement.clientWidth);
          assert.ok(over <= 0, "no sideways scroll at " + width + ": " + over);
          const box = await row.boundingBox(), sbox = await row.locator(".sub").boundingBox();
          assert.ok(sbox.x + sbox.width <= box.x + box.width + 1, "the text stays in its row");
          await check.click();
          await row.locator(".sub", { hasText: w.said }).waitFor();
          assert.equal(ctl.checks, 1, "Check asks again");
          assert.deepEqual(errors, []);
          await page.context().close();
        });
      }
    }
  });
}

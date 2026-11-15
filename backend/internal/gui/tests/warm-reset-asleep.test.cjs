// Run with Node's test runner and Playwright on the module path; see README.md.
// Warm up on reset across sleep (LEO_CHEN_X on X, 10-24): nothing runs
// while the computer sleeps, so a window that resets then is started when
// it wakes (keepWarm, codex_warmup.go, within a minute) and counts from
// then: a full 5-hour window on opening the lid. Codex's and Claude Code's
// Warm up on reset rows say so in their line's title (index.html's
// data-tt, translated with the page), as Daily warm-up's says what a
// sleeping computer does with its time. en, zh, zh-TW, ja, de;
// the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");

function serve(lang) {
  const settings = { theme: "light", lang, currency: "usd", codexWarmup: "all", claudeWarmup: "week", codexWarmed: "2026-10-24T01:00:00Z", codexUsers: ["a@example.com"] };
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const ASLEEP = {
  en: "Nothing is sent while the computer sleeps: a window that resets then starts when it wakes, and counts from then.",
  zh: "电脑睡眠时不会发送：睡眠期间重置的窗口在唤醒后开启，从唤醒时开始计时。",
  "zh-TW": "電腦睡眠時不會發送：睡眠期間重置的時段在喚醒後開啟，從喚醒時開始計時。",
  ja: "コンピュータのスリープ中は送信しません。スリープ中にリセットされた期間は復帰時に開始され、そこから数えます。",
  de: "Während der Computer schläft, wird nichts gesendet: Ein Zeitfenster, das dann zurückgesetzt wird, startet beim Aufwachen und zählt ab dann.",
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(ASLEEP)) {
    test(`${engine} ${lang}: Warm up on reset says what a sleeping computer does`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 440, height: 800 } })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      await watchSettingsReads(page);
      await page.goto("http://uni-switch.test/?view=settings&tab=usage");
      await settingsDrawn(page);
      for (const id of ["#warmSub", "#claudeWarmSub"]) {
        const sub = page.locator(id);
        // the language's own, once the page has put its words in
        await page.waitForFunction(([id, want]) => document.querySelector(id)?.title === want, [id, ASLEEP[lang]]).catch(() => {});
        const [shown, title] = await sub.evaluate((e) => [e.textContent, e.title]);
        assert.equal(title, ASLEEP[lang], `${id}: what a sleeping computer does`);
        assert.ok(shown && !shown.includes(title), `${id}: the line keeps its purpose, the fine print in the title (${shown})`);
      }
      assert.deepEqual(errors, []);
    });
  }
}

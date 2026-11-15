const assert = require("node:assert/strict");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { fixture } = require("./fixtures/caller-keys.cjs");
const { openPart } = require("./reader.cjs");

// A user asked for uni-switch's log had none: the app opened from the Finder
// wrote it nowhere (rayem1w on X). It writes app.log now, and Settings ›
// About names it with a Log row whose Open shows its folder, in every
// language and at the tray panel's width. uni-switch web writes none, and
// shows no row.
const words = {
  en: ["Log", "Open"], zh: ["日志", "打开"], "zh-TW": ["日誌", "開啟"], ja: ["ログ", "開く"], de: ["Protokoll", "Öffnen"],
};
const logPath = "~/Library/Logs/uni-switch/app.log";

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(words)) {
    for (const width of [1100, 420]) {
      test(`${engine} ${lang} ${width}: About names the app's log`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await (await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(6000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const base = fixture(lang, "light", []);
        await page.route("**/*", (route, req) => {
          const url = new URL(req.url());
          if (url.pathname === "/api/settings/log/reveal") {
            return route.fulfill({ json: { ok: true } });
          }
          if (url.pathname !== "/api/settings") return base(route);
          // the fixture's settings, with the log this app writes
          return base({ request: () => req, fulfill: (o) => route.fulfill(o.json ? { ...o, json: { ...o.json, log: logPath } } : o) });
        });
        await page.goto("http://uni-switch.test/");
        await page.locator("#prefs").click();
        await openPart(page, "about");
        const row = page.locator("#logRow");
        await row.waitFor();
        assert.equal(await row.locator(".who").first().textContent().then((s) => s.trim().startsWith(words[lang][0])), true, "the row is named " + words[lang][0]);
        assert.equal(await row.locator("code").textContent(), logPath);
        const opened = page.waitForRequest((r) => r.url().includes("/api/settings/log/reveal"));
        await row.locator("button.text", { hasText: words[lang][1] }).click();
        assert.equal((await opened).method(), "POST");
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, "About wider than the window");
        assert.deepEqual(errors, []);
      });
    }
  }
  test(`${engine}: uni-switch web writes no log, and About names none`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const page = await (await browser.newContext({ viewport: { width: 1100, height: 800 }, reducedMotion: "reduce" })).newPage();
    page.setDefaultTimeout(6000);
    page.setDefaultNavigationTimeout(60000);
    await page.route("**/*", fixture("en", "light", []));
    await page.goto("http://uni-switch.test/");
    await page.locator("#prefs").click();
    await openPart(page, "about");
    await page.locator("#about .row", { has: page.locator(".who", { hasText: "Config folder" }) }).waitFor();
    assert.equal(await page.locator("#logRow").count(), 0);
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// The reset reminder (#720, thedavidweng: 额度重置 / Banked Reset 过期前主动提醒):
// Settings › Usage has "Reset reminder", Off to begin with, then 12, 24 or
// 48 hours before; a pick saves resetReminder, another setting saved later
// keeps it, and no click moves the page. English and Chinese; the API is
// faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn, reach } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");

function server(lang, posts) {
  const fixed = {
    theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd", dock: false, dockWindow: false, proxy: "",
    redact: false, redactPersonal: false, redactWords: [], codexWarmup: "", claudeWarmup: "", codexWarmAt: "", claudeWarmAt: "",
    trayUsage: "", trayUsageEvery: 3, vision: "", imageGen: "", version: "0.1.900", dir: "~/.config/uni-switch",
    gateway: "http://127.0.0.1:3425", proxyNow: "none", proxySource: "none", visionModels: [], imageGenModels: [], lanURLs: [],
    fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
  };
  let cur = fixed;
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" }, fx: fixed.fx });
    if (url.pathname === "/api/settings") {
      if (req.method() === "POST") {
        const body = req.postDataJSON();
        posts.push(body);
        cur = { ...fixed, ...body };
      }
      return json(cur);
    }
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { name: "Reset reminder", off: "Off", h: (n) => `${n} h before`, cny: "¥ CNY",
    sub: "A notification this long before a weekly or monthly window renews with under 85% of it used, or before unused resets expire" },
  zh: { name: "重置前提醒", off: "关闭", h: (n) => `提前 ${n} 小时`, cny: "¥ 人民币",
    sub: "每周或每月窗口用量不到 85% 即将重置时，或重置卡将过期未用时，提前这么久通知" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: the reset reminder is set on the Settings page`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const page = await (await browser.newContext({ viewport: { width: 900, height: 600 }, reducedMotion: "reduce" })).newPage();
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-reset-reminder.png`) });
        }
        await browser.close();
      });
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [], posts = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(lang, posts));
      const posted = async (n) => { for (let i = 0; i < 100 && posts.length < n; i++) await page.waitForTimeout(20); assert.equal(posts.length, n, "posts"); return posts[n - 1]; };
      const w = words[lang];
      await watchSettingsReads(page);
      await page.goto("http://uni-switch.test/?view=settings&tab=usage");
      await settingsDrawn(page); // rows found are the ones that stay (#1659)
      const opts = page.locator("#resetReminderSegs .opt");
      await opts.first().waitFor();

      assert.equal((await page.locator("#resetReminderRow .name").textContent()).trim(), w.name);
      assert.equal((await page.locator("#resetReminderSub").textContent()).trim(), w.sub);
      assert.deepEqual(await opts.allTextContents(), [w.off, w.h(12), w.h(24), w.h(48)]);
      assert.equal(await page.locator("#resetReminderSegs .opt.on").textContent(), w.off);
      assert.equal(await page.locator("#resetReminderRow select").count(), 0);

      // each control wheeled wholly into sight, clear of the tabs and the
      // window's foot, before it is clicked: Playwright's own scroll to one
      // cut off moved the page, not the click (10-22, taller rows)
      const scrolled = () => page.locator("#view-settings").evaluate((v) => v.scrollTop);
      await reach(page, opts.nth(2));
      let top = await scrolled();
      await opts.nth(2).click();
      assert.equal((await posted(1)).resetReminder, 24);
      await page.waitForTimeout(300);
      assert.equal(await page.locator("#resetReminderSegs .opt.on").textContent(), w.h(24));
      assert.equal(await scrolled(), top, "the click moved the page");

      // another setting saved later keeps it
      await reach(page, page.locator("#currencySegs .opt", { hasText: w.cny }));
      top = await scrolled();
      await page.locator("#currencySegs .opt", { hasText: w.cny }).click();
      assert.equal((await posted(2)).resetReminder, 24, "kept by another save");
      await page.waitForTimeout(300);
      assert.equal(await scrolled(), top, "no click moved the page");

      await opts.nth(0).click();
      assert.equal((await posted(3)).resetReminder, 0, "Off saves 0");
      assert.equal(await page.locator("#resetReminderRow").evaluate((e) => getComputedStyle(e).borderLeftWidth), "0px");
      assert.deepEqual(errors, []);
    });
  }
}

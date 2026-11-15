// Run with Node's test runner and Playwright on the module path; see README.md.
// The task done alert (itieniu on X: 长任务跑完叮铃一声): Settings › Usage
// has "Task done alert", Off to begin with, then 1, 3 or 10 minutes; a pick
// saves turnDoneAlert in seconds, another setting saved later keeps it, and
// no click moves the page. Every language, wide and narrow; the API is
// faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn, reach } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");

function server(lang, posts, start) {
  const fixed = {
    theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd", dock: false, dockWindow: false, proxy: "",
    redact: false, redactPersonal: false, redactWords: [], codexWarmup: "", claudeWarmup: "", codexWarmAt: "", claudeWarmAt: "",
    trayUsage: "", trayUsageEvery: 3, vision: "", imageGen: "", version: "0.1.900", dir: "~/.config/uni-switch",
    gateway: "http://127.0.0.1:3425", proxyNow: "none", proxySource: "none", visionModels: [], imageGenModels: [], lanURLs: [],
    fx: { rate: 7.2, at: new Date().toISOString(), stale: false }, turnDoneAlert: start,
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
  en: { name: "Task done alert", off: "Off", m: (n) => `${n} min`, s: (n) => `${n} s`, cny: "¥ CNY",
    sub: "A notification when Claude Code, Codex, OpenCode or another agent going through uni-switch finishes a task that ran at least this long" },
  zh: { name: "任务完成提醒", off: "关闭", m: (n) => `${n} 分钟`, s: (n) => `${n} 秒`, cny: "¥ 人民币",
    sub: "Claude Code、Codex、OpenCode 等经由 uni-switch 的智能体完成一个至少跑了这么久的任务时通知" },
  "zh-TW": { name: "任務完成提醒", off: "關閉", m: (n) => `${n} 分鐘`, s: (n) => `${n} 秒`,
    sub: "Claude Code、Codex、OpenCode 等經由 uni-switch 的智慧體完成一個至少跑了這麼久的任務時通知" },
  ja: { name: "タスク完了通知", off: "オフ", m: (n) => `${n} 分`, s: (n) => `${n} 秒`,
    sub: "Claude Code、Codex、OpenCode など uni-switch を通るエージェントが、少なくともこの時間かかったタスクを終えたときに通知" },
  de: { name: "Benachrichtigung bei fertiger Aufgabe", off: "Aus", m: (n) => `${n} min`, s: (n) => `${n} s`,
    sub: "Eine Benachrichtigung, wenn Claude Code, Codex, OpenCode oder ein anderer Agent über uni-switch eine Aufgabe beendet, die mindestens so lange lief" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(words)) {
    for (const width of [900, 380]) {
      test(`${engine} ${lang} ${width}px: the task done alert is set on the Settings page`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const page = await (await browser.newContext({ viewport: { width, height: 700 }, reducedMotion: "reduce" })).newPage();
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-turn-done.png`) });
          }
          await browser.close();
        });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], posts = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, posts, 0));
        const posted = async (n) => { for (let i = 0; i < 100 && posts.length < n; i++) await page.waitForTimeout(20); assert.equal(posts.length, n, "posts"); return posts[n - 1]; };
        const w = words[lang];
        await watchSettingsReads(page);
        await page.goto("http://uni-switch.test/?view=settings&tab=usage");
        await settingsDrawn(page); // rows found are the ones that stay (#1659)
        const opts = page.locator("#turnDoneSegs .opt");
        await opts.first().waitFor();

        assert.equal((await page.locator("#turnDoneRow .name").textContent()).trim(), w.name);
        assert.equal((await page.locator("#turnDoneSub").textContent()).trim(), w.sub);
        assert.deepEqual(await opts.allTextContents(), [w.off, w.m(1), w.m(3), w.m(10)]);
        assert.equal(await page.locator("#turnDoneSegs .opt.on").textContent(), w.off);
        assert.equal(await page.locator("#turnDoneRow select").count(), 0);
        // nothing of the row runs past the page's edge
        const over = await page.evaluate(() => {
          const row = document.querySelector("#turnDoneRow").getBoundingClientRect();
          return [...document.querySelectorAll("#turnDoneRow *")].filter((e) => e.getBoundingClientRect().right > Math.max(row.right, innerWidth) + 1).map((e) => e.className || e.id);
        });
        assert.deepEqual(over, [], "nothing past the edge");

        // each control wheeled wholly into sight, clear of the tabs and the
        // window's foot, before it is clicked: Playwright's own scroll to one
        // cut off moved the page, not the click (10-22, taller rows)
        const scrolled = () => page.locator("#view-settings").evaluate((v) => v.scrollTop);
        await reach(page, opts.nth(2));
        let top = await scrolled();
        await opts.nth(2).click();
        assert.equal((await posted(1)).turnDoneAlert, 180);
        await page.waitForTimeout(300);
        assert.equal(await page.locator("#turnDoneSegs .opt.on").textContent(), w.m(3));
        assert.equal(await scrolled(), top, "the click moved the page");

        // another setting saved later keeps it
        await reach(page, page.locator("#currencySegs .opt").nth(1));
        top = await scrolled();
        await page.locator("#currencySegs .opt").nth(1).click();
        assert.equal((await posted(2)).turnDoneAlert, 180, "kept by another save");
        await page.waitForTimeout(300);
        assert.equal(await scrolled(), top, "no click moved the page");

        // back up past Currency's rows, taller now their lines wrap whole
        // (arculongus on Discord)
        await reach(page, opts.nth(0));
        await opts.nth(0).click();
        assert.equal((await posted(3)).turnDoneAlert, 0, "Off saves 0");
        assert.equal(await page.locator("#turnDoneRow").evaluate((e) => getComputedStyle(e).borderLeftWidth), "0px");
        assert.deepEqual(errors, []);
      });
    }
  }
  test(`${engine}: a length set by hand in settings.json is shown among the choices`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const page = await (await browser.newContext({ viewport: { width: 900, height: 600 }, reducedMotion: "reduce" })).newPage();
    t.after(() => browser.close());
    page.setDefaultTimeout(5000);
    page.setDefaultNavigationTimeout(60000);
    await page.route("**/*", server("en", [], 45));
    await watchSettingsReads(page);
    await page.goto("http://uni-switch.test/?view=settings&tab=usage");
    await settingsDrawn(page); // rows found are the ones that stay (#1659)
    await page.locator("#turnDoneSegs .opt").first().waitFor();
    assert.deepEqual(await page.locator("#turnDoneSegs .opt").allTextContents(), ["Off", "1 min", "3 min", "10 min", "45 s"]);
    assert.equal(await page.locator("#turnDoneSegs .opt.on").textContent(), "45 s");
  });
}

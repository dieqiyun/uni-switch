// Run with Node's test runner and Playwright on the module path; see README.md.
// #661: the site failing to give the release notes (a 503 while GitHub
// limited it) left Settings' What's new with nothing but a passing "Couldn't
// load the release notes". Now the dialog opens and says so, with Try again
// and the release page; a version whose notes are really empty says "No
// release notes provided." instead, with the release page and no retry. Try
// again shows the notes once they come; no click moves the page; the release
// page opens in the browser through the app. In English and Chinese; the API
// is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const PAGE = "https://github.com/dieqiyun/uni-switch-releases/releases/tag/v0.1.737";

function settingsPayload(lang) {
  return {
    theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd",
    dock: false, dockWindow: false, proxy: "", redact: false, redactPersonal: false, redactWords: [],
    codexWarmup: "", claudeWarmup: "", codexWarmAt: "", claudeWarmAt: "", workbuddyCheckin: false, noStats: false,
    trayUsage: "", trayUsageEvery: 3, vision: "", imageGen: "",
    version: "0.1.737", dir: "~/.config/uni-switch", gateway: "http://127.0.0.1:3425",
    proxyNow: "none", proxySource: "none", login: false,
    visionModels: [], imageGenModels: [], workbuddyCheckins: [], lanURLs: [],
    fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
  };
}

// ctl.mode: "fail" (the site failing), "none" (no notes), "ok"
function serve(lang, ctl) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/settings") return json(settingsPayload(lang));
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/update") return json({ state: "latest", current: "0.1.737" });
    if (url.pathname === "/api/whatsnew") {
      const all = url.searchParams.has("all");
      if (!all) return json({ show: false, current: "0.1.737", releases: [] });
      ctl.asked++;
      const base = { show: false, current: "0.1.737", url: PAGE };
      if (ctl.mode === "fail") return json({ ...base, releases: [], error: "release notes: 503 Service Unavailable" });
      if (ctl.mode === "none") return json({ ...base, releases: [] });
      return json({ ...base, releases: [{ version: "0.1.737", notes: "## Bug Fixes\n\n- Notes came back. (#661)" }] });
    }
    if (url.pathname === "/api/open") { ctl.opened.push(req.postDataJSON().url); return route.fulfill({ status: 204 }); }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { open: "Open", failed: "Couldn't load the release notes", none: "No release notes provided.", retry: "Try again", page: "Open the release page", title: "What's new in v0.1.737" },
  zh: { open: "打开", failed: "无法获取更新说明", none: "这个版本没有更新说明。", retry: "重试", page: "打开发布页面", title: "v0.1.737 更新内容" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    const w = words[lang];
    test(`${engine} ${lang}: release notes that couldn't be had, apart from none (#661)`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const context = await browser.newContext({ viewport: { width: 900, height: 700 }, reducedMotion: "reduce" });
      t.after(async () => browser.close());
      const ctl = { mode: "fail", asked: 0, opened: [] };
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      page.on("dialog", (d) => { errors.push("dialog: " + d.message()); d.dismiss(); });
      await page.route("**/*", serve(lang, ctl));
      await watchSettingsReads(page);
      await page.goto("http://uni-switch.test/?view=settings&tab=about");
      await settingsDrawn(page); // rows found are the ones that stay (#1659)
      const dialog = page.locator("#modal .editor.whatsnew");
      const view = page.locator("#view-settings");
      const again = page.locator("#about .row.pref.whatsnew-row").getByRole("button", { name: w.open, exact: true });
      await again.waitFor();
      await page.mouse.move(450, 400);
      for (let i = 0; i < 60; i++) {
        const b = await again.boundingBox();
        if (b && b.y > 80 && b.y + b.height < 640) break;
        await page.mouse.wheel(0, !b || b.y > 80 ? 120 : -120);
        await page.waitForTimeout(30);
      }
      await page.waitForTimeout(200);
      const at = await view.evaluate((v) => v.scrollTop);
      const still = async (what) => assert.equal(await view.evaluate((v) => v.scrollTop), at, what + " moved the page");

      // the site failing: said so, with Try again and the release page
      await again.click();
      await dialog.waitFor();
      await still("Open");
      assert.equal(await dialog.locator(".ehead b").textContent(), w.title);
      assert.equal(await dialog.locator(".wn-msg").textContent(), w.failed);
      assert.equal(await dialog.getByText(w.none).count(), 0);
      const retry = dialog.getByRole("button", { name: w.retry });
      const link = dialog.getByRole("link", { name: w.page });
      assert.equal(await link.getAttribute("href"), PAGE);
      if (process.env.ARTIFACT_DIR) {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        await page.waitForTimeout(400);
        await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-notes-failed.png`) });
      }
      const border = await page.evaluate(() => [...document.querySelectorAll("#modal .whatsnew, #modal .whatsnew *")].map((e) => getComputedStyle(e).borderLeftWidth).filter((b) => parseFloat(b) > 1));
      assert.deepEqual(border, [], "no left-border accent");
      // the release page opens in the browser, through the app
      await link.click();
      for (let i = 0; i < 40 && !ctl.opened.length; i++) await page.waitForTimeout(25);
      assert.deepEqual(ctl.opened, [PAGE]);
      assert.equal(page.url(), "http://uni-switch.test/?view=settings&tab=about", "the page didn't navigate");
      // Try again while the site still fails: the same, asked again
      await retry.click();
      for (let i = 0; i < 40 && ctl.asked < 2; i++) await page.waitForTimeout(25);
      await dialog.getByRole("button", { name: w.retry }).waitFor();
      assert.equal(ctl.asked, 2);
      // Try again once the site answers: the notes
      ctl.mode = "ok";
      await dialog.getByRole("button", { name: w.retry }).click();
      await dialog.locator(".wn-notes li").waitFor();
      assert.equal(await dialog.locator(".wn-msg").count(), 0);
      assert.equal(await dialog.locator(".wn-notes li").textContent(), "Notes came back. (#661)");
      await still("Try again");
      await page.keyboard.press("Escape");
      await page.locator("#modal").waitFor({ state: "hidden" });

      // a version with no notes: said so, the release page, no retry
      ctl.mode = "none";
      await again.click();
      await dialog.waitFor();
      await still("Open");
      assert.equal(await dialog.locator(".wn-msg").textContent(), w.none);
      assert.equal(await dialog.getByRole("button", { name: w.retry }).count(), 0);
      assert.equal(await dialog.getByRole("link", { name: w.page }).getAttribute("href"), PAGE);
      await page.keyboard.press("Escape");
      await page.locator("#modal").waitFor({ state: "hidden" });
      assert.deepEqual(errors, []);
    });
  }
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// Settings › About › What's new picks which notes it shows (Linka on
// Discord): since the last update, as before and by default, or this
// version and the 10, 20, 50 or 100 before it. The pick is the app's own
// menu (a pill in the dialog's head), never a native <select>, and only in
// the dialog Settings opens, not the one shown by itself after an update.
// The page asks the app for the range picked (?last=); a pick that fails
// keeps the notes it showed, says it failed above them, and Try again asks
// for the same range. No click moves the page, nothing leaves the dialog
// sideways at a narrow window, no left-border accent. In all five
// languages, Chromium and WebKit; the API is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const CURRENT = "0.1.1410";
// uni-switch-releases' shape: newest first, 0.1.1407 never released
const ALL = [];
for (let v = 1410; v >= 1290; v--) if (v !== 1407) ALL.push({ version: "0.1." + v, notes: `## Bug Fixes\n\n- fix ${v}` });
const back = (n) => ALL.slice(0, n + 1);

function settingsPayload(lang) {
  return {
    theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd",
    dock: false, dockWindow: false, proxy: "", redact: false, redactPersonal: false, redactWords: [],
    codexWarmup: "", claudeWarmup: "", codexWarmAt: "", claudeWarmAt: "", workbuddyCheckin: false, noStats: false,
    trayUsage: "", trayUsageEvery: 3, vision: "", imageGen: "",
    version: CURRENT, dir: "~/.config/uni-switch", gateway: "http://127.0.0.1:3425",
    proxyNow: "none", proxySource: "none", login: false,
    visionModels: [], imageGenModels: [], workbuddyCheckins: [], lanURLs: [],
    fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
  };
}

// ctl.down: the site failing for a range; ctl.asked: each Settings ask's
// last ("" for the default)
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
    if (url.pathname === "/api/update") return json({ state: "latest", current: CURRENT });
    if (url.pathname === "/api/whatsnew") {
      if (!url.searchParams.has("all")) return json({ show: false, current: CURRENT, releases: [] });
      const last = url.searchParams.get("last") || "";
      ctl.asked.push(last);
      const base = { show: false, current: CURRENT, url: "https://github.com/dieqiyun/uni-switch-releases/releases/tag/v" + CURRENT };
      if (last && ctl.down) return json({ ...base, releases: [], error: "release notes: 503 Service Unavailable" });
      return json({ ...base, releases: last ? back(+last) : back(1) });
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { span: (n, a, b) => `${n} releases · ${a} – ${b}`, open: "Open", since: "Since the last update", back: (n) => `This version and ${n} before`, head: "Release notes shown", failed: "Couldn't load the release notes", retry: "Try again" },
  zh: { span: (n, a, b) => `${n} 个版本 · ${a} – ${b}`, open: "打开", since: "上次更新以来", back: (n) => `当前版本及之前 ${n} 个版本`, head: "显示的更新说明", failed: "无法获取更新说明", retry: "重试" },
  "zh-TW": { span: (n, a, b) => `${n} 個版本 · ${a} – ${b}`, open: "開啟", since: "上次更新以來", back: (n) => `目前版本及之前 ${n} 個版本`, head: "顯示的更新說明", failed: "無法取得更新說明", retry: "重試" },
  ja: { span: (n, a, b) => `${n} リリース · ${a} – ${b}`, open: "開く", since: "前回のアップデート以降", back: (n) => `このバージョンと前の ${n} 件`, head: "表示するリリースノート", failed: "リリースノートを読み込めませんでした", retry: "再試行" },
  de: { span: (n, a, b) => `${n} Versionen · ${a} – ${b}`, open: "Öffnen", since: "Seit dem letzten Update", back: (n) => `Diese Version und ${n} davor`, head: "Angezeigte Versionshinweise", failed: "Versionshinweise konnten nicht geladen werden", retry: "Erneut versuchen" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(words)) {
    const w = words[lang];
    test(`${engine} ${lang}: What's new picks since the last update or this version and 10 to 100 before (Linka)`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(async () => browser.close());
      const context = await browser.newContext({ viewport: { width: 900, height: 700 }, reducedMotion: "reduce" });
      const ctl = { asked: [], down: false };
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      page.on("dialog", (d) => { errors.push("dialog: " + d.message()); d.dismiss(); });
      await page.route("**/*", serve(lang, ctl));
      await watchSettingsReads(page);
      await page.goto("http://uni-switch.test/?view=settings&tab=about");
      await settingsDrawn(page);
      const dialog = page.locator("#modal .editor.whatsnew");
      const view = page.locator("#view-settings");
      const open = page.locator("#about .row.pref.whatsnew-row").getByRole("button", { name: w.open, exact: true });
      await open.waitFor();
      await page.mouse.move(450, 400);
      for (let i = 0; i < 60; i++) {
        const b = await open.boundingBox();
        if (b && b.y > 80 && b.y + b.height < 640) break;
        await page.mouse.wheel(0, !b || b.y > 80 ? 120 : -120);
        await page.waitForTimeout(30);
      }
      await page.waitForTimeout(200);
      const at = await view.evaluate((v) => v.scrollTop);
      const still = async (what) => assert.equal(await view.evaluate((v) => v.scrollTop), at, what + " moved the page");
      // the releases are merged (the owner): each line says its version
      const shown = () => dialog.locator(".wn-merged li .wn-v").allTextContents();
      const vs = (list) => list.map((r) => "v" + r.version);
      const pill = dialog.locator(".ehead .wn-range");
      const pick = async (name) => {
        await pill.click();
        const menu = page.locator(".pop.proto-menu");
        await menu.waitFor();
        assert.equal(await menu.locator(".pm-head").textContent(), w.head);
        assert.deepEqual(await menu.locator(".pm-name").allTextContents(), [w.since, ...[10, 20, 50, 100].map(w.back)]);
        await menu.locator(".pm-item", { hasText: name }).click();
      };

      // the default, as before: since the last update, nothing else asked
      await open.click();
      await dialog.waitFor();
      await still("Open");
      assert.equal(await pill.textContent(), w.since);
      assert.equal(await pill.getAttribute("aria-haspopup"), "menu");
      assert.deepEqual(await shown(), vs(back(1)));
      assert.deepEqual(ctl.asked, [""]);
      assert.equal(await page.locator("select").count(), 0, "no native select");

      // this version and the 10 before it: eleven, newest first
      await pick(w.back(10));
      await dialog.locator(".wn-merged li").nth(10).waitFor();
      assert.equal(await dialog.locator(".wn-span").textContent(), w.span(11, "v0.1.1399", "v0.1.1410"));
      assert.deepEqual(ctl.asked, ["", "10"]);
      assert.deepEqual(await shown(), vs(back(10)));
      assert.equal(await pill.textContent(), w.back(10));
      await still("a pick");
      // picking what is shown asks nothing
      await pick(w.back(10));
      await page.waitForTimeout(150);
      assert.deepEqual(ctl.asked, ["", "10"]);

      // 100: past the 40 the default stops at
      await pick(w.back(100));
      await dialog.locator(".wn-merged li").nth(100).waitFor();
      assert.deepEqual(await shown(), vs(back(100)));

      // a range that fails keeps the notes shown, says so above them, and
      // Try again asks for the same range
      ctl.down = true;
      await pick(w.back(50));
      await dialog.locator(".wn-failed").waitFor();
      assert.equal(await pill.textContent(), w.back(50));
      assert.equal(await dialog.locator(".wn-failed .wn-msg").textContent(), w.failed);
      assert.deepEqual(await shown(), vs(back(100)), "the notes shown before are kept");
      const firstRel = await dialog.locator(".wn-rel").first().getAttribute("class");
      assert.match(firstRel, /wn-failed/, "the failure is said above the kept notes");
      ctl.down = false;
      await dialog.getByRole("button", { name: w.retry }).click();
      await page.waitForFunction(() => !document.querySelector("#modal .wn-failed") && document.querySelectorAll("#modal .wn-merged li").length === 51);
      assert.deepEqual(ctl.asked.slice(-2), ["50", "50"]);
      assert.deepEqual(await shown(), vs(back(50)));
      await still("Try again");

      const border = await page.evaluate(() => [...document.querySelectorAll("#modal .whatsnew, #modal .whatsnew *")].map((e) => getComputedStyle(e).borderLeftWidth).filter((b) => parseFloat(b) > 1));
      assert.deepEqual(border, [], "no left-border accent");
      if (process.env.ARTIFACT_DIR) {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-whatsnew-range.png`) });
      }
      await page.keyboard.press("Escape");
      await page.locator("#modal").waitFor({ state: "hidden" });

      // opened again: the default, as before
      await open.click();
      await dialog.waitFor();
      assert.equal(await pill.textContent(), w.since);
      assert.equal(ctl.asked.at(-1), "");

      // a narrow window: the head's pill stays inside the dialog
      await page.setViewportSize({ width: 440, height: 700 });
      await page.waitForTimeout(200);
      const fit = await page.evaluate(() => {
        const ed = document.querySelector("#modal .whatsnew"), p = ed.querySelector(".wn-range");
        const e = ed.getBoundingClientRect(), b = p.getBoundingClientRect();
        return { inside: b.left >= e.left - 0.5 && b.right <= e.right + 0.5, sideways: ed.scrollWidth > ed.clientWidth + 1 };
      });
      assert.deepEqual(fit, { inside: true, sideways: false });
      await page.keyboard.press("Escape");
      assert.deepEqual(errors, []);
    });
  }
}

// The dialog shown by itself after an update has no pick: it lists what came
// since the last update, as it always did.
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: the dialog after an update has no range pick`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(async () => browser.close());
    const page = await (await browser.newContext({ viewport: { width: 900, height: 700 }, reducedMotion: "reduce" })).newPage();
    page.setDefaultTimeout(5000);
    page.setDefaultNavigationTimeout(60000);
    const ctl = { asked: [] };
    const base = serve("en", ctl);
    await page.route("**/*", (route) => {
      const url = new URL(route.request().url());
      if (url.pathname === "/api/whatsnew" && !url.searchParams.has("all")) return route.fulfill({ json: { show: true, current: CURRENT, releases: back(1) } });
      if (url.pathname === "/api/whatsnew/seen") return route.fulfill({ status: 204 });
      return base(route);
    });
    await page.goto("http://uni-switch.test/");
    const dialog = page.locator("#modal .editor.whatsnew");
    await dialog.waitFor();
    assert.equal(await dialog.locator(".wn-range").count(), 0);
    assert.deepEqual(ctl.asked, []);
  });
}

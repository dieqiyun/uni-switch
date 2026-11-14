// Run with Node's test runner and Playwright on the module path; see README.md.
// PapainTea on X: where GitHub is slow to uni-switch, the update took ages to
// download in the app. While an update is out, downloading, or failed to
// download, Settings › About › Version offers the release page, opened in
// the browser, where it can be fetched through the user's own proxy or
// accelerator. The button outlives the download's redraws (every 700ms), so
// a click on it isn't lost, and it goes once there is nothing to fetch.
// Every language, both engines, narrow and wide; no backend, the API is
// faked. No click moves the page, nothing is a native select.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const URL_ = "https://github.com/dieqiyun/uni-switch-releases/releases/tag/v0.1.901";

const words = {
  en: { page: "Open the release page", download: "Download", check: "Check" },
  zh: { page: "打开发布页面", download: "下载", check: "检查" },
  "zh-TW": { page: "打開發布頁面", download: "下載", check: "檢查" },
  ja: { page: "リリースページを開く", download: "ダウンロード", check: "確認" },
  de: { page: "Release-Seite öffnen", download: "Herunterladen", check: "Prüfen" },
};

function server(lang, ctl) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data, status) => route.fulfill({ json: data, status: status || 200 });
    const settings = () => ({ theme: "light", lang, version: "0.1.900", dir: "~/.config/uni-switch", gateway: "http://127.0.0.1:3425", lanURLs: [], fx: { rate: 7.2 } });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: settings() });
    if (url.pathname === "/api/open") { ctl.posts.push(["open", req.postDataJSON()]); return json({}); }
    if (url.pathname === "/api/update/install") { ctl.posts.push("install"); return json(ctl.update); }
    if (url.pathname === "/api/update/check") { ctl.posts.push("check"); return json(ctl.update); }
    if (url.pathname === "/api/update") {
      // a download goes on: each read is a little further
      if (ctl.update.state === "downloading") ctl.update = { ...ctl.update, done: Math.min(ctl.update.total, ctl.update.done + 1e6) };
      return json(ctl.update);
    }
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const scrolls = (page) => page.evaluate(() => [window.scrollY, document.scrollingElement.scrollTop, ...[...document.querySelectorAll(".view")].map((v) => v.scrollTop)].join(","));

async function open(browser, lang, ctl, errors, width) {
  const page = await (await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" })).newPage();
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  // webkit's note that a resize settled a frame late is not the page's error
  page.on("pageerror", (e) => { if (!/^ResizeObserver loop completed with undelivered notifications/.test(e.message)) errors.push(e.message); });
  await page.route("**/*", server(lang, ctl));
  await page.goto("http://uni-switch.test/?view=settings");
  await page.locator("#setTab-about").click();
  await page.locator("#setPage-about").waitFor({ state: "visible" });
  return page;
}

async function click(page, loc) {
  const before = await scrolls(page);
  await loc.click();
  await page.waitForTimeout(150);
  assert.equal(await scrolls(page), before, "the click must not scroll the page");
}

// the button sits inside its row, wholly in sight, not over another
async function inRow(ver, btn) {
  const ok = await btn.evaluate((b, row) => {
    const r = row.getBoundingClientRect(), x = b.getBoundingClientRect();
    const others = [...row.querySelectorAll("button")].filter((o) => o !== b).map((o) => o.getBoundingClientRect());
    const apart = others.every((o) => o.right <= x.left + 0.5 || o.left >= x.right - 0.5 || o.bottom <= x.top + 0.5 || o.top >= x.bottom - 0.5);
    return x.width > 0 && x.left >= r.left - 1 && x.right <= r.right + 1 && x.right <= innerWidth && apart;
  }, await ver.elementHandle());
  assert.ok(ok, "the release page's button stays in its row, clear of the others");
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": the update can be fetched from its release page", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of Object.keys(words)) for (const width of [560, 900]) {
      const w = words[lang];
      await t.test(`${lang} at ${width}px`, async () => {
        const ctl = { posts: [], update: { state: "available", current: "0.1.900", latest: "0.1.901", url: URL_ } };
        const errors = [];
        let page = await open(browser, lang, ctl, errors, width);
        let ver = page.locator("#about .row.pref", { has: page.locator(".sub", { hasText: "0.1.901" }) });
        await ver.waitFor();
        assert.equal(await page.locator("#about select").count(), 0, "no native select");

        // out: Download, and the page beside it
        let btn = ver.locator("button.update-page");
        assert.equal(await btn.innerText(), w.page);
        assert.equal(await ver.locator("button", { hasText: w.download }).count(), 1);
        await inRow(ver, btn);
        await click(page, btn);
        assert.deepEqual(ctl.posts.at(-1), ["open", { url: URL_ }]);

        await page.context().close();

        // downloading: the same button, kept through the redraws
        ctl.update = { state: "downloading", current: "0.1.900", latest: "0.1.901", url: URL_, done: 1e6, total: 3e7 };
        page = await open(browser, lang, ctl, errors, width);
        ver = page.locator("#about .row.pref", { has: page.locator(".sub", { hasText: "0.1.901" }) });
        btn = ver.locator("button.update-page");
        await page.locator("#about .sub", { hasText: "0.1.901" }).filter({ hasText: "%" }).waitFor();
        await btn.waitFor();
        await btn.evaluate((b) => { b.dataset.mark = "1"; });
        const pct = await ver.locator(".sub").innerText();
        await page.waitForFunction((p) => [...document.querySelectorAll("#about .sub")].some((s) => s.innerText.includes("%") && s.innerText !== p), pct);
        assert.equal(await ver.locator("button.update-page[data-mark='1']").count(), 1, "a redraw keeps the button, so a click on it isn't lost");
        assert.equal(await ver.locator("button.update-page").count(), 1);
        assert.equal(await btn.isEnabled(), true);
        await inRow(ver, btn);
        ctl.posts.length = 0;
        await click(page, btn);
        assert.deepEqual(ctl.posts.filter((p) => p[0] === "open"), [["open", { url: URL_ }]]);

        // failed: Check, and the page
        ctl.update = { state: "error", current: "0.1.900", latest: "0.1.901", url: URL_, error: "Get \"https://github.com/x\": net/http: TLS handshake timeout" };
        await ver.locator("button", { hasText: w.check }).waitFor();
        assert.equal(await ver.locator("button.update-page").count(), 1);
        await inRow(ver, btn);

        // up to date: nothing to fetch, so no page
        ctl.update = { state: "latest", current: "0.1.901" };
        await click(page, ver.locator("button", { hasText: w.check }));
        await page.locator("#about button.update-page").waitFor({ state: "detached" });
        assert.deepEqual(errors, []);
        await page.context().close();
      });
    }
  });
}

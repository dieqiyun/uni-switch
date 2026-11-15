// Run with Node's test runner and Playwright on the module path; see README.md.
// #894: on Windows with Smart App Control on, the new unsigned version was
// refused when uni-switch restarted into it. uni-switch now puts itself back and
// runs on, and Settings › About › Version says the version didn't start
// here and why, offering Download and Check (which tries it once more).
// No click moves the page, nothing is a native select, no left border.
// English and Chinese; no backend, the API is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { openPart } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");

const words = {
  en: { policy: "Smart App Control", stays: "uni-switch stays on 0.1.900", download: "Download", check: "Check", other: "didn't start on this computer" },
  zh: { policy: "智能应用控制", stays: "uni-switch 仍是 0.1.900", download: "下载", check: "检查", other: "没能在这台电脑上启动" },
};

const blocked = (policy) => ({ state: "blocked", current: "0.1.900", latest: "0.1.901", url: "https://github.com/dieqiyun/uni-switch-releases/releases/tag/v0.1.901",
  blocked: { version: "0.1.901", policy, error: policy ? "fork/exec uni-switch.exe: An Application Control policy has blocked this file." : "it quit as soon as it started" } });

function server(lang, ctl) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data, status) => route.fulfill({ json: data, status: status || 200 });
    const settings = () => ({ theme: "light", lang, version: "0.1.900", dir: "~/.config/uni-switch", gateway: "http://127.0.0.1:3425", lanURLs: [], fx: { rate: 7.2 } });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: settings() });
    if (url.pathname === "/api/update/check") {
      ctl.posts.push("check");
      ctl.update = { state: "downloading", current: "0.1.900", latest: "0.1.901", done: 1e6, total: 3e7 };
      return json(ctl.update);
    }
    if (url.pathname === "/api/open") { ctl.posts.push(["open", req.postDataJSON()]); return json({}); }
    if (url.pathname === "/api/update") return json(ctl.update);
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

async function open(browser, lang, ctl, errors) {
  const page = await (await browser.newContext({ viewport: { width: 900, height: 760 }, reducedMotion: "reduce" })).newPage();
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  page.on("pageerror", (e) => { if (!/^ResizeObserver loop completed with undelivered notifications/.test(e.message)) errors.push(e.message); });
  await page.route("**/*", server(lang, ctl));
  await page.goto("http://uni-switch.test/?view=settings");
  await openPart(page, "about");
  await page.locator("#setPage-about").waitFor({ state: "visible" });
  return page;
}

async function click(page, loc) {
  const before = await scrolls(page);
  await loc.click();
  await page.waitForTimeout(150);
  assert.equal(await scrolls(page), before, "the click must not scroll the page");
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a version this computer didn't start says why", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh"]) {
      const w = words[lang];
      await t.test(lang + ": Smart App Control", async () => {
        const ctl = { posts: [], update: blocked(true) };
        const errors = [];
        const page = await open(browser, lang, ctl, errors);
        const ver = page.locator("#about .row.pref", { has: page.locator(".sub", { hasText: w.policy }) });
        await ver.waitFor();
        const sub = await ver.locator(".sub").innerText();
        assert.ok(sub.includes("0.1.901") && sub.includes(w.stays), sub);
        assert.equal(await ver.locator(".sub").getAttribute("title"), ctl.update.blocked.error, "the error in full on hover");
        assert.equal(await page.locator("#about select").count(), 0, "no native select");
        const widths = await ver.evaluate((e) => [e, ...e.querySelectorAll("*")].map((x) => parseFloat(getComputedStyle(x).borderLeftWidth) || 0));
        assert.ok(widths.every((x) => x === 0), "no left border");
        // the text wraps inside the row, not out of it
        const fits = await ver.evaluate((e) => { const r = e.getBoundingClientRect(), s = e.querySelector(".sub").getBoundingClientRect(); return s.right <= r.right + 1 && s.left >= r.left - 1; });
        assert.ok(fits, "the reason stays in the row");

        await click(page, ver.locator("button", { hasText: w.download }));
        assert.deepEqual(ctl.posts.at(-1), ["open", { url: ctl.update.url }]);
        // Check tries it again: downloading
        await click(page, ver.locator("button", { hasText: w.check }));
        await page.locator("#about .sub", { hasText: "0.1.901" }).filter({ hasText: "%" }).waitFor();
        assert.ok(ctl.posts.includes("check"));
        assert.deepEqual(errors, []);
        await page.context().close();
      });

      await t.test(lang + ": another reason", async () => {
        const ctl = { posts: [], update: blocked(false) };
        const errors = [];
        const page = await open(browser, lang, ctl, errors);
        const ver = page.locator("#about .row.pref", { has: page.locator(".sub", { hasText: w.other }) });
        await ver.waitFor();
        const sub = await ver.locator(".sub").innerText();
        assert.ok(!sub.includes(w.policy), sub);
        assert.ok(sub.includes("it quit as soon as it started") && sub.includes(w.stays), sub);
        assert.equal(await ver.locator("button", { hasText: w.check }).count(), 1);
        assert.deepEqual(errors, []);
        await page.context().close();
      });
    }
  });
}

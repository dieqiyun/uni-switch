// Run with Node's test runner and Playwright on the module path; see README.md.
// Settings' search (sgfvdsg86265 on X: "配置界面太多了，找不到了"): a box
// at the end of the Settings tabs. Words typed in it show every part's rows
// that say them — in the language shown or in English — each part under its
// name, the tabs set aside; a part's name or a section's heading brings all
// of it; the warm-ups' cards all show, Codex's and Claude Code's alike;
// nothing found says so. Escape, or clearing the box, brings back the part
// picked as it was. Nothing is posted and the page doesn't scroll. In the
// smallest window (560 x 420) and uni-switch web on a phone (390 wide), nothing
// runs off to the side. Every language; no backend, the API is faked here.
// ARTIFACT_DIR gets a screenshot of each.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { openPart } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");

function server(lang, posts, web) {
  const cur = {
    theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd", textSize: 100,
    dock: false, dockWindow: false, proxy: "", redact: false, redactPersonal: false, redactWords: [],
    codexWarmup: "", claudeWarmup: "", codexWarmAt: "", claudeWarmAt: "", workbuddyCheckin: false, noStats: false,
    trayUsage: "", trayUsageEvery: 3, vision: "", imageGen: "",
    version: "0.1.400", dir: "~/.config/uni-switch", gateway: "http://127.0.0.1:3425",
    proxyNow: "none", proxySource: "none", login: false,
    visionModels: [], imageGenModels: [], workbuddyCheckins: [], lanURLs: [],
    fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
  };
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (req.method() !== "GET") posts.push(url.pathname);
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:${web},textSize:100};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light", textSize: 100 }, fx: cur.fx });
    if (url.pathname === "/api/settings") return json(cur);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/window/fit") return route.fulfill({ status: 204 });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// the parts shown, the tabs' look, what the box says, and what is set aside
const state = (page) => page.evaluate(() => ({
  parts: [...document.querySelectorAll("#view-settings > [role=tabpanel]")].filter((p) => p.getClientRects().length).map((p) => p.id.replace("setPage-", "")),
  tabs: !!document.querySelector("#setTabs").getClientRects().length,
  none: document.querySelector("#setNone").getClientRects().length ? document.querySelector("#setNone").textContent : "",
  misses: document.querySelectorAll("#view-settings .set-miss").length,
}));
const seen = (page, sel) => page.locator(sel).first().evaluate((e) => !!e.getClientRects().length && getComputedStyle(e).visibility !== "hidden");
const overflow = (page) => page.evaluate(() => {
  const out = [];
  const de = document.documentElement, v = document.querySelector("#view-settings");
  if (de.scrollWidth > de.clientWidth) out.push("page " + de.scrollWidth + ">" + de.clientWidth);
  if (v.scrollWidth > v.clientWidth) out.push("view " + v.scrollWidth + ">" + v.clientWidth);
  const r = v.getBoundingClientRect(), q = document.querySelector("#setQ").getBoundingClientRect();
  if (q.left < r.left - 0.5 || q.right > r.right + 0.5) out.push("box " + q.left + "-" + q.right);
  return out;
});

const LANGS = ["en", "zh", "zh-TW", "ja", "de"];
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": Settings' search", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const shots = [];
    t.after(async () => {
      if (process.env.ARTIFACT_DIR) {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        for (const [name, buf] of shots) await fs.writeFile(path.join(process.env.ARTIFACT_DIR, `${engine}-settings-search-${name}.png`), buf);
      }
      await browser.close();
    });
    const open = async (lang, posts, { width = 560, height = 420, web = false } = {}) => {
      const context = await browser.newContext({ viewport: { width, height }, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      page.errors = [];
      page.on("pageerror", (e) => page.errors.push(e.message));
      await page.route("**/*", server(lang, posts, web));
      await page.goto("http://uni-switch.test/?view=settings&tab=general");
      await page.locator("#themeSegs .opt").first().waitFor();
      return page;
    };
    const type = async (page, s) => {
      await page.locator("#setQ").fill(s);
      await page.waitForTimeout(120);
    };

    for (const lang of LANGS) {
      await t.test(lang, async () => {
        const posts = [];
        const page = await open(lang, posts);
        const tr = (s, vars) => page.evaluate(([s, vars]) => t(s, vars), [s, vars]);
        const q = page.locator("#setQ");
        assert.equal(await q.getAttribute("placeholder"), await tr("Find a setting"));
        assert.equal(await q.getAttribute("aria-label"), await tr("Find a setting"));
        assert(await seen(page, "#setQ"), "the box shows beside the tabs");
        assert.deepEqual(await overflow(page), []);
        const before = await state(page);
        assert.deepEqual(before, { parts: ["general"], tabs: true, none: "", misses: 0 });

        // a row's own words, in the language shown: that row alone, under
        // its part's name, the tabs set aside
        await openPart(page, "usage");
        await type(page, await tr("Appearance"));
        let s = await state(page);
        assert.deepEqual([s.parts, s.tabs, s.none], [["general"], false, ""]);
        assert(await seen(page, "#themeSegs"), "Appearance's row shows");
        assert.equal(await seen(page, "#langPick"), false, "Language's row doesn't");
        assert.equal(await page.locator("#setPage-general").getAttribute("data-part"), await tr("General"));
        assert.equal(await page.locator("#setPage-general").evaluate((p) => getComputedStyle(p, "::before").content), JSON.stringify(await tr("General")));
        assert.deepEqual(await overflow(page), []);
        shots.push([lang + "-appearance", await page.screenshot()]);

        // the English words find it in every language
        await type(page, "currency");
        s = await state(page);
        assert.deepEqual(s.parts, ["usage"]);
        assert(await seen(page, "#currencySegs"), "Currency's row shows");
        assert.equal(await seen(page, "#trayUsagePick"), false);
        assert.equal(await seen(page, "#warmTabs"), false, "the warm-ups' tabs set aside");

        // the warm-ups: every agent's card that says it, not just the one picked
        await type(page, (await tr("Warm up on reset")).toLowerCase());
        assert(await seen(page, "#warmSegs"), "Codex's warm-up shows");
        assert(await seen(page, "#claudeWarmSegs"), "Claude Code's warm-up shows");
        assert.equal(await page.locator("#claudeWarmList").evaluate((p) => getComputedStyle(p, "::before").content), JSON.stringify("Claude Code"));

        // a part's name brings the whole part
        await type(page, await tr("Privacy"));
        s = await state(page);
        assert(s.parts.includes("privacy"), "Privacy's part: " + s.parts);
        assert(await seen(page, "#redactList .row"), "its rows show");

        // nothing found says so
        await type(page, "zzqqxx");
        s = await state(page);
        assert.deepEqual([s.parts, s.none], [[], await tr("No setting matches “{q}”", { q: "zzqqxx" })]);
        assert.deepEqual(await overflow(page), []);

        // Escape brings back the part picked, as it was
        await q.focus();
        await page.keyboard.press("Escape");
        await page.waitForTimeout(100);
        assert.equal(await q.inputValue(), "");
        assert.deepEqual(await state(page), { parts: ["usage"], tabs: true, none: "", misses: 0 });
        assert.equal(await page.locator(":is(#setTab-usage, .nav-s[data-of=setTab-usage]):visible").getAttribute("aria-selected"), "true");
        assert(await seen(page, "#warmTabs"), "the warm-ups' tabs back");
        assert(await seen(page, "#trayUsagePick"), "every row back");
        // so does clearing it by hand
        await type(page, "currency");
        await type(page, "");
        assert.deepEqual(await state(page), { parts: ["usage"], tabs: true, none: "", misses: 0 });

        assert.equal(await page.locator("#view-settings").evaluate((v) => v.scrollTop), 0, "searching scrolls nothing");
        assert.deepEqual(posts, [], "searching posts nothing");
        assert.deepEqual(page.errors, []);
        await page.context().close();
      });
    }

    await t.test("uni-switch web on a phone", async () => {
      const page = await open("zh", [], { width: 390, height: 760, web: true });
      assert.deepEqual(await overflow(page), []);
      await page.evaluate(() => { const q = document.querySelector("#setQ"); q.value = "github"; q.dispatchEvent(new Event("input")); });
      await page.waitForTimeout(120);
      assert.deepEqual(await overflow(page), []);
      shots.push(["web-phone", await page.screenshot()]);
      assert.deepEqual(page.errors, []);
      await page.context().close();
    });
  });
}

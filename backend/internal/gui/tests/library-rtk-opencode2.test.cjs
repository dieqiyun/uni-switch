// Run with Node's test runner and Playwright on the module path; see README.md.
// #2295 (witt-bit): rtk 0.52 on (rtk-ai/rtk#4187; the reporter's 1.0.1)
// writes an OpenCode plugin OpenCode 2 loads, so the RTK tab no longer says
// "Not for OpenCode 2". With an older rtk the OpenCode 2 row says to update
// RTK, can't be switched on, and an old plugin already there can be switched
// off. Every language, Chromium and WebKit, 1100 and 420 wide, nothing
// sideways. No backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/aimer";
const agent = (id, name) => ({ id, name, icon: "", skills: `${HOME}/.${id}/skills`, mcp: `${HOME}/.${id}/mcp.json` });
const lib = () => ({
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: [agent("opencode", "OpenCode")],
  instructions: { agents: [], sets: [] }, foundServers: [], projects: [], foundSkills: [], skills: [], servers: [],
});
// what ReadRTK says with rtk 0.51.0 and OpenCode 2.0.25 (read with the
// real binaries); on: rtk 0.51's plugin is there from before
const rtkView = (on) => ({
  path: `${HOME}/.local/bin/rtk`, version: "0.51.0", url: "https://www.rtk-ai.app",
  agents: [
    { id: "opencode", name: "OpenCode", icon: "", on, blocked: "OpenCode 2.0.25 needs RTK 0.52 or newer, and this RTK is 0.51.0, whose plugin is written for OpenCode 1: OpenCode 2 refuses to load it (github.com/rtk-ai/rtk/issues/4311). Update RTK (brew upgrade rtk, or its installer again), then switch it on" },
  ],
});

function server(lang, calls, on) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/library") return route.fulfill({ json: lib() });
    if (url.pathname === "/api/library/rtk") {
      if (req.method() !== "GET") calls.push(req.postData());
      return route.fulfill({ json: rtkView(on) });
    }
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const LANGS = ["en", "zh", "zh-TW", "ja", "de"];
const words = {
  en: { tag: "Update RTK", off: "OpenCode 2 needs RTK 0.52 or newer", on: "OpenCode 2 refuses to load this plugin: RTK before 0.52" },
  zh: { tag: "需更新 RTK", off: "OpenCode 2 需要 RTK 0.52 或更高版本", on: "OpenCode 2 拒绝加载这个插件：RTK 0.52 之前" },
  "zh-TW": { tag: "需更新 RTK", off: "OpenCode 2 需要 RTK 0.52 或更高版本", on: "OpenCode 2 拒絕載入這個外掛：RTK 0.52 之前" },
  ja: { tag: "RTK の更新が必要", off: "OpenCode 2 には RTK 0.52 以降が必要です", on: "OpenCode 2 はこのプラグインの読み込みを拒否します" },
  de: { tag: "RTK aktualisieren", off: "OpenCode 2 benötigt RTK 0.52 oder neuer", on: "OpenCode 2 lädt diese Erweiterung nicht: RTK vor 0.52" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": OpenCode 2 with an RTK before 0.52 says to update RTK, and an old plugin there can be switched off", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const lang of LANGS) for (const width of [1100, 420]) for (const on of [false, true]) {
      await t.test(`${lang} ${width} ${on ? "on" : "off"}`, async () => {
        const w = words[lang], calls = [];
        const ctx = await browser.newContext({ viewport: { width, height: 800 } });
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "rtk"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, calls, on));
        await page.goto("http://uni-switch.test/");
        await page.locator('button[data-view="library"]').click();
        const v = page.locator("#view-library");
        const row = v.locator(".lib-row").filter({ has: page.locator(".name", { hasText: "OpenCode" }) });
        await row.waitFor();
        const tag = row.locator(".lib-tag");
        assert.deepEqual(await tag.allTextContents(), [w.tag]);
        const why = await tag.getAttribute("title");
        assert(why.includes(on ? w.on : w.off), why);
        assert(!why.includes("{"), why);
        const sw = row.locator(".lib-switch");
        assert.equal(await sw.isDisabled(), !on);
        if (on) {
          await sw.click();
          for (let i = 0; i < 250 && !calls.length; i++) await page.waitForTimeout(20);
          assert.equal(calls.length, 1);
          assert.deepEqual(JSON.parse(calls[0]), { agent: "opencode", on: false });
        } else {
          await sw.click({ force: true });
          assert.deepEqual(calls, []);
        }
        const sideways = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
        assert(sideways <= 0, `the page scrolls sideways by ${sideways}px`);
        await ctx.close();
      });
    }
    assert.deepEqual(errors, []);
  });
}

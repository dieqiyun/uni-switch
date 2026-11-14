// Run with Node's test runner and Playwright on the module path; see README.md.
// #2202 (Chichengmo): Antigravity CLI wasn't on the RTK tab, though rtk
// 0.51 has a global plugin for it. With an rtk older than 0.51 (0.50 knows
// it only per project) its row says to update RTK with its own reason, not
// Codex's @RTK.md one, and its switch can't be turned on; Codex's row keeps
// its own words. Every language, Chromium and WebKit, 1100 and 420 wide,
// nothing sideways. No backend: the API is faked here.
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
  agents: [agent("agy", "Antigravity CLI"), agent("codex", "Codex")],
  instructions: { agents: [], sets: [] }, foundServers: [], projects: [], foundSkills: [], skills: [], servers: [],
});
const rtkView = () => ({
  path: `${HOME}/.local/bin/rtk`, version: "0.49.0", url: "https://www.rtk-ai.app",
  agents: [
    { id: "agy", name: "Antigravity CLI", icon: "", on: false, blocked: "Antigravity CLI's hook needs RTK 0.51 or newer" },
    { id: "codex", name: "Codex", icon: "", on: false, blocked: "Codex's hook needs RTK 0.50 or newer" },
  ],
});

function server(lang, calls) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/library") return route.fulfill({ json: lib() });
    if (url.pathname === "/api/library/rtk") {
      if (req.method() !== "GET") calls.push(req.postData());
      return route.fulfill({ json: rtkView() });
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
  en: { tag: "Update RTK", agy: "needs RTK 0.51 or newer: older ones install it only per project", codex: "needs RTK 0.50 or newer" },
  zh: { tag: "需更新 RTK", agy: "需要 RTK 0.51 或更高版本：旧版只能按项目安装", codex: "需要 RTK 0.50" },
  "zh-TW": { tag: "需更新 RTK", agy: "需要 RTK 0.51 或更高版本：舊版只能按專案安裝", codex: "需要 RTK 0.50" },
  ja: { tag: "RTK の更新が必要", agy: "RTK 0.51 以降が必要です。古いバージョンはプロジェクト単位でしかインストールできません", codex: "RTK 0.50 以降" },
  de: { tag: "RTK aktualisieren", agy: "benötigt RTK 0.51 oder neuer: Ältere Versionen installieren ihn nur pro Projekt", codex: "RTK 0.50 oder neuer" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": Antigravity CLI with an RTK before 0.51 says to update, in its own words", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const lang of LANGS) for (const width of [1100, 420]) {
      await t.test(`${lang} ${width}`, async () => {
        const w = words[lang], calls = [];
        const ctx = await browser.newContext({ viewport: { width, height: 800 } });
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "rtk"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, calls));
        await page.goto("http://uni-switch.test/");
        await page.locator('button[data-view="library"]').click();
        const v = page.locator("#view-library");
        const row = (name) => v.locator(".lib-row").filter({ has: page.locator(".name", { hasText: name }) });
        await row("Antigravity CLI").waitFor();
        const tag = row("Antigravity CLI").locator(".lib-tag");
        assert.deepEqual(await tag.allTextContents(), [w.tag]);
        const why = await tag.getAttribute("title");
        assert(why.includes(w.agy), why);
        assert(!why.includes("@RTK.md"), why);
        const ctag = await row("Codex").locator(".lib-tag").getAttribute("title");
        assert(ctag.includes(w.codex) && ctag.includes("@RTK.md"), ctag);
        const sw = row("Antigravity CLI").locator(".lib-switch");
        assert.equal(await sw.isDisabled(), true);
        await sw.click({ force: true });
        assert.deepEqual(calls, []);
        const sideways = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
        assert(sideways <= 0, `the page scrolls sideways by ${sideways}px`);
        await ctx.close();
      });
    }
    assert.deepEqual(errors, []);
  });
}

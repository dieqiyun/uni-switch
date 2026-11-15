// Run with Node's test runner and Playwright on the module path; see README.md.
// Scott__D on X: Install RTK never succeeded. The page said "Installing
// RTK… this can take a minute." and, when the install failed, said why only
// in a status line gone after ten seconds. Now the button is held while the
// install runs, and once it fails the card keeps the reason beside Get RTK,
// with Install RTK to try again; a second try that works shows rtk
// installed and drops the reason. Every language, Chromium and WebKit, 1100
// and 420 wide, nothing sideways. No backend: the API is faked here.
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
  agents: [agent("claude", "Claude Code")],
  instructions: { agents: [], sets: [] }, foundServers: [], projects: [], foundSkills: [], skills: [], servers: [],
});
const script = "curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh | sh";
const missing = () => ({ url: "https://www.rtk-ai.app", install: script, agents: [{ id: "claude", name: "Claude Code", icon: "", on: false }] });
const installed = () => ({ path: `${HOME}/.local/bin/rtk`, version: "1.1.0", url: "https://www.rtk-ai.app", agents: [{ id: "claude", name: "Claude Code", icon: "", on: false }] });
// InstallRTK's error for a failed script run (installFailed in rtk_upgrade.go)
const why = "RTK's install script failed (exit code 1) — [ERROR] Failed to download binary https://github.com/rtk-ai/rtk/releases/download/v1.1.0/rtk-aarch64-apple-darwin.tar.gz";

function server(lang, st) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/library") return route.fulfill({ json: lib() });
    if (url.pathname === "/api/library/rtk") return route.fulfill({ json: st.done ? installed() : missing() });
    if (url.pathname === "/api/library/rtk/install") {
      st.calls++;
      await st.hold; // the install runs until the test lets it end
      if (st.calls === 1) return route.fulfill({ status: 400, json: { error: why } });
      st.done = true;
      return route.fulfill({ json: installed() });
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
const failed = { en: "The install failed: ", zh: "安装失败：", "zh-TW": "安裝失敗：", ja: "インストールに失敗しました：", de: "Die Installation ist fehlgeschlagen: " };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a failed RTK install keeps its reason on the card, and Install RTK tries again", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const lang of LANGS) for (const width of [1100, 420]) {
      await t.test(`${lang} ${width}`, async () => {
        let release;
        const st = { calls: 0, done: false, hold: new Promise((r) => { release = r; }) };
        const ctx = await browser.newContext({ viewport: { width, height: 800 } });
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "rtk"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, st));
        await page.goto("http://uni-switch.test/");
        await page.locator('button[data-view="library"]').click();
        const v = page.locator("#view-library");
        const cmd = v.locator(".lib-rtk-cmd");
        await cmd.waitFor();
        assert.equal(await cmd.textContent(), script);
        const install = v.locator(".lib-acts button").first();
        assert.equal(await install.isDisabled(), false);
        const label = await install.textContent();

        // running: held, and no reason yet
        await install.click();
        await page.waitForFunction(() => document.querySelector("#view-library .lib-acts button")?.disabled);
        assert.equal(await v.locator(".lib-rtk-install-err").count(), 0);
        release();

        // failed: the reason stays on the card, and the button is back
        const err = v.locator(".lib-rtk-install-err");
        await err.waitFor();
        assert.equal(await err.textContent(), failed[lang] + why);
        await page.waitForFunction(() => !document.querySelector("#view-library .lib-acts button")?.disabled);
        assert.equal(await install.textContent(), label);
        const sideways = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
        assert(sideways <= 0, `the page scrolls sideways by ${sideways}px`);
        // a re-render (Check again) keeps it
        await v.locator(".lib-acts button").last().click();
        await err.waitFor();

        // tried again, and it works: the reason is gone, rtk is shown installed
        await v.locator(".lib-acts button").first().click();
        await page.waitForFunction(() => !document.querySelector("#view-library .lib-rtk-cmd"));
        assert.equal(await v.locator(".lib-rtk-install-err").count(), 0);
        assert.equal(st.calls, 2);
        await ctx.close();
      });
    }
    assert.deepEqual(errors, []);
  });
}

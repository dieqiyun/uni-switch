// Run with Node's test runner and Playwright on the module path; see README.md.
// Fate on Discord: a set of instructions was made and written, Claude Code
// switched on below, and its CLAUDE.md didn't change — the agents read the
// set in use (the round button), which was the empty Default. The agents'
// head now says which set they read; an open set not in use has a Switch the
// agents to this set button, which sends activate with the text typed; and
// an agent switched on while the set in use is empty says so. Chromium and
// WebKit, English and Chinese; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/fate";
const library = (claudeOn) => ({
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: [{ id: "claude", name: "Claude Code", icon: "", instructions: `${HOME}/.claude/CLAUDE.md` }],
  instructions: {
    shared: "",
    sets: [{ id: "default", name: "", active: true, text: "" }, { id: "work", name: "Work", active: false, text: "Company rules." }],
    agents: [{ agent: "claude", name: "Claude Code", icon: "", path: `${HOME}/.claude/CLAUDE.md`, on: claudeOn, extra: "" }],
  },
  foundServers: [], projects: [], foundSkills: [], skills: [], servers: [],
});

function server(lang, lib, sent) {
  return async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/library") return route.fulfill({ json: lib });
    if (url.pathname.startsWith("/api/library/")) {
      sent.push({ path: url.pathname, body: route.request().postDataJSON() });
      return route.fulfill({ json: { ...lib, result: { changed: [] } } });
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

const view = "#view-library";
const words = {
  en: { reads: "They read Default · Empty", use: "Switch the agents to this set", empty: "Claude Code is on, but it reads Default, which is empty" },
  zh: { reads: "它们读「默认」 · 空", use: "让 Agent 改用这一套", empty: "Claude Code 已开启，但它读的是「默认」，这一套是空的" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": the Library says which set the agents read, and switches them to another", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const lang of ["en", "zh"]) {
      await t.test(lang, async () => {
        const w = words[lang];
        const ctx = await browser.newContext({ viewport: { width: 980, height: 860 }, reducedMotion: "reduce" });
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "instructions"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        const sent = [];
        await page.route("**/*", server(lang, library(false), sent));
        await page.goto("http://uni-switch.test/");
        await page.locator('button[data-view="library"]').click();
        const reads = page.locator(`${view} .lib-reads`);
        await reads.waitFor();
        assert.equal((await reads.textContent()).trim(), w.reads);

        // the set in use has no button to switch to it; the Work set has
        await page.locator(`${view} .lib-set`).first().click();
        await page.locator(`${view} textarea[data-lib="set:default"]`).waitFor();
        assert.equal(await page.locator(`${view} .lib-use`).count(), 0);
        await page.locator(`${view} .lib-set`).nth(1).click();
        const ta = page.locator(`${view} textarea[data-lib="set:work"]`);
        await ta.waitFor();
        await ta.fill("Company rules.\nUse tabs.");
        const use = page.locator(`${view} .lib-use`);
        assert.equal((await use.textContent()).trim(), w.use);
        await use.click();
        await page.waitForTimeout(300);
        const s = sent.find((x) => x.path === "/api/library/instructions/save");
        assert(s, "nothing sent");
        assert.equal(s.body.activate, "work");
        assert.deepEqual(s.body.texts, { work: "Company rules.\nUse tabs." });

        // Claude Code switched on while the set in use is empty says so
        const sw = page.locator(`${view} button.lib-switch`).first();
        await sw.click();
        await page.waitForFunction((x) => document.querySelector("#status").textContent.includes(x), w.empty);
        await ctx.close();
      });
    }
    assert.deepEqual(errors, []);
  });
}

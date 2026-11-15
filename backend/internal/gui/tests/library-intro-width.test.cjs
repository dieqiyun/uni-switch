// Run with Node's test runner and Playwright on the module path; see README.md.
// #2319 (emo172): Library's intro text was held to 640px, so on a wide window
// RTK's broke off mid-sentence with the RTK card under it running on far
// past it. Each tab's intro is now as wide as the cards under it (less its
// 2px sides), at 1100px and 420px, in every language, and nothing on the
// page goes sideways. No backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/emo";
const agent = (id, name) => ({ id, name, icon: "", skills: `${HOME}/.${id}/skills`, mcp: `${HOME}/.${id}/mcp.json` });
const lib = () => ({
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: [agent("codex", "Codex"), agent("claude", "Claude Code")],
  instructions: { agents: [], sets: [] }, foundServers: [], projects: [], foundSkills: [], skills: [], servers: [],
});

function server(lang) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/library") return route.fulfill({ json: lib() });
    if (url.pathname === "/api/library/rtk") {
      return route.fulfill({ json: { path: `${HOME}/.local/bin/rtk`, version: "0.51.0", url: "https://www.rtk-ai.app", agents: [{ id: "codex", name: "Codex", icon: "", on: true }], gain: { commands: 42, input: 9000, saved: 8000, pct: 88.9 } } });
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

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": Library's intro is as wide as the cards under it", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      for (const width of [1100, 420]) {
        for (const tab of ["rtk", "mcp", "instructions"]) {
          await t.test(`${lang} ${width} ${tab}`, async () => {
            const ctx = await browser.newContext({ viewport: { width, height: 800 } });
            await ctx.addInitScript((tab) => { try { localStorage.setItem("uni-switch.libTab", tab); } catch {} }, tab);
            const page = await ctx.newPage();
            page.setDefaultTimeout(5000);
            page.setDefaultNavigationTimeout(60000);
            page.on("pageerror", (e) => errors.push(e.message));
            await page.route("**/*", server(lang));
            await page.goto("http://uni-switch.test/");
            await page.locator('button[data-view="library"]').click();
            const v = page.locator("#view-library");
            await v.locator(".lib-body > p.lib-intro").waitFor();
            if (tab === "rtk") await v.locator(".lib-body > .lib-card").first().waitFor();
            const m = await page.evaluate(() => {
              const body = document.querySelector("#view-library .lib-body");
              const intro = body.querySelector(":scope > p.lib-intro");
              const card = body.querySelector(":scope > .lib-card");
              const r = (e) => e && e.getBoundingClientRect();
              const ir = r(intro), br = r(body), cr = r(card);
              return {
                intro: ir.width, body: br.width, card: cr ? cr.width : null,
                introRight: ir.right, cardRight: cr ? cr.right : null,
                text: intro.textContent,
                sideways: document.documentElement.scrollWidth - document.documentElement.clientWidth,
              };
            });
            assert(m.text.length > 20, `${lang} ${tab}: no intro text`);
            // the intro spans the column, less its own 2px sides
            assert(Math.abs(m.intro - (m.body - 4)) <= 1, `${lang} ${width} ${tab}: intro ${m.intro}px in a ${m.body}px column`);
            if (tab === "rtk") {
              assert(m.card !== null, "no RTK card");
              assert(Math.abs(m.introRight - (m.cardRight - 2)) <= 1, `${lang} ${width}: intro ends at ${m.introRight}, the RTK card at ${m.cardRight}`);
            }
            assert(m.sideways <= 0, `${lang} ${width} ${tab}: the page scrolls sideways by ${m.sideways}px`);
            await ctx.close();
          });
        }
      }
    }
    assert.deepEqual(errors, []);
  });
}

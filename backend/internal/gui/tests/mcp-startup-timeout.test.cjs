// Run with Node's test runner and Playwright on the module path; see README.md.
// #3168 (miaomiaozi on Discord): a slow-starting MCP server couldn't be
// given a longer startup timeout in uni-switch. The server's editor has a
// "Startup timeout" in seconds, shown only when an agent that takes one
// (Codex, startup_timeout_sec) is here, digits only, saved with the
// server as startupTimeout and shown again; emptied, it saves 0 (the
// agent's own). Every language, Chromium and WebKit, 1100 and 390 wide;
// nothing scrolls sideways. No backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const { I18N } = require("./strings.cjs").strings(); // i18n.js's and the plugins'
const tr = (lang, s, vars = {}) => ((lang === "en" ? s : I18N[lang][s]) || s).replace(/\{(\w+)\}/g, (_, k) => vars[k]);
const HOME = "/Users/xv";
const agent = (id, name) => ({ id, name, icon: "", skills: `${HOME}/.${id}/skills`, mcp: `${HOME}/.${id}/mcp.json` });


function server(lang, calls, agents) {
  const lib = {
    dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
    agents,
    instructions: { agents: [], sets: [] }, foundServers: [], projects: [], foundSkills: [], skills: [],
    servers: [{ name: "slow", transport: "stdio", command: "npx", args: ["-y", "slow-mcp"], agents: agents.map((a) => a.id) }],
  };
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/library") return route.fulfill({ json: lib });
    if (url.pathname === "/api/library/mcp/check") return route.fulfill({ json: { servers: {} } });
    if (url.pathname === "/api/library/servers/save") {
      const { old, server: s } = req.postDataJSON();
      calls.push(s.name + " " + s.startupTimeout);
      const i = lib.servers.findIndex((x) => x.name === old);
      lib.servers[i] = { ...lib.servers[i], ...s };
      if (!s.startupTimeout) delete lib.servers[i].startupTimeout;
      return route.fulfill({ json: { ...lib, result: { changed: agents.map((a) => a.id) } } });
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
  test(engine + ": an MCP server's startup timeout, for Codex", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      for (const width of [1100, 390]) {
        await t.test(`${lang} ${width}`, async () => {
          const w = (s, v) => tr(lang, s, v);
          const go = async (agents) => {
            const calls = [];
            const ctx = await browser.newContext({ viewport: { width, height: 800 } });
            await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "mcp"); } catch {} });
            const page = await ctx.newPage();
            page.setDefaultTimeout(5000);
            page.setDefaultNavigationTimeout(60000);
            page.on("pageerror", (e) => errors.push(e.message));
            await page.route("**/*", server(lang, calls, agents));
            await page.goto("http://uni-switch.test/");
            await page.locator('button[data-view="library"]').click();
            const row = page.locator("#view-library .lib-server", { has: page.locator('.lib-health[data-server="slow"]') });
            const ed = page.locator("#modal .lib-editor");
            const open = async () => { await row.locator(".name").click(); await ed.waitFor(); };
            const save = async () => { await ed.locator(":scope > .bar button.primary").click(); await ed.waitFor({ state: "detached" }); };
            return { ctx, page, ed, calls, open, save, wait: ed.getByLabel(w("Startup timeout"), { exact: true }) };
          };

          // no agent that takes one: no field
          {
            const { ctx, ed, open, wait } = await go([agent("claude", "Claude Code")]);
            await open();
            await ed.getByText(w("Agents"), { exact: true }).first().waitFor();
            assert.equal(await wait.count(), 0, "a startup timeout with no Codex here");
            await ctx.close();
          }

          const { ctx, page, ed, calls, open, save, wait } = await go([agent("claude", "Claude Code"), { ...agent("codex", "Codex"), startup: true }]);
          // empty: Codex's own; the hint names Codex and that a hand-written value stays
          await open();
          assert.equal(await wait.inputValue(), "");
          assert.equal(await wait.getAttribute("placeholder"), "30");
          await ed.locator(".hint", { hasText: w("Seconds {agents} waits for it to start and list its tools. Empty leaves {agents}'s own 30; a value you wrote in its config.toml by hand is kept", { agents: "Codex" }) }).waitFor();
          // digits only, saved with the server
          await wait.pressSequentially("1a20");
          assert.equal(await wait.inputValue(), "120");
          await save();
          assert.deepEqual(calls, ["slow 120"]);
          // shown again; emptied, saved as the agent's own
          await open();
          assert.equal(await wait.inputValue(), "120");
          await wait.fill("");
          await save();
          assert.deepEqual(calls, ["slow 120", "slow 0"]);
          assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, "no sideways scroll");
          await ctx.close();
        });
      }
    }
    assert.deepEqual(errors, []);
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// #332: a skill's or an MCP server's agent icon turned off takes it out of
// that agent, and the status says so ("Removed from Pi" / 「已从 Pi 中移除」),
// not "Written to Pi" as when it is turned on; in English and Chinese. No
// backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/aimer";
const agent = (id, name) => ({ id, name, icon: "", skills: `${HOME}/.${id}/skills`, mcp: `${HOME}/.${id}/mcp.json` });
const base = () => ({
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: [agent("codex", "Codex"), agent("pi", "Pi"), agent("dsh", "DeepSeek Harness")],
  instructions: { agents: [], sets: [] }, foundServers: [], projects: [], foundSkills: [],
  skills: [{ name: "grilling", description: "Grill a plan", kind: "folder", agents: ["codex"], source: `${HOME}/skills/grilling` }],
  servers: [{ name: "fs", transport: "stdio", command: "npx", args: ["fs-mcp"], agents: ["codex"] }],
});

function server(lang) {
  const lib = base();
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/library") return route.fulfill({ json: lib });
    const m = url.pathname.match(/^\/api\/library\/(skills|servers)\/agents$/);
    if (m) {
      const body = req.postDataJSON();
      const x = lib[m[1]].find((y) => y.name === body.name);
      // every agent turned on or off is written
      const changed = [...body.agents.filter((a) => !x.agents.includes(a)), ...x.agents.filter((a) => !body.agents.includes(a))];
      x.agents = body.agents;
      return route.fulfill({ json: { ...lib, result: { changed, problems: [] } } });
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

const words = {
  en: { on: "Written to DeepSeek Harness", off: "Removed from DeepSeek Harness", offCodex: "Removed from Codex" },
  zh: { on: "已写入 DeepSeek Harness", off: "已从 DeepSeek Harness 中移除", offCodex: "已从 Codex 中移除" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": an agent icon turned off says removed", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const lang of ["en", "zh"]) {
      for (const tab of ["skills", "mcp"]) {
        await t.test(lang + " " + tab, async () => {
          const w = words[lang];
          const ctx = await browser.newContext({ viewport: { width: 980, height: 800 } });
          await ctx.addInitScript((tab) => { try { localStorage.setItem("uni-switch.libTab", tab); } catch {} }, tab);
          const page = await ctx.newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", server(lang));
          await page.goto("http://uni-switch.test/");
          await page.locator('button[data-view="library"]').click();
          const chip = (id) => page.locator(`#view-library .lib-row .lib-ag[data-agent="${id}"]`);
          await chip("dsh").waitFor();
          const said = async (want) => {
            await page.waitForFunction((want) => document.querySelector("#status").textContent === want, want).catch(() => {});
            assert.equal(await page.locator("#status").textContent(), want);
          };
          await chip("dsh").click();
          await said(w.on);
          assert.equal(await chip("dsh").getAttribute("aria-pressed"), "true");
          await chip("dsh").click();
          await said(w.off);
          assert.equal(await chip("dsh").getAttribute("aria-pressed"), "false");
          await chip("codex").click();
          await said(w.offCodex);
          await ctx.close();
        });
      }
    }
    assert.deepEqual(errors, []);
  });
}

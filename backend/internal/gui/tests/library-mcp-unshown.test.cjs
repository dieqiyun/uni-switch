// Run with Node's test runner and Playwright on the module path; see README.md.
// A library MCP server is checked on its own only when an agent on the page
// has it on, or a project's file on the page does (#1597, xiaozhu1337: every
// chip of ida-pro-mcp and windbg-mcp was off, and both were still started
// each time uni-switch opened). The library keeps what each agent it ever gave
// a server to has, those the page doesn't show too: one hidden on the Agents
// page, one no longer installed, one with no MCP of its own, and a project
// whose folder is gone. A server on only for those is "not checked", and
// its tooltip says why; a click checks it. In every language, in both
// engines, narrow and wide. No backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "C:/Users/xz";
const fixture = () => ({
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: [
    { id: "claude", name: "Claude Code", icon: "claudecode-color", skills: `${HOME}/.claude/skills`, mcp: `${HOME}/.claude.json`, projectMCP: ".mcp.json" },
    // hidden on the Agents page
    { id: "codex", name: "Codex", icon: "codex", skills: `${HOME}/.codex/skills`, mcp: `${HOME}/.codex/config.toml`, projectMCP: ".codex/config.toml" },
    // skills only: no MCP servers uni-switch can write
    { id: "agy", name: "Antigravity", icon: "generic", skills: `${HOME}/.gemini/antigravity/skills` },
  ],
  instructions: { agents: [], sets: [] }, foundServers: [], foundSkills: [], skills: [],
  projects: [
    { dir: `${HOME}/code/app`, name: "app", copy: false, skills: {}, placed: [], servers: { projok: ["claude"], projhidden: ["codex"] }, wrote: {} },
    // the folder is gone: nothing starts there
    { dir: `${HOME}/code/old`, name: "old", copy: false, skills: {}, placed: [], servers: { projgone: ["claude"] }, wrote: {}, missing: true },
  ],
  servers: [
    { name: "files", transport: "stdio", command: "npx", args: ["files-mcp"], agents: ["claude"] },
    { name: "ida-pro-mcp", transport: "stdio", command: "idalib-mcp.exe", args: ["--stdio"], agents: ["codex"] },
    { name: "windbg-mcp", transport: "stdio", command: "uvx", args: ["mcp-windbg"], agents: ["cursor"] },
    { name: "skillonly", transport: "stdio", command: "skill-mcp", agents: ["agy"] },
    { name: "projok", transport: "stdio", command: "projok-mcp", agents: [] },
    { name: "projhidden", transport: "stdio", command: "projhidden-mcp", agents: [] },
    { name: "projgone", transport: "stdio", command: "projgone-mcp", agents: [] },
    { name: "idle", transport: "stdio", command: "idle-mcp", agents: [] },
  ],
});
const UNSHOWN = ["ida-pro-mcp", "windbg-mcp", "skillonly", "projhidden", "projgone"];

function server(lang, checks) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, agentsHidden: ["codex"] } } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/library") return route.fulfill({ json: fixture() });
    if (url.pathname === "/api/library/mcp/check") {
      const body = req.postDataJSON();
      checks.push(body);
      return route.fulfill({ json: { servers: Object.fromEntries(body.names.map((n) => [n, { state: "ok", tools: 3 }])) } });
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
  en: { not: "not checked", none: "No agent has it, so it isn't checked on its own", unshown: "No agent shown here has it on, so it isn't checked on its own", click: "Click to check it", ok: "3 tools" },
  zh: { not: "未检查", none: "没有 agent 在用它，所以不会自动检查", unshown: "这里显示的 agent 都没有开着它，所以不会自动检查", click: "点击检查", ok: "3 个工具" },
  "zh-TW": { not: "未檢查", none: "沒有 agent 在用它，所以不會自動檢查", unshown: "這裡顯示的 agent 都沒有開著它，所以不會自動檢查", click: "點選檢查", ok: "3 個工具" },
  ja: { not: "未確認", none: "どのエージェントも使っていないため、自動では確認しません", unshown: "ここに表示されているエージェントはどれも使っていないため、自動では確認しません", click: "クリックして確認", ok: "3 個のツール" },
  de: { not: "nicht geprüft", none: "Kein Agent nutzt ihn, daher wird er nicht von selbst geprüft", unshown: "Kein hier angezeigter Agent nutzt ihn, daher wird er nicht von selbst geprüft", click: "Zum Prüfen klicken", ok: "3 Tools" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a server on only for agents or projects the page doesn't show isn't checked on its own", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const lang of Object.keys(words)) {
      for (const width of [420, 980]) {
        await t.test(`${lang} ${width}`, async () => {
          const w = words[lang], checks = [];
          const ctx = await browser.newContext({ viewport: { width, height: 700 } });
          await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "mcp"); } catch {} });
          const page = await ctx.newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("http://uni-switch.test/**", server(lang, checks));
          await page.goto("http://uni-switch.test/");
          await page.locator('button[data-view="library"]').click();
          await page.locator("#view-library .lib-body:not(.lib-skel) .lib-row").first().waitFor();
          const status = (name) => page.locator(`#view-library .lib-server .lib-health[data-server="${name}"]`);
          for (let i = 0; i < 50 && !checks.length; i++) await page.waitForTimeout(20);
          await page.waitForTimeout(200);

          // only those an agent or a project on the page has are checked
          assert.equal(checks.length, 1);
          assert.deepEqual([...checks[0].names].sort(), ["files", "projok"]);
          // the hidden agent's chip isn't on the page: every chip of theirs is off
          for (const n of UNSHOWN) {
            assert.equal(await page.locator(`#view-library .lib-server:has(.lib-health[data-server="${n}"]) .lib-ag.on`).count(), 0, n);
            assert.equal(await status(n).isVisible(), true, n);
            assert.equal(await status(n).textContent(), w.not, n);
            assert.equal(await status(n).getAttribute("title"), w.unshown + "\n" + w.click, n);
          }
          assert.equal(await status("idle").getAttribute("title"), w.none + "\n" + w.click);
          await page.waitForFunction((t) => document.querySelector('#view-library .lib-server .lib-health[data-server="files"]').textContent === t, w.ok);

          // a click checks it, and only it, without moving the page
          const v = page.locator("#view-library");
          const top = await v.evaluate((e) => e.scrollTop);
          // wheeled down to it as the reader would, when it's past the window's foot
          await page.mouse.move(Math.min(490, width / 2), 250);
          for (let i = 0; i < 40; i++) {
            const b = await status("windbg-mcp").boundingBox(), foot = await v.evaluate((e) => e.getBoundingClientRect().bottom);
            if (b.y + b.height < foot - 20) break;
            await page.mouse.wheel(0, 60);
            await page.waitForTimeout(60);
          }
          await page.waitForTimeout(400);
          const at = await v.evaluate((e) => e.scrollTop);
          await status("windbg-mcp").click();
          for (let i = 0; i < 50 && checks.length < 2; i++) await page.waitForTimeout(20);
          assert.deepEqual({ names: checks[1].names, fresh: checks[1].fresh }, { names: ["windbg-mcp"], fresh: true });
          await page.waitForFunction((t) => document.querySelector('#view-library .lib-server .lib-health[data-server="windbg-mcp"]').textContent === t, w.ok);
          assert.equal(await v.evaluate((e) => e.scrollTop), at, "the click moved the page");
          assert(top <= at);
          assert.equal(await page.evaluate(() => document.querySelector("#modal").classList.contains("lib")), false, "the click opened the editor");
          // nothing runs off a narrow window
          assert.deepEqual(await page.evaluate(() => [document.documentElement.scrollWidth <= innerWidth, scrollX]), [true, 0]);
          assert.equal(checks.length, 2);
          await ctx.close();
        });
      }
    }
    assert.deepEqual(errors, []);
  });
}

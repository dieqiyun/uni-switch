// Run with Node's test runner and Playwright on the module path; see README.md.
// #1478 (xiaozhu1337): a project the user doesn't keep in uni-switch had no way
// to get the library's servers but adding it as a project first. The
// servers picked (Select), and a server's own editor, have Copy config…: a
// menu of the agents that read a project's servers from a file, each with
// that file, and a pick asks uni-switch for the file's text
// (library/mcp/config, nothing written) and puts it on the clipboard. The
// status names the file to paste it into, and a server the agent can't take
// there (SSE for Codex) as left out. At 440px wide in en, zh, ja and de:
// no sideways scroll. No backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { strings } = require("./strings.cjs");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/xz";
const agent = (id, name, more = {}) => ({ id, name, icon: "", skills: `${HOME}/.${id}/skills`, mcp: `${HOME}/.${id}/mcp.json`, ...more });
const sv = (name, transport, agents, more = {}) => ({ name, transport, command: transport === "stdio" ? name + "-mcp" : "", url: transport === "stdio" ? "" : "http://localhost:9/" + name, agents, ...more });
const lib = (servers) => ({
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: [
    agent("claude", "Claude Code", { projectMCP: ".mcp.json" }),
    agent("codex", "Codex", { noSSE: true, projectMCP: ".codex/config.toml", projectNoSSE: true }),
    agent("copilot", "Copilot CLI"),
  ],
  instructions: { agents: [] }, servers, skills: [], foundServers: [], projects: [], foundSkills: [], problems: [],
});
const FILES = {
  claude: { file: ".mcp.json", text: '{\n  "mcpServers": {\n    "fs": {\n      "type": "stdio",\n      "command": "fs-mcp"\n    }\n  }\n}\n' },
  codex: { file: ".codex/config.toml", text: '[mcp_servers.fs]\ncommand = "fs-mcp"\n' },
};

let I18N;
async function loadI18N() {
  I18N = strings().I18N; // i18n.js's and the plugins'
}
function tr(lang, key, vars = {}) {
  let s = key;
  if (lang !== "en") {
    assert.equal(typeof I18N[lang][key], "string", `${lang} has no ${JSON.stringify(key)}`);
    s = I18N[lang][key];
  }
  return s.replace(/\{(\w+)\}/g, (m, k) => (k in vars ? String(vars[k]) : m));
}

function server(lang, state, posts, copied) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light", agentsHidden: ["copilot"] } });
    if (url.pathname === "/api/library") return json(lib(state.servers));
    if (url.pathname === "/api/library/mcp/check") return json({ servers: Object.fromEntries(req.postDataJSON().names.map((n) => [n, { state: "ok", tools: 2 }])) });
    if (url.pathname === "/api/library/mcp/config") {
      const body = req.postDataJSON();
      posts.push(body);
      const f = FILES[body.agent];
      const skipped = body.agent === "codex" ? body.names.filter((n) => state.servers.find((s) => s.name === n)?.transport === "sse") : [];
      return json({ file: f.file, text: skipped.length === body.names.length ? "" : f.text, ...(skipped.length ? { skipped } : {}) });
    }
    if (url.pathname === "/api/copy") { copied.push(req.postDataJSON().text); return json({}); }
    if (url.pathname.startsWith("/api/library/") && req.method() === "POST") return route.fulfill({ status: 500, body: "nothing else is written" });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    if (url.host !== "uni-switch.test") return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const sideways = (page) => page.evaluate(() => {
  const over = [];
  const w = document.documentElement.clientWidth;
  if (document.documentElement.scrollWidth > w) over.push("page " + document.documentElement.scrollWidth);
  for (const e of document.querySelectorAll("#view-library .lib-pickbar > *, #view-library .lib-pickbar, #modal .lib-editor .bar > *, .proto-menu")) {
    const r = e.getBoundingClientRect();
    if (r.width && (r.right > w + 0.5 || r.left < -0.5)) over.push((e.className || e.tagName) + " " + Math.round(r.left) + "–" + Math.round(r.right));
  }
  return over;
});

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": Copy config… puts the servers on the clipboard as an agent's project file has them", async (t) => {
    await loadI18N();
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    t.after(() => browser.close());

    for (const lang of ["en", "zh", "ja", "de"]) {
      await t.test(lang, async () => {
        const w = (key, vars) => tr(lang, key, vars);
        const state = { servers: [sv("fs", "stdio", ["claude"]), sv("web", "sse", ["claude"]), sv("docs", "http", [])] };
        const posts = [], copied = [], errors = [];
        const ctx = await browser.newContext({ viewport: { width: 440, height: 1000 }, reducedMotion: "reduce" });
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "mcp"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, state, posts, copied));
        await page.goto("http://uni-switch.test/");
        await page.locator('button[data-view="library"]').click();
        const v = page.locator("#view-library");
        await v.locator(".lib-server").first().waitFor();
        const row = (name) => v.locator(".lib-server").filter({ has: page.locator(".name", { hasText: new RegExp("^" + name) }) });
        const menuItems = async () => (await page.locator(".proto-menu .pm-item").evaluateAll((bs) => bs.map((b) => b.querySelector(".pm-name").textContent + " " + b.querySelector(".pm-note").textContent)));

        // nothing picked: no button
        await v.locator(".lib-serverhead .lib-select").click();
        assert.equal(await v.locator(".lib-pickbar .lib-copyconfig").count(), 0);
        await row("fs").locator(".lib-pickbox").click();
        await row("web").locator(".lib-pickbox").click();
        const cc = v.locator(".lib-pickbar .lib-copyconfig");
        assert.equal((await cc.textContent()).trim(), w("Copy config…"));
        assert.equal(await cc.getAttribute("title"), w("Copy these servers as an agent's project file has them, to paste into a project yourself"));
        assert.deepEqual(await sideways(page), [], "sideways scroll with the bar");

        // the agents with a project file, each with its file; the hidden
        // one and one with no project file aren't offered
        await cc.click();
        await page.locator(".proto-menu").waitFor();
        assert.equal((await page.locator(".proto-menu .pm-head").textContent()).trim(), w("Copy as an agent's project file"));
        assert.deepEqual(await menuItems(), ["Claude Code .mcp.json", "Codex .codex/config.toml"]);
        assert.deepEqual(await sideways(page), [], "sideways scroll with the menu");
        await page.locator(".proto-menu .pm-item", { hasText: "Codex" }).click();
        await page.locator("#status").filter({ hasText: w("Copied for {agent}, to paste into {file} in your project. Left out, as {agent} can't take them there: {names}", { agent: "Codex", file: ".codex/config.toml", names: "web" }) }).waitFor();
        assert.deepEqual(posts.shift(), { agent: "codex", names: ["fs", "web"] });
        assert.deepEqual(copied, [FILES.codex.text]);

        // only what Codex can't take: nothing copied, and said
        await row("fs").locator(".lib-pickbox").click();
        await cc.click();
        await page.locator(".proto-menu .pm-item", { hasText: "Codex" }).click();
        await page.locator("#status").filter({ hasText: w("{agent} can't take {names} from a project's file", { agent: "Codex", names: "web" }) }).waitFor();
        assert.deepEqual(posts.shift(), { agent: "codex", names: ["web"] });
        assert.equal(copied.length, 1, "an empty file was copied");

        // a server's own editor has it too
        await v.locator(".lib-serverhead .lib-select").click();
        await row("docs").click();
        const ed = page.locator("#modal .lib-editor");
        await ed.waitFor();
        const ec = ed.locator(".bar .lib-copyconfig");
        assert.equal(await ec.getAttribute("title"), w("Copy {name} as an agent's project file has it, to paste into a project yourself", { name: "docs" }));
        assert.deepEqual(await sideways(page), [], "sideways scroll in the editor's bar");
        await ec.click();
        await page.locator(".proto-menu .pm-item", { hasText: "Claude Code" }).click();
        await page.locator("#status").filter({ hasText: w("Copied for {agent}, to paste into {file} in your project", { agent: "Claude Code", file: ".mcp.json" }) }).waitFor();
        assert.deepEqual(posts.shift(), { agent: "claude", names: ["docs"] });
        assert.deepEqual(copied.slice(1), [FILES.claude.text]);
        assert.equal(await ed.count(), 1, "the editor closed");
        assert.deepEqual(errors, []);
        await ctx.close();
      });
    }
  });
}

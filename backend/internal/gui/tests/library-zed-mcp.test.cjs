// Run with Node's test runner and Playwright on the module path; see README.md.
// lc on Discord: Zed wasn't among the agents an MCP server could be given
// to. uni-switch gives Zed servers in its settings.json (context_servers), so
// Zed's chip is on every server's row and in the editor, and a click gives
// it the server. Zed speaks a command or streamable HTTP, not SSE, and
// takes a header as written, not ${NAME}: its chip is greyed for those
// with the reason, on the row and in the editor.
// In en, zh, zh-TW, ja and de, at 1100px and 560px: no sideways scroll.
// No backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { strings } = require("./strings.cjs");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/emo";
const agent = (id, name, more = {}) => ({ id, name, icon: "", skills: `${HOME}/.${id}/skills`, mcp: `${HOME}/.${id}/mcp.json`, ...more });
const lib = (servers) => ({
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: [
    agent("claude", "Claude Code"),
    // as library.Read gives it: no skills folder, its settings.json for MCP
    agent("zed", "Zed", { skills: "", mcp: `${HOME}/.config/zed/settings.json`, noSSE: true, noEnvRefs: true }),
  ],
  instructions: { agents: [] }, servers, skills: [], foundServers: [], projects: [], foundSkills: [], problems: [],
});

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

function server(lang, state, posts) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/library") return json(lib(state.servers));
    if (url.pathname === "/api/library/mcp/check") return json({ servers: Object.fromEntries(req.postDataJSON().names.map((n) => [n, { state: "ok", tools: 2 }])) });
    if (url.pathname === "/api/library/servers/agents" && req.method() === "POST") {
      const body = req.postDataJSON();
      posts.push(body);
      state.servers = state.servers.map((x) => (x.name === body.name ? { ...x, agents: body.agents } : x));
      return json({ ...lib(state.servers), result: { changed: ["zed"] } });
    }
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
  for (const e of document.querySelectorAll("#view-library .lib-server, #view-library .lib-agents, #modal .lib-editor, #modal .lib-agents")) {
    const r = e.getBoundingClientRect();
    if (r.width && (r.right > w + 0.5 || r.left < -0.5)) over.push((e.className || e.tagName) + " " + Math.round(r.left) + "–" + Math.round(r.right));
  }
  return over;
});

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": Zed takes MCP servers", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    await loadI18N();
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    t.after(() => browser.close());

    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      for (const width of [1100, 560]) {
        await t.test(`${lang} ${width}px`, async () => {
          const w = (key, vars) => tr(lang, key, vars);
          const state = {
            servers: [
              { name: "fs", transport: "stdio", command: "fs-mcp", args: [], env: { ROOT: "/tmp" }, agents: ["claude", "zed"] },
              { name: "web", transport: "http", url: "https://example.com/mcp", headers: { Authorization: "Bearer x" }, agents: ["claude"] },
              { name: "old", transport: "sse", url: "https://example.com/sse", agents: ["claude"] },
              { name: "gh", transport: "http", url: "https://example.com/gh", headers: { Authorization: "Bearer ${GH_TOKEN}" }, agents: ["claude"] },
            ],
          };
          const posts = [], errors = [];
          const ctx = await browser.newContext({ viewport: { width, height: 1000 }, reducedMotion: "reduce" });
          await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "mcp"); } catch {} });
          const page = await ctx.newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", server(lang, state, posts));
          await page.goto("http://uni-switch.test/");
          await page.locator('button[data-view="library"]').click();
          const v = page.locator("#view-library");
          await v.locator(".lib-server").first().waitFor();
          const row = (name) => v.locator(".lib-server").filter({ has: page.locator(".name", { hasText: new RegExp("^" + name) }) });
          const zed = (name) => row(name).locator('.lib-ag[data-agent="zed"]');
          const noSSE = w("{agent} can't reach a server over SSE — only a command or streamable HTTP", { agent: "Zed" });
          const noRef = w("{agent} can't read {ref} from its settings — given this server, the token would be written there as plain text", { agent: "Zed", ref: "${NAME}" });

          // every row has Zed's chip: lit where Zed has it, greyed with the
          // reason where it can't take it
          assert.equal(await zed("fs").getAttribute("aria-pressed"), "true");
          assert.equal(await zed("fs").getAttribute("aria-disabled"), null);
          assert.equal(await zed("web").getAttribute("aria-pressed"), "false");
          assert.equal(await zed("web").getAttribute("aria-disabled"), null, "a streamable HTTP server was held from Zed");
          assert.equal(await zed("old").getAttribute("aria-disabled"), "true");
          assert.equal(await zed("old").getAttribute("title"), noSSE);
          assert.equal(await zed("gh").getAttribute("aria-disabled"), "true");
          assert.equal(await zed("gh").getAttribute("title"), noRef);
          assert.deepEqual(await sideways(page), [], "sideways scroll on the page");

          // a click gives web to Zed
          await zed("web").click();
          for (let i = 0; !posts.length && i < 100; i++) await page.waitForTimeout(20);
          assert.deepEqual(posts[0], { name: "web", agents: ["claude", "zed"] });
          assert.equal(await zed("web").getAttribute("aria-pressed"), "true");
          // a greyed chip sends nothing
          await zed("old").click({ force: true });
          await page.waitForTimeout(150);
          assert.equal(posts.length, 1, "a click on Zed's greyed chip gave it an SSE server");

          // the editor names Zed, and greys it for SSE
          await row("old").locator(".sub").click();
          const sheet = page.locator("#modal .lib-editor");
          await sheet.waitFor();
          const chip = sheet.locator('.lib-ag[data-agent="zed"]');
          assert.equal((await chip.locator(".n").textContent()).trim(), "Zed");
          assert.equal(await chip.getAttribute("aria-disabled"), "true");
          assert.equal(await chip.getAttribute("title"), noSSE);
          assert.deepEqual(await sideways(page), [], "sideways scroll in the editor");
          await page.keyboard.press("Escape");
          await sheet.waitFor({ state: "detached" });

          await row("fs").locator(".sub").click();
          await sheet.waitFor();
          assert.equal(await chip.getAttribute("aria-pressed"), "true");
          assert.equal(await chip.getAttribute("aria-disabled"), null);

          assert.deepEqual(errors, []);
          await ctx.close();
        });
      }
    }
  });
}

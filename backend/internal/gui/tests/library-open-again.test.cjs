// Run with Node's test runner and Playwright on the module path; see README.md.
// #3099: opening the Library (its nav clicked, while on it too, or coming
// back from another page) read the library and drew the whole page again,
// changed or not. The rows were replaced under the reader's pointer, so a
// hover's trash button went away mid-click, and library-row-remove's click
// found a row that was gone by the time it was measured. Opening it now
// draws it again only for a change: the same rows stay when nothing
// changed, and a server added meanwhile shows. Chromium and WebKit, 1100px
// and 420px, in en and zh. No backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/szoxidy";
const agent = (id, name) => ({ id, name, icon: "", skills: `${HOME}/.${id}/skills`, mcp: `${HOME}/.${id}/mcp.json` });
const sv = (name, url, agents) => ({ name, transport: "http", url, agents });
const lib = (st) => ({
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: [agent("claude", "Claude Code"), agent("codex", "Codex")],
  instructions: { agents: [] }, servers: st.servers, skills: [], foundServers: [], foundSkills: [], problems: [],
  projects: [], hooks: [], hookAgents: [],
});

function server(lang, state) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/library") {
      if (!url.searchParams.has("glance")) state.reads++;
      return json(lib(state));
    }
    if (url.pathname === "/api/library/mcp/check") return json({ servers: Object.fromEntries(req.postDataJSON().names.map((n) => [n, { state: "ok", tools: 2 }])) });
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

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": opening the Library again keeps its rows unless it changed", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    t.after(() => browser.close());
    for (const width of [1100, 420]) for (const lang of ["en", "zh"]) {
      await t.test(`${lang} at ${width}px`, async () => {
        const state = { servers: [sv("exa", "https://mcp.exa.ai/mcp", ["claude"])], reads: 0 };
        const errors = [];
        const ctx = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "mcp"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, state));
        await page.goto("http://uni-switch.test/");
        const nav = page.locator('button[data-view="library"]');
        const v = page.locator("#view-library");
        const srow = (name) => v.locator(".lib-server").filter({ has: page.locator(".name", { hasText: new RegExp("^" + name + "$") }) });
        // the read an opening asks for has come back, and what it draws is drawn
        const opened = async (reads) => {
          for (let i = 0; state.reads < reads; i++) {
            assert(i < 500, `the library was read ${state.reads} times, not ${reads}`);
            await page.evaluate(() => new Promise((r) => setTimeout(r, 10)));
          }
          await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
        };
        await nav.click();
        await srow("exa").waitFor();
        await opened(1);
        await page.evaluate(() => { document.querySelector("#view-library .lib-server").dataset.kept = "1"; });

        // its nav clicked while on it: read again, the same rows
        await nav.click();
        await opened(2);
        assert.equal(await srow("exa").getAttribute("data-kept"), "1", "the row was drawn over again, nothing changed");

        // a server added meanwhile: it shows on the next opening
        state.servers = [...state.servers, sv("context7", "https://mcp.context7.com/mcp", [])];
        await nav.click();
        await srow("context7").waitFor();
        assert.equal(await srow("exa").count(), 1);
        assert.deepEqual(errors, []);
        await ctx.close();
      });
    }
  });
}

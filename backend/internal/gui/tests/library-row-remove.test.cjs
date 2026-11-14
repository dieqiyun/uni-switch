// Run with Node's test runner and Playwright on the module path; see README.md.
// #2364 (szoxidy): an MCP server turned off for every agent stayed in the
// Library's list, and its row had no Remove: the only one was inside the
// editor the row opens, and Remove all (or Select) was all the list
// offered. A server's row now has a skill's trash button, shown on hover,
// and so does a hook's (the same gap). Its click opens no editor; it asks
// first in the page (no window.confirm), naming the one it takes ("Remove
// the MCP server exa-2?") and what goes with it: the agents it is taken
// out of (none for one turned off), the projects uni-switch placed it in, its
// sign-in, and that the agents' own servers stay. Cancel posts nothing;
// Remove posts servers/remove (hooks/remove) with its name, the row goes,
// the others stay. Chromium and WebKit, 1100px and 420px, in en, zh,
// zh-TW, ja and de; no click moves the page, nothing runs off it sideways.
// No backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { strings } = require("./strings.cjs");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/szoxidy";
const agent = (id, name) => ({ id, name, icon: "", skills: `${HOME}/.${id}/skills`, mcp: `${HOME}/.${id}/mcp.json` });
const sv = (name, url, agents, more = {}) => ({ name, transport: "http", url, agents, ...more });
const hk = (name, agents) => ({ name, event: "turn-end", matcher: "", command: "notify-send done", agents });
const HOOK_AGENTS = [
  { id: "claude", file: `${HOME}/.claude/settings.json`, events: ["PreToolUse", "PostToolUse", "Notification", "Stop"] },
  { id: "codex", file: `${HOME}/.codex/hooks.json`, events: ["PreToolUse", "PostToolUse", "Stop"] },
];
const lib = (st) => ({
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: [agent("claude", "Claude Code"), agent("codex", "Codex")],
  instructions: { agents: [] }, servers: st.servers, skills: [], foundServers: [], foundSkills: [], problems: [],
  projects: [{ path: `${HOME}/work/app`, name: "app", servers: { exa: ["claude"] } }],
  hooks: st.hooks, hookAgents: HOOK_AGENTS,
});

let I18N;
// the words the page says in lang; a string missing from a language fails
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
    if (url.pathname === "/api/library") return json(lib(state));
    // every server answers, so no row is checked again under a click
    if (url.pathname === "/api/library/mcp/check") return json({ servers: Object.fromEntries(req.postDataJSON().names.map((n) => [n, { state: "ok", tools: 2 }])) });
    if (url.pathname.startsWith("/api/library/") && req.method() === "POST" && !url.pathname.endsWith("/reveal")) {
      const body = req.postDataJSON(), what = url.pathname.replace("/api/library/", "");
      posts.push({ what, body });
      if (what === "servers/remove") state.servers = state.servers.filter((x) => x.name !== body.name);
      if (what === "hooks/remove") state.hooks = state.hooks.filter((x) => x.name !== body.name);
      return json({ ...lib(state), result: { changed: ["claude"] } });
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

// a click where the thing is, as the reader's: the pointer comes over the
// row first (the trash shows on hover), and nothing on the page scrolls
const press = async (page, loc) => {
  await page.waitForFunction(() => !document.querySelector("#view-library").getAnimations({ subtree: true }).some((a) => a.effect?.getComputedTiming().endTime !== Infinity));
  const b = await loc.boundingBox();
  const scrolls = () => page.evaluate(() => [...document.querySelectorAll("*")].filter((e) => e.scrollTop).map((e) => (e.id || e.className) + ":" + e.scrollTop).join(" "));
  const top = await scrolls();
  await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2);
  await page.waitForTimeout(200);
  await page.mouse.click(b.x + b.width / 2, b.y + b.height / 2);
  await page.waitForTimeout(200);
  assert.equal(await scrolls(), top, `${loc}'s click scrolled the page`);
};
const settled = (page) => page.waitForFunction(() => !document.querySelector("#modal").getAnimations({ subtree: true }).length);
const sideways = (page) => page.evaluate(() => {
  const over = [];
  const w = document.documentElement.clientWidth;
  if (document.documentElement.scrollWidth > w) over.push("page " + document.documentElement.scrollWidth);
  const v = document.querySelector("#view-library");
  if (v.scrollWidth > v.clientWidth + 1) over.push("library " + v.scrollWidth + ">" + v.clientWidth);
  for (const e of document.querySelectorAll("#view-library .lib-server, #view-library .lib-hook, #view-library .lib-rowacts, #modal .lib-editor, #modal .lib-editor .bar > *, #modal .lib-editor .lib-confirm")) {
    const r = e.getBoundingClientRect();
    if (r.width && (r.right > w + 0.5 || r.left < -0.5)) over.push((e.className || e.tagName) + " " + Math.round(r.left) + "–" + Math.round(r.right));
  }
  return over;
});

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": one MCP server, or one hook, out of the library from its row", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    I18N = strings().I18N;
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    t.after(() => browser.close());
    for (const width of [1100, 420]) for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      await t.test(`${lang} at ${width}px`, async () => {
        const w = (key, vars) => tr(lang, key, vars);
        const state = {
          servers: [sv("exa", "https://mcp.exa.ai/mcp?tools=web_search_exa,web_fetch_exa", ["claude", "codex"], { signIn: { signedIn: true } }), sv("exa-2", "https://mcp.exa.ai/mcp", [])],
          hooks: [hk("notify", ["claude"]), hk("fmt", ["claude", "codex"])],
        };
        const posts = [], errors = [];
        const ctx = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
        // the tab the page opens on: MCP servers, then Hooks after a reload (at
        // 1100px the tabs are in the side bar)
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", sessionStorage.getItem("tab") || "mcp"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        page.on("dialog", (d) => { errors.push("a browser dialog: " + d.message()); d.dismiss(); });
        await page.route("**/*", server(lang, state, posts));
        await page.goto("http://uni-switch.test/");
        await page.locator('button[data-view="library"]').click();
        const v = page.locator("#view-library");
        const sheet = page.locator("#modal .lib-editor");
        // its name, and a tag after it (Signed in) when it has one
        const srow = (name) => v.locator(".lib-server").filter({ has: page.locator(".name", { hasText: new RegExp("^" + name + "(?!-)") }) });
        await srow("exa-2").waitFor();

        // the trash shows over the row the pointer is on, titled Remove
        const rm = srow("exa-2").locator(".lib-rowacts .lib-icon.danger");
        assert.equal(await rm.count(), 1, "exa-2's row has no Remove");
        assert.equal(await rm.getAttribute("title"), w("Remove"));

        // one turned off for every agent: no agent has it; Cancel posts nothing
        await press(page, rm);
        await sheet.waitFor();
        await settled(page);
        assert.equal(await page.locator("#modal .lib-srvedit, #modal .lib-editor input").count(), 0, "the click opened the editor");
        assert.equal((await sheet.locator(".ehead b").textContent()).trim(), w("Remove the MCP server {name}?", { name: "exa-2" }));
        const lines = async () => (await sheet.locator(".lib-confirm").allTextContents()).map((s) => s.trim());
        assert.deepEqual(await lines(), [
          w("It is taken out of the library; no agent has it."),
          w("Servers your agents have that aren't in the library stay as they are."),
        ]);
        assert.deepEqual(await sideways(page), [], "sideways with the sheet");
        await sheet.locator(".bar button", { hasText: w("Cancel") }).click();
        await sheet.waitFor({ state: "detached" });
        assert.deepEqual(posts, [], "Cancel posted");
        assert.equal(await v.locator(".lib-server").count(), 2);

        // one given to agents, placed in a project, signed in: each said
        await press(page, srow("exa").locator(".lib-rowacts .lib-icon.danger"));
        await sheet.waitFor();
        await settled(page);
        assert.equal((await sheet.locator(".ehead b").textContent()).trim(), w("Remove the MCP server {name}?", { name: "exa" }));
        assert.deepEqual(await lines(), [
          w("It is taken out of the library and out of {agents}.", { agents: "Claude Code, Codex" }),
          w("It is taken out of the {n} projects uni-switch placed it in too.", { n: 1 }),
          w("uni-switch's sign-in to it is forgotten too."),
          w("Servers your agents have that aren't in the library stay as they are."),
        ]);
        await sheet.locator(".bar button", { hasText: w("Cancel") }).click();
        await sheet.waitFor({ state: "detached" });
        assert.deepEqual(posts, [], "Cancel posted");

        // Remove: exa-2 alone goes
        await press(page, srow("exa-2").locator(".lib-rowacts .lib-icon.danger"));
        await sheet.waitFor();
        await settled(page);
        await sheet.locator(".bar button.primary").click();
        await sheet.waitFor({ state: "detached" });
        assert.deepEqual(posts.shift(), { what: "servers/remove", body: { name: "exa-2" } });
        await page.locator("#status").filter({ hasText: w("{name} removed", { name: "exa-2" }) }).waitFor();
        await srow("exa-2").waitFor({ state: "detached" });
        assert.equal(await srow("exa").count(), 1, "exa went too");
        assert.deepEqual(await sideways(page), [], "sideways on the servers");

        // a hook's row, the same
        await page.evaluate(() => sessionStorage.setItem("tab", "hooks"));
        await page.reload();
        await page.locator('button[data-view="library"]').click();
        const hrow = (name) => v.locator(".lib-hook").filter({ has: page.locator(".name", { hasText: new RegExp("^" + name + "$") }) });
        await hrow("fmt").waitFor();
        await press(page, hrow("fmt").locator(".lib-rowacts .lib-icon.danger"));
        await sheet.waitFor();
        await settled(page);
        assert.equal((await sheet.locator(".ehead b").textContent()).trim(), w("Remove the hook {name}?", { name: "fmt" }));
        assert.deepEqual(await lines(), [
          w("It is taken out of the library and out of {agents}.", { agents: "Claude Code, Codex" }),
          w("Hooks your agents have that aren't in the library stay as they are, and so does one you changed in an agent's file."),
        ]);
        assert.deepEqual(await sideways(page), [], "sideways with the hook's sheet");
        await sheet.locator(".bar button.primary").click();
        await sheet.waitFor({ state: "detached" });
        assert.deepEqual(posts.shift(), { what: "hooks/remove", body: { name: "fmt" } });
        await hrow("fmt").waitFor({ state: "detached" });
        assert.equal(await hrow("notify").count(), 1, "notify went too");
        assert.deepEqual(posts, []);
        assert.deepEqual(errors, []);
        await ctx.close();
      });
    }
  });
}

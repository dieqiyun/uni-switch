// Run with Node's test runner and Playwright on the module path; see README.md.
// packinggoodsllc_07668 on Discord: two uni-switchs, one in China and one in
// Los Angeles, serve the same MCP tools through their routers, and an
// agent fetching Google should pick the one in Los Angeles. With the
// router on, the Library's MCP tab has a Router name row under the
// router's address: a field holding what the router says of itself to
// agents, with this computer's name as its placeholder (what agents on
// other computers are told with it empty). Typing and Enter posts
// servers/hub-note with the words, one line; Escape puts the saved words
// back and posts nothing; emptying it posts "" and says so. Off, there is
// no row. A Chinese input method's Enter is the IME's, not a save. In
// every language, both engines, wide and narrow: no sideways scroll, the
// field inside its row.
// No backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { strings } = require("./strings.cjs");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/mg";
const fixture = (st) => ({
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  mcpHub: st.on, hubPath: "/mcp/uni-switch/hub/", hubNote: st.note, hubName: "la-box",
  agents: [{ id: "claude", name: "Claude Code", icon: "claudecode-color", skills: `${HOME}/.claude/skills`, mcp: `${HOME}/.claude.json`, ...(st.on ? { hub: "on" } : {}) }],
  instructions: { agents: [], sets: [] }, foundServers: [], projects: [], foundSkills: [], skills: [], problems: [],
  servers: [{ name: "fetch", transport: "stdio", command: "uvx", args: ["mcp-server-fetch"], agents: ["claude"] }],
});

let I18N;
function tr(lang, key, vars = {}) {
  let s = key;
  if (lang !== "en") {
    assert.equal(typeof I18N[lang][key], "string", `${lang} has no ${JSON.stringify(key)}`);
    s = I18N[lang][key];
  }
  return s.replace(/\{(\w+)\}/g, (m, k) => (k in vars ? String(vars[k]) : m));
}

function server(lang, st) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:${st.web}};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light", gateway: "http://127.0.0.1:3425", publicURL: "https://mac.tail0.ts.net" } });
    if (url.pathname === "/api/library") return json(fixture(st));
    if (url.pathname === "/api/library/servers/hub-note") {
      const body = req.postDataJSON();
      st.sent.push(body);
      st.note = body.note;
      return json(fixture(st));
    }
    if (url.pathname === "/api/library/mcp/check") return json({ servers: Object.fromEntries(req.postDataJSON().names.map((n) => [n, { state: "ok", tools: 1 }])) });
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

const shot = async (page, name) => {
  if (!process.env.ARTIFACT_DIR) return;
  await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
  await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `router-note-${name}.png`) });
};

const sideways = (page) => page.evaluate(() => {
  const over = [];
  const w = document.documentElement.clientWidth;
  if (document.documentElement.scrollWidth > w) over.push("page " + document.documentElement.scrollWidth);
  for (const e of document.querySelectorAll("#view-library .lib-hubnoterow, #view-library .lib-hubnoterow .lib-hubname, #view-library .lib-hubnoterow .who")) {
    const r = e.getBoundingClientRect();
    if (r.width && (r.right > w + 0.5 || r.left < -0.5)) over.push(e.className + " " + Math.round(r.left) + "–" + Math.round(r.right));
  }
  const row = document.querySelector("#view-library .lib-hubnoterow")?.getBoundingClientRect();
  const f = document.querySelector("#view-library .lib-hubname")?.getBoundingClientRect();
  if (row && f && (f.right > row.right + 0.5 || f.top < row.top - 0.5 || f.bottom > row.bottom + 0.5)) over.push("field out of its row");
  return over;
});

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": the router's name for agents", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    I18N = strings().I18N;
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    t.after(() => browser.close());

    for (const width of [1100, 420]) for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      await t.test(`${lang} ${width}px`, async () => {
        const w = (key, vars) => tr(lang, key, vars);
        const st = { on: false, note: "", sent: [], web: width < 800 };
        const errors = [];
        const ctx = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "mcp"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, st));
        await page.goto("http://uni-switch.test/");
        await page.locator('button[data-view="library"]').click();
        const v = page.locator("#view-library");
        await v.locator(".lib-hub").waitFor();
        // off: no row
        assert.equal(await v.locator(".lib-hubnoterow").count(), 0);
        await ctx.close();

        st.on = true;
        const ctx2 = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
        await ctx2.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "mcp"); } catch {} });
        const p = await ctx2.newPage();
        p.setDefaultTimeout(5000);
        p.setDefaultNavigationTimeout(60000);
        p.on("pageerror", (e) => errors.push(e.message));
        await p.route("**/*", server(lang, st));
        await p.goto("http://uni-switch.test/");
        await p.locator('button[data-view="library"]').click();
        const row = p.locator("#view-library .lib-hubnoterow");
        await row.waitFor();
        // under the router's address
        const after = await p.evaluate(() => {
          const at = document.querySelector("#view-library .lib-hubat").getBoundingClientRect();
          return document.querySelector("#view-library .lib-hubnoterow").getBoundingClientRect().top >= at.bottom - 0.5;
        });
        assert.ok(after, "the row isn't under the address");
        assert.equal((await row.locator(".name").textContent()).trim(), w("Router name"));
        assert.equal((await row.locator(".sub").textContent()).trim(), w("Agents connected to more than one router tell them apart by it. Empty, agents on other computers are told this computer's name."));
        const f = row.locator("input.lib-hubname");
        assert.equal(await f.getAttribute("placeholder"), "la-box");
        assert.equal(await f.getAttribute("aria-label"), w("Router name"));
        assert.equal(await f.inputValue(), "");
        assert.equal(await p.locator("#view-library select").count(), 0);
        assert.deepEqual(await sideways(p), [], "sideways scroll");
        await shot(p, `${engine}-${lang}-${width}-empty`);

        // typed, Enter: saved one line
        await f.fill("  Los   Angeles ");
        await f.press("Enter");
        await p.waitForFunction((x) => document.querySelector("#status")?.textContent === x, w("Agents are told this router is {name} — restart them to read it", { name: "Los Angeles" }));
        assert.deepEqual(st.sent, [{ note: "Los Angeles" }]);
        const f2 = p.locator("#view-library input.lib-hubname");
        await p.waitForFunction(() => document.querySelector("#view-library input.lib-hubname")?.value === "Los Angeles");
        // Escape puts the saved words back, and posts nothing
        await f2.fill("China");
        await f2.press("Escape");
        assert.equal(await f2.inputValue(), "Los Angeles");
        // leaving it unchanged posts nothing
        await f2.focus();
        await f2.blur();
        assert.equal(st.sent.length, 1);
        // an input method's Enter ends its composition, not the field
        await f2.focus();
        await f2.dispatchEvent("keydown", { key: "Enter", keyCode: 229, isComposing: true });
        assert.equal(st.sent.length, 1);
        assert.ok(await f2.evaluate((e) => document.activeElement === e), "an IME's Enter left the field");
        await f2.blur();
        // emptied: this computer's name again
        await f2.fill("");
        await f2.press("Enter");
        await p.waitForFunction((x) => document.querySelector("#status")?.textContent === x, w("Agents on other computers are told this computer's name — restart them to read it"));
        assert.deepEqual(st.sent, [{ note: "Los Angeles" }, { note: "" }]);
        assert.deepEqual(await sideways(p), [], "sideways scroll, after");
        assert.deepEqual(errors, []);
        await ctx2.close();
      });
    }
  });
}

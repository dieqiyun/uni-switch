// Run with Node's test runner and Playwright on the module path; see README.md.
// cybercaius on Discord: with WSL not started, the Library's MCP page
// didn't list the agents installed in it. A stopped distro's agents
// (asleep) are listed by what uni-switch last saw there: a chip is lit as the
// library has it, quietly greyer, and says the distro isn't running; a
// click is sent as any is, and uni-switch writes it there when the distro
// next runs. One whose places uni-switch never saw (unseen) is only on the
// rows that name it, greyed out, kept by every click on the row, and a
// click on it says why. One line under the list names the distro. Nothing
// here starts WSL (#3896): the page only reads /api/library. The
// Instructions page lists them the same way (#4182), and the Hooks and
// Subagents pages as while the distro runs (#4237, #4238). Chromium and
// WebKit, 1100px and 390px, in en, zh, zh-TW, ja and de. No backend: the
// API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { strings } = require("./strings.cjs");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/me", D = "Ubuntu-24.04";
const CX = "codex@wsl:" + D, PI = "pi@wsl:" + D;
const srv = (name, agents, more = {}) => ({ name, transport: "stdio", command: "uvx", args: [name], agents, ...more });
const lib = (servers, instructions = { agents: [] }) => ({
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: [
    { id: "claude", name: "Claude Code", icon: "", instructions: `${HOME}/.claude/CLAUDE.md`, mcp: `${HOME}/.claude.json`, skills: `${HOME}/.claude/skills` },
    { id: CX, name: "Codex · WSL " + D, icon: "", mcp: "\\\\wsl.localhost\\" + D + "\\home\\me\\.codex\\config.toml", skills: "\\\\wsl.localhost\\" + D + "\\home\\me\\.codex\\skills", noSSE: true, mustCopy: true, how: "copy", asleep: D },
    { id: PI, name: "Pi · WSL " + D, icon: "", asleep: D, unseen: true },
  ],
  instructions, servers, skills: [], foundServers: [], projects: [], foundSkills: [], problems: [],
});
// the Instructions page's rows: Claude Code here, Codex in the stopped
// distro at the file uni-switch last saw, and Pi on there, its file never seen
const instr = (on) => ({
  shared: "Be brief.", sets: [{ id: "default", name: "", active: true, text: "Be brief." }],
  agents: [
    { agent: "claude", name: "Claude Code", icon: "", path: `${HOME}/.claude/CLAUDE.md`, on: on.includes("claude"), extra: "" },
    { agent: CX, name: "Codex · WSL " + D, icon: "", path: "\\\\wsl.localhost\\" + D + "\\home\\me\\.codex\\AGENTS.md", on: on.includes(CX), extra: "", asleep: D },
    { agent: PI, name: "Pi · WSL " + D, icon: "", path: "", on: on.includes(PI), extra: "", asleep: D, unseen: true },
  ],
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

function server(lang, state, posts) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data, status = 200) => route.fulfill({ status, json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/library") return json({ ...lib(state.servers, state.instr), ...state.more });
    if (url.pathname === "/api/library/reveal") { (state.reveals ||= []).push(req.postDataJSON()); return json({}); }
    if (url.pathname.startsWith("/api/library/") && req.method() === "POST" && !url.pathname.includes("/check")) {
      const body = req.postDataJSON(), what = url.pathname.replace("/api/library/", "");
      posts.push({ what, body });
      if (what === "instructions/save" && state.instr) {
        state.instr = instr(body.agents);
        return json({ part: "instructions", instructions: state.instr, result: { changed: [] } });
      }
      if (what === "servers/agents") {
        state.servers = state.servers.map((s) => (s.name === body.name ? { ...s, agents: [...body.agents].sort() } : s));
        return json({ ...lib(state.servers), result: { changed: [] } });
      }
      return json(lib(state.servers));
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
  for (const e of document.querySelectorAll("#view-library .lib-server, #view-library .lib-agents, #view-library .lib-asleep")) {
    const r = e.getBoundingClientRect();
    if (r.width && (r.right > w + 0.5 || r.left < -0.5)) over.push((e.className || e.tagName) + " " + Math.round(r.left) + "–" + Math.round(r.right));
  }
  return over;
});

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a stopped WSL distro's agents are on the MCP page as last seen", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    I18N = strings().I18N;
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    t.after(() => browser.close());
    for (const width of [1100, 390]) for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      await t.test(`${lang} at ${width}px`, async () => {
        const w = (key, vars) => tr(lang, key, vars);
        const state = { servers: [srv("fetch", [CX, PI]), srv("git", ["claude"]), srv("events", [], { transport: "sse", command: "", url: "https://example.invalid/sse" })] };
        const posts = [], errors = [];
        const ctx = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "mcp"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        page.on("dialog", (d) => { errors.push("a browser dialog: " + d.message()); d.dismiss(); });
        await page.route("**/*", server(lang, state, posts));
        await page.goto("http://uni-switch.test/");
        await page.locator('button[data-view="library"]').click();
        const v = page.locator("#view-library");
        const row = (name) => v.locator(".lib-server").filter({ has: page.locator(".name", { hasText: new RegExp("^" + name) }) });
        const chips = (name) => row(name).locator(":scope > .lib-agents > .lib-ag[data-agent]").evaluateAll((cs) => cs.map((c) => c.dataset.agent));
        const chip = (name, id) => row(name).locator(`.lib-ag[data-agent="${id}"]`);
        await row("git").waitFor();

        // Codex in the stopped distro is on every row, Pi only on fetch's
        assert.deepEqual(await chips("fetch"), ["claude", CX, PI]);
        assert.deepEqual(await chips("git"), ["claude", CX]);
        assert.deepEqual(await chips("events"), ["claude", CX]);
        const cx = chip("fetch", CX);
        assert.match(await cx.getAttribute("class"), /\basleep\b/);
        assert.equal(await cx.getAttribute("aria-pressed"), "true");
        assert.equal(await cx.getAttribute("aria-disabled"), null);
        assert.equal(await cx.getAttribute("title"), w("WSL {distro} isn't running: {agent} has it — click to take it away, and uni-switch writes the change there the next time it runs", { distro: D, agent: "Codex · WSL " + D }));
        assert.equal(await chip("git", CX).getAttribute("title"), w("WSL {distro} isn't running: a click gives it to {agent} the next time the distro runs", { distro: D, agent: "Codex · WSL " + D }));
        // what it can't take says so, as on any chip: no SSE for Codex
        assert.match(await chip("events", CX).getAttribute("class"), /\bblocked\b/);
        assert.notEqual(await chip("events", CX).getAttribute("title"), await chip("git", CX).getAttribute("title"));
        const pi = chip("fetch", PI);
        assert.match(await pi.getAttribute("class"), /\bunseen\b/);
        assert.equal(await pi.getAttribute("aria-disabled"), "true");
        assert.equal(await pi.getAttribute("aria-pressed"), "true");
        const unseen = w("WSL {distro} isn't running, and uni-switch hasn't seen {agent}'s files there yet: start the distro to change this", { distro: D, agent: "Pi · WSL " + D });
        assert.equal(await pi.getAttribute("title"), unseen);
        // one line for the distro, naming Pi; the line of agents with no
        // MCP servers doesn't
        const aside = v.locator(".lib-asleep");
        assert.equal(await aside.count(), 1);
        assert.equal(await aside.textContent(), w("WSL {distro} isn't running: its agents show what uni-switch last saw there, and what you change is written there the next time it runs.", { distro: D }) + " " + w("{agents} can be changed once it runs.", { agents: "Pi · WSL " + D }));
        assert.equal(await v.locator(".lib-aside").filter({ hasText: w("{agents} has no MCP servers uni-switch can write.", { agents: "Pi · WSL " + D }) }).count(), 0);
        assert.deepEqual(await sideways(page), [], "sideways with the rows");

        // Pi's chip clicked: nothing sent, the reason shown
        await row("fetch").locator(":scope > .lib-agents").hover();
        await pi.click({ force: true });
        await page.waitForTimeout(200);
        assert.deepEqual(posts, []);
        // Claude Code given fetch: Pi keeps it, as Codex does
        await chip("fetch", "claude").click();
        await page.waitForTimeout(300);
        assert.deepEqual(posts.map((p) => p.what), ["servers/agents"]);
        assert.deepEqual([...posts[0].body.agents].sort(), ["claude", CX, PI]);
        // Codex given git while its distro is stopped: sent, kept
        await row("git").locator(":scope > .lib-agents").hover();
        await chip("git", CX).click();
        await page.waitForTimeout(300);
        assert.deepEqual(posts[1], { what: "servers/agents", body: { name: "git", agents: ["claude", CX] } });
        assert.deepEqual(errors, []);
        await ctx.close();
      });
    }
  });
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  // #4182: the Instructions page leaves them out no more. Codex's row is
  // at the file uni-switch last saw, greyer, its path no button (the file
  // manager would start WSL); its switch says the distro isn't running and
  // is sent as any is. Pi, on, whose file uni-switch never saw, is greyed out
  // and kept on by every switch: before, a switch sent the agents without
  // the stopped distro's, which took them off.
  test(engine + ": a stopped WSL distro's agents are on the Instructions page as last seen", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    I18N = strings().I18N;
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    t.after(() => browser.close());
    for (const width of [1100, 390]) for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      await t.test(`${lang} at ${width}px`, async () => {
        const w = (key, vars) => tr(lang, key, vars);
        const state = { servers: [], instr: instr(["claude", PI]) };
        const posts = [], errors = [];
        const ctx = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "instructions"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.on("pageerror", (e) => errors.push(e.message));
        page.on("dialog", (d) => { errors.push("a browser dialog: " + d.message()); d.dismiss(); });
        await page.route("**/*", server(lang, state, posts));
        await page.goto("http://uni-switch.test/");
        await page.locator('button[data-view="library"]').click();
        const v = page.locator("#view-library");
        const row = (name) => v.locator(".lib-row:not(.lib-set)").filter({ has: page.locator(".name", { hasText: new RegExp("^" + name) }) });
        const sw = (name) => row(name).locator(".lib-switch");
        const asleep = w("WSL {distro} isn't running: its agents show what uni-switch last saw there, and what you change is written there the next time it runs.", { distro: D });
        await row("Codex").waitFor();

        assert.doesNotMatch(await row("Claude Code").getAttribute("class"), /\basleep\b/);
        assert.match(await row("Codex").getAttribute("class"), /\basleep\b/);
        assert.equal(await row("Codex").locator(".lib-path").evaluate((e) => e.tagName), "SPAN");
        assert.equal(await sw("Codex").getAttribute("aria-checked"), "false");
        assert.equal(await sw("Codex").getAttribute("aria-disabled"), null);
        assert.equal(await sw("Codex").getAttribute("title"), asleep);
        assert.match(await row("Pi").getAttribute("class"), /\basleep\b/);
        assert.equal(await row("Pi").locator(".lib-path").count(), 0);
        assert.equal(await sw("Pi").getAttribute("aria-checked"), "true");
        assert.equal(await sw("Pi").getAttribute("aria-disabled"), "true");
        assert.equal(await sw("Pi").getAttribute("title"), w("WSL {distro} isn't running, and uni-switch hasn't seen {agent}'s files there yet: start the distro to change this", { distro: D, agent: "Pi · WSL " + D }));
        // one line for the distro, naming Pi; neither is said to keep no
        // instructions file
        const aside = v.locator(".lib-asleep");
        assert.equal(await aside.count(), 1);
        assert.equal(await aside.textContent(), asleep + " " + w("{agents} can be changed once it runs.", { agents: "Pi · WSL " + D }));
        for (const x of await v.locator(".lib-aside:not(.lib-asleep)").allTextContents()) assert.doesNotMatch(x, /WSL/);
        assert.deepEqual(await sideways(page), [], "sideways with the rows");

        // Codex's path clicked: no file manager asked for
        await row("Codex").locator(".lib-path").click();
        await page.waitForTimeout(200);
        assert.deepEqual(state.reveals || [], []);
        // Pi's switch clicked: nothing sent
        await sw("Pi").click({ force: true });
        await page.waitForTimeout(200);
        assert.deepEqual(posts, []);
        // Codex switched on while its distro is stopped: sent, Pi kept on,
        // and the status says when it is written
        await sw("Codex").click();
        await page.waitForFunction((want) => document.querySelector("#status").textContent === want, asleep).catch(() => {});
        assert.deepEqual(posts.map((p) => p.what), ["instructions/save"]);
        assert.deepEqual(posts[0].body, { agents: ["claude", CX, PI] });
        assert.equal(await page.locator("#status").textContent(), asleep);
        assert.equal(await sw("Codex").getAttribute("aria-checked"), "true");
        // Claude Code switched off: both of the stopped distro's stay on
        await sw("Claude Code").click();
        await page.waitForTimeout(300);
        assert.deepEqual(posts[1], { what: "instructions/save", body: { agents: [CX, PI] } });
        assert.deepEqual(errors, []);
        await ctx.close();
      });
    }
  });
}

// #4237, #4238: the Hooks and Subagents pages list the stopped distro's
// Codex as they do while the distro runs: its chip greyed, saying uni-switch
// gives no agent in WSL any yet, named in the line under the list, and a
// click on it sends nothing. Pi, whose places uni-switch never saw, isn't on
// a row that doesn't name it. The page draws what /api/library says
// (hooksView and subagentsView list the stopped agents); this holds it to
// that.
const GREYED = {
  hooks: {
    lib: { hooks: [{ name: "notify", event: "turn-end", command: "notify-send done", agents: ["claude"] }], hookAgents: [{ id: "claude", file: `${HOME}/.claude/settings.json`, events: ["Stop"] }, { id: CX, no: "uni-switch gives hooks only to agents on this computer, not yet to Codex · WSL " + D + " in WSL", wsl: true }] },
    row: ".lib-hook", name: "notify",
    tip: "uni-switch gives hooks only to agents on this computer, not yet to {agent} in WSL",
    line: [".lib-hooks-no", "uni-switch can't give hooks to {agents} yet: their chips are greyed, and say why."],
  },
  subagents: {
    lib: { subagents: [{ name: "reviewer", description: "Reviews a diff", prompt: "Review it.", skills: [], mcp: [], agents: ["claude"] }], subagentAgents: [{ id: "claude", dir: `${HOME}/.claude/agents`, skills: true, mcp: true }, { id: CX, no: "uni-switch gives no subagents to an agent in WSL yet", wsl: true }] },
    row: ".lib-subagent", name: "reviewer",
    tip: "uni-switch gives subagents only to agents on this computer, not yet to {agent} in WSL",
    line: [".lib-subagents-no", "uni-switch can't give subagents to {agents} yet: their chips are greyed, and say why."],
  },
};
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) for (const [tab, g] of Object.entries(GREYED)) {
  test(engine + `: a stopped WSL distro's agents are on the ${tab} page, greyed as while it runs`, async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    I18N = strings().I18N;
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    t.after(() => browser.close());
    for (const width of [1100, 390]) for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      await t.test(`${lang} at ${width}px`, async () => {
        const w = (key, vars) => tr(lang, key, vars);
        const state = { servers: [], more: g.lib };
        const posts = [], errors = [];
        const ctx = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
        await ctx.addInitScript((tab) => { try { localStorage.setItem("uni-switch.libTab", tab); } catch {} }, tab);
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.on("pageerror", (e) => errors.push(e.message));
        page.on("dialog", (d) => { errors.push("a browser dialog: " + d.message()); d.dismiss(); });
        await page.route("**/*", server(lang, state, posts));
        await page.goto("http://uni-switch.test/");
        await page.locator('button[data-view="library"]').click();
        const v = page.locator("#view-library");
        const row = v.locator(g.row).filter({ has: page.locator(".name", { hasText: new RegExp("^" + g.name) }) });
        await row.waitFor();
        assert.deepEqual(await row.locator(":scope > .lib-agents > .lib-ag[data-agent]").evaluateAll((cs) => cs.map((c) => c.dataset.agent)), ["claude", CX]);
        const cx = row.locator(`.lib-ag[data-agent="${CX}"]`);
        assert.match(await cx.getAttribute("class"), /\bblocked\b/);
        assert.match(await cx.getAttribute("class"), /\basleep\b/);
        assert.equal(await cx.getAttribute("aria-disabled"), "true");
        assert.equal(await cx.getAttribute("aria-pressed"), "false");
        assert.equal(await cx.getAttribute("title"), w(g.tip, { agent: "Codex · WSL " + D }));
        assert.equal(await v.locator(g.line[0]).textContent(), w(g.line[1], { agents: "Codex · WSL " + D }));
        assert.deepEqual(await sideways(page), [], "sideways with the rows");
        await row.locator(":scope > .lib-agents").hover();
        await cx.click({ force: true });
        await page.waitForTimeout(200);
        assert.deepEqual(posts, []);
        assert.deepEqual(errors, []);
        await ctx.close();
      });
    }
  });
}

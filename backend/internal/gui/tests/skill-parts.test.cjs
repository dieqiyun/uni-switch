// Run with Node's test runner and Playwright on the module path; see README.md.
// The parts of the skills on this computer (#791, mintonight: 没有看到自动
// 分组). Skills whose names start alike are a part of their own wherever
// they sit: lark-* beside skills in another folder, and a few lark-* among
// many, each get a lark heading, and the rest go by folder or under Others.
// English and Chinese; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/me";
const agent = (id, name, icon) => ({ id, name, icon, skills: `${HOME}/.${id}/skills`, mcp: `${HOME}/.${id}/mcp.json` });
const local = (dir) => (name) => ({ name, kind: "folder", source: `${HOME}/${dir}/${name}`, description: "d", agents: ["claude"] });

function server(lang, state, posts) {
  return async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/library") return json(state.lib);
    if (url.pathname === "/api/library/skills/agents-some") {
      const b = req.postDataJSON();
      posts.push(b);
      for (const s of state.lib.skills) {
        if (!b.names.includes(s.name)) continue;
        const kept = s.agents.filter((a) => !b.agents.includes(a));
        s.agents = (b.on ? [...kept, ...b.agents] : kept).sort();
      }
      return json({ ...state.lib, result: { changed: b.agents } });
    }
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname.startsWith("/api/")) return json({});
    if (url.host !== "uni-switch.test") return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const LARK = ["lark-apps", "lark-base", "lark-doc", "lark-drive", "lark-im", "lark-wiki"];
const MORE = ["brainstorm", "canvas", "debugging", "docx", "notes", "pdf", "planning", "review", "tdd", "websearch", "writing", "xlsx"];
const L = { en: { others: "Others" }, zh: { others: "其他" } };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": skills that start alike have a part of their own", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    t.after(() => browser.close());
    const cases = [
      // lark-* among skills in two folders: lark, then each folder's rest
      { skills: [...LARK.map(local(".agents/skills")), ...MORE.slice(0, 4).map(local(".agents/skills")), ...MORE.slice(4).map(local(".claude/skills"))],
        heads: () => [["lark", "6"], ["~/.agents/skills", "4"], ["~/.claude/skills", "8"]] },
      // three lark-* among many in one folder: lark and the rest
      { skills: [...LARK.slice(0, 3), ...MORE, ...MORE.map((n) => n + "2"), ...MORE.map((n) => n + "3")].map(local(".agents/skills")),
        heads: (w) => [["lark", "3"], [w.others, "36"]] },
    ];
    for (const lang of ["en", "zh"]) {
      for (const [i, c] of cases.entries()) {
        await t.test(lang + " " + i, async () => {
          const state = {
            lib: {
              dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
              agents: [agent("claude", "Claude Code", "claudecode-color"), agent("codex", "Codex", "openai")],
              instructions: { agents: [], sets: [] }, servers: [], foundServers: [], projects: [], foundSkills: [], problems: [],
              skills: c.skills,
            },
          };
          const ctx = await browser.newContext({ viewport: { width: 1100, height: 760 }, reducedMotion: "reduce" });
          await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "skills"); } catch {} });
          const page = await ctx.newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          const errors = [];
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", server(lang, state, []));
          await page.goto("http://uni-switch.test/");
          await page.locator('button[data-view="library"]').click();
          const v = page.locator("#view-library");
          await v.locator(".lib-subhead").first().waitFor();
          const heads = await v.locator(".lib-subhead").evaluateAll((hs) => hs.map((h) => [h.querySelector(".name").textContent, h.querySelector(".sub").textContent]));
          assert.deepEqual(heads, c.heads(L[lang]));
          assert.deepEqual(errors, []);
          await ctx.close();
        });
      }
    }
  });
}

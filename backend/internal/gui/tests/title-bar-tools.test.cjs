// Run with Node's test runner and Playwright on the module path; see README.md.
// #2625 (linusxiong): beside the sidebar, a page whose head is only its
// tools (Library, Plugins, Settings) has them in the title bar, after
// where the reader is ("Work / Library / MCP servers"). In a narrow
// window the tools ran on to the left, over the title: 「MCP 服务器」's
// last character sat on 「发现」. Now nothing in the title bar lies over
// anything else, the tools keep inside it, and the title shows whole
// wherever the tools can make room for it by showing only their icons;
// a tool shown as its icon keeps its name for the reader and a tooltip.
// Every Library tab, Plugins' and Settings', the window from its least
// width (560px) up, the sidebar picked, in all five languages, Chromium
// and WebKit. No backend: the API is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(process.env.UNI_SWITCH_GUI_ASSETS || path.join(__dirname, "../assets"));
const HOME = "/Users/tester";
const agent = (id, name, icon) => ({ id, name, icon, skills: `${HOME}/.${id}/skills`, mcp: `${HOME}/.${id}/mcp.json` });

function serve(lang) {
  const view = {
    dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
    agents: [agent("claude", "Claude Code", "claudecode-color")],
    instructions: { agents: [], sets: [] },
    servers: [{ name: "filesystem", transport: "stdio", command: "npx", args: ["-y", "fs"], agents: ["claude"] }],
    foundServers: [], projects: [], foundSkills: [], problems: [], hooks: [], subagents: [],
    skills: [{ name: "pdf", kind: "folder", description: "PDFs", agents: ["claude"], source: `${HOME}/skills/pdf` }],
  };
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/plugins") return json({ plugins: [], mirror: false });
    if (url.pathname === "/api/library") return json(view);
    if (url.pathname === "/api/library/market/servers") return json({ items: [{ id: "fetch", name: "fetch", title: "Fetch", description: "Read the web.", transport: "stdio", runs: "uvx", featured: true }] });
    if (url.pathname === "/api/library/market/skills") return json({ items: [] });
    if (url.pathname === "/api/library/rtk") return json({ path: `${HOME}/.local/bin/rtk`, version: "0.51.0", url: "https://www.rtk-ai.app", agents: [{ id: "claude", name: "Claude Code", icon: "", on: true }] });
    if (url.pathname === "/api/plugins/builtin") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, url: "http://127.0.0.1:3425" } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    const body = await fs.readFile(file).catch(() => null);
    await (body ? route.fulfill({ body, contentType }) : route.fulfill({ status: 404, body: "" }));
  };
}

// what the title bar shows, once it has settled: the title's parts as
// far as they show, each tool, and the window's own buttons
function bar(head) {
  const box = (r) => ({ left: Math.round(r.left), right: Math.round(r.right), top: Math.round(r.top), bottom: Math.round(r.bottom) });
  const crumb = document.querySelector("#crumb"), c = crumb.getBoundingClientRect();
  const parts = [...crumb.children].map((e) => {
    const r = e.getBoundingClientRect();
    return { text: e.textContent, shown: r.width > 0, right: Math.min(r.right, c.right), cut: r.right > c.right + 0.5 || e.scrollWidth > e.clientWidth + 0.5 };
  });
  const h = document.querySelector(head);
  const tools = [...h.querySelectorAll("button, label, input")].filter((e) => e.getClientRects().length && getComputedStyle(e).visibility !== "hidden" && !e.closest("[hidden]") && !e.closest(".lib-tabs, .set-tabs"))
    .filter((e) => !e.parentElement.closest("button, label"))
    .map((e) => {
      const label = [...e.querySelectorAll("span")].find((s) => s.textContent.trim() && !s.closest(".lib-mini"));
      return {
        ...box(e.getBoundingClientRect()), aria: e.getAttribute("aria-label"), title: e.title,
        label: label ? label.getClientRects().length > 0 : null, clipped: e.scrollWidth > e.clientWidth + 1,
      };
    });
  const acts = document.querySelector(".top .actions").getBoundingClientRect();
  return { parts, crumb: box(c), tools, acts: box(acts), side: document.querySelector("#side").getBoundingClientRect().right, view: innerWidth };
}

const pages = [
  ...["instructions", "mcp", "skills", "hooks", "subagents", "rtk"].map((tab) => ({ name: "library " + tab, url: "?view=library", head: "#view-library .lib-head", init: ["uni-switch.libTab", tab] })),
  ...["builtin", "discover", "installed"].map((tab) => ({ name: "plugins " + tab, url: "?view=plugins", head: "#view-plugins .pm-head", init: ["uni-switch.pluginTab", tab] })),
  { name: "settings", url: "?view=settings", head: "#view-settings .set-head" },
];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["zh", "zh-TW", "ja", "de", "en"]) {
    test(`${engine} ${lang}: the title bar's tools never lie over where the reader is`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      for (const width of [560, 650, 760, 900, 1024, 1280]) {
        const context = await browser.newContext({ viewport: { width, height: 640 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        let on = "";
        // going from page to page cancels the reads still on their way, and
        // WebKit reports each one as a page error, "Fetch API cannot load …
        // due to access control checks" (#1350, #1307): not this test's
        page.on("pageerror", (e) => {
          if (engine === "webkit" && /^Fetch API cannot load .* due to access control checks/.test(e.message)) return;
          errors.push(`${width}px ${on}: ${e.stack}`);
        });
        await page.route("**/*", serve(lang));
        await page.addInitScript(() => { localStorage.setItem("uni-switch.navLayout", "side"); });
        for (const p of pages) {
          on = p.name;
          if (p.init) await page.evaluate(([k, v]) => localStorage.setItem(k, v), p.init).catch(() => {});
          await page.goto("http://uni-switch.test/" + p.url);
          if (p.init) { await page.evaluate(([k, v]) => localStorage.setItem(k, v), p.init); await page.reload(); }
          await page.locator(p.head).waitFor();
          await page.locator("#crumb b").waitFor();
          // the Library's buttons have their tooltips once it has loaded
          if (p.name.startsWith("library")) await page.locator("#view-library .lib-more:not([disabled])").waitFor();
          if (p.name === "library mcp") await page.locator("#view-library .lib-discover").waitFor();
          if (/plugins (discover|installed)/.test(p.name)) await page.locator("#view-plugins .pm-mirror").waitFor();
          // settled: two frames with the same bar
          let b = null;
          for (let i = 0; i < 40; i++) {
            await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
            const now = await page.evaluate(bar, p.head);
            if (b && JSON.stringify(now) === JSON.stringify(b)) break;
            b = now;
          }
          const at = `${width}px ${p.name}: ${JSON.stringify(b)}`;
          // Plugins' Built-in has no search and no mirrors to pick
          assert(b.tools.length > 0 || p.name === "plugins builtin", `no tools in the bar at ${at}`);
          const title = b.parts[b.parts.length - 1];
          const shown = Math.max(...b.parts.map((x) => x.right));
          for (const tool of b.tools) {
            assert(tool.left >= shown + 8, `a tool lies over the title at ${at}`);
            assert(tool.left >= b.side && tool.right <= b.acts.left, `a tool runs out of the bar at ${at}`);
            assert(!tool.clipped, `a tool's label is cut at ${at}`);
            if (tool.label === false) assert(tool.aria && tool.title, `a tool shown as its icon has no name or tooltip at ${at}`);
          }
          for (let i = 1; i < b.tools.length; i++) {
            const [x, y] = [b.tools[i - 1], b.tools[i]].sort((m, n) => m.left - n.left);
            assert(x.right <= y.left, `two tools lie over each other at ${at}`);
          }
          assert(b.crumb.right <= b.acts.left, `the title runs under the window's buttons at ${at}`);
          // the page the reader is on shows, if its start only; the parts
          // before it go first
          assert(title.shown && title.right - (b.crumb.left) > 0, `the page's name is gone at ${at}`);
          const kept = b.parts.findIndex((x) => x.shown);
          assert(b.parts.slice(kept).every((x) => x.shown), `the title lost a part in its middle at ${at}`);
          if (title.cut) assert(b.parts.slice(0, -1).every((x) => !x.shown), `the page's name is cut while parts before it show at ${at}`);
          // where the tools have their icons alone, nothing more of them
          // can give way: the title shows whole wherever it fits beside them
          if (width >= 650) assert(!title.cut, `the title is cut at ${at}`);
        }
        assert.deepEqual(errors, []);
        await context.close();
      }
    });
  }
}

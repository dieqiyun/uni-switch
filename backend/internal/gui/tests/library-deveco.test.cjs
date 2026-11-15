// Run with Node's test runner and Playwright on the module path; see README.md.
// DevEco Code (#1885) is OpenCode underneath: with no AGENTS.md of its own
// it reads Claude Code's CLAUDE.md, and the Library says so on its
// instructions row, as it does for OpenCode and MiMo Code. Its icon is the
// official logo and draws. Chromium and WebKit, every language; no backend,
// the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/foobra";
const library = {
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: [{ id: "deveco", name: "DevEco Code", icon: "deveco-color", instructions: `${HOME}/.config/deveco/AGENTS.md` }],
  instructions: {
    shared: "Be brief.",
    sets: [{ id: "default", name: "", active: true, text: "Be brief." }],
    agents: [{ agent: "deveco", name: "DevEco Code", icon: "deveco-color", path: `${HOME}/.config/deveco/AGENTS.md`, on: true, extra: "" }],
  },
  foundServers: [], projects: [], foundSkills: [], skills: [], servers: [],
};

function server(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/library") return route.fulfill({ json: library });
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const view = "#view-library";
const note = {
  en: "DevEco Code reads Claude Code's CLAUDE.md when it has no AGENTS.md of its own.",
  zh: "DevEco Code 在没有自己的 AGENTS.md 时会读取 Claude Code 的 CLAUDE.md。",
  "zh-TW": "DevEco Code 在沒有自己的 AGENTS.md 時會讀取 Claude Code 的 CLAUDE.md。",
  ja: "DevEco Code は独自の AGENTS.md がないとき、Claude Code の CLAUDE.md を読み込みます。",
  de: "DevEco Code liest die CLAUDE.md von Claude Code, wenn es keine eigene AGENTS.md hat.",
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": DevEco Code's instructions row says it reads CLAUDE.md, and its icon draws", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const lang of Object.keys(note)) {
      await t.test(lang, async () => {
        const ctx = await browser.newContext({ viewport: { width: 980, height: 860 }, reducedMotion: "reduce" });
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "instructions"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang));
        await page.goto("http://uni-switch.test/");
        await page.locator('button[data-view="library"]').click();
        const row = page.locator(`${view} .lib-row`, { hasText: "DevEco Code" });
        await row.first().click();
        await page.locator(`${view} p.lib-aside`, { hasText: note[lang] }).waitFor();
        // the icon as the GUI draws a -color one: an <img> of icons/deveco-color.svg
        const drawn = await page.evaluate(async () => {
          const img = new Image();
          img.src = "icons/deveco-color.svg";
          try { await img.decode(); } catch { return 0; }
          return img.naturalWidth;
        });
        assert(drawn > 0, "icons/deveco-color.svg doesn't draw");
        assert.deepEqual(errors, []);
        await ctx.close();
      });
    }
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// #638: Claude Desktop takes skills (copied into Cowork's skills-plugin), so
// the Skills tab gives it an icon on each skill, and says Desktop shows
// changes once its window is reloaded; without a skills-plugin it is one
// with no skills folder, and the note isn't there. English and Chinese, no
// backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/aimer";
const lib = (desktopSkills) => ({
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: [
    { id: "codex", name: "Codex", icon: "", skills: `${HOME}/.codex/skills` },
    { id: "claude-desktop", name: "Claude Desktop", icon: "claude-color", mcp: `${HOME}/Library/Application Support/Claude/claude_desktop_config.json`,
      ...(desktopSkills ? { skills: `${HOME}/Library/Application Support/Claude-3p/local-agent-mode-sessions/skills-plugin/o/a/skills` } : {}) },
  ],
  instructions: { agents: [], sets: [] }, foundServers: [], projects: [], foundSkills: [], servers: [],
  skills: [{ name: "grilling", description: "Grill a plan", kind: "", agents: ["codex"] }],
});

function server(lang, desktopSkills) {
  return async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/library") return route.fulfill({ json: lib(desktopSkills) });
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    const body = await fs.readFile(file).catch(() => null);
    await (body ? route.fulfill({ body, contentType }) : route.fulfill({ status: 404, body: "" }));
  };
}

const words = {
  en: { note: "Claude Desktop shows skill changes once its window is reloaded", none: "Claude Desktop has no skills folder." },
  zh: { note: "Claude Desktop 刷新窗口", none: "Claude Desktop 没有技能文件夹。" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": Claude Desktop takes skills", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      await browser.close();
    });
    for (const lang of ["en", "zh"]) {
      for (const has of [true, false]) {
        await t.test(`${lang} ${has ? "with" : "without"} a skills-plugin`, async () => {
          const ctx = await browser.newContext({ viewport: { width: 980, height: 800 } });
          await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "skills"); } catch {} });
          const page = await ctx.newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", server(lang, has));
          await page.goto("http://uni-switch.test/");
          await page.locator('button[data-view="library"]').click();
          await page.locator('#view-library .lib-row .lib-ag[data-agent="codex"]').first().waitFor();
          const asides = await page.locator("#view-library p.lib-aside").allTextContents();
          const chip = await page.locator('#view-library .lib-row .lib-ag[data-agent="claude-desktop"]').count();
          if (has) {
            assert.ok(chip > 0, "no Claude Desktop icon on the skill");
            const note = asides.find((s) => s.includes(words[lang].note));
            assert.ok(note && note.includes(/Mac/.test(await page.evaluate(() => navigator.platform)) ? "⌘R" : "Ctrl+R"), asides.join(" | "));
            assert.ok(!asides.some((s) => s.includes(words[lang].none)), asides.join(" | "));
          } else {
            assert.equal(chip, 0);
            assert.ok(asides.some((s) => s.includes(words[lang].none)), asides.join(" | "));
            assert.ok(!asides.some((s) => s.includes(words[lang].note)), asides.join(" | "));
          }
          await ctx.close();
        });
      }
    }
    assert.deepEqual(errors, []);
  });
}

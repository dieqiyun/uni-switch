// Run with Node's test runner and Playwright on the module path; see README.md.
// #514: removing a project took away every skill and MCP server uni-switch had
// put in it, so a project given some once had to stay in uni-switch for good.
// The Remove dialog now has "Keep its skills and MCP servers": checked, its
// text says what stays, and Remove posts projects/remove with keep, the
// status saying they were kept; unchecked, it posts as before. A project
// uni-switch put nothing in has no such box. Pressing the box scrolls nothing;
// in English and Chinese. No backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/emo";
const APP = HOME + "/code/app", BARE = HOME + "/code/bare";
const agent = (id, name, more = {}) => ({ id, name, icon: "", skills: `${HOME}/.${id}/skills`, mcp: `${HOME}/.${id}/mcp.json`, ...more });
const lib = (projects) => ({
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: [
    agent("claude", "Claude Code", { projectSkills: ".claude/skills", projectMCP: ".mcp.json" }),
    agent("pi", "Pi", { projectSkills: ".agents/skills", projectMCP: ".pi/mcp.json", projectNoSSE: true }),
  ],
  instructions: { agents: [] }, servers: [{ name: "fs", transport: "stdio", command: "fs", url: "", agents: [] }],
  skills: [], foundServers: [], foundSkills: [],
  projects,
});
const projects = () => [
  { dir: APP, name: "app", copy: false, skills: {}, placed: [], servers: { fs: ["claude", "pi"] }, wrote: { ".mcp.json": ["fs"], ".pi/mcp.json": ["fs"] } },
  { dir: BARE, name: "bare", copy: false, skills: {}, placed: [], servers: {}, wrote: {} },
];

function server(lang, posts) {
  let ps = projects();
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/library") return route.fulfill({ json: lib(ps) });
    if (url.pathname === "/api/library/projects/remove") {
      const b = req.postDataJSON();
      posts.push(b);
      ps = ps.filter((p) => p.dir !== b.dir);
      return route.fulfill({ json: { ...lib(ps), result: { changed: [], problems: [] } } });
    }
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const scrolled = (page) => page.evaluate(() => [window.scrollX, window.scrollY, ...[...document.querySelectorAll("*")].filter((e) => e.scrollTop || e.scrollLeft).map((e) => `${e.className}:${e.scrollTop},${e.scrollLeft}`)].join(" "));
// a click where the element is, as the reader's: Playwright's own click
// first scrolls it into view
// the dialog grows out of the button that opened it: it's pressed once
// it's still
const still = (page) => page.waitForFunction(() => !document.querySelector("#modal").getAnimations({ subtree: true }).some((a) => a.playState === "running"));
const press = async (page, loc) => {
  const b = await loc.boundingBox();
  await page.mouse.click(b.x + b.width / 2, b.y + b.height / 2);
};

const words = {
  en: {
    keep: "Keep its skills and MCP servers", gone: "are taken away", stays: "stay there as they are",
    kept: "app removed, its skills and servers kept", removed: "bare removed", remove: "Remove",
  },
  zh: {
    keep: "保留项目里的技能和 MCP 服务器", gone: "移除 uni-switch 放入的", stays: "原样保留",
    kept: "已移除 app，项目里的技能和服务器已保留", removed: "已移除 bare", remove: "移除",
  },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a project removed, keeping what uni-switch put in it", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const lang of ["en", "zh"]) {
      await t.test(lang, async () => {
        const w = words[lang], posts = [];
        const ctx = await browser.newContext({ viewport: { width: 860, height: 700 } });
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "mcp"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, posts));
        await page.goto("http://uni-switch.test/");
        await page.locator('button[data-view="library"]').click();
        const cards = page.locator("#view-library .lib-project");
        await cards.nth(1).waitFor();
        const dialog = page.locator("#modal .editor");

        // a project uni-switch wrote servers into: the box is there, unchecked
        await press(page, cards.filter({ hasText: "app" }).first().locator(".lib-icon.danger"));
        await dialog.waitFor();
        await still(page);
        const box = dialog.locator(".lib-keep");
        assert.equal((await box.locator(".name").textContent()).trim(), w.keep);
        assert.equal(await box.locator("input").isChecked(), false);
        const text = dialog.locator(".lib-confirm");
        assert.match(await text.textContent(), new RegExp(w.gone));
        assert.equal(await box.evaluate((e) => getComputedStyle(e).borderLeftWidth), "0px", "no stripe");

        const before = await scrolled(page);
        await press(page, box.locator(".name"));
        assert.equal(await box.locator("input").isChecked(), true);
        assert.match(await text.textContent(), new RegExp(w.stays));
        assert.equal(await scrolled(page), before, "pressing the box scrolls nothing");
        await press(page, dialog.locator("button.primary"));
        await page.waitForFunction((s) => document.querySelector("#status").textContent.trim() === s, w.kept);
        assert.deepEqual(posts, [{ dir: APP, keep: true }]);
        await page.waitForFunction(() => document.querySelectorAll("#view-library .lib-project").length === 1);

        // one uni-switch put nothing in: no box, removed as before
        await page.waitForFunction(() => document.querySelector("#modal").hidden);
        await press(page, cards.first().locator(".lib-icon.danger"));
        await page.waitForFunction(() => document.querySelector("#modal .editor b")?.textContent.includes("bare"));
        await still(page);
        assert.equal(await dialog.locator(".lib-keep").count(), 0);
        assert.match(await dialog.locator(".lib-confirm").textContent(), new RegExp(w.gone));
        assert.equal((await dialog.locator("button.primary").textContent()).trim(), w.remove);
        await press(page, dialog.locator("button.primary"));
        await page.waitForFunction((s) => document.querySelector("#status").textContent.trim() === s, w.removed);
        assert.deepEqual(posts, [{ dir: APP, keep: true }, { dir: BARE }]);
        assert.deepEqual(errors, []);
        await ctx.close();
      });
    }
  });
}

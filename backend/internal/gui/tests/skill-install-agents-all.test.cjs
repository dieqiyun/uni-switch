// Run with Node's test runner and Playwright on the module path; see README.md.
// #1531 (liuweifeng: 安装技能时默认选择所有 Agent，只想给一个需要逐个关掉):
// the agents a skill is installed for start all lit, and one agent alone
// was a click on each of the others. The picker now has the All chip the
// library's rows have: lit while every agent is, a click takes them all off,
// another puts them all back, and the install sends only what is lit. The
// MCP server editor's picker has it too. Every language, 1100px and 420px,
// nothing scrolls and nothing runs past the page's side. No backend: the
// API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/liuweifeng";
const agent = (id, name, icon) => ({ id, name, icon, skills: `${HOME}/.${id}/skills`, mcp: `${HOME}/.${id}/mcp.json` });
const AGENTS = ["claude", "codex", "zcode", "pi", "gemini"];
const lib = () => ({
  dir: `${HOME}/.config/uni-switch/library`, backups: `${HOME}/.config/uni-switch/backups`, home: HOME,
  agents: [
    agent("claude", "Claude Code", "claudecode-color"),
    agent("codex", "Codex", "codex-color"),
    agent("zcode", "ZCode", "zcode"),
    agent("pi", "Pi", "pi"),
    agent("gemini", "Gemini CLI", "gemini-color"),
  ],
  instructions: { agents: [], sets: [] }, servers: [], skills: [], foundServers: [], projects: [], foundSkills: [],
});
// the reporter's repository, as the screenshot shows it
const probe = {
  source: "https://github.com/cloudflare/security-audit-skill", kind: "github",
  candidates: [{ path: ".", name: "security-audit", description: "Security guidance and vulnerability review for codebases, APIs, services, CLI tools, libraries, and daemons.", have: false }],
};
const ALL = { en: "All", zh: "全部", "zh-TW": "全部", ja: "すべて", de: "Alle" };

function server(lang, posts) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/library") return route.fulfill({ json: lib() });
    if (url.pathname === "/api/library/skills/probe") return route.fulfill({ json: probe });
    if (url.pathname === "/api/library/skills/install") {
      posts.push(req.postDataJSON());
      return route.fulfill({ json: { ...lib(), result: { changed: req.postDataJSON().agents, problems: [], installed: ["security-audit"] } } });
    }
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname.startsWith("/api/library/market")) return route.fulfill({ json: { items: [] } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const scrolled = (page) => page.evaluate(() => [window.scrollX, window.scrollY, ...[...document.querySelectorAll("*")].filter((e) => e.scrollTop || e.scrollLeft).map((e) => `${e.className}:${e.scrollTop},${e.scrollLeft}`)].join(" "));
const lit = (box) => box.locator(".lib-ag[data-agent]").evaluateAll((cs) => cs.filter((c) => c.getAttribute("aria-pressed") === "true").map((c) => c.dataset.agent));
const pastSide = (page) => page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a skill's install picker and a server's editor have All", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    const open = async (lang, width, libTab) => {
      const posts = [];
      const ctx = await browser.newContext({ viewport: { width, height: 800 } });
      await ctx.addInitScript((x) => { try { localStorage.setItem("uni-switch.libTab", x); } catch {} }, libTab);
      const page = await ctx.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(lang, posts));
      await page.goto("http://uni-switch.test/");
      await page.locator('button[data-view="library"]').click();
      return { ctx, page, posts };
    };

    for (const lang of Object.keys(ALL)) {
      for (const width of [1100, 420]) {
        await t.test(`installing a skill, ${lang}, ${width}px`, async () => {
          const { ctx, page, posts } = await open(lang, width, "skills");
          const src = page.locator('#view-library input[data-lib="source"]');
          await src.fill(probe.source);
          await src.press("Enter");
          const foot = page.locator("#view-library .lib-probefoot");
          const all = foot.locator(".lib-ag.all");
          await all.waitFor();
          assert.equal((await all.textContent()).trim(), ALL[lang]);
          assert.equal(await all.getAttribute("aria-pressed"), "true", "every agent starts lit, and so does All");
          assert.deepEqual(await lit(foot), AGENTS);
          const before = await scrolled(page);
          await all.click();
          assert.deepEqual(await lit(foot), [], "All takes every agent off");
          assert.equal(await all.getAttribute("aria-pressed"), "false");
          await foot.locator('.lib-ag[data-agent="codex"]').click();
          assert.deepEqual(await lit(foot), ["codex"]);
          assert.equal(await scrolled(page), before, "the clicks scroll nothing");
          assert.ok(await pastSide(page) <= 0, "nothing runs past the page's side");
          // the All chip and the agents sit on the picker's lines, inside the card
          const card = await page.locator("#view-library .lib-probe").boundingBox();
          const chip = await all.boundingBox();
          assert.ok(chip.x >= card.x && chip.x + chip.width <= card.x + card.width, "All is inside the card");
          await foot.locator("button.primary").click();
          await page.waitForTimeout(300);
          assert.deepEqual(posts.map((p) => p.agents), [["codex"]], "installed for Codex alone");
          await ctx.close();
        });
      }
    }

    await t.test("All puts every agent back", async () => {
      const { ctx, page, posts } = await open("en", 1100, "skills");
      const src = page.locator('#view-library input[data-lib="source"]');
      await src.fill(probe.source);
      await src.press("Enter");
      const foot = page.locator("#view-library .lib-probefoot");
      await foot.locator(".lib-ag.all").click();
      await foot.locator('.lib-ag[data-agent="zcode"]').click();
      assert.equal(await foot.locator(".lib-ag.all").getAttribute("aria-pressed"), "false");
      await foot.locator(".lib-ag.all").click();
      assert.deepEqual(await lit(foot), AGENTS);
      await foot.locator("button.primary").click();
      await page.waitForTimeout(300);
      assert.deepEqual(posts.map((p) => p.agents.sort()), [[...AGENTS].sort()]);
      await ctx.close();
    });

    for (const width of [1100, 420]) {
      await t.test(`adding a server, ${width}px`, async () => {
        const { ctx, page } = await open("zh", width, "mcp");
        await page.getByRole("button", { name: "＋ 添加服务器", exact: true }).click();
        const box = page.locator("#modal .lib-agents");
        const all = box.locator(".lib-ag.all");
        await all.waitFor();
        assert.equal((await all.textContent()).trim(), "全部");
        assert.deepEqual(await lit(box), AGENTS);
        await all.click();
        assert.deepEqual(await lit(box), []);
        await box.locator('.lib-ag[data-agent="pi"]').click();
        assert.deepEqual(await lit(box), ["pi"]);
        assert.ok(await pastSide(page) <= 0, "nothing runs past the page's side");
        await ctx.close();
      });
    }

    assert.deepEqual(errors, []);
  });
}

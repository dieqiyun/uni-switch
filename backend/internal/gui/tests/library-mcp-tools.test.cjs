// Run with Node's test runner and Playwright on the module path; see README.md.
// nf1qaz2w on Discord: "mcp现在好像只能看到有几个工具，看不到具体的工具名和描述".
// A server that listed its tools shows them under its row, by name and what
// each says it does, when its "N tools" status is clicked: no editor opens,
// nothing scrolls, the list fits a narrow window, and Check again under it
// checks that server afresh while the list stays put. A server that listed
// no tools still checks again on a click. In every language, both engines.
// No backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { reach } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/nf";
const LONG = "Read the complete contents of a file from the file system as text. Handles various text encodings and provides detailed error messages if the file cannot be read. Use this tool when you need to examine the contents of a single file.";
const fixture = () => ({
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: [{ id: "claude", name: "Claude Code", icon: "claudecode-color", skills: `${HOME}/.claude/skills`, mcp: `${HOME}/.claude.json`, toolsOff: true }],
  instructions: { agents: [], sets: [] }, foundServers: [], projects: [], foundSkills: [], skills: [],
  servers: [
    { name: "filesystem", transport: "stdio", command: "npx", args: ["-y", "@modelcontextprotocol/server-filesystem", HOME], agents: ["claude"] },
    { name: "bare", transport: "stdio", command: "bare-mcp", agents: ["claude"] },
    ...Array.from({ length: 6 }, (_, i) => ({ name: "more" + i, transport: "stdio", command: "more-mcp", agents: ["claude"] })),
  ],
});
const tools = [
  { name: "read_text_file", description: LONG },
  { name: "list_allowed_directories" }, // no description
  { name: "a_tool_with_a_very_long_name_that_has_no_spaces_in_it_at_all_anywhere" },
];
const found = (fresh) => ({
  // 4 tools, 3 of them listed: one more than uni-switch keeps
  filesystem: fresh ? { state: "ok", tools: 1, toolList: [{ name: "only_one", description: "The one left." }] } : { state: "ok", tools: 4, toolList: tools },
  bare: { state: "ok", tools: 0 },
  ...Object.fromEntries(Array.from({ length: 6 }, (_, i) => ["more" + i, { state: "ok", tools: 0 }])),
});

function server(lang, checks) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang } } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/library") return route.fulfill({ json: fixture() });
    if (url.pathname === "/api/library/mcp/check") {
      const body = req.postDataJSON();
      let release;
      const held = new Promise((r) => (release = r));
      checks.push({ ...body, release });
      await held;
      const all = found(body.fresh);
      const names = body.names?.length ? body.names : Object.keys(all);
      return route.fulfill({ json: { servers: Object.fromEntries(names.map((n) => [n, all[n]])) } });
    }
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { four: "4 tools", one: "1 tool", none: "0 tools", tip: "It started and listed its tools", see: "Click to see them", hide: "Click to hide them", again: "Click to check again", check: "Check again", more: "1 more not shown" },
  zh: { four: "4 个工具", one: "1 个工具", none: "0 个工具", tip: "已启动并列出了它的工具", see: "点击查看工具", hide: "点击收起", again: "点击重新检查", check: "重新检测", more: "另有 1 个未显示" },
  "zh-TW": { four: "4 個工具", one: "1 個工具", none: "0 個工具", tip: "已啟動並列出了它的工具", see: "點選查看工具", hide: "點選收起", again: "點選重新檢查", check: "重新檢測", more: "另有 1 個未顯示" },
  ja: { four: "4 個のツール", one: "1 個のツール", none: "0 個のツール", tip: "起動してツールを一覧表示しました", see: "クリックしてツールを表示", hide: "クリックして閉じる", again: "クリックして再確認", check: "再確認", more: "ほか 1 個は非表示" },
  de: { four: "4 Tools", one: "1 Tool", none: "0 Tools", tip: "Gestartet, Tools aufgelistet", see: "Klicken, um die Tools zu zeigen", hide: "Klicken zum Ausblenden", again: "Zum erneuten Prüfen klicken", check: "Erneut prüfen", more: "1 weitere nicht angezeigt" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(words)) {
    for (const width of [980, 420]) {
      test(`${engine} ${lang} ${width}: an MCP server's tools are listed by name and description`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
        const errors = [];
        t.after(async () => { await browser.close(); });
        const w = words[lang], checks = [];
        const ctx = await browser.newContext({ viewport: { width, height: width < 600 ? 820 : 500 } });
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "mcp"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("http://uni-switch.test/**", server(lang, checks));
        await page.goto("http://uni-switch.test/");
        await page.locator('button[data-view="library"]').click();
        await page.locator("#view-library .lib-body:not(.lib-skel) .lib-row").first().waitFor();
        const status = (name) => page.locator(`#view-library .lib-health[data-server="${name}"]`);
        const list = page.locator('#view-library .lib-tools[data-server="filesystem"]');
        const scrollTop = () => page.evaluate(() => document.querySelector("#view-library").scrollTop);
        const where = () => status("filesystem").evaluate((b) => b.getBoundingClientRect().top);
        for (let i = 0; i < 50 && !checks.length; i++) await page.waitForTimeout(20);
        assert.equal(checks.length, 1);
        checks[0].release();
        await page.waitForFunction(() => !document.querySelector("#view-library .lib-health.checking"));

        // the count says it opens the list; the list is shut till then
        assert.equal(await status("filesystem").textContent(), w.four);
        assert.equal(await status("filesystem").getAttribute("title"), w.tip + "\n" + w.see);
        assert.equal(await status("filesystem").getAttribute("aria-expanded"), "false");
        assert.equal(await list.isHidden(), true);
        // one that listed none still checks again on a click
        assert.equal(await status("bare").textContent(), w.none);
        assert.equal(await status("bare").getAttribute("title"), w.tip + "\n" + w.again);
        assert.equal(await status("bare").getAttribute("aria-expanded"), null);

        // scrolled a little first, by the wheel as the reader would
        await page.mouse.move(Math.min(490, width / 2), 250);
        await page.mouse.wheel(0, 30);
        await page.waitForFunction(() => document.querySelector("#view-library").scrollTop > 0);
        await page.waitForTimeout(400);
        const top = await scrollTop(), pillAt = await where();

        // a click shows each tool's name and description, and asks nothing
        await status("filesystem").click();
        await list.waitFor();
        await page.waitForTimeout(100);
        // the row grows under it, and what was clicked stays where it was
        // (the click guard takes back the pixel a centred row gives up)
        assert.equal(await where(), pillAt, "the click moved what was clicked");
        assert.ok(Math.abs((await scrollTop()) - top) <= 1, "the click scrolled the page");
        assert.equal(await page.evaluate(() => document.querySelector("#modal").classList.contains("lib")), false, "the click opened the editor");
        assert.equal(checks.length, 1, "opening the list checked the server again");
        assert.equal(await status("filesystem").getAttribute("aria-expanded"), "true");
        assert.equal(await status("filesystem").getAttribute("title"), w.tip + "\n" + w.hide);
        const items = await list.locator("li").evaluateAll((lis) => lis.map((li) => [li.querySelector(".lib-toolname")?.textContent, li.querySelector(".lib-tooldesc")?.textContent ?? null]));
        assert.deepEqual(items, [["read_text_file", LONG], ["list_allowed_directories", null], [tools[2].name, null]]);
        assert.equal(await list.locator(".lib-toolfoot .note").textContent(), w.more);
        // it fits the window: nothing wider than the page, the long name wraps
        const fit = await page.evaluate(() => {
          const v = document.querySelector("#view-library"), box = document.querySelector('.lib-tools[data-server="filesystem"]').getBoundingClientRect();
          return { over: v.scrollWidth - v.clientWidth, right: box.right, w: innerWidth };
        });
        assert.ok(fit.over <= 0 && fit.right <= fit.w, "the list is wider than the window: " + JSON.stringify(fit));
        // no coloured stripe down its side
        const side = await list.evaluate((b) => { const s = getComputedStyle(b); return [s.borderLeftWidth, s.borderLeftColor, s.borderTopWidth, s.borderTopColor]; });
        assert.deepEqual(side.slice(0, 2), side.slice(2), "the list has a left border of its own");
        // reading it (a click on a description) opens no editor
        await list.locator(".lib-tooldesc").first().click();
        assert.equal(await page.evaluate(() => document.querySelector("#modal").classList.contains("lib")), false);

        // Check again checks that one afresh; the list stays while it does.
        // At 420 in German it is under the window's foot since the Hooks
        // tab came in (e3a497d7a): the reader scrolls down to it
        await reach(page, list.locator(".lib-toolfoot button", { hasText: w.check }));
        const at = await scrollTop(), pillThen = await where();
        await list.locator(".lib-toolfoot button", { hasText: w.check }).click();
        for (let i = 0; i < 50 && checks.length < 2; i++) await page.waitForTimeout(20);
        assert.deepEqual({ names: checks[1].names, fresh: checks[1].fresh }, { names: ["filesystem"], fresh: true });
        assert.equal(await list.isVisible(), true, "the list went away while checking");
        assert.equal(await list.locator("li").count(), 3);
        checks[1].release();
        await page.waitForFunction((t) => document.querySelector('.lib-health[data-server="filesystem"]').textContent === t, w.one);
        assert.deepEqual(await list.locator("li").evaluateAll((lis) => lis.map((li) => li.textContent)), ["only_oneThe one left."]);
        assert.equal(await list.locator(".lib-toolfoot .note").count(), 0);
        assert.equal(await scrollTop(), at, "Check again scrolled the page");
        assert.equal(await where(), pillThen, "Check again moved the row");

        // a click on the count shuts it again
        await status("filesystem").click();
        await list.waitFor({ state: "hidden" });
        assert.equal(await status("filesystem").getAttribute("aria-expanded"), "false");
        assert.equal(checks.length, 2);
        // and the one with none checks again
        await status("bare").click();
        for (let i = 0; i < 50 && checks.length < 3; i++) await page.waitForTimeout(20);
        assert.deepEqual({ names: checks[2].names, fresh: checks[2].fresh }, { names: ["bare"], fresh: true });
        checks[2].release();
        assert.equal(await page.locator("#view-library select").count(), 0);
        assert.deepEqual(errors, []);
        await ctx.close();
      });
    }
  }
}

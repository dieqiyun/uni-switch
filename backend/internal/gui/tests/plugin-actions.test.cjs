// Run with Node's test runner and Playwright on the module path; see README.md.
// A community plugin's actions (uni-switch.actions, pluginpages.js, P6): items
// its page adds to a provider's and an agent's row menu, after uni-switch's
// own, named in the plugin's words in the window's language. Picking one
// opens the plugin's page and tells it the action and that row's id and
// name, and nothing else of it; a page that gives uni-switch.onAction late
// still gets every action picked before. A page with no action adds
// nothing to a menu. The Plugins page says which menus a plugin adds to.
// Every language, both engines, a narrow window. The API is faked here;
// the server's side is TestWindowActions and pluginpage_test.go.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const bridge = path.resolve(__dirname, "../pagebridge.js");

const TOK = "7".repeat(32);
const withActions = [{
  spec: "/x/uni-switch-row-tools", name: "uni-switch-row-tools", key: "plugin-row-tools", title: "Row tools", titles: { zh: "行工具" }, reads: [],
  url: `/_uni-switch/plugin/${TOK}/index.html`,
  actions: [
    { id: "check", menu: "provider", title: "Check balance", titles: { zh: "查余额", "zh-TW": "查餘額", ja: "残高を確認", de: "Guthaben prüfen" } },
    { id: "logs", menu: "agent", title: "Row tools", titles: { zh: "行工具" } },
  ],
}];
// the page gives its handler late: what was picked before waits for it
const HTML = `<!doctype html><meta charset="utf-8"><script src="_uni-switch/bridge.js"></script><ol id="got"></ol><script src="app.js"></script>`;
const JS = `setTimeout(() => uni-switch.onAction((a) => { const li = document.createElement("li"); li.textContent = JSON.stringify(a); document.getElementById("got").append(li); }), 400);`;

const providerList = [{ id: "openai", name: "OpenAI", icon: "generic", host: "api.openai.com", chat: "https://api.openai.com/v1", models: [] }]
  .map((p) => ({ responses: "", anthropic: "", agents: [], fallback: [], headers: {}, key: { set: false }, keyList: [], ready: true, ...p }));
const agents = [{ id: "codex", name: "Codex", icon: "generic", path: "/fixture/codex", fields: [{ key: "model", label: "model", value: "gpt-5.4", options: [{ value: "gpt-5.4", label: "gpt-5.4" }] }] }];

function server(lang, opts) {
  return async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents, profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: providerList, presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/plugin-pages") return json(opts.pages);
    if (url.pathname === "/api/plugin-panels") return json([]);
    if (url.pathname === "/api/plugins") return json(opts.plugins || { plugins: [] });
    if (url.pathname === "/api/plugins/market") return json({ listings: [], state: opts.plugins || { plugins: [] } });
    if (url.pathname === "/api/plugins/listings") return json({ listings: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const m = url.pathname.match(/^\/_uni-switch\/plugin\/([0-9a-f]{32})\/(.+)$/);
    if (m) {
      const rel = m[2];
      const body = rel === "_uni-switch/bridge.js" ? await fs.readFile(bridge) : rel === "index.html" ? HTML : rel === "app.js" ? JS : null;
      if (body == null) return route.fulfill({ status: 404, body: "" });
      const type = { ".html": "text/html", ".js": "text/javascript" }[path.extname(rel)];
      return route.fulfill({ status: 200, headers: { "Content-Security-Policy": "sandbox allow-scripts; connect-src 'none'", "Content-Type": type }, body });
    }
    if (url.host !== "uni-switch.test") return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const want = { en: "Check balance", zh: "查余额", "zh-TW": "查餘額", ja: "残高を確認", de: "Guthaben prüfen" };
const got = async (page) => {
  const f = page.frames().find((x) => x.url().includes(TOK));
  if (!f) return [];
  return (await f.locator("#got li").allInnerTexts().catch(() => [])).map((x) => JSON.parse(x));
};
const menuNames = (page) => page.locator(".pop.row-menu .rm-item .rm-name").allInnerTexts();
const closeMenu = async (page) => { await page.keyboard.press("Escape"); await page.locator(".pop.row-menu").waitFor({ state: "detached" }); };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: a plugin's actions in the row menus open its page and tell it the row`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      for (const width of [1100, 420]) {
        const where = `${engine} ${lang} ${width}`;
        for (const pages of [[], withActions]) {
          const context = await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" });
          const page = await context.newPage();
          page.setDefaultTimeout(8000);
          page.setDefaultNavigationTimeout(60000);
          const errors = [];
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", server(lang, { pages }));
          await page.goto("http://uni-switch.test/");
          // the agent's menu: uni-switch's own items, then the plugin's
          await page.locator('.row.agent[data-id="codex"] .ag-handle').click();
          await page.locator(".pop.row-menu").waitFor();
          const own = await page.evaluate(() => t("Hide"));
          const agentItems = await menuNames(page);
          if (!pages.length) {
            assert.equal(agentItems.at(-1), own, `${where}: no plugin, nothing added`);
            await closeMenu(page);
            await context.close();
            continue;
          }
          const logs = lang.startsWith("zh") ? "行工具" : "Row tools"; // zh-TW takes zh's
          assert.deepEqual(agentItems.slice(-2), [own, logs], `${where}: agent menu`);
          assert.equal(await page.locator(".pop.row-menu .rm-sep").count() >= 1, true, where);
          await closeMenu(page);

          // the provider's menu: its item in the window's language
          await page.evaluate(() => show("providers"));
          const row = page.locator('#providers > .row.provider[data-id="openai"]');
          await row.locator(".name").click({ button: "right" });
          await page.locator(".pop.row-menu").waitFor();
          const pvItems = await menuNames(page);
          assert.equal(pvItems.at(-1), want[lang], `${where}: provider menu`);
          assert.ok(await page.locator(".pop.row-menu").evaluate((m) => m.getBoundingClientRect().right <= innerWidth + 1), `${where}: menu fits`);
          await page.locator(".pop.row-menu .rm-item", { hasText: want[lang] }).click();
          await page.waitForFunction(() => view === "plugin-row-tools");
          await page.waitForFunction(() => document.querySelector("#view-plugin-row-tools iframe.plugin-frame"));
          for (let i = 0; i < 100 && (await got(page)).length < 1; i++) await page.waitForTimeout(50);
          assert.deepEqual(await got(page), [{ id: "check", provider: { id: "openai", name: "OpenAI" } }], `${where}: told the row, and only its id and name`);

          // picked again with the page open: told again, nothing drawn anew
          await page.evaluate(() => show("agents"));
          await page.locator('.row.agent[data-id="codex"] .ag-handle').click();
          await page.locator(".pop.row-menu .rm-item", { hasText: logs }).click();
          await page.waitForFunction(() => view === "plugin-row-tools");
          for (let i = 0; i < 100 && (await got(page)).length < 2; i++) await page.waitForTimeout(50);
          assert.deepEqual((await got(page))[1], { id: "logs", agent: { id: "codex", name: "Codex" } }, `${where}: agent action`);
          assert.equal(await page.locator("#view-plugin-row-tools iframe.plugin-frame").count(), 1, where);
          assert.deepEqual(errors, [], where);
          await context.close();
        }
      }
    }
  });

  test(`${engine}: Plugins says which menus a plugin adds to`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const plugins = { bun: true, bunVersion: "1.3.0", plugins: [
      { spec: "/x/uni-switch-row-tools", providers: [], moved: [], inUniSwitchOnly: true, page: { title: "Row tools", reads: [] }, actions: [{ menu: "provider", title: "Check balance" }, { menu: "agent", title: "Row tools" }] },
    ] };
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      for (const width of [980, 420]) {
        const where = `${engine} ${lang} ${width}`;
        const page = await (await browser.newContext({ viewport: { width, height: 820 } })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, { pages: [], plugins }));
        await page.goto("http://uni-switch.test/?view=plugins");
        const view = page.locator("#view-plugins");
        const installed = await page.evaluate(() => t("Installed"));
        await page.locator(":is(.lib-tabs .opt, .nav-s):visible", { hasText: installed }).first().click();
        const tr = (k, v) => page.evaluate(([k, v]) => t(k, v), [k, v]);
        const row = view.locator(".pm-row").filter({ has: page.locator(".name", { hasText: "uni-switch-row-tools" }) });
        await row.waitFor();
        const lines = (await row.locator(".pm-page").allInnerTexts()).map((x) => x.trim());
        const menus = [await tr("providers' menus"), await tr("agents' menus")].join(await tr(", "));
        assert.deepEqual(lines, [await tr("Adds a page to the sidebar, which reads nothing of uni-switch's"), await tr("Adds items to {where}, which open its page", { where: menus })], where);
        if (lang !== "en") assert.ok(!lines[1].includes("Adds items"), `${where}: translated`);
        assert.ok(await view.evaluate((v) => v.scrollWidth <= v.clientWidth + 1), `${where}: fits`);
        assert.deepEqual(errors, [], where);
        await page.context().close();
      }
    }
  });
}

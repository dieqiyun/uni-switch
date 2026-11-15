// Run with Node's test runner and Playwright on the module path; see README.md.
// Claude Code on a model of uni-switch's, Default picked and Use default
// confirmed (EZN7L2C3, #834 on v0.1.920): the row said Not connected with
// 「接入」 off, yet its model read Pick a model and its list had uni-switch's
// models alone, Default and Claude Code's own gone. It now says Default,
// and lists Default (the one it is on), its own models and uni-switch's, as
// when connected; and it goes under Not set up as the ask said. No click
// moves the page. In English and Chinese.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const claude = [
  { value: "claude-sonnet-5-5", note: "Claude Sonnet 5.5", icon: "claude-color", group: "Claude Code", direct: "Anthropic" },
  { value: "claude-opus-5-5", note: "Claude Opus 5.5", icon: "claude-color", group: "Claude Code", direct: "Anthropic" },
  { value: "deepseek/pro", label: "DeepSeek Pro", note: "DeepSeek · via uni-switch", icon: "deepseek-color", group: "DeepSeek", ref: "deepseek/pro" },
];
const codex = [{ value: "relay/m1", label: "m1", note: "Relay · via uni-switch", ref: "relay/m1", group: "Relay" }];
const fresh = () => ({
  agents: [
    { id: "claude", name: "Claude Code", icon: "claudecode-color", path: "~/.claude/settings.json", wired: true,
      fields: [{ key: "model", label: "model", value: "deepseek/pro", options: claude }] },
    { id: "codex", name: "Codex", icon: "generic", path: "/fixture/codex", wired: true, fields: [{ key: "model", label: "model", value: "relay/m1", options: codex }] },
  ],
  profiles: [],
});

function server(lang, sets) {
  let cur = fresh();
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ ...cur, settings: { lang, theme: "light" } });
    if (url.pathname === "/api/set") {
      const body = req.postDataJSON();
      sets.push(body);
      cur = JSON.parse(JSON.stringify(cur));
      const a = cur.agents.find((x) => x.id === body.agent);
      const f = a.fields.find((x) => x.key === body.field);
      f.value = body.value;
      a.wired = !!f.options.find((o) => o.value === body.value)?.ref;
      if (!a.wired) a.source = "sub";
      return json({ ...cur, settings: { lang, theme: "light" } });
    }
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { def: "Default", shown: "default", use: "Use default", direct: "direct, not via uni-switch", via: "via uni-switch" },
  zh: { def: "默认", shown: "默认", use: "使用默认", direct: "直连，不经 uni-switch", via: "经 uni-switch" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    const w = words[lang];
    test(`${engine} ${lang}: Claude Code taken off uni-switch by Default says Default, lists every choice, and goes under Not set up`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const page = await (await browser.newContext({ viewport: { width: 980, height: 640 }, reducedMotion: "reduce" })).newPage();
      t.after(() => browser.close());
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const sets = [];
      await page.route("**/*", server(lang, sets));
      await page.goto("http://uni-switch.test/");
      const row = page.locator('.row.agent[data-id="claude"]');
      const field = row.locator('> .field.ag-start[data-key="model"]');
      await field.waitFor();
      const view = page.locator("#view-agents");
      const top = await view.evaluate((v) => v.scrollTop);
      const open = async () => { await field.click(); await page.locator("#pop:not([hidden]) #list li").first().waitFor(); };

      // Default, asked, then confirmed
      await open();
      await page.locator("#pop #list li.reset", { hasText: w.def }).first().click();
      const ask = page.locator("#modal .leave-ask");
      await ask.waitFor();
      await ask.locator("button", { hasText: w.use }).click();
      await page.waitForFunction(() => document.querySelector("#modal").hidden);
      assert.deepEqual(sets, [{ agent: "claude", field: "model", value: "" }]);

      // under Not set up, as the ask said
      await page.locator(".agent-more").waitFor();
      assert.equal(await page.locator(".agent-more").getAttribute("aria-expanded"), "false");
      await page.locator(".agent-more").click();
      await field.waitFor();
      assert.equal(await row.locator(".lib-switch.ag-conn").getAttribute("aria-checked"), "false");

      // the row says Default, not Pick a model
      assert.equal(await field.locator(".v").textContent(), w.shown);

      // and its list has Default, the one it is on, its own and uni-switch's
      await open();
      const cur = page.locator("#pop #list li.cur");
      assert.equal(await cur.count(), 1);
      assert.ok((await cur.textContent()).includes(w.def));
      assert.equal(await page.locator("#pop #list li", { hasText: "claude-opus-5-5" }).first().locator(".badge.path.direct").textContent(), w.direct);
      assert.equal(await page.locator("#pop #list li", { hasText: "DeepSeek Pro" }).first().locator(".badge.path.via").textContent(), w.via);
      await page.keyboard.press("Escape");
      assert.equal(await view.evaluate((v) => v.scrollTop), top, "a click moved the page");
      assert.deepEqual(errors, []);
    });
  }
}

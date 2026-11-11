// Run with Node's test runner and Playwright on the module path; see README.md.
// One rule for the row's model and its picker (EZN7L2C3, #834): Claude Code
// taken off uni-switch by one of its own models (Opus, asked of Anthropic
// directly) still says that model on its row, where it read Pick a model,
// and its list has its own models with uni-switch's, the one picked marked, as
// when connected, where it had uni-switch's alone. And every agent's picker
// says "via uni-switch" the same way, as the tag Claude Code's has, not at the
// end of a note an ellipsis cuts off. No click moves the page. In English
// and Chinese.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const claude = [
  { value: "claude-sonnet-5-5", note: "Claude Sonnet 5.5", icon: "claude-color", group: "Claude Code", direct: "Anthropic" },
  { value: "claude-opus-5-5", note: "Claude Opus 5.5", icon: "claude-color", group: "Claude Code", direct: "Anthropic" },
  { value: "uni-switch/deepseek/pro", label: "DeepSeek Pro", note: "DeepSeek · via uni-switch", icon: "deepseek-color", group: "DeepSeek", ref: "deepseek/pro" },
];
const codex = [
  { value: "relay/m1", label: "m1", note: "someone.with.a.long.address@example.com · via uni-switch", ref: "relay/m1", group: "Relay" },
  { value: "relay/m2", label: "m2", note: "Relay · via uni-switch", ref: "relay/m2", group: "Relay" },
];
const fresh = () => ({
  agents: [
    { id: "codex", name: "Codex", icon: "generic", path: "/fixture/codex", wired: true, fields: [{ key: "model", label: "model", value: "relay/m2", options: codex }] },
    { id: "claude", name: "Claude Code", icon: "claudecode-color", path: "~/.claude/settings.json", wired: false, source: "sub",
      fields: [{ key: "model", label: "model", value: "claude-opus-5-5", options: claude }] },
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
      a.fields.find((f) => f.key === body.field).value = body.value;
      a.wired = body.value.startsWith("uni-switch/") || a.id === "codex";
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

const words = { en: { via: "via uni-switch", direct: "direct, not via uni-switch" }, zh: { via: "经 uni-switch", direct: "直连，不经 uni-switch" } };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    const w = words[lang];
    test(`${engine} ${lang}: the row says the model picked off uni-switch, and every picker says via uni-switch alike`, async (t) => {
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
      // Claude Code is under Not set up: the fold opened
      const row = page.locator('.row.agent[data-id="claude"]');
      await page.locator(".agent-more").click();
      const field = row.locator('> .field.ag-start[data-key="model"]');
      await field.waitFor();
      const view = page.locator("#view-agents");
      const top = await view.evaluate((v) => v.scrollTop);

      // the model it is on, not Pick a model
      assert.equal(await field.locator(".v").textContent(), "claude-opus-5-5");
      assert.equal(await field.locator(".v.empty").count(), 0);
      assert.equal(await row.locator(".lib-switch.ag-conn").getAttribute("aria-checked"), "false");

      // its list: its own models, the one picked marked, beside uni-switch's
      await field.click();
      await page.locator("#pop:not([hidden]) #list li").first().waitFor();
      const cur = page.locator("#list li.cur");
      assert.equal(await cur.count(), 1);
      assert.ok((await cur.textContent()).includes("claude-opus-5-5"));
      assert.equal(await cur.locator(".badge.path.direct").textContent(), w.direct);
      const ds = page.locator("#list li", { hasText: "DeepSeek Pro" }).first();
      assert.equal(await ds.locator(".badge.path.via").textContent(), w.via);
      // picking uni-switch's from it connects it, as the row's list did
      await ds.click();
      await page.waitForFunction(() => /DeepSeek Pro/.test(document.querySelector("#status")?.textContent || ""));
      assert.deepEqual(sets, [{ agent: "claude", field: "model", value: "uni-switch/deepseek/pro" }]);
      assert.equal(await view.evaluate((v) => v.scrollTop), top, "the pick moved the page");

      // Codex's picker, uni-switch's models alone: the same tag, said once
      await page.locator('.row.agent[data-id="codex"] > .field.ag-start[data-key="model"]').click();
      await page.locator("#pop:not([hidden]) #list li").first().waitFor();
      const m1 = page.locator("#list li", { hasText: "someone.with" }).first();
      const tag = m1.locator(".badge.path.via");
      assert.equal(await tag.textContent(), w.via);
      assert.ok(await tag.evaluate((b) => b.scrollWidth <= b.clientWidth + 1 && b.getBoundingClientRect().right <= b.closest("li").getBoundingClientRect().right), "the tag isn't cut off");
      assert.ok(!(await m1.locator(".n").textContent()).includes("via uni-switch"), "said once, as the tag");
      assert.equal(await page.locator("#list li", { hasText: "m2" }).first().locator(".badge.path.via").count(), 1);
      await page.keyboard.press("Escape");
      assert.equal(await view.evaluate((v) => v.scrollTop), top, "a click moved the page");
      assert.deepEqual(errors, []);
    });
  }
}

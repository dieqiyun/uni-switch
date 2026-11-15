// Run with Node's test runner and Playwright on the module path; see README.md.
// #1582 (SwimmingLiu): Codex connected, an API provider A (Anthropic
// protocol) on beside two Codex OAuth accounts. The button beside Codex's
// switch edits its model, its menu Codex's own models (direct) with A's
// (via uni-switch). With A switched off and the Agents page refreshed, Codex's
// model has no option of uni-switch's left, and the same button became the
// subagent model's (子代理模型), editing subagent_model. The button is the
// model's whatever the providers: a provider switched off only changes what
// it lists. An agent whose model lists nothing (Ante's, with none of
// uni-switch's models) still starts from the field it connects through. No
// click moves the page; in English and Chinese, no backend: the API is
// faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
// Codex's own models while it signs in with ChatGPT: asked of OpenAI
// directly (codex.go's modelOptions)
const own = [
  { value: "gpt-5.5", label: "GPT-5.5", group: "OpenAI", direct: "OpenAI" },
  { value: "gpt-5.4", label: "GPT-5.4", group: "OpenAI", direct: "OpenAI" },
];
const fromA = [{ value: "a/claude-x", label: "Claude X", group: "A", ref: "a/claude-x", note: "A · via uni-switch" }];
// both ChatGPT accounts' models, offered to its subagents (subagent_model)
const seats = ["one@example.com", "two@example.com"].flatMap((who, i) => [
  { value: `codex${i || ""}/gpt-5.5`, label: "GPT-5.5", note: `${who} · via uni-switch`, group: "ChatGPT", ref: `codex${i || ""}/gpt-5.5`, icon: "openai" },
]);
const state = (aOn) => ({
  agents: [
    {
      id: "codex", name: "Codex", path: "/fixture/config.toml", icon: "codex-color", wired: true,
      fields: [
        { key: "model", label: "model", value: "gpt-5.5", options: aOn ? [...own, ...fromA] : own },
        { key: "effort", label: "effort", value: "", options: [{ value: "", label: "Default" }, { value: "high", label: "high" }] },
        { key: "subagent", label: "subagents", value: "", options: aOn ? [...own, ...fromA] : own },
        { key: "subagent_model", label: "subagent model", value: "", options: seats },
      ],
    },
    // Ante: its model lists only uni-switch's, none while no provider is on;
    // the row starts from the provider field it connects through
    {
      id: "ante", name: "Ante", path: "/fixture/ante.json", icon: "generic", wired: true,
      fields: [
        { key: "provider", label: "provider", value: "uni-switch", options: [{ value: "uni-switch", label: "uni-switch", icon: "uni-switch" }] },
        { key: "model", label: "model", value: "", options: [] },
      ],
    },
  ],
  profiles: [],
});

function server(lang, posts, world) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    const reply = () => json({ ...world.cur, settings: { lang, theme: "light" } });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return reply();
    if (url.pathname === "/api/set") {
      const body = JSON.parse(req.postData());
      posts.push(body);
      world.cur = JSON.parse(JSON.stringify(world.cur));
      world.cur.agents.find((a) => a.id === body.agent).fields.find((f) => f.key === body.field).value = body.value;
      return reply();
    }
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = { en: { sub: "subagent model" }, zh: { sub: "子代理模型" } };
const row = '.row.agent[data-id="codex"]';

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    const w = words[lang];
    test(`${engine} ${lang}: Codex's model button edits model with a provider switched off (#1582)`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 980, height: 560 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [], posts = [], world = { cur: state(true) };
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(lang, posts, world));
      const start = page.locator(`${row} > .field.ag-start`);
      const listed = async () => {
        await page.locator("#pop:not([hidden]) #list li").first().waitFor();
        return page.locator("#pop #list li").allTextContents();
      };
      const look = async (aOn) => {
        assert.equal(await start.count(), 1);
        assert.equal(await start.getAttribute("data-key"), "model", `A ${aOn ? "on" : "off"}: the button is the model's`);
        assert.match((await start.textContent()).trim(), /GPT-5\.5/);
        assert.ok(!((await start.getAttribute("title")) || "").includes(w.sub), "the button says subagent model");
        const top = await page.evaluate(() => [document.scrollingElement.scrollTop, document.querySelector("#view-agents")?.scrollTop]);
        await start.click();
        const items = await listed();
        assert.ok(items.some((s) => s.includes("GPT-5.4")), items.join(" | "));
        assert.equal(items.some((s) => s.includes("Claude X")), aOn, "A's models listed while A is on: " + items.join(" | "));
        assert.ok(!items.some((s) => s.includes("two@example.com")), "a subagent's ChatGPT seats in the model's menu: " + items.join(" | "));
        assert.deepEqual(await page.evaluate(() => [document.scrollingElement.scrollTop, document.querySelector("#view-agents")?.scrollTop]), top, "the click moved the page");
      };

      // 1. A on: the model's menu, Codex direct and A via uni-switch
      await page.goto("http://uni-switch.test/?view=agents");
      await page.locator(`${row} .ag-conn`).waitFor();
      await look(true);
      await page.keyboard.press("Escape");

      // 2. A off, only the Agents page refreshed: still the model's button
      world.cur = state(false);
      await page.reload();
      await page.locator(`${row} .ag-conn`).waitFor();
      await look(false);
      await page.locator("#pop #list li", { hasText: "GPT-5.4" }).first().click();
      await page.waitForFunction((r) => /GPT-5\.4/.test(document.querySelector(r + " > .field.ag-start")?.textContent || ""), row);
      assert.deepEqual(posts, [{ agent: "codex", field: "model", value: "gpt-5.4" }]);

      // Ante, its model with nothing to list: still started from uni-switch's provider
      assert.equal(await page.locator('.row.agent[data-id="ante"] > .field.ag-start').getAttribute("data-key"), "provider");

      // 3. A on again: the same
      world.cur = state(true);
      await page.reload();
      await page.locator(`${row} .ag-conn`).waitFor();
      assert.equal(await start.getAttribute("data-key"), "model");
      assert.deepEqual(errors, []);
    });
  }
}

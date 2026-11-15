// Run with Node's test runner and Playwright on the module path; see README.md.
// Every agent's picker says which way each of its models goes (EZN7L2C3,
// #834: Codex 的下拉选项中没有 via uni-switch). Codex's own models, while
// Codex is routed through uni-switch by its base URL, are asked of uni-switch's
// gateway: they say via uni-switch, as the catalog's do. Not routed, they and
// Grok Build's own say direct, not via uni-switch. WebKit or Chromium, English
// and Chinese; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const relay = { value: "relay/m1", label: "m1", ref: "relay/m1", note: "Relay" };
const state = (lang) => ({
  agents: [
    { id: "codex", name: "Codex", icon: "generic", path: "/fixture/codex", wired: true, fields: [{ key: "model", label: "model", value: "relay/m1", options: [
      { value: "gpt-5.4", label: "GPT-5.4", group: "OpenAI", via: true }, relay] }] },
    { id: "grok", name: "Grok Build", icon: "generic", path: "/fixture/grok", fields: [{ key: "model", label: "model", value: "grok-4.7", options: [
      { value: "grok-4.7", label: "Grok 4.7", group: "Grok Build", direct: "xAI" }, relay] }] },
  ],
  profiles: [], settings: { lang, theme: "light" },
});

function server(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state(lang));
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

const words = { en: { via: "via uni-switch", direct: "direct, not via uni-switch" }, zh: { via: "经 uni-switch", direct: "直连，不经 uni-switch" } };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    const w = words[lang];
    test(`${engine} ${lang}: an agent's own models say whether they go via uni-switch`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 980, height: 560 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(lang));
      await page.goto("http://uni-switch.test/?view=agents");
      const tops = () => page.evaluate(() => [document.scrollingElement.scrollTop, document.querySelector("#view-agents")?.scrollTop]);
      const top = await tops();
      // the path tag on the picker's line for a model
      const tagOf = async (id, label) => {
        await page.locator(`.row.agent[data-id="${id}"] > .field.ag-start`).click();
        const li = page.locator("#pop:not([hidden]) #list li", { hasText: label }).first();
        await li.waitFor();
        const tag = (await li.locator(".badge.path").allTextContents()).join(" ").trim();
        await page.keyboard.press("Escape");
        return tag;
      };
      assert.equal(await tagOf("codex", "GPT-5.4"), w.via, "Codex's own, routed through uni-switch");
      assert.equal(await tagOf("codex", "m1"), w.via, "a catalog model");
      // Grok Build, its own model set, is under the fold
      await page.locator(".agent-more").click();
      assert.equal(await tagOf("grok", "Grok 4.7"), w.direct, "Grok Build's own, straight to xAI");
      assert.deepEqual(await tops(), top, "no click moved the page");
      assert.deepEqual(errors, []);
    });
  }
}

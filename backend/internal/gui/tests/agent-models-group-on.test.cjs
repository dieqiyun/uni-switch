// Run with Node's test runner and Playwright on the module path; see README.md.
// A routing group switched on doesn't fold away the one provider's models
// (shiqi_40904 on Discord): Codex's model list with only Codex serving showed
// its 10 models, and with a group of the user's on, the list grew a Routing
// groups section and Codex's 10 started folded, so they looked gone. Folding
// tells providers apart, so routing groups don't count toward it; with two
// providers the long one still starts folded (agent-models.test.cjs).
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const codex = ["gpt-6.1-sol", "gpt-5.5", "gpt-5.4", "gpt-5.4-mini", "gpt-5.3-codex", "gpt-5.2", "gpt-5.2-codex", "gpt-5.1", "gpt-5.1-codex", "gpt-5"];
const models = (groupOn) => [
  ...(groupOn ? [{ id: "group:mygpt", name: "MyGPT", group: "Routing groups", icons: ["openai"] }] : []),
  ...codex.map((m) => ({ id: "codex/" + m, name: m.toUpperCase(), group: "Codex", icon: "openai", context: 400e3 })),
];

function fixture(lang, groupOn) {
  const list = models(groupOn);
  const count = () => {
    const by = [];
    for (const m of list) {
      let g = by.find((x) => x.name === m.group);
      if (!g) by.push(g = { name: m.group, icon: m.icon, n: 0 });
      g.n++;
    }
    return { shown: list.length, listed: list.length, by };
  };
  const state = () => ({
    agents: [{
      id: "codex", name: "Codex", path: "/test/config.toml", icon: "codex-color", wired: true,
      // set to a model none of these is, so nothing is held open by being in use
      fields: [{ key: "model", label: "model", value: "gpt-5.5", options: [{ value: "gpt-5.5", label: "GPT-5.5" }] }],
      models: count(),
    }],
    profiles: [], settings: { lang, theme: "light" },
  });
  async function serve(route) {
    const req = route.request();
    const url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state());
    if (url.pathname === "/api/agent-models/codex") return json({ models: list, count: count() });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  }
  return serve;
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    for (const groupOn of [false, true]) {
      test(`${engine} ${lang}: Codex's 10 stay open with a routing group ${groupOn ? "on" : "off"}`, async () => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        try {
          const page = await browser.newPage({ viewport: { width: 1000, height: 700 } });
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          const errors = [];
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", fixture(lang, groupOn));
          await page.goto("http://uni-switch.test/");
          const row = page.locator('.row.agent[data-id="codex"]');
          await row.locator(".ag-link").click();
          const entry = row.locator(".ag-exp .ag-chips .ag-quiet");
          await entry.waitFor();
          await page.waitForFunction(() => document.getAnimations().length === 0);
          await entry.click();
          const pop = page.locator(".am-pop:not(.leaving)");
          await pop.waitFor();
          const names = await pop.locator(".am-fold .gn").allInnerTexts();
          assert.deepEqual(names, groupOn ? [lang === "zh" ? "路由组" : "Routing groups", "Codex"] : ["Codex"]);
          const sec = pop.locator(".am-g").last();
          assert.equal(await sec.locator(".am-mr").count(), 10, "every Codex model is on the screen");
          assert.equal(await sec.locator(".am-fold").getAttribute("aria-expanded"), "true");
          if (groupOn) {
            // and the group itself is there to pick
            assert.equal(await pop.locator(".am-g").first().locator(".am-mr", { hasText: "MyGPT" }).count(), 1);
          }
          assert.deepEqual(errors, []);
        } finally {
          await browser.close();
        }
      });
    }
  }
}

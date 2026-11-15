// Run with Node's test runner and Playwright on the module path; see README.md.
// #3668: an absent Grok plan is unknown, not an invented SuperGrok tier.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const accounts = [
  { agent: "grok", plan: "SuperGrok" },
  { agent: "grok-plugin", builtin: "grok", plan: "SuperGrok Heavy" },
  { agent: "grok" },
  { agent: "grok-plugin", builtin: "grok", plan: "" },
  { agent: "cursor", plan: "pro" },
  { agent: "cursor" },
];

function serve(lang) {
  const settings = { lang, theme: "light" };
  const providers = accounts.map((account, i) => ({
    id: "plan-" + i, name: account.builtin || account.agent, icon: "xai", host: "fixture",
    models: [], agents: [], key: {}, keyList: [], headers: {}, fallback: [],
    account: { user: "fixture-" + i, agentName: "Grok", ...account },
  }));
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json({ providers, presets: [], excluded: [], gateway: { running: false } });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

test("Grok account pills preserve known plans and leave absent plans unnamed", async (t) => {
  for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      // Given: plugin and built-in accounts, with known and absent plans.
      const context = await browser.newContext({ viewport: { width: 900, height: 700 } });
      const page = await context.newPage();
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      // When: the real Providers page renders its account pills.
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator(".row.provider .key.acct").first().waitFor();
      const labels = await page.locator(".row.provider .key.acct > span").allTextContents();
      // Then: no plan is invented; other agents retain their labels.
      assert.deepEqual(labels.slice(0, 2), ["SuperGrok", "SuperGrok Heavy"], `${engine} ${lang}: known plans`);
      assert.deepEqual(labels.slice(2, 4), ["Grok", "Grok"], `${engine} ${lang}: absent Grok plan names the provider, no invented tier`);
      assert.deepEqual(labels.slice(4), ["Cursor Pro", "Cursor"], `${engine} ${lang}: other agents`);
      assert.deepEqual(errors, []);
      await context.close();
    }
  }
});

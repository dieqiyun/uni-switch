// Run with Node's test runner and Playwright on the module path; see README.md.
// The quiet line over the Providers list names every built-in subscription a
// community plugin can run ("Cursor, Grok, Kiro can run on community
// plugins…"), and its Take a look opened the first one's editor: Cursor's,
// as if the line were about Cursor alone. With several named it now opens the
// Plugins tab, whose cards offer each one's Move; with one named it still
// opens that one's editor (plugin-move.test.cjs). The line is now a
// deprecation notice whose button reads Review in Plugins. In English and
// Chinese, Chromium and WebKit; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const subs = [["cursor", "Cursor"], ["grok", "Grok"], ["kiro", "Kiro"]];
const pkg = (id) => `the retired opencode-${id}-auth`;

function serve(lang) {
  const providers = subs.map(([id, name]) => ({
    id, name, icon: id, chat: "", responses: "", anthropic: "", catalog: "", models: [{ id: "m", name: "M", on: true }],
    agents: [], fallback: [], headers: {}, keyList: [], key: {},
    account: { agent: id, agentName: name, user: "ada", logins: [{ user: "ada", active: true, on: true, own: true }] },
    move: { package: pkg(id), state: "" },
  }));
  const market = {
    listings: subs.map(([id, name]) => ({ package: pkg(id), name, icon: id, providers: [id], community: true, replaces: id, summary: { en: name, zh: name }, npm: { version: "0.1.0" } })),
    state: { bun: true, bunVersion: "1.3.0", plugins: [], movable: subs.map(([id, name]) => ({ id, name, package: pkg(id), accounts: 1 })) },
  };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers, presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [], onPlugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json(market.state);
    if (url.pathname === "/api/plugins/listings") return json({ listings: market.listings });
    if (url.pathname === "/api/plugins/market") return json(market);
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const L = { en: { look: "Review in Plugins", move: "Move" }, zh: { look: "前往插件页迁移", move: "迁移" } };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: Review in Plugins over several deprecated subscriptions opens Plugins`, async (t) => {
      const w = L[lang];
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 900, height: 700 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      await page.goto("http://uni-switch.test/?view=providers");

      const look = page.locator("#movable button", { hasText: w.look });
      await look.waitFor();
      const line = await page.locator("#movable").innerText();
      for (const [, name] of subs) assert.ok(line.includes(name), `the line doesn't name ${name}`);
      await look.click();

      await page.locator("#view-plugins").waitFor({ state: "visible" });
      assert.equal(await page.locator("#view-providers").isHidden(), true, "Providers is still open");
      assert.equal(await page.locator("#modal .editor").count(), 0, "a provider's editor opened");
      await page.locator("#view-plugins .pm-card .pm-act.move").nth(subs.length - 1).waitFor();
      const cards = await page.locator("#view-plugins .pm-card").evaluateAll((cs) => cs.filter((c) => c.querySelector(".pm-act.move")).map((c) => c.innerText));
      for (const [, name] of subs) assert.ok(cards.some((c) => c.includes(name) && c.includes(w.move)), `no card offers to move ${name}`);
      assert.deepEqual(errors, []);
    });
  }
}

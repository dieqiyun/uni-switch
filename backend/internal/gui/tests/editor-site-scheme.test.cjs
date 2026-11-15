// Run with Node's test runner and Playwright on the module path; see README.md.
// The provider editor's head links to the provider's site. With no website,
// it opens the host its base is at under that base's own scheme: a local
// server at http://127.0.0.1:7863/v1 opens over http, not https (#2377).
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const playwright = require("playwright");

const engines = (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"]);
const assets = path.resolve(__dirname, "../assets");

const providers = [
  // the reporter's: a custom OpenAI-compatible server on this computer
  { id: "test", name: "test", icon: "generic", host: "127.0.0.1:7863", chat: "http://127.0.0.1:7863/v1", models: [] },
  { id: "lan", name: "LAN", icon: "generic", host: "box.local:8080", anthropic: "HTTP://Box.Local:8080/anthropic", models: [] },
  { id: "far", name: "Far", icon: "generic", host: "api.example.com", chat: "https://api.example.com/v1", models: [] },
  // a subscription moved onto its plugin: its base is plugin://<id>, its
  // host the vendor's it had
  { id: "moved", name: "Moved", icon: "generic", host: "api.vendor.test", chat: "plugin://moved", models: [] },
  // a plugin's own provider has no address to open
  { id: "plug", name: "Plug", icon: "generic", host: "plug", chat: "plugin://plug", models: [] },
].map((p) => ({ responses: "", anthropic: "", agents: [], fallback: [], headers: {}, key: { set: false }, keyList: [], ready: true, ...p }));

for (const engine of engines) for (const lang of ["en", "zh"]) {
  test(`editor's site link keeps the base's scheme (${engine}, ${lang})`, async (t) => {
    const browser = await playwright[engine].launch();
    t.after(() => browser.close());
    const page = await browser.newPage({ viewport: { width: 420, height: 720 } });
    page.setDefaultTimeout(5000);
    page.setDefaultNavigationTimeout(60000);
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.route("**/*", async (route) => {
      const url = new URL(route.request().url());
      const json = (data) => route.fulfill({ json: data });
      if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"dark",web:true};` });
      if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
      if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "dark" } });
      if (url.pathname === "/api/providers") return json({ providers, presets: [], excluded: [], gateway: {} });
      if (url.pathname === "/api/groups") return json({ groups: [] });
      if (url.pathname.startsWith("/api/")) return json({});
      const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
      const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
      return route.fulfill({ body: await fs.readFile(file), contentType });
    });
    // in a browser the app opens a link with window.open (api "open")
    await page.addInitScript(() => { window.opened = []; window.open = (u) => { window.opened.push(u); return null; }; });
    await page.goto("http://uni-switch.test/?view=providers");
    await page.waitForFunction((l) => state.settings.lang === l, lang);
    await page.waitForFunction(() => providers?.providers?.length === 5);

    const site = async (id) => {
      await page.evaluate((id) => openModal(renderEditor(providers.providers.find((p) => p.id === id))), id);
      const b = page.locator("#modal .ehead .site");
      if (!(await b.count())) return null;
      return [await b.getAttribute("data-url"), (await b.textContent()).replace(" ↗", "")];
    };
    assert.deepEqual(await site("test"), ["http://127.0.0.1:7863", "127.0.0.1:7863"]);
    assert.deepEqual(await site("lan"), ["http://Box.Local:8080", "box.local:8080"]);
    assert.deepEqual(await site("far"), ["https://api.example.com", "api.example.com"]);
    assert.deepEqual(await site("moved"), ["https://api.vendor.test", "api.vendor.test"]);
    assert.equal(await site("plug"), null);

    // the click opens what the link says: the reporter's http page
    await site("test");
    await page.locator("#modal .ehead .site").click();
    await page.waitForFunction(() => window.opened.length > 0);
    assert.deepEqual(await page.evaluate(() => window.opened), ["http://127.0.0.1:7863"]);
    assert.deepEqual(errors, []);
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// Dani Ariyanto on Discord (uni-switch model spec --reasoning off): a model the
// user said takes no reasoning field is offered no levels by the gateway,
// so the Gateway page's row says none, though its list has some. The
// backend sends such a model reasonless with no kept levels; the row went
// back to all the list's levels. Every language, at a wide and a narrow
// window.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const providers = {
  providers: [
    { id: "kenari", name: "Kenari", icon: "generic", agents: [], models: [
      { id: "glm-5", name: "glm-5", on: true, efforts: ["low", "medium", "high"], reasonless: true, images: false, context: 128000 },
      { id: "qwen-x", name: "qwen-x", on: true, efforts: ["low", "medium", "high"], images: false, context: 64000 },
    ] },
  ],
  gateway: { running: true, window: true, mine: true, url: "http://127.0.0.1:3999", models: 2, calls: [], groups: [] },
};

function serve(lang) {
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) await new Promise((r) => setTimeout(r, 20e3));
      return json({ mine: true, now: new Date().toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const rowOf = (page, id) => page.locator("#gwModels .row.model").filter({ has: page.locator(".name", { hasText: new RegExp("^" + id.replace(/[./]/g, "\\$&") + "$") }) });

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const width of [1100, 420]) {
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      test(`${engine} ${width} ${lang}: a model sent no reasoning field shows no levels on the Gateway page`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
        const page = await (await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" })).newPage();
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${width}-${lang}-gw-reasonless.png`) });
          }
          await browser.close();
        });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        await page.goto("http://uni-switch.test/?view=gateway");
        await page.locator("#gwModels .row.model").first().waitFor();
        const chips = async (id) => (await rowOf(page, id).locator(".minfo .badge").allTextContents()).map((s) => s.trim());
        const levels = await chips("kenari/qwen-x");
        assert.equal(levels.length, 2, "a model with its levels says them: " + levels);
        assert.equal(levels[1], "64K");
        assert.deepEqual(await chips("kenari/glm-5"), ["128K"], "no levels for a model sent no reasoning field");
        assert.deepEqual(errors, []);
      });
    }
  }
}

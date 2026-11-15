// Run with Node's test runner and Playwright on the module path; see README.md.
// A custom provider's Catalog field says it decides the models' list prices
// too, not only their names and reasoning levels (#2052, mengyianx: a
// provider at AMD's own endpoint had no price, and the field's hint gave no
// sign that naming models.dev's amd there would price it). In every
// language, at a wide and a narrow window, the hint is the Catalog box's
// description and fits the window.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const amd = {
  id: "002", name: "002", icon: "", preset: "", host: "developer.amd.com.cn", chat: "https://developer.amd.com.cn/radeon/api/v1",
  responses: "", anthropic: "", catalog: "", website: "", keysUrl: "", proxy: "", balanceToken: { takes: false, set: false },
  key: { set: true, masked: "sk-…abcd" }, ready: true, chosen: [], fallback: [], routing: "", affinity: "",
  models: [{ id: "Qwen3.8-27B", name: "Qwen3.8 27B", efforts: ["low", "medium"], images: false, on: true, list: { input: 0, output: 0, cache_read: 0, cache_write: 0 } }],
  exposed: 1, unlisted: false, off: false, fetched: "just now", agents: [], sponsored: false, keyList: [],
};
const providers = { providers: [amd], presets: [], excluded: [], gateway: { running: true, window: true } };

function serve(lang) {
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { more: "More endpoints", prices: "list prices" },
  zh: { more: "更多端点", prices: "官方价格" },
  "zh-TW": { more: "更多端點", prices: "官方價格" },
  ja: { more: "その他のエンドポイント", prices: "公式価格" },
  de: { more: "Weitere Endpunkte", prices: "Listenpreise" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(words)) {
    for (const width of [1100, 420]) {
      const w = words[lang];
      test(`${engine} ${lang} ${width}: the Catalog field says it sets list prices`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await (await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        await page.goto("http://uni-switch.test/?view=providers");
        await page.locator(".row.provider", { has: page.locator(".name", { hasText: /^002$/ }) }).click();
        const ed = page.locator(".editor");
        await ed.waitFor();
        const more = ed.locator("details.more:not(.advanced)");
        await more.locator("summary", { hasText: w.more }).click();
        const hint = more.locator(".hint", { hasText: w.prices });
        assert.equal(await hint.count(), 1, "one hint names list prices");
        await hint.scrollIntoViewIfNeeded();
        assert.ok(await hint.isVisible(), "the hint is shown");
        // it describes the Catalog box
        const id = await hint.getAttribute("id");
        const box = more.locator(`input[aria-describedby~="${id}"]`);
        assert.equal(await box.count(), 1, "the hint describes one box");
        assert.match(await box.getAttribute("placeholder"), /models\.dev/);
        const b = await hint.boundingBox();
        assert.ok(b.x >= 0 && b.x + b.width <= width + 0.5, `the hint fits the window: ${b.x}+${b.width} at ${width}`);
        assert.deepEqual(errors, [], "nothing throws");
      });
    }
  }
}

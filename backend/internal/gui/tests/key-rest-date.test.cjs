// Run with Node's test runner and Playwright on the module path; see README.md.
// A key out of credit rests days at a time (creditLadder), and its note in
// the provider's key list said only the time it is back: "Resting · 402
// out of credit · back at 04:10 PM" read as this afternoon (PaulT on
// Discord). A rest that ends on another day says its date too; one that
// ends today still says the time alone. In every language, both engines,
// at a wide and a narrow window.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
// three days on at noon, and a minute on, which is today unless it is
// the last minute of the day
const later = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 3, 12, 0);
const soon = new Date(now.getTime() + 60e3);
const relay = () => ({
  id: "relay", name: "Relay", icon: "generic", chat: "https://relay.example.com/v1", responses: "", anthropic: "", catalog: "",
  models: [{ id: "big", name: "", on: true }], agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-…one" }, balanceToken: { takes: false, set: false }, proxy: "",
  keyList: [
    { id: "aaaaaaaaaa", name: "Soon", masked: "sk-…one", on: true, active: true, rest: { why: "rate", status: 429, until: soon.toISOString(), key: "relay#aaaaaaaaaa" } },
    { id: "bbbbbbbbbb", name: "Broke", masked: "sk-…two", on: true, rest: { why: "credit", status: 402, until: later.toISOString(), by: "credit", failures: 5, key: "relay#bbbbbbbbbb" } },
  ],
});

function serve(lang) {
  const state = { agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  const providers = { providers: [relay()], presets: [], excluded: [], gateway: { running: true, window: true } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const intl = { en: "en-US", zh: "zh-CN", "zh-TW": "zh-TW", ja: "ja-JP", de: "de-DE" };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(intl)) {
    for (const width of [1100, 420]) {
      test(`${engine} ${lang} ${width}px: a key resting days says the day it is back`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const context = await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        await page.goto("http://uni-switch.test/?view=providers");
        await page.locator(".row.provider", { hasText: "Relay" }).first().click();
        const broke = page.locator('.acc[data-account-id="bbbbbbbbbb"] .key-rest');
        await broke.waitFor();
        // the date as the page's own language writes it
        const date = await page.evaluate(([iso, l]) => new Date(iso).toLocaleDateString(l, { month: "short", day: "numeric" }), [later.toISOString(), intl[lang]]);
        const text = await broke.textContent();
        assert(text.includes(date), `"${text}" doesn't say ${date}`);
        if (soon.toDateString() === now.toDateString()) {
          const today = await page.locator('.acc[data-account-id="aaaaaaaaaa"] .key-rest').textContent();
          const day = await page.evaluate(([iso, l]) => new Date(iso).toLocaleDateString(l, { month: "short", day: "numeric" }), [soon.toISOString(), intl[lang]]);
          assert(!today.includes(day), `today's rest "${today}" says its date`);
        }
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1), false, "the page scrolls sideways");
        assert.deepEqual(errors, []);
      });
    }
  }
}

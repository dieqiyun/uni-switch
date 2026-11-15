// Run with Node's test runner and Playwright on the module path; see README.md.
// Kylin_Cloud on X: a provider's key was in its Accounts list, with Copy,
// the pencil that replaces it and Remove, and again below in an "API key"
// box to "paste a new key to replace it": the same thing twice. A provider
// with a key now has its Accounts alone; the box is for one with no key
// yet (a local server's, a new one). A curl command pasted into such a
// provider's Base URL fills what it can and leaves the keys alone: the
// key it carries isn't saved over the first one unseen.
// Chromium and WebKit, wide and narrow, in every language.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const base = { icon: "generic", chat: "https://relay.example.com/v1", responses: "", anthropic: "", catalog: "", models: [{ id: "m", name: "m", on: true }], agents: [], fallback: [], headers: {}, routing: "", balanceToken: { takes: false, set: false }, proxy: "", ready: true };
const relay = { ...base, id: "relay", name: "Relay", key: { set: true, masked: "sk-…one" }, keyList: [{ id: "aaaaaaaaaa", name: "", masked: "sk-…one", on: true, active: true }] };
const twoOneOn = { ...base, id: "pair", name: "Pair", key: { set: true, masked: "sk-…one" }, keyList: [{ id: "aaaaaaaaaa", name: "", masked: "sk-…one", on: true, active: true }, { id: "bbbbbbbbbb", name: "", masked: "sk-…two", on: false }] };
const local = { ...base, id: "local", name: "Local", chat: "http://127.0.0.1:11434/v1", key: { set: false, optional: true }, keyList: [] };
const curl = `curl https://relay.example.com/v1/chat/completions -H "Authorization: Bearer sk-from-curl-0123456789" -H "Content-Type: application/json" -d '{"model":"m2","messages":[]}'`;

function serve(lang, posts) {
  const providers = { providers: [relay, twoOneOn, local], presets: [], excluded: [], gateway: { running: true, window: true } };
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (req.method() === "POST" && url.pathname.startsWith("/api/provider")) posts.push({ path: url.pathname, body: req.postDataJSON() });
    if (url.pathname.startsWith("/api/provider/")) return json(providers);
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file).catch(() => ""), contentType });
  };
}

const esc = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [900, 420]) {
      test(`${engine} ${lang} ${width}px: a provider's key is in its Accounts once, not in a box below too`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
        t.after(() => browser.close());
        const page = await (await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], posts = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, posts));
        await page.goto("http://uni-switch.test/?view=providers");
        const w = await page.evaluate((lang) => Object.fromEntries(["API key", "Accounts", "Save"].map((k) => [k, lang === "en" ? k : I18N[lang][k]])), lang);
        assert.deepEqual(Object.entries(w).filter(([, v]) => !v).map(([k]) => k), []);
        const ed = page.locator(".editor");
        const label = (k) => ed.locator("label", { hasText: new RegExp(`^${esc(w[k])}$`) });
        const open = async (name) => {
          await page.locator(".row.provider", { hasText: name }).first().click();
          await ed.waitFor();
        };

        for (const name of ["Relay", "Pair"]) {
          await open(name);
          await label("Accounts").waitFor();
          assert.equal(await ed.locator(".accts.keys .acc .key-edit").first().count(), 1, `${name}: its key is replaced from its row`);
          assert.equal(await label("API key").count(), 0, `${name}: no second key box`);
          assert.equal(await ed.locator('input[placeholder*="sk-…one"]').count(), 0, `${name}: no box to paste a key over the first`);
          assert.equal(await page.evaluate(() => document.scrollingElement.scrollWidth <= innerWidth), true, "no sideways scroll");
          await page.keyboard.press("Escape");
          await ed.waitFor({ state: "detached" }).catch(() => {});
        }

        // with no key, the box is where one goes
        await open("Local");
        await label("API key").waitFor();
        assert.equal(await label("Accounts").count(), 0);
        await page.keyboard.press("Escape");
        await ed.waitFor({ state: "detached" }).catch(() => {});

        // a curl pasted into a keyed provider's Base URL leaves its keys
        await open("Relay");
        const took = await ed.locator(".base-url").evaluate((i, s) => {
          i.focus();
          const dt = new DataTransfer();
          dt.setData("text/plain", s);
          const e = new ClipboardEvent("paste", { clipboardData: dt, bubbles: true, cancelable: true });
          i.dispatchEvent(e);
          return e.defaultPrevented;
        }, curl);
        assert.ok(took, "the curl command is read");
        const y = await page.evaluate(() => document.scrollingElement.scrollTop);
        await ed.locator("button.primary", { hasText: w.Save }).click();
        for (let i = 0; i < 40 && !posts.some((p) => p.path === "/api/provider/save"); i++) await page.waitForTimeout(50);
        const saved = posts.find((p) => p.path === "/api/provider/save")?.body;
        assert.ok(saved, "saved");
        assert.ok(!saved.key, `no key saved over the first: ${JSON.stringify(saved.key)}`);
        assert.equal(await page.evaluate(() => document.scrollingElement.scrollTop), y, "a click never scrolls the page");
        assert.deepEqual(errors, []);
      });
    }
  }
}

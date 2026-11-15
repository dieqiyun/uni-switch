// Run with Node's test runner and Playwright on the module path; see README.md.
// A key set to a protocol its provider has no URL for (#1702, edussss: one
// key set to Responses, the address in the OpenAI-compatible field only)
// says so. In the editor its row has "No Responses URL", whose tooltip
// says what to do; a key set to Any, or to a protocol the provider has a
// URL for, has nothing. On the Routing page the provider tried whole on
// the URLs it has says its key is set to that protocol only. A click on
// the row's chip scrolls nothing, and nothing runs off a 420px window. In
// every language, Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const relay = () => ({
  id: "relay", name: "Relay", icon: "generic", chat: "https://relay.example.com/v1", responses: "", anthropic: "https://relay.example.com", catalog: "",
  models: [{ id: "gpt-5.5", name: "", on: true }], agents: [], fallback: [], headers: {}, routing: "",
  key: { set: true, masked: "sk-…one" }, balanceToken: { takes: false, set: false }, proxy: "",
  keyList: [
    { id: "aaaaaaaaaa", name: "resp", masked: "sk-…one", on: true, active: true, protocol: "responses" },
    { id: "bbbbbbbbbb", name: "any", masked: "sk-…two", on: true },
    { id: "cccccccccc", name: "chat", masked: "sk-…three", on: true, protocol: "chat" },
  ],
});
const route = {
  id: 1, seq: 1, time: new Date(now.getTime() - 60e3).toISOString(), agent: "codex", model: "relay/gpt-5.5", provider: "relay", done: true, status: 200, ms: 900, tokens: 1200,
  order: [{ id: "relay", provider: "relay", name: "Relay", kind: "provider", model: "gpt-5.5", noURL: "responses" }],
  tries: [{ id: "relay", model: "gpt-5.5", start: new Date(now.getTime() - 60e3).toISOString(), done: true, status: 200, ms: 900 }],
};

function serve(lang) {
  return async (request) => {
    const url = new URL(request.request().url());
    const json = (data) => request.fulfill({ json: data });
    if (url.pathname === "/boot.js") return request.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return request.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [relay()], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) return new Promise(() => {}); // nothing more comes
      return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 1 }, routes: [route] });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await request.fulfill({ body: await fs.readFile(file).catch(() => ""), contentType });
  };
}

async function open(t, engine, lang, view, width) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  t.after(() => browser.close());
  const page = await (await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" })).newPage();
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/*", serve(lang));
  await page.goto(`http://uni-switch.test/?view=${view}`);
  // what a string says in lang, its {names} filled in
  const say = (key, vars = {}) => page.evaluate(([lang, key, vars]) => {
    const s = (lang !== "en" && I18N[lang]?.[key]) || key;
    return s.replace(/\{(\w+)\}/g, (_, k) => vars[k] ?? `{${k}}`);
  }, [lang, key, vars]);
  return { page, errors, say };
}

const sideways = (page) => page.evaluate(() => document.documentElement.scrollWidth > innerWidth);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [420, 1000]) {
      test(`${engine} ${lang} ${width}px: a key set to a protocol with no URL says so in the editor`, async (t) => {
        const { page, errors, say } = await open(t, engine, lang, "providers", width);
        await page.locator(".row.provider", { hasText: "Relay" }).first().click();
        const row = (id) => page.locator(`.editor .accts.keys .acc[data-account-id="${id}"]`);
        await row("aaaaaaaaaa").waitFor();
        const api = await say("Responses");
        const chip = row("aaaaaaaaaa").locator(".key-nourl");
        assert.equal((await chip.textContent()).trim(), await say("No {api} URL", { api }));
        assert.equal(await chip.getAttribute("title"), await say("This key is set to {api} only, but {name} has no {api} URL, so no request reaches {api} with it. Fill in the {api} URL, or set the key to Any protocol.", { api, name: "Relay" }));
        // Any, and Chat (which it has a URL for), have nothing
        assert.equal(await row("bbbbbbbbbb").locator(".key-nourl").count(), 0);
        assert.equal(await row("cccccccccc").locator(".key-nourl").count(), 0);
        await chip.scrollIntoViewIfNeeded();
        const box = await chip.boundingBox();
        assert.ok(box && box.x >= 0 && box.x + box.width <= width, `the chip runs off the window: ${JSON.stringify(box)}`);
        const scrolled = () => page.evaluate(() => [window.scrollY, ...[...document.querySelectorAll("*")].filter((e) => e.scrollTop > 0).map((e) => `${e.className}:${e.scrollTop}`)].join(" "));
        const sc = await scrolled();
        await chip.click();
        assert.equal(await scrolled(), sc, "a click scrolled");
        assert.equal(await sideways(page), false, "the page scrolls sideways");
        assert.deepEqual(errors, []);
      });

      test(`${engine} ${lang} ${width}px: Routing says the provider went out on its other URLs`, async (t) => {
        const { page, errors, say } = await open(t, engine, lang, "routing", width);
        const req = page.locator(".rt-req").first();
        await req.waitFor();
        if (await req.getAttribute("aria-pressed") !== "true") await req.click();
        await page.locator(".rt-steps li.why").first().waitFor();
        const want = await say("key set to {api} only, no {api} URL · sent on the others", { api: "OpenAI Responses" });
        await page.waitForFunction((w) => [...document.querySelectorAll(".rt-act .st")].some((e) => e.textContent.trim() === w), want);
        assert.equal(await sideways(page), false, "the page scrolls sideways");
        assert.deepEqual(errors, []);
      });
    }
  }
}

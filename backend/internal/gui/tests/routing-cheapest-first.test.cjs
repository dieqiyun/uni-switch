// Run with Node's test runner and Playwright on the module path; see README.md.
// Cheapest first (routes.Cheapest, a routing group's) on the Routing page:
// a request's story says why the first went first in the words of its
// price now, as the gateway's trace gives it (Weighed.price): "costs least
// on ollama-cloud now — $0.150 in, $0.600 out a million tokens". One that
// went first even at its peak price says so, and one with no known price
// says it went in its listed place. The mode line says what Cheapest first
// does. In both engines and every language.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const ago = (minutes) => new Date(now.getTime() - minutes * 60e3).toISOString();
const key = (who, more) => ({ id: who + "#k", provider: who, name: who, who, kind: "key", model: "deepseek-v4-flash", routing: "cheap", ...more });

const scenes = {
  // DeepSeek's peak hours: ollama-cloud at its one price goes first
  peakHours: { order: [key("ollama-cloud", { price: { input: 0.15, output: 0.6 } }), key("deepseek", { price: { input: 0.3, output: 1.2, peak: true } })] },
  // cheapest even at its peak price
  stillCheapest: { order: [key("deepseek", { price: { input: 0.1, output: 0.2, peak: true } }), key("relay", { price: { input: 1, output: 4 } })] },
  // none priced: the listed first
  unpriced: { order: [key("relay"), key("other")] },
};

function serve(lang, scene) {
  const s = scenes[scene];
  const route = {
    id: 1, seq: 1, time: ago(1), agent: "codex", model: "deepseek-v4-flash", provider: s.order[0].provider, done: true, status: 200, ms: 900, tokens: 1200, order: s.order,
    tries: [{ id: s.order[0].id, model: "deepseek-v4-flash", start: ago(1), done: true, status: 200, ms: 900 }],
  };
  return async (request) => {
    const url = new URL(request.request().url());
    const json = (data) => request.fulfill({ json: data });
    if (url.pathname === "/boot.js") return request.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return request.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) return new Promise(() => {}); // nothing more comes
      return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 1 }, routes: [route] });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await request.fulfill({ body: await fs.readFile(file), contentType });
  };
}

async function open(t, engine, lang, scene, width = 1100) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  t.after(() => browser.close());
  const page = await browser.newPage({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.route("**/*", serve(lang, scene));
  await page.goto("http://uni-switch.test/?view=routing");
  const req = page.locator(".rt-req").first();
  await req.waitFor();
  if (await req.getAttribute("aria-pressed") !== "true") await req.click();
  await page.locator(".rt-steps li.why").first().waitFor();
  // what a string says in lang, its {names} filled in; a language without
  // it would say it in English, which the i18n-keys test catches
  const say = (key, vars = {}) => page.evaluate(([lang, key, vars]) => {
    const s = (lang !== "en" && I18N[lang]?.[key]) || key;
    return s.replace(/\{(\w+)\}/g, (_, k) => vars[k] ?? `{${k}}`);
  }, [lang, key, vars]);
  return { page, errors, say, why: async () => (await page.locator(".rt-steps li.why").first().textContent()).trim() };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${engine} ${lang}: the cheapest now says its price`, async (t) => {
      const { page, errors, say, why } = await open(t, engine, lang, "peakHours");
      const price = await say("{in} in, {out} out a million tokens", { in: "$0.150", out: "$0.600" });
      assert.equal(await why(), await say("Cheapest first: {model} costs least on {who} now — {price}.", { who: "ollama-cloud", model: "deepseek-v4-flash", price }));
      assert.equal((await page.locator(".rt-mode").textContent()).trim(),
        await say("Requests go to the member whose model costs least now, by its price a million tokens in and out at this hour — a vendor's peak hours count double where it bills them so, as DeepSeek and Ollama Cloud do — and the cheaper cache read when two cost the same; those that cost the same keep their listed order, and one with no known price goes last."));
      if (lang !== "en") assert.notEqual(await say("Cheapest first"), "Cheapest first", `${lang} names Cheapest first`);
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: cheapest even at its peak price`, async (t) => {
      const { errors, say, why } = await open(t, engine, lang, "stillCheapest");
      const price = await say("{in} in, {out} out a million tokens", { in: "$0.100", out: "$0.200" });
      assert.equal(await why(), await say("Cheapest first: {model} costs least on {who} now, even at its peak price — {price}.", { who: "deepseek", model: "deepseek-v4-flash", price }));
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: no price known, its listed place`, async (t) => {
      const { errors, say, why } = await open(t, engine, lang, "unpriced");
      assert.equal(await why(), await say("Cheapest first: no price is known for {model} on {who}, so it goes in its listed place after every one that has a price.", { who: "relay", model: "deepseek-v4-flash" }));
      assert.deepEqual(errors, []);
    });
  }
}

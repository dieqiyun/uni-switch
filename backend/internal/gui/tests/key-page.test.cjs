const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { fixture } = require("./fixtures/caller-keys.cjs");

// zytszone on X: uni-switch shared with a whole department, whose people can't
// be given the admin's app. Each opens the gateway's /me with their own
// gateway key and sees that key's limits, usage and models, and the setup
// for Claude Code, Codex and an OpenAI-compatible client with the address
// they opened it at. The page is the gateway's own file
// (internal/gateway/keypage.html), served as the gateway serves it, under
// its Content-Security-Policy; its data is a fixture that answers only the
// key sent as Authorization, never in the address.
const html = fs.readFileSync(path.resolve(__dirname, "../../gateway/keypage.html"), "utf8");
const CSP = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; img-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'";
const KEY = "sk-uni-switch-alice-0123456789";
const BASE = "http://gw.test:3999";
const ME = {
  name: "Alice", readOnly: false, version: "dev",
  limits: [{ period: "day", start: "2026-10-30T00:00:00Z", reset: "2026-10-31T00:00:00Z", calls: 4, tokens: 750, cost: 0, tokenLimit: 1000, tokensLeft: 250, costLeft: 0, spent: false }],
  usage: {
    today: { calls: 4, errors: 0, input: 600, output: 150, cache_read: 0, cache_write: 0, cost: 0.12, unpriced: 0 },
    "7d": { calls: 9, errors: 0, input: 1200, output: 300, cache_read: 0, cache_write: 0, cost: 0.3, unpriced: 0 },
    "30d": { calls: 20, errors: 1, input: 3000, output: 700, cache_read: 500, cache_write: 0, cost: 0.8, unpriced: 0 },
  },
  topModels: [{ id: "m2", calls: 12, errors: 0, input: 2000, output: 500, cache_read: 0, cache_write: 0, cost: 0.5, unpriced: 0 }],
  models: [{ id: "plan/m1", label: "m1 · Plan" }, { id: "plan/m2", label: "m2 · Plan" }],
};
const TITLE = { en: "Your gateway key", zh: "你的网关密钥", "zh-TW": "你的閘道金鑰", ja: "あなたのゲートウェイキー", de: "Ihr Zugangsschlüssel" };
const LOCALE = { en: "en-US", zh: "zh-CN", "zh-TW": "zh-TW", ja: "ja-JP", de: "de-DE" };

async function open(engine, lang, me = ME) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  const page = await (await browser.newContext({ viewport: { width: 390, height: 844 }, locale: LOCALE[lang], reducedMotion: "reduce" })).newPage();
  page.setDefaultTimeout(6000);
  page.setDefaultNavigationTimeout(60000);
  const seen = { urls: [], errors: [] };
  page.on("pageerror", (e) => seen.errors.push(e.message));
  page.on("request", (r) => seen.urls.push(r.url()));
  await page.route("**/*", (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/me") return route.fulfill({ contentType: "text/html; charset=utf-8", headers: { "Content-Security-Policy": CSP }, body: html });
    if (url.pathname === "/v1/uni-switch/me") {
      if (url.searchParams.has("key")) return route.fulfill({ status: 400, json: {} });
      if (req.headers().authorization !== "Bearer " + KEY) return route.fulfill({ status: 401, json: { error: { message: "no" } } });
      return route.fulfill({ json: me });
    }
    return route.fulfill({ status: 404, body: "" });
  });
  await page.goto(BASE + "/me");
  return { browser, page, seen };
}

const signIn = async (page, key) => {
  await page.fill("#key", key);
  await page.click("#go");
};
const snippet = (page) => page.locator("#snip pre").first().textContent();

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${engine} ${lang}: a key's holder sees their own key and its setup`, async (t) => {
      const { browser, page, seen } = await open(engine, lang);
      t.after(() => browser.close());
      assert.equal(await page.locator("#signin h1").textContent(), TITLE[lang]);
      // a wrong key is told so, in the page's language, and shows nothing
      await signIn(page, "sk-uni-switch-wrong");
      await page.locator("#err").waitFor();
      const err = await page.locator("#err").textContent();
      assert.ok(err.length > 10, err);
      if (lang !== "en") assert.doesNotMatch(err, /enabled gateway key/);
      assert.equal(await page.locator("#info").isHidden(), true);
      await signIn(page, KEY);
      await page.locator("#info").waitFor();
      assert.equal(await page.locator("#name").textContent(), "Alice");
      assert.equal(await page.locator("#key").inputValue(), "", "the key is taken out of the field");
      // its limit, with how much is used
      assert.equal(await page.locator("#limits .bar").count(), 1);
      assert.equal(await page.locator("#limits .bar").getAttribute("aria-valuenow"), "75");
      // its usage: three periods
      assert.equal(await page.locator("#stats .stat").count(), 3);
      assert.match(await page.locator("#top").textContent(), /m2/);
      // the model it called most is picked; the setup has the address it
      // was opened at and a placeholder, not the key
      assert.equal(await page.locator('#models button[aria-checked="true"]').getAttribute("data-id"), "plan/m2");
      let s = await snippet(page);
      const claude = JSON.parse(s);
      assert.equal(claude.env.ANTHROPIC_BASE_URL, BASE);
      assert.equal(claude.env.ANTHROPIC_MODEL, "plan/m2");
      assert.equal(claude.env.ANTHROPIC_AUTH_TOKEN, "YOUR_GATEWAY_KEY");
      await page.check("#withKey");
      assert.equal(JSON.parse(await snippet(page)).env.ANTHROPIC_AUTH_TOKEN, KEY);
      // Codex: its provider at /v1, the picked model
      await page.click('#tabs [data-tab="codex"]');
      await page.click('#models [data-id="plan/m1"]');
      s = await snippet(page);
      assert.match(s, /^model = "plan\/m1"\nmodel_provider = "uni-switch"\n/);
      assert.match(s, /base_url = "http:\/\/gw\.test:3999\/v1"/);
      assert.match(s, /wire_api = "responses"/);
      assert.match(s, new RegExp(`experimental_bearer_token = "${KEY}"`));
      await page.click('#tabs [data-tab="openai"]');
      assert.deepEqual(await page.locator("#snip .kv code").allTextContents(), [BASE + "/v1", KEY, "plan/m1"]);
      await page.click('#tabs [data-tab="curl"]');
      assert.match(await snippet(page), /curl http:\/\/gw\.test:3999\/v1\/chat\/completions/);
      // filtering the models
      await page.fill("#filter", "m1");
      assert.equal(await page.locator("#models button").count(), 1);
      // nothing sideways at a phone's width
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      // a reload in the tab keeps it; Forget takes it away
      await page.reload();
      await page.locator("#info").waitFor();
      await page.click("#forget");
      await page.locator("#signin").waitFor();
      assert.equal(await page.evaluate(() => sessionStorage.getItem("uni-switch-key")), null);
      await page.reload();
      await page.locator("#signin").waitFor();
      assert.equal(await page.locator("#info").isHidden(), true);
      // the key never went in an address
      assert.deepEqual(seen.urls.filter((u) => u.includes("alice") || u.includes("key=")), []);
      assert.deepEqual(seen.errors, []);
    });
  }

  test(`${engine}: a read-only key is shown its usage and no setup`, async (t) => {
    const { browser, page } = await open(engine, "en", { ...ME, readOnly: true, models: [], limits: [] });
    t.after(() => browser.close());
    await signIn(page, KEY);
    await page.locator("#info").waitFor();
    assert.equal(await page.locator("#setupBlock").isHidden(), true);
    assert.equal(await page.locator("#readOnly").isVisible(), true);
    assert.equal(await page.locator("#limits").textContent(), "No limit on this key.");
  });

  // the admin finds the page's address under the gateway keys, the one
  // other machines reach, and copies it
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${engine} ${lang}: the gateway keys say where their holders look`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 390, height: 844 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(6000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", fixture(lang, "light", [], { lan: true, web: true }));
      await page.goto("http://uni-switch.test/?view=gateway");
      const line = page.locator("#gatewayKeys .gw-key-page");
      await line.waitFor();
      assert.equal(await line.locator("code").textContent(), "http://192.168.1.10:3999/me");
      const text = await line.locator("span").first().textContent();
      if (lang === "en") assert.equal(text, "Key holders see their usage and setup at");
      else assert.notEqual(text, "Key holders see their usage and setup at");
      assert.equal(await line.locator("button.copy").count(), 1);
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      assert.deepEqual(errors, []);
    });
  }

  test(`${engine}: UNI_SWITCH_PUBLIC_URL is the address given, and no keys gives none`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const ctx = await browser.newContext({ viewport: { width: 1000, height: 800 }, reducedMotion: "reduce" });
    let page = await ctx.newPage();
    page.setDefaultTimeout(6000);
    page.setDefaultNavigationTimeout(60000);
    await page.route("**/*", fixture("en", "light", [], { lan: true, web: true, publicURL: "https://ai.corp.example" }));
    await page.goto("http://uni-switch.test/?view=gateway");
    await page.locator("#gatewayKeys .gw-key-page").waitFor();
    assert.equal(await page.locator("#gatewayKeys .gw-key-page code").textContent(), "https://ai.corp.example/me");
    page = await ctx.newPage();
    await page.route("**/*", fixture("en", "light", [], { lan: true, web: true, keys: [] }));
    await page.goto("http://uni-switch.test/?view=gateway");
    await page.locator("#gatewayKeys .none").waitFor();
    assert.equal(await page.locator("#gatewayKeys .gw-key-page").count(), 0);
  });
}

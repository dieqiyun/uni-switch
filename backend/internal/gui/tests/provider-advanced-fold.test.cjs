// Run with Node's test runner and Playwright on the module path; see README.md.
// The provider editor folds what few need under Advanced (the owner: "Provider
// 表单太复杂了"): headers, proxy, limits, windows and fallback sit in a closed
// fold on a provider with none of them set, beside its key and models in
// view. One set opens it; opened or closed by hand it stays so across the
// editor's redraws; and a Save that refuses a field inside opens it on that
// field. A signed-in account folds the same. It opens and closes growing
// and shrinking, the dialog round it on the same curve (the owner: "整个弹窗
// 高度的改变也应该是丝滑的动画"), its arrow turning, and at once with Reduce
// motion. In
// every language, in a window 420px wide.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const base = {
  icon: "generic", responses: "", anthropic: "", catalog: "",
  models: [{ id: "model-a", name: "Model A", on: true }], agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-…one" }, keyList: [], balanceToken: { takes: false, set: false }, proxy: "", maxConcurrency: null,
};
const plain = { ...base, id: "plain", name: "Plain", chat: "https://plain.example.com/v1" };
const proxied = { ...base, id: "proxied", name: "Proxied", chat: "https://proxied.example.com/v1", proxy: "http://127.0.0.1:7890" };
const codex = {
  ...base, id: "codex", name: "Codex", icon: "codex-color", chat: "", key: undefined,
  models: [{ id: "gpt-6", name: "GPT-6", on: true }],
  account: { agent: "codex", agentName: "Codex", user: "me@example.com", plan: "PLUS", logins: [{ user: "me@example.com", plan: "PLUS", active: true, on: true }] },
};

function serve(lang, posts) {
  const providers = { providers: [plain, proxied, codex], presets: [], excluded: [], gateway: { running: true, window: true } };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/provider/") && route.request().method() === "POST") {
      posts.push({ action: url.pathname.slice("/api/provider/".length), body: route.request().postDataJSON() });
      return json(providers);
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    const open = async (t, name, motion) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 420, height: motion ? 1600 : 760 }, reducedMotion: motion ? "no-preference" : "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const posts = [];
      await page.route("**/*", serve(lang, posts));
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator(".row.provider", { hasText: name }).click();
      await page.locator(".editor details.advanced").waitFor({ state: "attached" });
      await page.waitForTimeout(300);
      const words = await page.evaluate((l) => ({ advanced: l === "en" ? "Advanced" : I18N[l]["Advanced"], save: l === "en" ? "Save" : I18N[l]["Save"] }), lang);
      assert(words.advanced && words.save, "every string has its translation");
      return { page, errors, posts, words };
    };
    const fold = (page) => page.locator(".editor details.advanced");
    const isOpen = (page) => fold(page).evaluate((d) => d.open);

    test(`${engine} ${lang}: a provider with nothing advanced set opens with Advanced folded`, async (t) => {
      const { page, errors, words } = await open(t, "Plain");
      assert.equal(await isOpen(page), false);
      assert.equal(await fold(page).locator(":scope > summary").textContent(), words.advanced);
      assert(await page.locator(".editor .mchips, .editor .models").first().isVisible(), "the models stay in view");
      for (const sel of [".connection-limits", "input.concurrency", ".proxy-url, .seg", "input.outputs"]) {
        assert.equal(await fold(page).locator(sel).count() > 0, true, `${sel} is under Advanced`);
      }
      assert.equal(await page.locator(".editor input.price-rate").count(), 0, "price controls removed");
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, "nothing runs off the side");

      await fold(page).locator(":scope > summary").click();
      assert.equal(await isOpen(page), true);
      assert(await page.locator(".editor .connection-limits > summary").isVisible());
      // a redraw (a model ticked, the list fetched) keeps it as it was left
      await page.evaluate(() => renderProviders());
      assert.equal(await isOpen(page), true, "opened by hand stays open");
      await fold(page).locator(":scope > summary").click();
      await page.evaluate(() => renderProviders());
      assert.equal(await isOpen(page), false, "closed by hand stays closed");
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: a saved proxy is kept with Advanced folded`, async (t) => {
      const { page, errors } = await open(t, "Proxied");
      assert.equal(await isOpen(page), false);
      assert.equal(await page.locator(".editor .proxy-url").first().inputValue(), "http://127.0.0.1:7890");
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: a Save refused on a folded field opens Advanced on it`, async (t) => {
      const { page, errors, posts, words } = await open(t, "Plain");
      await fold(page).locator(":scope > summary").click();
      await page.locator(".editor .connection-limits > summary").click();
      await page.locator(".editor input.concurrency").fill("1001");
      await fold(page).locator(":scope > summary").click();
      assert.equal(await isOpen(page), false);
      await page.locator(".editor .bar").getByRole("button", { name: words.save, exact: true }).click();
      await page.locator(".editor .editor-error").waitFor();
      assert.equal(await isOpen(page), true, "opened on the field it names");
      assert.equal(await page.evaluate(() => document.activeElement?.matches("input.concurrency")), true, "and the field has the focus");
      assert.equal(posts.length, 0);

      // the context window, which no helper of its own focused before
      await page.locator(".editor input.concurrency").fill("");
      await page.locator(".editor input.contexts").fill("lots");
      await fold(page).locator(":scope > summary").click();
      await page.locator(".editor .bar").getByRole("button", { name: words.save, exact: true }).click();
      await page.waitForFunction(() => document.querySelector(".editor details.advanced").open);
      assert.equal(await page.evaluate(() => document.activeElement?.matches("input.contexts")), true);
      assert.equal(posts.length, 0);
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: Advanced grows open and shrinks shut, its arrow turning`, async (t) => {
      const { page, errors } = await open(t, "Plain", true);
      const inner = fold(page).locator(":scope > .inner");
      const arrow = () => fold(page).locator(":scope > summary").evaluate((s) => getComputedStyle(s, "::before").transform);
      const y = await page.evaluate(() => scrollY);
      await fold(page).locator(":scope > summary").click();
      assert.equal(await isOpen(page), true, "open from the click on");
      const boxMoves = () => page.evaluate(() => document.querySelector(".modal .dialog").getAnimations().length > 0);
      // a window tall enough that the dialog isn't already as tall as it goes
      assert(await boxMoves(), "the dialog grows with it");
      assert(await inner.evaluate((x) => [...x.children].some((r) => r.getAnimations().length)), "its rows come in");
      const mid = await inner.evaluate((x) => x.getBoundingClientRect().height);
      await page.waitForFunction(() => !document.querySelector(".editor details.advanced > .inner").getAnimations().length);
      const full = await inner.evaluate((x) => x.getBoundingClientRect().height);
      assert(mid < full, `it grows (${mid} then ${full})`);
      await page.waitForFunction(() => !document.getAnimations().length);
      assert.equal(await arrow(), "matrix(0, 1, -1, 0, 0, 0)", "the arrow turned");
      assert.equal(await page.evaluate(() => scrollY), y, "the click moves no page");

      await fold(page).locator(":scope > summary").click();
      assert.equal(await isOpen(page), true, "shown while it shrinks");
      assert(await inner.evaluate((x) => x.getAnimations().length) > 0, "it shrinks");
      assert(await boxMoves(), "and the dialog with it");
      await page.waitForFunction(() => !document.querySelector(".editor details.advanced").open);
      await page.waitForFunction(() => !document.getAnimations().length);
      assert.equal(await arrow(), "none", "the arrow turned back");
      // clicked again while it shrinks, it turns back and stays open
      await fold(page).locator(":scope > summary").click();
      await page.waitForTimeout(80);
      await fold(page).locator(":scope > summary").click();
      await fold(page).locator(":scope > summary").click();
      await page.waitForFunction(() => !document.querySelector(".editor details.advanced > .inner").getAnimations().length);
      assert.equal(await isOpen(page), true);
      assert.equal(await page.evaluate(() => advanced.open), true, "kept as it ends");
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: a signed-in account folds the same`, async (t) => {
      const { page, errors } = await open(t, "Codex");
      assert.equal(await isOpen(page), false);
      assert.equal(await fold(page).locator("input.price-rate").count(), 0);
      assert.equal(await fold(page).locator(".connection-limits").count(), 1);
      assert.equal(await fold(page).locator("input.contexts").count(), 1);
      assert.deepEqual(errors, []);
    });
  }
}

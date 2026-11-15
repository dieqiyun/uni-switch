// Run with Node's test runner and Playwright on the module path; see README.md.
// #875: a provider's Max output has the usual sizes under it, one click
// each, as its Context window has (8K … 128K): a click fills the box and
// lights, a size typed lights its pick, and the Save sends it. Under More
// endpoints, Anthropic URL and Responses URL stand on one line rather than
// wrap their last character (Anthropic 地址's 址) under them. English and
// Chinese; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
// the editor folds what few need under Advanced, this field among them:
// opened as a reader would, unless something set in it opened it already
const unfold = async (page) => {
  const adv = page.locator(".editor details.advanced");
  await adv.waitFor({ state: "attached" });
  if (!(await adv.evaluate((d) => d.open))) await adv.locator(":scope > summary").click();
};

function serve(lang, posts) {
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  const models = [1, 2].map((i) => ({ id: `model-${i}`, name: `Model ${i}`, on: true }));
  const provider = { id: "relay", name: "Relay", icon: "generic", chat: "https://relay.example/v1", responses: "", anthropic: "", models, agents: [], key: { set: true, masked: "sk-…1234" }, ready: true };
  const providers = { providers: [provider], presets: [], excluded: [], gateway: { running: true, window: true } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/provider/")) {
      posts.push({ path: url.pathname, body: route.request().postDataJSON() });
      return json(providers);
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    await route.fulfill({ body: await fs.readFile(file), contentType: { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)] });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: Max output's picks and More endpoints' labels`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 900, height: 720 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [], posts = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, posts));
      await page.goto("http://uni-switch.test/?view=providers");
      const zh = lang === "zh";
      await page.locator(".row.provider").click();
      await unfold(page);
      const box = page.locator("input.outputs");
      await box.waitFor();
      const picks = box.locator("xpath=following-sibling::div[contains(@class,'cxpicks')]").locator(".cxpick");
      assert.deepEqual(await picks.allInnerTexts(), ["8K", "16K", "32K", "64K", "128K"]);
      assert.equal(await page.locator(".cxpicks .cxpick.on").count(), 0, "nothing lit while empty");

      // a click fills the box and lights; a size typed lights its pick
      await picks.nth(2).click();
      assert.equal(await box.inputValue(), "32k");
      assert.deepEqual(await box.locator("xpath=following-sibling::div").locator(".cxpick.on").allInnerTexts(), ["32K"]);
      await box.fill("64000");
      assert.deepEqual(await box.locator("xpath=following-sibling::div").locator(".cxpick.on").allInnerTexts(), ["64K"]);
      await box.fill("64k, model-1=8k");
      assert.equal(await box.locator("xpath=following-sibling::div").locator(".cxpick.on").count(), 0, "a per-model value lights no pick");
      await picks.nth(4).click();

      // More endpoints: each label on one line
      await page.locator(".editor .more:not(.advanced) > summary").click();
      for (const name of zh ? ["Anthropic 地址", "Responses 地址"] : ["Anthropic URL", "Responses URL"]) {
        const l = page.locator(".editor .more .inner label", { hasText: name });
        await l.waitFor();
        const lines = await l.evaluate((e) => Math.round(e.getBoundingClientRect().height / parseFloat(getComputedStyle(e).lineHeight || "16")));
        assert.equal(lines, 1, `${name} wraps onto ${lines} lines`);
      }
      if (process.env.ARTIFACT_DIR) await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `provider-output-picks-${engine}-${lang}.png`) });

      await page.getByRole("button", { name: zh ? "保存" : "Save", exact: true }).click();
      await page.waitForFunction(() => !document.querySelector("input.outputs"));
      assert.deepEqual(posts.map((p) => p.path), ["/api/provider/save"]);
      assert.deepEqual(posts[0].body.outputs, { "*": 128000 });
      assert.deepEqual(errors, []);
    });
  }
}

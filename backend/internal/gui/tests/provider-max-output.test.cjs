// A provider's editor sets the most a reply of its models may hold, as it
// sets their context window (ARNO on Discord: 有些模型的 maxtokens 还是错误的
// …上下文长度可以手动覆盖，maxtoken 错了如何解决): one for all its models and
// model=size for one, shown as set, sent with the Save, and a value that
// isn't a number of tokens is refused before anything is sent.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function serve(lang, posts) {
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  const models = [1, 2].map((i) => ({ id: `model-${i}`, name: `Model ${i}`, on: true, output: 8000 }));
  const provider = { id: "relay", name: "Relay", icon: "generic", chat: "https://relay.example/v1", responses: "", anthropic: "", models, agents: [], key: { set: true, masked: "sk-…1234" }, ready: true, outputs: { "*": 64000, "model-2": 8000 } };
  const providers = { providers: [provider], presets: [], excluded: [], gateway: { running: true, window: true } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:true};` });
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
    test(`${engine} ${lang}: a provider's max output is set in its editor`, async (t) => {
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
      const label = page.getByText(zh ? "最大输出" : "Max output", { exact: true });
      assert(await label.isVisible(), "the field is there");
      const box = page.locator("input.outputs");
      assert.equal(await box.inputValue(), "64k, model-2=8k", "what is set is shown");
      // not a number of tokens: refused, nothing sent
      await box.fill("lots");
      await page.getByRole("button", { name: zh ? "保存" : "Save", exact: true }).click();
      await page.getByText(zh ? "最大输出：lots 不是 32k 这样的 token 数" : "Max output: lots is not a number of tokens like 32k").waitFor();
      assert.deepEqual(posts, []);
      await box.fill("32k, model-1=128k");
      await page.getByRole("button", { name: zh ? "保存" : "Save", exact: true }).click();
      await page.waitForFunction(() => !document.querySelector("input.outputs"));
      assert.deepEqual(posts.map((p) => p.path), ["/api/provider/save"]);
      assert.deepEqual(posts[0].body.outputs, { "*": 32000, "model-1": 128000 });
      assert.deepEqual(errors, []);
    });
  }

  // the hint says the size is also what a turn naming none is sent on
  // Anthropic's API as max_tokens (#1792, tensornull: Codex's turns were
  // sent a flat 16384), in every language, and fits a narrow window
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [900, 420]) {
      test(`${engine} ${lang} ${width}px: the max output hint names max_tokens on Anthropic's API`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
        t.after(() => browser.close());
        const page = await (await browser.newContext({ viewport: { width, height: 720 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, []));
        await page.goto("http://uni-switch.test/?view=providers");
        await page.locator(".row.provider").click();
        const box = page.locator("input.outputs");
        await box.waitFor();
        const hint = await box.evaluate((b) => document.getElementById(b.getAttribute("aria-describedby").split(" ").pop()).textContent);
        assert.match(hint, /max_tokens/, hint);
        assert.match(hint, /Anthropic/, hint);
        if (lang !== "en") assert.doesNotMatch(hint, /sent as max_tokens/, "translated: " + hint);
        const over = await box.evaluate((b) => {
          const h = document.getElementById(b.getAttribute("aria-describedby").split(" ").pop());
          return h.scrollWidth - h.clientWidth + Math.max(0, document.documentElement.scrollWidth - document.documentElement.clientWidth);
        });
        assert.equal(over, 0, "the hint wraps inside the window");
        assert.deepEqual(errors, []);
      });
    }
  }
}

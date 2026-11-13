// Run with Node's test runner and Playwright on the module path; see README.md.
// After Test models, the picks that didn't answer are unpicked at one click
// (H20 on Discord: 测试完很多不可用，只能一个个取消，希望有"只选可用").
// Working only isn't in the row until a test has answered. Then it sits by
// Select all and Select none, looking as they do; a click unpicks every
// pick whose last test failed and keeps those that answered and those not
// tested (a model picked after the test), says how many it unpicked, and is
// off with nothing left to unpick. A Save sends exactly the picks kept.
// Nothing scrolls the page, and no stripe. In English and Chinese,
// Chromium and WebKit, wide and narrow.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const fetched = new Date(Date.now() - 3600e3).toISOString();
const m = (id, i) => ({ id, name: id, on: i < 4 });
const ids = ["glm-5", "kimi-k3", "qwen4-max", "deepseek-v4", "minimax-m3", "gpt-oss-200b"];
const relay = {
  id: "mixed", name: "Mixed Relay", icon: "generic", host: "mixed.example.com", chat: "https://mixed.example.com/v1", responses: "", anthropic: "", catalog: "",
  models: ids.map(m), chosen: ids.slice(0, 4), fetched, agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-…one" }, keyList: [], balanceToken: { takes: false, set: false }, proxy: "",
};
// what the vendor answered: two of the four picks fail
const answers = {
  "glm-5": { ok: true, ms: 410, protocol: "openai" },
  "kimi-k3": { ok: false, status: 404, error: "model not found", protocol: "openai" },
  "qwen4-max": { ok: true, ms: 380, protocol: "openai" },
  "deepseek-v4": { ok: false, status: 503, error: "no available channel", protocol: "openai" },
};
const KEPT = ["glm-5", "minimax-m3", "qwen4-max"];

function serve(lang, posts) {
  const providers = { providers: [relay], presets: [], excluded: [], gateway: { running: true, window: true } };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (req.method() === "POST" && url.pathname.startsWith("/api/")) posts.push({ path: url.pathname, body: req.postDataJSON() || {} });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/provider/test") {
      const body = req.postDataJSON();
      return json({ results: body.test.map((model) => ({ model, ...answers[model] })) });
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { works: "Working only", all: "Select all", none: "Select none", test: "Test models", save: "Save", done: "Unpicked 2 models that didn't answer" },
  zh: { works: "只选可用", all: "全选", none: "全不选", test: "测试模型", save: "保存", done: "已取消选中 2 个没有响应的模型" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    for (const width of [900, 420]) {
      const w = words[lang];
      test(`${engine} ${lang} ${width}px: Working only unpicks the models that failed their test`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const page = await (await browser.newContext({ viewport: { width, height: 700 }, reducedMotion: "reduce" })).newPage();
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-models-works-only.png`) });
          }
          await browser.close();
        });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const posts = [];
        await page.route("**/*", serve(lang, posts));
        await page.goto("http://uni-switch.test/?view=providers");

        const editor = page.locator("#modal:not([hidden]) .editor");
        await page.locator(".row.provider", { hasText: "Mixed Relay" }).click();
        await editor.locator(".mchips .mchip").first().waitFor();
        const bulk = (name) => editor.locator(".mbulk").getByRole("button", { name, exact: true });
        const works = bulk(w.works);
        const picks = () => editor.locator(".mchips .mchip.on").evaluateAll((cs) => cs.map((c) => c.querySelector("span").textContent).sort());
        const scrolled = () => page.evaluate(() => [window.scrollY, ...[...document.querySelectorAll("*")].filter((e) => e.scrollTop > 0).map((e) => `${e.className}:${e.scrollTop}`)].join(" "));

        // not offered before a test has answered
        await bulk(w.all).waitFor();
        assert.equal(await editor.locator(".mbulk .works-only").count(), 0, "offered before any test");

        await editor.getByRole("button", { name: w.test, exact: true }).click();
         await works.waitFor();
        assert.equal(await editor.locator(".mchips .tdot.bad").count(), 2);
        const look = (b) => b.evaluate((e) => { const s = getComputedStyle(e); return [e.className.replace(" works-only", ""), s.fontSize, s.color, s.backgroundColor, s.borderLeftWidth].join("|"); });
        assert.equal(await look(works), await look(bulk(w.all)), "styled as Select all");
        assert.deepEqual(await editor.locator(".mbulk button").evaluateAll((bs) => bs.map((b) => b.textContent)), [w.all, w.none, w.works]);
        assert(await works.isEnabled());
        assert(await works.getAttribute("title"));
        const border = await page.evaluate(() => [...document.querySelectorAll(".mbulk, .mbulk *")].map((e) => getComputedStyle(e).borderLeftWidth).filter((b) => parseFloat(b) > 1));
        assert.deepEqual(border, [], "no border stripes");
        // in the editor at a narrow width, not off its edge
        const fits = await works.evaluate((b) => { const r = b.getBoundingClientRect(), e = b.closest(".editor").getBoundingClientRect(); return r.right <= e.right + 1 && r.left >= e.left - 1; });
        assert(fits, "the button runs off the editor");

        // a model picked after the test: not tested, so it stays
        await editor.locator(".mchips .mchip", { hasText: "minimax-m3" }).click();

        // the editor's body scrolled to the buttons; the click leaves it,
        // the page and the button where they are
        await editor.locator(".ebody").evaluate((b) => { b.scrollTop += b.querySelector(".mbulk").getBoundingClientRect().top - b.getBoundingClientRect().top - 8; });
        const before = await scrolled(), at = await works.evaluate((e) => Math.round(e.getBoundingClientRect().top));
        await works.click();
        assert.deepEqual(await picks(), KEPT, "the failed picks unpicked, the rest kept");
        assert.equal(await scrolled(), before, "the page moved");
        assert.equal(await works.evaluate((e) => Math.round(e.getBoundingClientRect().top)), at, "the button moved");
        assert.equal((await page.locator("#status").textContent()).trim(), w.done);
        assert(await works.isDisabled(), "nothing left to unpick, yet it is on");

        // a Save sends exactly those
        await editor.locator(".bar").getByRole("button", { name: w.save, exact: true }).click();
        for (let i = 0; i < 100 && !posts.some((p) => p.path === "/api/provider/save"); i++) await page.waitForTimeout(30);
        const saved = posts.find((p) => p.path === "/api/provider/save");
        assert(saved, "nothing saved");
        assert.deepEqual([...saved.body.models].sort(), KEPT);
        assert.deepEqual(errors, []);
      });
    }
  }
}

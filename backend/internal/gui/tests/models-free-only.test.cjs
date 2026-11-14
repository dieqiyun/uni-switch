// Run with Node's test runner and Playwright on the module path; see README.md.
// A provider's model list can be set to its free models alone (Discord:
// 是否可以支持只选择免费模型？现在只有"全选"和"全不选"两个按键). Free only
// sits by Select all and Select none when the list has both free models
// (the plan's own, m.free, or one its vendor names free, foo:free) and paid
// ones; a list with none free, or with every one free, doesn't offer it, as
// it would pick nothing or be Select all. A click leaves exactly the free
// models picked (those the filter shows, as Select all), the paid ones
// unpicked, and a Save sends those. Nothing scrolls the page, and no stripe.
// In English and Chinese, Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const fetched = new Date(Date.now() - 3600e3).toISOString();
const relay = (id, name, models, on) => ({
  id, name, icon: "generic", host: id + ".example.com", chat: `https://${id}.example.com/v1`, responses: "", anthropic: "", catalog: "",
  models, chosen: on, fetched, agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-…one" }, keyList: [], balanceToken: { takes: false, set: false }, proxy: "",
});
const m = (id, free) => ({ id, name: id, on: false, ...(free ? { free: true } : {}) });
// enough models for the filter box; the free ones: two the plan serves at
// no cost, one named free
const paid = Array.from({ length: 26 }, (_, i) => m(`paid-model-${i + 1}`));
const mixed = relay("mixed", "Mixed Relay", [m("cline-free/deepseek-v4.1-flash", true), ...paid.slice(0, 13), m("cline-free/mimo-v2.6-flash", true), ...paid.slice(13), m("qwen/qwen3.8-27b:free")], ["paid-model-1", "paid-model-2"]);
const allPaid = relay("allpaid", "Paid Relay", [m("gpt-5"), m("gpt-5-mini")], ["gpt-5"]);
const allFree = relay("allfree", "Free Relay", [m("free-a", true), m("free-b", true)], []);
const FREE = ["cline-free/deepseek-v4.1-flash", "cline-free/mimo-v2.6-flash", "qwen/qwen3.8-27b:free"];

function serve(lang, posts) {
  const providers = { providers: [mixed, allPaid, allFree], presets: [], excluded: [], gateway: { running: true, window: true } };
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
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { free: "Free only", all: "Select all", none: "Select none", save: "Save" },
  zh: { free: "只选免费", all: "全选", none: "全不选", save: "保存" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    const w = words[lang];
    test(`${engine} ${lang}: Free only picks just the free models`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const page = await (await browser.newContext({ viewport: { width: 900, height: 600 }, reducedMotion: "reduce" })).newPage();
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-models-free-only.png`) });
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
      const open = async (name) => {
        await page.locator(".row.provider", { hasText: name }).click();
        await editor.locator(".ehead b", { hasText: name }).waitFor();
        await editor.locator(".mchips .mchip").first().waitFor();
      };
      const close = async () => {
        await page.keyboard.press("Escape");
        await page.locator("#modal").waitFor({ state: "hidden" });
      };
      const bulk = (name) => editor.locator(".mbulk").getByRole("button", { name, exact: true });
      const picks = () => editor.locator(".mchips .mchip.on").evaluateAll((cs) => cs.map((c) => c.querySelector("span").textContent).sort());
      const scrolled = () => page.evaluate(() => [window.scrollY, ...[...document.querySelectorAll("*")].filter((e) => e.scrollTop > 0).map((e) => `${e.className}:${e.scrollTop}`)].join(" "));

      // none free, or all free: not offered
      for (const name of ["Paid Relay", "Free Relay"]) {
        await open(name);
        await bulk(w.all).waitFor();
        assert.equal(await bulk(w.free).count(), 0, `${name} offers Free only`);
        await close();
      }

      // free and paid: offered, beside the other two and looking as they do
      await open("Mixed Relay");
      const free = bulk(w.free);
      await free.waitFor();
      const look = (b) => b.evaluate((e) => { const s = getComputedStyle(e); return [e.className, s.fontSize, s.color, s.backgroundColor, s.borderLeftWidth].join("|"); });
      assert.equal(await look(free), await look(bulk(w.all)), "styled as Select all");
      assert.deepEqual(await editor.locator(".mbulk button").evaluateAll((bs) => bs.map((b) => b.textContent)), [w.all, w.none, w.free]);
      assert(await free.getAttribute("title"));
      const border = await page.evaluate(() => [...document.querySelectorAll(".mbulk, .mbulk *")].map((e) => getComputedStyle(e).borderLeftWidth).filter((b) => parseFloat(b) > 1));
      assert.deepEqual(border, [], "no border stripes");

      // with a filter: only the free ones it shows
      const q = editor.locator(".mbulk input");
      await q.fill("cline");
      await free.click();
      assert.deepEqual(await picks(), FREE.slice(0, 2));
      await q.fill("");
      assert.deepEqual(await picks(), FREE.slice(0, 2), "nothing else picked under the filter");

      // the editor's body scrolled down to the buttons, still in view; the
      // click leaves it, the page and the button where they are
      await editor.locator(".ebody").evaluate((b) => { b.scrollTop += b.querySelector(".mbulk").getBoundingClientRect().top - b.getBoundingClientRect().top - 8; });
      assert(await editor.locator(".ebody").evaluate((b) => b.scrollTop > 0), "the editor's body must have scrolled");
      const before = await scrolled(), at = await free.evaluate((e) => Math.round(e.getBoundingClientRect().top));
      await free.click();
      assert.deepEqual(await picks(), FREE, "exactly the free models picked");
      assert.equal(await scrolled(), before, "the page moved");
      assert.equal(await free.evaluate((e) => Math.round(e.getBoundingClientRect().top)), at, "the button moved");

      // a Save sends exactly those
      await editor.locator(".bar").getByRole("button", { name: w.save, exact: true }).click();
      for (let i = 0; i < 100 && !posts.some((p) => p.path === "/api/provider/save"); i++) await page.waitForTimeout(30);
      const saved = posts.find((p) => p.path === "/api/provider/save");
      assert(saved, "nothing saved");
      assert.deepEqual([...saved.body.models].sort(), FREE);

      const missing = await page.evaluate(() => ["Free only", "Pick only the free models (those the filter shows), unpicking the others"].filter((k) => !I18N.zh[k]));
      assert.deepEqual(missing, [], "every string has its Chinese");
      assert.deepEqual(errors, []);
    });
  }
}

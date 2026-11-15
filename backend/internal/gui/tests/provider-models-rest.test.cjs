// Run with Node's test runner and Playwright on the module path; see README.md.
// A long model list in the provider editor is drawn up to its first 80, but
// a picked model is never left out of it, and the foot of the list is a
// button that draws the rest (#685: Sun1090 picked 10 of OpenCode Zen's 84
// models and saw 7, then "… 4 more, filter to find them" with no names to
// filter by). Opening the rest doesn't move the editor. In English and
// Chinese, Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const fetched = new Date(Date.now() - 3600e3).toISOString();
const ids = Array.from({ length: 84 }, (_, i) => `zen/model-${i}`);
// ten picked, three of them past the first 80
const chosen = ["zen/model-1", "zen/model-4", "zen/model-9", "zen/model-20", "zen/model-33", "zen/model-50", "zen/model-77", "zen/model-80", "zen/model-82", "zen/model-83"];

const provider = {
  id: "zen", name: "Zen", icon: "generic", host: "zen.example.com", chat: "https://zen.example.com/v1", responses: "", anthropic: "", catalog: "",
  models: ids.map((id) => ({ id, name: id, on: chosen.includes(id) })), chosen, fetched, agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-…one" }, keyList: [], balanceToken: { takes: false, set: false }, proxy: "",
};

function serve(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/providers") return json({ providers: [provider], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const rest = { en: "Show the other 1", zh: "显示其余 1 个" };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: every picked model is in sight, and the rest are a click away`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const page = await (await browser.newContext({ viewport: { width: 900, height: 760 }, reducedMotion: "reduce" })).newPage();
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-provider-models-rest.png`) });
        }
        await browser.close();
      });
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      await page.goto("http://uni-switch.test/?view=providers");
      const editor = page.locator("#modal:not([hidden]) .editor");
      await page.locator(".row.provider", { hasText: "Zen" }).click();
      await editor.locator(".mchips .mchip").first().waitFor();
      const chips = editor.locator(".mchips").first();
      const names = (sel) => chips.locator(sel).evaluateAll((cs) => cs.map((c) => c.firstChild.textContent));

      // all ten picked are drawn, the three past 80 too; one unpicked is left out
      assert.deepEqual(await names(".mchip.on"), chosen);
      assert.equal(await chips.locator(".mchip").count(), 83);
      const more = chips.getByRole("button", { name: rest[lang], exact: true });
      await more.waitFor();
      assert.equal(await chips.getByText(/filter to find them|用筛选查找/).count(), 0);

      // the rest come in where the button was; the editor stays put
      await more.scrollIntoViewIfNeeded();
      const last = chips.locator(".mchip", { hasText: /^zen\/model-79$/ });
      const before = await last.evaluate((e) => e.getBoundingClientRect().top);
      await more.click();
      await page.waitForTimeout(300);
      assert.equal(await chips.locator(".mchip").count(), 84);
      assert.deepEqual(await names(".mchip:not(.on)"), ids.filter((id) => !chosen.includes(id)));
      assert.equal(await chips.getByRole("button", { name: rest[lang], exact: true }).count(), 0);
      assert.deepEqual(await names(".mchip.on"), chosen, "opening the rest picks nothing");
      assert.equal(await last.evaluate((e) => e.getBoundingClientRect().top), before, "the click moved the list");

      // a chip clicked keeps the rest open
      await chips.locator(".mchip", { hasText: /^zen\/model-81$/ }).click();
      assert.equal(await chips.locator(".mchip").count(), 84);
      assert.ok((await names(".mchip.on")).includes("zen/model-81"));
      assert.deepEqual(errors, []);
    });
  }
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// Codex's effort left at Default reads as Default, with the level Codex
// then takes for the model beside it (lgtm on Discord: Default turned into
// 中 on Codex · WSL Ubuntu). Its row and slider say "default (medium)", a
// level picked shows the level, and Default picked again posts "" and
// reads as Default again, not as medium.
// English, Chinese, Japanese and German, Chromium and WebKit, the window
// and the tray panel.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const levels = ["low", "medium", "high", "xhigh"];
const codex = (effort) => ({
  id: "codex", name: "Codex", icon: "codex-color", path: "/h/.codex/config.toml", wired: true,
  fields: [
    { key: "model", label: "model", value: "volc/glm", options: [{ value: "volc/glm", label: "GLM", ref: "volc/glm" }] },
    { key: "effort", label: "effort", value: effort, options: [{ value: "", note: "Codex's default for the model: medium", takes: "medium" }, ...levels.map((value) => ({ value }))] },
  ],
});
const filler = Array.from({ length: 6 }, (_, i) => ({ id: "pi" + i, name: "Pi " + i, path: "/p", fields: [{ key: "model", label: "model", value: "", options: [] }] }));

function serve(lang, sets) {
  let effort = "";
  const st = () => ({ agents: [codex(effort), ...filler], profiles: [], settings: { lang, theme: "light" } });
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: st() });
    if (url.pathname === "/api/set") {
      const b = req.postDataJSON();
      sets.push(b);
      if (b.field === "effort") effort = b.value;
      return route.fulfill({ json: st() });
    }
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { def: "default (medium)", high: "high" },
  zh: { def: "默认（中）", high: "高" },
  ja: { def: "デフォルト（中）", high: "高" },
  de: { def: "Standard (mittel)", high: "hoch" },
};
const row = `.row.agent[data-id="codex"]`;
const noSideScroll = (page) => page.evaluate(() => document.scrollingElement.scrollWidth <= innerWidth);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "ja", "de"]) {
    const w = words[lang];
    test(`${engine} ${lang}: Codex's Default effort reads as Default`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const errors = [];
      t.after(async () => {
        if (errors.length) console.log(errors);
        await browser.close();
      });
      const open = async (mode, sets, width) => {
        const page = await (await browser.newContext({ viewport: { width, height: 640 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, sets));
        await page.goto("http://uni-switch.test/" + (mode ? "?mode=" + mode : ""));
        await page.locator(row).waitFor();
        return page;
      };
      const slide = (page, i) => page.locator("#effortRange").evaluate((r, i) => {
        r.value = String(i);
        r.dispatchEvent(new Event("input", { bubbles: true }));
        r.dispatchEvent(new Event("change", { bubbles: true }));
      }, i);

      for (const width of [560, 360]) {
        await t.test(`the window at ${width}px`, async () => {
          const sets = [];
          const page = await open("", sets, width);
          let field = page.locator(`${row} .field[data-key="effort"]`).first();
          if (!(await field.isVisible())) {
            await page.locator(`${row} .ag-link`).click();
            field = page.locator(`${row} .ag-exp .field[data-key="effort"]`);
          }
          await field.waitFor();
          assert.equal(await field.locator(".v").textContent(), w.def);
          const box = await field.boundingBox();
          assert(box.x >= 0 && box.x + box.width <= width, `the effort runs off the window: ${JSON.stringify(box)}`);

          await field.click();
          await page.locator("#effortControl:not([hidden])").waitFor();
          assert.equal(await page.locator("#effortValue").textContent(), w.def);
          assert.equal(await page.locator("#effortMin").textContent(), w.def);
          // Default, then the model's four levels: no stop of its own added
          assert.equal(await page.locator("#effortTicks i").count(), 5);
          await slide(page, 3);
          assert.equal(await page.locator("#effortValue").textContent(), w.high);
          await page.waitForFunction(() => document.querySelector('.row.agent[data-id="codex"] .field[data-key="effort"] .v')?.textContent !== document.querySelector("#effortMin")?.textContent);
          await slide(page, 0);
          assert.equal(await page.locator("#effortValue").textContent(), w.def);
          for (let i = 0; i < 100 && sets.length < 2; i++) await page.waitForTimeout(50);
          await page.waitForFunction((d) => [...document.querySelectorAll('.row.agent[data-id="codex"] .field[data-key="effort"] .v')].some((v) => v.textContent === d), w.def);
          assert.deepEqual(sets, [{ agent: "codex", field: "effort", value: "high" }, { agent: "codex", field: "effort", value: "" }]);
          assert(await noSideScroll(page), "the window scrolls sideways");
        });
      }

      await t.test("the panel", async () => {
        const sets = [];
        const page = await open("panel", sets, 440);
        assert.equal(await page.locator(`${row} .ag-sum .eff`).getAttribute("data-l"), "0");
        await page.locator(`${row} .ag-sum`).click();
        await page.waitForTimeout(700);
        const box = page.locator(`${row} .ag-open .effort-control`);
        assert.equal(await box.locator(".effort-ticks i").count(), 5);
        assert.equal(await box.locator(".effort-head b").textContent(), w.def);
        assert.equal(await box.locator(".effort-ends span").first().textContent(), w.def);
        await box.locator(".eslide").press("ArrowRight");
        await page.waitForTimeout(300);
        assert.deepEqual(sets, [{ agent: "codex", field: "effort", value: "low" }]);
        assert(await noSideScroll(page), "the panel scrolls sideways");
      });

      assert.deepEqual(errors, []);
    });
  }
}

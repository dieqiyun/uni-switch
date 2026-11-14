// Run with Node's test runner and Playwright on the module path; see README.md.
// Opening the Gateway page's models leaves the heads over them where they
// were. The view is a flex column, and once the open list made it taller
// than the window it shrank every head of a set height: Connect's from 26px
// to 21 and the models' to their search field's 24, so the models' head
// moved up ~5px (the owner, 10-29: "展开模型的时候，这块部分会整体向上移动几个像素").
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
// enough models for the search field, and for the open list to run past the window
const models = Array.from({ length: 30 }, (_, i) => ({ id: "model-" + i, name: "Model " + i, on: true }));
const providers = {
  providers: [{ id: "fixture", name: "Fixture", icon: "generic", models, agents: [] }],
  gateway: { running: true, window: true, mine: true, url: "http://127.0.0.1:3999", calls: [], groups: [] },
};

function serve(lang, web) {
  const state = { agents: [], profiles: [], settings: { lang, theme: "dark" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"dark",web:${web}};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) await new Promise((r) => setTimeout(r, 20e3));
      return json({ mine: true, now: new Date().toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const heads = (page) => page.evaluate(() => {
  const box = (e) => { const r = e.getBoundingClientRect(); return { top: r.top, height: r.height }; };
  return {
    connect: box(document.querySelector("#foldConnect").parentElement),
    models: box(document.querySelector("#foldModels").parentElement),
    label: box(document.querySelector("#foldModels .label")),
    // every head on the page, Recent calls' under the list too
    all: [...document.querySelectorAll("#view-gateway > .row-head")].filter((h) => h.offsetParent).map((h) => h.getBoundingClientRect().height),
  };
});

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    for (const web of [false, true]) {
      test(`${engine} ${lang}${web ? " web" : ""}: opening the models moves no head over them`, async (t) => {
        assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const context = await browser.newContext({ viewport: { width: 1100, height: 700 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.addInitScript(() => { try { localStorage.setItem("uni-switch.gwModelsFolded", "1"); } catch {} });
        await page.route("**/*", serve(lang, web));
        await page.goto("http://uni-switch.test/?view=gateway");
        await page.locator("#modelsCount").filter({ hasText: String(models.length) }).waitFor();
        const fold = page.locator("#foldModels");
        assert.equal(await fold.getAttribute("aria-expanded"), "false", "the models start folded");
        const was = await heads(page);

        await fold.click();
        await page.locator("#gwModels .row.model").first().waitFor();
        await page.locator("#findModel").waitFor();
        assert(await page.locator("#view-gateway").evaluate((v) => v.scrollHeight > v.clientHeight), "open, the page is taller than the window");
        const is = await heads(page);
        assert.deepEqual(is.all, was.all, "every head keeps its height");
        for (const k of ["connect", "models", "label"]) {
          assert(Math.abs(is[k].top - was[k].top) <= 0.5, `opening moved ${k} ${(is[k].top - was[k].top).toFixed(1)}px`);
          assert(Math.abs(is[k].height - was[k].height) <= 0.5, `opening took ${k} from ${was[k].height}px to ${is[k].height}px`);
        }

        await fold.click();
        await page.locator("#gwModels").waitFor({ state: "hidden" });
        const back = await heads(page);
        assert(Math.abs(back.label.top - was.label.top) <= 0.5, "folded again, the head is back where it was");
        assert.deepEqual(errors, []);
      });
    }
  }
}

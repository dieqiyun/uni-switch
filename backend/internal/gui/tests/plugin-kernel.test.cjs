// Run with Node's test runner and Playwright on the module path; see README.md.
// The window's plugin kernel (kernel.js), with the Gateway page, the first
// page a plugin adds (plugins/gateway). A page whose code throws breaks
// only itself: it says what broke and offers Try again, and the other
// pages go on; Try again draws it once its code works. A plugin's setting
// reaches its page: from boot.js before the first paint, and when Plugins
// › Built-in changes it. Every language, both engines, a narrow window.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = Date.now();
const calls = Array.from({ length: 60 }, (_, i) => ({
  time: new Date(now - (i + 1) * 60e3).toISOString(),
  agent: "fixture",
  model: `fixture/model-${i}`,
  status: 200,
  ms: 900,
}));
const providers = {
  providers: [{ id: "fixture", name: "Fixture", icon: "generic", models: [{ id: "model-0", name: "Model", on: true }], agents: [] }],
  presets: [],
  gateway: { running: true, window: true, mine: true, url: "http://127.0.0.1:3999", calls, groups: [] },
};

// the Gateway page's code, broken while window.__break says so: its
// drawing throws as a plugin's bug would
const BREAK = `
;(() => {
  const real = renderGatewayView;
  renderGatewayView = function () {
    if (window.__break) throw new Error("fixture: the page broke");
    return real();
  };
})();
`;

function serve(lang, config) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",pluginConfig:${JSON.stringify(config)}};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/caller-keys") return json({ keys: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    if (url.host !== "uni-switch.test") return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml" }[path.extname(file)];
    let body;
    try { body = await fs.readFile(file); } catch { return route.fulfill({ status: 404, body: "" }); }
    if (url.pathname === "/plugins/gateway/client.js") body = body.toString() + BREAK;
    await route.fulfill({ body, contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: a plugin's page that throws breaks only itself, and Try again draws it`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      for (const width of [1100, 420]) {
        const where = `${engine} ${lang} ${width}`;
        const context = await browser.newContext({ viewport: { width, height: 700 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], logged = [];
        page.on("pageerror", (e) => errors.push(e.message));
        page.on("console", (m) => { if (m.type() === "error") logged.push(m.text()); });
        await page.addInitScript(() => { window.__break = true; });
        await page.route("**/*", serve(lang, {}));
        await page.goto("http://uni-switch.test/?view=gateway");
        const card = page.locator("#view-gateway .slot-error");
        await card.waitFor();
        assert.equal(await card.getAttribute("role"), "alert", where);
        assert.equal(await card.locator(".name").textContent(), await page.evaluate(() => t("This page stopped working")), where);
        if (lang !== "en") assert.notEqual(await card.locator(".name").textContent(), "This page stopped working", `${where}: translated`);
        assert.equal(await card.locator(".sub").textContent(), "fixture: the page broke", `${where}: what broke`);
        assert.equal(await page.locator("#view-gateway").getAttribute("aria-busy"), null, `${where}: not left loading`);
        assert.ok(logged.some((l) => l.includes("gateway") && l.includes("fixture: the page broke")), `${where}: logged`);
        // nothing escaped the boundary, and the card fits
        assert.deepEqual(errors, [], where);
        assert.ok(await page.evaluate(() => { const v = document.querySelector("#view-gateway"); return v.scrollWidth <= v.clientWidth + 1; }), `${where}: fits`);

        // the other pages go on: Providers draws
        await page.evaluate(() => show("providers"));
        await page.locator("#view-providers #addProvider").waitFor();
        assert.equal(await page.locator("#view-gateway").isVisible(), false, `${where}: Providers shown`);
        // back on the page, still broken, it still says so and draws nothing
        await page.evaluate(() => show("gateway"));
        await card.waitFor();
        assert.equal(await page.locator("#view-gateway #activity").count(), 0, `${where}: nothing half drawn`);

        // fixed, Try again draws it whole, and it keeps working
        await page.evaluate(() => { window.__break = false; });
        await card.locator(".slot-again").click();
        await page.locator("#activity .call-item").first().waitFor();
        assert.equal(await card.count(), 0, `${where}: the card gone`);
        assert.ok((await page.locator("#gateway").evaluate((g) => g.childElementCount)) > 0, `${where}: the status card`);
        // its markup came back translated, and its wiring with it
        assert.equal((await page.locator("#foldConnect .label").textContent()).trim(), await page.evaluate(() => t("Connect")), `${where}: translated again`);
        await page.locator("#foldConnect").click();
        await page.waitForFunction(() => !document.querySelector("#connect").hidden);
        await page.locator("#foldConnect").click();
        await page.waitForFunction(() => document.querySelector("#connect").hidden);
        assert.deepEqual(errors, [], where);
        await context.close();
      }
    }
  });

  test(`${engine}: the Gateway page shows as many recent calls at a time as its setting says`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      const where = `${engine} ${lang}`;
      const context = await browser.newContext({ viewport: { width: 420, height: 700 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, { gateway: { callsPage: 10 } }));
      await page.goto("http://uni-switch.test/?view=gateway");
      const rows = page.locator("#activity .call-item");
      await rows.first().waitFor();
      // from boot.js, before the first paint
      assert.equal(await rows.count(), 10, `${where}: boot.js's 10`);
      const more = page.locator("#activity .activity-more");
      assert.equal(await more.textContent(), await page.evaluate(() => t("Show {n} more", { n: 10 })), where);
      await more.evaluate((b) => b.click());
      assert.equal(await rows.count(), 20, `${where}: 10 more`);
      // Plugins › Built-in sets 50 (applyFeatures with the list it posts back)
      await page.evaluate(() => applyFeatures([{ id: "gateway", on: true, schema: { type: "object", properties: {}, "x-order": [] }, config: { callsPage: 50 } }]));
      await page.waitForFunction(() => document.querySelectorAll("#activity .call-item").length === 50);
      assert.equal(await more.textContent(), await page.evaluate(() => t("Show {n} more", { n: 10 })), `${where}: the 10 left`);
      assert.deepEqual(errors, [], where);
      await context.close();
    }
  });
}

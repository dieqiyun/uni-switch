// Run with Node's test runner and Playwright on the module path.
// Recent calls starts at one page and can unroll the calls the gateway kept.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = Date.now();
const calls = Array.from({ length: 40 }, (_, i) => ({
  time: new Date(now - (i + 1) * 60e3).toISOString(),
  agent: "fixture",
  model: `fixture/model-${i}`,
  status: 200,
  ms: 900,
}));
const providers = {
  providers: [{ id: "fixture", name: "Fixture", icon: "generic", models: [{ id: "model-0", name: "Model", on: true }], agents: [] }],
  gateway: { running: true, window: true, mine: true, url: "http://127.0.0.1:3999", calls, groups: [] },
};

async function serve(route) {
  const url = new URL(route.request().url());
  const json = (data) => route.fulfill({ json: data });
  if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: 'window.bootPrefs = {lang:"en",theme:"light",web:true};' });
  if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
  if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang: "en", theme: "light" } });
  if (url.pathname === "/api/providers") return json(providers);
  if (url.pathname === "/api/groups") return json({ groups: [] });
  if (url.pathname === "/api/gateway/trace") return json({ mine: true, now: new Date(now).toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
  if (url.pathname.startsWith("/api/")) return json({});
  const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
  const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css" }[path.extname(file)];
  await route.fulfill({ body: await fs.readFile(file), contentType });
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: recent calls can show the calls the gateway kept`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    const context = await browser.newContext({ viewport: { width: 900, height: 560 }, reducedMotion: "reduce" });
    const page = await context.newPage();
    page.setDefaultTimeout(5000);
    page.setDefaultNavigationTimeout(60000);
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.route("**/*", serve);
    t.after(() => browser.close());

    await page.goto("http://uni-switch.test/?view=gateway");
    const rows = page.locator("#activity .call-item");
    await rows.first().waitFor();
    assert.equal(await rows.count(), 20, "the first page");
    const more = page.locator("#activity .activity-more");
    assert.equal(await more.textContent(), "Show 20 more");
    // reach it as a reader does, with the wheel: the page holds its scroll
    // against a scroll nobody asked for, which a locator's click would be
    await page.mouse.move(450, 300);
    for (let i = 0; i < 10 && !(await more.evaluate((b) => b.getBoundingClientRect().bottom <= innerHeight)); i++) await page.mouse.wheel(0, 300);
    const top = () => page.evaluate(() => document.querySelector("#view-gateway").scrollTop);
    const before = await top();
    await more.click();
    assert.equal(await rows.count(), 40, "every call the gateway keeps");
    assert.equal(await top(), before, "the click doesn't move the page");
    assert.equal(await page.locator("#activity .activity-more").count(), 0, "no button past the end");
    assert.deepEqual(errors, []);
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// Hide emails masks the whole of an address a vendor has half masked
// itself (#236: Zhipu shows abc***gh@abcd.com, and only ***gh@abcd.com
// was hidden, leaving abc in sight), in the text and in a tooltip, and
// puts it back as it was.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function serve() {
  const providers = { providers: [], presets: [], excluded: [], gateway: { running: true, window: true } };
  const state = { agents: [], profiles: [], settings: { lang: "en", theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"en",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const TEXT = "ZCode as abc***gh@abcd.com, Codex as dev.one@example.com";

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: a half-masked address is hidden whole`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const context = await browser.newContext({ viewport: { width: 900, height: 760 }, reducedMotion: "reduce" });
    const page = await context.newPage();
    page.setDefaultTimeout(5000);
    page.setDefaultNavigationTimeout(60000);
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.route("**/*", serve());
    t.after(() => browser.close());
    await page.goto("http://uni-switch.test/?view=routing");
    await page.evaluate(() => { try { localStorage.removeItem("uni-switch.maskEmails"); } catch {} });
    await page.evaluate((text) => {
      const d = document.createElement("div");
      d.id = "sample";
      d.textContent = text;
      d.title = "abc***gh@abcd.com";
      document.querySelector("#view-routing").append(d);
    }, TEXT);
    await page.locator("#rtMask").click();
    await page.waitForFunction(() => document.querySelectorAll("#sample .pii").length === 2);
    const got = await page.evaluate(() => {
      const s = document.querySelector("#sample");
      return { text: s.textContent, raws: [...s.querySelectorAll(".pii")].map((e) => e.dataset.raw), title: s.title };
    });
    assert.deepEqual(got.raws, ["abc***gh@abcd.com", "dev.one@example.com"]);
    assert.doesNotMatch(got.text, /abc|gh@abcd|dev\.one/);
    assert.match(got.text, /^ZCode as .+, Codex as .+$/);
    assert.equal(got.title, "••••••••@••••.•••");
    // a redraw is masked as it comes
    await page.evaluate(() => { const d = document.createElement("span"); d.id = "later"; d.textContent = "x***y@z.cn"; document.querySelector("#view-routing").append(d); });
    await page.waitForFunction(() => document.querySelector("#later .pii, #later.pii, #view-routing .pii[data-raw='x***y@z.cn']"));
    await page.locator("#rtMask").click();
    await page.waitForFunction(() => !document.querySelector("#view-routing .pii"));
    assert.equal(await page.locator("#sample").textContent(), TEXT);
    assert.equal(await page.locator("#sample").getAttribute("title"), "abc***gh@abcd.com");
    assert.deepEqual(errors, []);
  });
}

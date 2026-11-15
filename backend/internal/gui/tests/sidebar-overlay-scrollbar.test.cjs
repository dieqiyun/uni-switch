// Run with Node's test runner and Playwright on the module path; see README.md.
// A short window's sidebar (app.js sideBars): its pages run longer than the
// window, and no bar stands beside them at rest. A thin one is laid over
// their edge while they scroll and fades once they stop, as a Mac's overlay
// scroll bar does, and it can be dragged (xybio on Discord).
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function serve(lang) {
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { url: "http://127.0.0.1:3425", running: true, window: true } });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const frame = (page) => page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
const bar = (page) => page.evaluate(() => {
  const box = document.querySelector("#side .side-scroll");
  const b = [...document.querySelectorAll("#side .side-bar")][0];
  const th = b?.querySelector("i");
  return {
    gutter: box.offsetWidth - box.clientWidth,
    long: box.scrollHeight > box.clientHeight,
    shown: !!b && !b.hidden && getComputedStyle(b).opacity !== "0",
    hidden: !b || b.hidden,
    thumb: th ? th.getBoundingClientRect() : null,
    box: box.getBoundingClientRect(),
    top: box.scrollTop,
    max: box.scrollHeight - box.clientHeight,
  };
});

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: the sidebar's overlay scroll bar`, async (t) => {
    // Chromium's headless shell hides every bar unless told not to, which
    // would hide the gutter this is about
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium", ignoreDefaultArgs: ["--hide-scrollbars"] }));
    t.after(() => browser.close());

    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      await t.test(`${lang}: no bar at rest, one while it scrolls`, async () => {
        const context = await browser.newContext({ viewport: { width: 1100, height: 320 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.addInitScript(() => { try { localStorage.setItem("uni-switch.navLayout", "side"); } catch {} });
        await page.route("**/*", serve(lang));
        await page.goto("http://uni-switch.test/");
        await page.waitForSelector("#nav button.on");
        await page.locator("#side").waitFor();
        await frame(page);

        let s = await bar(page);
        assert.ok(s.long, "a short window's sidebar runs longer than it");
        assert.equal(s.gutter, 0, "no bar takes room beside the pages");
        assert.ok(!s.shown, "and none is drawn at rest");

        // scrolled, the thin bar shows over its edge, the thumb where it is
        await page.locator("#side .side-scroll").hover();
        await page.mouse.wheel(0, 60);
        await page.waitForFunction(() => document.querySelector("#side .side-scroll").scrollTop > 0);
        await frame(page);
        s = await bar(page);
        assert.ok(s.shown, "shown while it scrolls");
        assert.ok(s.thumb.width > 0 && s.thumb.width <= 8, `thin: ${s.thumb.width}`);
        assert.ok(s.thumb.right <= s.box.right && s.thumb.left >= s.box.right - 14, "over the sidebar's own edge");
        assert.ok(s.thumb.top >= s.box.top && s.thumb.bottom <= s.box.bottom + 1, "within the box it scrolls");

        // stopped, it fades
        await page.mouse.move(600, 160);
        await page.waitForFunction(() => getComputedStyle(document.querySelector("#side .side-bar")).opacity === "0", null, { timeout: 4000 });

        // shown again and dragged to the end, the box follows to its end
        // measured only once the scroll has placed the thumb: before its
        // scroll event the thumb still sits where the wheel left it, and a
        // press there lands on the bar below it, which pages the box instead
        await page.locator("#side .side-scroll").evaluate((b) => new Promise((done) => {
          b.addEventListener("scroll", () => requestAnimationFrame(() => done()), { once: true });
          b.scrollTop = 1;
        }));
        await frame(page);
        s = await bar(page);
        const x = s.thumb.left + s.thumb.width / 2;
        await page.mouse.move(x, s.thumb.top + 4);
        await page.mouse.down();
        await page.mouse.move(x, s.box.bottom + 200, { steps: 6 });
        await page.mouse.up();
        await frame(page);
        s = await bar(page);
        assert.ok(s.top >= s.max - 1, `dragged to the end: ${s.top} of ${s.max}`);
        assert.ok(Math.abs(s.thumb.bottom - s.box.bottom) <= 2, "the thumb at the bottom");
        assert.deepEqual(errors, []);
        await context.close();
      });
    }

    await t.test("a tall window's sidebar has no bar at all", async () => {
      const context = await browser.newContext({ viewport: { width: 1100, height: 1000 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      await page.addInitScript(() => { try { localStorage.setItem("uni-switch.navLayout", "side"); } catch {} });
      await page.route("**/*", serve("en"));
      await page.goto("http://uni-switch.test/");
      await page.waitForSelector("#nav button.on");
      await frame(page);
      const s = await bar(page);
      assert.ok(!s.long);
      assert.ok(s.hidden, "nothing to scroll, no bar");
      await context.close();
    });
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// The sidebar's edge (app.js sideDrag): dragged, the sidebar is as wide as
// the reader wants, between its narrowest and widest, kept for the next time
// the window opens; dragged well past its narrowest it folds away, as a Mac
// app's sidebar does (enzyme_dev on X).
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function serve(lang, web = false) {
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:${web}};` });
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
const width = (page) => page.evaluate(() => Math.round(document.querySelector("#side").getBoundingClientRect().width));
const padding = (page) => page.evaluate(() => Math.round(parseFloat(getComputedStyle(document.body).paddingLeft)));
const side = (page) => page.evaluate(() => document.body.classList.contains("side-nav"));
// the edge dragged from where it is to x, in steps as a hand moves
async function drag(page, x, { up = true } = {}) {
  const e = await page.locator("#sideEdge").boundingBox();
  const y = e.y + e.height / 2;
  await page.mouse.move(e.x + e.width / 2, y);
  await page.mouse.down();
  await page.mouse.move(x, y, { steps: 8 });
  if (up) await page.mouse.up();
  // a move is drawn on the next frame, WebKit's a frame later
  await frame(page);
}
// as wide as asked, to the pixel the hand's half-pixel start leaves
const near = (got, want, what) => assert.ok(Math.abs(got - want) <= 1, `${what}: ${got} for ${want}`);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: the sidebar's edge`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());

    await t.test("dragged, it follows the hand and is kept", async () => {
      const context = await browser.newContext({ viewport: { width: 1280, height: 760 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve("en"));
      await page.goto("http://uni-switch.test/");
      await page.waitForSelector("#nav button.on");
      assert.equal(await width(page), 212, "app.css's width to start");

      // the edge is a separator a reader can find, and doesn't drag the window
      assert.equal(await page.locator("#sideEdge").getAttribute("role"), "separator");
      assert.equal(await page.locator("#sideEdge").evaluate((e) => getComputedStyle(e).cursor), "col-resize");
      assert.equal(await page.locator("#sideEdge").evaluate((e) => getComputedStyle(e).getPropertyValue("--wails-draggable").trim()), "no-drag");

      // following the hand while it moves, the page beside it
      await drag(page, 262, { up: false });
      near(await width(page), 262, "the sidebar under the hand mid-drag");
      near(await padding(page), 262, "the page beside it");
      await page.mouse.up();
      assert.equal(await page.evaluate(() => document.body.classList.contains("side-drag")), false);
      near(+await page.locator("#sideEdge").getAttribute("aria-valuenow"), 262, "the separator's value");

      // no wider than its widest, no narrower than its narrowest
      await drag(page, 700);
      assert.equal(await width(page), 320);
      await drag(page, 130);
      assert.equal(await width(page), 168);
      // at its narrowest every page's name, Settings and the uni-switch read whole
      const cut = await page.locator("#side :is(.nav-gl, button[data-view] > span, #prefs .side-l, .brand)").evaluateAll((es) => es.filter((e) => e.scrollWidth > e.clientWidth + 1 || e.getBoundingClientRect().right > 168).map((e) => e.textContent.trim()));
      assert.deepEqual(cut, []);

      // the keys move it too
      await page.locator("#sideEdge").focus();
      await page.keyboard.press("ArrowRight");
      await frame(page);
      assert.equal(await width(page), 176);
      await page.keyboard.press("Shift+ArrowRight");
      await frame(page);
      assert.equal(await width(page), 208);

      // kept for the next time the window opens (the sidebar picked, for
      // the narrow window below)
      await page.evaluate(() => localStorage.setItem("uni-switch.navLayout", "side"));
      await page.reload();
      await page.waitForSelector("#nav button.on");
      await page.locator("#side").waitFor();
      // the stylesheet laid out, not only the sidebar put in
      await page.waitForFunction(() => parseFloat(getComputedStyle(document.body).paddingLeft) > 0);
      assert.equal(await width(page), 208);
      assert.equal(await padding(page), 208);

      // a narrow window keeps the page its room: a third of it and a little
      await drag(page, 320);
      await page.setViewportSize({ width: 760, height: 760 });
      await frame(page);
      assert.ok(await side(page), "the sidebar the reader picked, at any width");
      assert.equal(await width(page), Math.round(760 * 0.36));
      await page.setViewportSize({ width: 1280, height: 760 });
      await frame(page);
      assert.equal(await width(page), 320, "and back as wide as it was");
      assert.deepEqual(errors, []);
      await context.close();
    });

    await t.test("dragged well past its narrowest, it folds away", async () => {
      const context = await browser.newContext({ viewport: { width: 1280, height: 760 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      await page.route("**/*", serve("zh"));
      await page.goto("http://uni-switch.test/");
      await page.waitForSelector("#nav button.on");
      await drag(page, 240);
      // short of folding it stays at its narrowest
      await drag(page, 120, { up: false });
      await frame(page);
      assert.ok(await side(page));
      assert.equal(await width(page), 168);
      await page.mouse.move(60, 300, { steps: 4 });
      await page.waitForFunction(() => !document.body.classList.contains("side-nav"));
      await page.mouse.up();
      assert.equal(await page.evaluate(() => document.body.classList.contains("side-drag")), false);
      assert.ok(await page.locator(".top #nav").isVisible(), "the pages are the header's tabs");
      // shown again with the button, as wide as it was before the fold
      await page.locator("#sideToggle").click();
      await page.waitForFunction(() => document.body.classList.contains("side-nav"));
      await frame(page);
      near(await width(page), 240, "as wide as before");
      await context.close();
    });

    await t.test("uni-switch web on an iPad: a finger drags it", async () => {
      const context = await browser.newContext({ viewport: { width: 1180, height: 820 }, reducedMotion: "reduce", hasTouch: true, isMobile: engine === "chromium" });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      await page.route("**/*", serve("en", true));
      await page.goto("http://uni-switch.test/");
      await page.waitForSelector("#nav button.on");
      assert.ok(await side(page), "an iPad's landscape width has the sidebar");
      const e = await page.locator("#sideEdge").boundingBox();
      assert.ok(e.width >= 24, `a finger's reach: ${e.width}`);
      // a touch drag, by the pointer events a finger sends
      const cdp = engine === "chromium" ? await context.newCDPSession(page) : null;
      if (!cdp) return context.close(); // WebKit's Playwright sends no touch moves
      const y = e.y + 300, at = (x) => [{ x, y }];
      await cdp.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: at(e.x + e.width / 2) });
      for (const x of [230, 250, 270, 280]) await cdp.send("Input.dispatchTouchEvent", { type: "touchMove", touchPoints: at(x) });
      await cdp.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
      await frame(page);
      near(await width(page), 280, "the sidebar where the finger left it");
      await context.close();
    });

    await t.test("uni-switch web on a phone: no sidebar, no edge", async () => {
      const context = await browser.newContext({ viewport: { width: 390, height: 844 }, reducedMotion: "reduce", hasTouch: true, isMobile: engine === "chromium" });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      await page.addInitScript(() => { try { localStorage.setItem("uni-switch.navLayout", "side"); localStorage.setItem("uni-switch.sideWidth", "320"); } catch {} });
      await page.route("**/*", serve("zh", true));
      await page.goto("http://uni-switch.test/");
      await page.waitForSelector("#nav button.on");
      assert.equal(await side(page), false);
      assert.equal(await page.locator("#sideEdge").isVisible(), false);
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth), "nothing scrolls sideways");
      await context.close();
    });
  });
}

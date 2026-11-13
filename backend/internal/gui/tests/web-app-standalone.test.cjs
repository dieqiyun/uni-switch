// Run with Node's test runner and Playwright on the module path; see README.md.
// uni-switch web added to an iPhone's home screen opened in Safari with its
// address bar (ninja_s6114 on Discord). The page now names a web app
// manifest and the iOS meta tags, so it opens as an app of its own. Opened
// so, with the status bar, the notch and the home indicator over the
// screen's edges, the header's uni-switch, icons and tabs are inside the safe
// area; the theme colour is the app window's alone, so a browser's tab
// and the desktop app's window take none of it.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const os = require("node:os");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit, devices } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function serve(web) {
  const providers = { providers: [], presets: [], excluded: [], gateway: { running: true, window: true } };
  const state = { agents: [], profiles: [], settings: { lang: "en", theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"en",theme:"light",web:${web}};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/plugins/market") return json({ listings: [], state: { bun: true, bunVersion: "1.3.0", plugins: [], picker: false } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png", ".json": "application/json" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

async function open(browser, opts, web) {
  const context = await browser.newContext({ reducedMotion: "reduce", ...opts });
  return load(await context.newPage(), web);
}
async function load(page, web) {
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  page.errors = [];
  page.on("pageerror", (e) => page.errors.push(e.message));
  await page.route("**/*", serve(web));
  await page.goto("http://uni-switch.test/?view=providers");
  await page.waitForSelector("#view-providers", { state: "visible" });
  await page.waitForTimeout(300);
  return page;
}

// uni-switch web opened from the home screen: Chromium's app window (--app),
// opened on a data: page (about:blank isn't made an app), whose
// display-mode is standalone as a home-screen app's is (CDP's
// emulated media has no display-mode), with an iPhone's safe area
async function openStandalone(t, opts, insets) {
  const { isMobile, defaultBrowserType, ...rest } = opts;
  opts = rest;
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "uni-switch-webapp-"));
  const context = await chromium.launchPersistentContext(dir, { channel: "chromium", reducedMotion: "reduce", ...opts, args: ["--app=data:text/html,uni-switch"] });
  t.after(async () => { await context.close(); await fs.rm(dir, { recursive: true, force: true }); });
  const page = context.pages()[0] || await context.newPage();
  const cdp = await context.newCDPSession(page);
  await cdp.send("Emulation.setSafeAreaInsetsOverride", { insets });
  // a phone's touch screen (pointer: coarse); isMobile would take the
  // app window's display-mode away
  await cdp.send("Emulation.setTouchEmulationEnabled", { enabled: true, maxTouchPoints: 5 });
  return load(page, true);
}
const box = (page, s) => page.locator(s).first().boundingBox();
const themeColours = (page) => page.evaluate(() => [...document.querySelectorAll('meta[name="theme-color"]')].filter((m) => matchMedia(m.media || "all").matches).map((m) => m.content));

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  const phone = { ...devices["iPhone 13"] };
  if (engine === "chromium") delete phone.defaultBrowserType;
  const launch = () => (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));

  test(`${engine}: the page names what a home screen needs`, async (t) => {
    const browser = await launch();
    t.after(() => browser.close());
    const page = await open(browser, phone, true);
    const attr = (s, a) => page.locator(s).getAttribute(a);
    assert.equal(await attr('meta[name="apple-mobile-web-app-capable"]', "content"), "yes");
    assert.equal(await attr('meta[name="mobile-web-app-capable"]', "content"), "yes");
    assert.equal(await attr('meta[name="apple-mobile-web-app-status-bar-style"]', "content"), "default", "the status bar keeps its own ground, the page starting under it");
    const manifest = JSON.parse(await fs.readFile(path.join(assets, await attr('link[rel="manifest"]', "href")), "utf8"));
    assert.equal(manifest.display, "standalone");
    assert.equal(manifest.name, "uni-switch");
    // the files are there (TestWebAppAddsToHomeScreen checks how they are served)
    for (const ic of [...manifest.icons, { src: await attr('link[rel="apple-touch-icon"]', "href") }]) {
      await fs.access(path.join(assets, ic.src));
    }
    // a browser's tab takes no theme colour from uni-switch
    assert.deepEqual(await themeColours(page), []);
    // nor does the app's window
    const win = await open(browser, { viewport: { width: 1200, height: 800 } }, false);
    assert.deepEqual(await themeColours(win), []);
    assert.deepEqual([...page.errors, ...win.errors], []);
  });

  test(`${engine}: a phone's browser tab keeps its header where it was`, async (t) => {
    const browser = await launch();
    t.after(() => browser.close());
    const page = await open(browser, phone, true);
    const brand = await box(page, ".top .brand");
    assert.ok(brand.y < 20, `the uni-switch at the top as before: ${brand.y}`);
    assert.deepEqual(page.errors, []);
  });

  if (engine !== "chromium") continue; // WebKit can't be made a home-screen app

  test(`${engine}: opened from the home screen, the header is inside the safe area`, async (t) => {
    // an iPhone 13 upright: the status bar over the notch, the home indicator
    const page = await openStandalone(t, phone, { top: 47, bottom: 34, left: 0, right: 0 });
    assert.equal(await page.evaluate(() => matchMedia("(display-mode: standalone)").matches), true, "the page is a home-screen app");
    assert.equal(await page.evaluate(() => matchMedia("(pointer: coarse)").matches), true, "on a touch screen");
    assert.deepEqual(await themeColours(page), ["#f4f4f6"], "the light window's colour");
    for (const s of [".top .brand", ".top .actions", ".top #nav"]) {
      const b = await box(page, s);
      assert.ok(b.y >= 47, `${s} under the status bar: ${b.y}`);
    }
    const foot = await box(page, ".foot");
    if (foot && foot.height) assert.ok(foot.y + foot.height <= 844 + 1, "the foot on the screen");
    // turned on its side: the notch at one side, and both sides kept clear
    const side = await openStandalone(t, { ...phone, viewport: { width: 844, height: 390 } }, { top: 0, bottom: 21, left: 47, right: 47 });
    for (const s of [".top .brand", ".top .actions"]) {
      const b = await box(side, s);
      assert.ok(b.x >= 47 && b.x + b.width <= 844 - 47, `${s} clear of the notch: ${b.x}..${b.x + b.width}`);
    }
    assert.deepEqual([...page.errors, ...side.errors], []);
  });

  test(`${engine}: opened from the home screen in the dark`, async (t) => {
    const page = await openStandalone(t, { ...phone, colorScheme: "dark" }, { top: 47, bottom: 34, left: 0, right: 0 });
    assert.deepEqual(await themeColours(page), ["#1a1a1e"], "the dark window's colour");
  });
}

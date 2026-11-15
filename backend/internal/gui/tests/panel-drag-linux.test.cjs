// Run with Node's test runner and Playwright on the module path; see README.md.
// The quick panel is dragged by its header on Linux, where KWin places it on
// Wayland and it has no title bar of KWin's (#1430); on the Mac and Windows it
// stays where the tray icon put it. The tabs and buttons stay plain clicks.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function serve(lang) {
  const settings = { theme: "light", lang, tray: "panel", currency: "usd" };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "window.__runtimeReady = true; export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/usage") return json({ days: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const drag = (page, sel) => page.locator(sel).first().evaluate((e) => getComputedStyle(e).getPropertyValue("--wails-draggable").trim());

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: the panel's header drags it on Linux only`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(async () => { await browser.close(); });
      const open = async (platform, mode) => {
        const context = await browser.newContext({ viewport: { width: 440, height: 640 } });
        await context.addInitScript((p) => Object.defineProperty(Navigator.prototype, "platform", { get: () => p }), platform);
        const page = await context.newPage();
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        await page.goto(`http://uni-switch.test/?mode=${mode}`);
        await page.waitForFunction(() => window.__runtimeReady === true);
        await page.waitForFunction(() => document.querySelector("header.top"));
        assert.deepEqual(errors, []);
        return page;
      };
      const linux = await open("Linux x86_64", "panel");
      assert.equal(await drag(linux, "header.top"), "drag", "Linux: the panel's header is its handle");
      // every control the header shows stays a click, not a drag
      const controls = await linux.locator("header.top").evaluate((h) => [...h.querySelectorAll("button, a, input, [role=tab]")]
        .filter((e) => e.getClientRects().length)
        .map((e) => [e.id || e.className || e.tagName, getComputedStyle(e).getPropertyValue("--wails-draggable").trim()]));
      assert(controls.length > 0, "the header has controls");
      assert.deepEqual(controls.filter(([, d]) => d !== "no-drag"), [], "Linux: the header's controls stay clicks");
      for (const platform of ["MacIntel", "Win32"]) {
        const page = await open(platform, "panel");
        assert.equal(await drag(page, "header.top"), "no-drag", `${platform}: the panel stays by the icon`);
      }
      const win = await open("Linux x86_64", "window");
      assert.equal(await drag(win, "header.top"), "drag", "Linux: the window is still dragged by its header");
    });
  }
}

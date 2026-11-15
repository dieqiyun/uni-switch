// Run with Node's test runner and Playwright on the module path; see README.md.
// #1803: on Linux the quick panel left a fainter copy of the old scroll
// position behind as it scrolled. Its page painted 93% of its colour over a
// see-through window, so whatever WebKitGTK didn't clear showed through the
// new paint. On Linux the panel is a solid window (panelOptions) and its page
// paints its colour whole, as the main window does; the Mac's and Windows'
// translucent panels are as they were.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const LANGS = ["en", "zh", "zh-TW", "ja", "de"];

function serve(lang, theme) {
  const settings = { theme, lang, tray: "panel", currency: "usd" };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"${theme}",web:false};` });
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

// the body's paint as [r, g, b, alpha], from either way an engine writes a
// computed colour (rgba(…) or color(srgb … / a))
const paint = (page) => page.evaluate(() => {
  const c = getComputedStyle(document.body).backgroundColor;
  let m = c.match(/^rgba?\(([^)]+)\)$/);
  if (m) {
    const v = m[1].split(/[\s,/]+/).filter(Boolean).map(Number);
    return [v[0], v[1], v[2], v[3] ?? 1, c];
  }
  m = c.match(/^color\(srgb ([^)]+)\)$/);
  if (m) {
    const [rgb, a] = m[1].split("/");
    const v = rgb.trim().split(/\s+/).map((x) => Math.round(Number(x) * 255));
    return [v[0], v[1], v[2], a == null ? 1 : Number(a), c];
  }
  return [NaN, NaN, NaN, NaN, c];
});

// the page's own colour, --bg, as the body resolves it
const bg = (page) => page.evaluate(() => {
  const p = document.createElement("i");
  p.style.color = "var(--bg)";
  document.body.append(p);
  const c = getComputedStyle(p).color;
  p.remove();
  return c.match(/[\d.]+/g).slice(0, 3).map(Number);
});

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of LANGS) {
    test(`${engine} ${lang}: the panel paints opaque on Linux only`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(async () => { await browser.close(); });
      const open = async (platform, theme) => {
        const context = await browser.newContext({ viewport: { width: 440, height: 560 } });
        await context.addInitScript((p) => Object.defineProperty(Navigator.prototype, "platform", { get: () => p }), platform);
        const page = await context.newPage();
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, theme));
        await page.goto("http://uni-switch.test/?mode=panel");
        await page.waitForFunction(() => window.__runtimeReady === true);
        await page.waitForFunction(() => document.querySelector("header.top"));
        assert.deepEqual(errors, []);
        return page;
      };
      for (const theme of ["light", "dark"]) {
        const linux = await open("Linux x86_64", theme);
        const [r, g, b, a, said] = await paint(linux);
        assert.equal(a, 1, `Linux ${theme}: the panel's page is opaque (${said})`);
        assert.deepEqual([r, g, b], await bg(linux), `Linux ${theme}: in the page's own colour (${said})`);
        for (const platform of ["MacIntel", "Win32"]) {
          const page = await open(platform, theme);
          const [, , , pa, psaid] = await paint(page);
          assert(pa > 0.9 && pa < 1, `${platform} ${theme}: the panel stays translucent (${psaid})`);
        }
      }
    });
  }
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// A folder plugin whose spec starts with \ (a Windows UNC share, or a
// leading-backslash path a backup wrote) is a folder, as plugin.IsPath
// says (#2229). The Plugins page used to read only file://, a leading .,
// a leading / and a drive letter, so it asked npm about the path, named
// the row by the whole path, and opened an npm page for it. A folder with
// an @ in its path (C:\Users\me@corp\…) is named by its folder too, not
// cut at the @ as a package's version is, as plugin.Name. Chromium and
// WebKit; the classifier does not depend on language or width.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const UNC = "\\\\server\\share\\plugin";
const LEADING = "\\Users\\me\\my-plugin";
const AT = "C:\\Users\\me@corp\\at-plugin";

function server(asked) {
  const installed = [
    { spec: UNC, version: "1.0.0", providers: [] },
    { spec: LEADING, version: "1.0.0", providers: [] },
    { spec: AT, version: "1.0.0", providers: [] },
    { spec: "opencode-copilot-auth", version: "0.0.7", providers: [] },
  ];
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"en",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang: "en", theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/plugins/listings") return json({ listings: [] });
    if (url.pathname === "/api/plugins") return json({ bun: true, bunVersion: "1.3.0", plugins: installed });
    if (url.pathname === "/api/plugins/npm") {
      const names = decodeURIComponent(url.searchParams.get("names") || "").split(",").filter(Boolean);
      asked.push(...names);
      const npm = {};
      for (const n of names) npm[n] = { version: n === "opencode-copilot-auth" ? "0.0.9" : "9.9.9" };
      return json({ npm });
    }
    if (url.pathname === "/api/plugins/page") return json({});
    if (url.pathname.startsWith("/api/")) return json({});
    if (!/^\/[\w./-]*$/.test(url.pathname) || url.host !== "uni-switch.test") return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a folder plugin on a Windows path is not an npm package", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const asked = [];
    const page = await (await browser.newContext({ viewport: { width: 900, height: 800 }, reducedMotion: "reduce" })).newPage();
    page.setDefaultTimeout(8000);
    page.setDefaultNavigationTimeout(60000);
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.route("**/*", server(asked));
    const npmReq = page.waitForRequest((r) => r.url().includes("/api/plugins/npm"));
    await page.goto("http://uni-switch.test/?view=plugins");
    await npmReq;
    const view = page.locator("#view-plugins");
    await view.locator(".lib-tabs .opt", { hasText: "Installed" }).click();
    await view.locator(".pm-row").nth(3).waitFor();
    const names = await view.locator(".pm-row .name > span:first-child").allTextContents();
    assert.deepEqual(names.slice().sort(), ["at-plugin", "my-plugin", "opencode-copilot-auth", "plugin"]);
    assert.deepEqual(asked.slice().sort(), ["opencode-copilot-auth"]);
    const unc = view.locator(".pm-row").filter({ has: page.locator(".name > span:first-child", { hasText: /^plugin$/ }) });
    assert.equal(await unc.locator(".pm-chip.up").count(), 0, "a folder is not offered an npm update");
    await unc.locator(".name").click();
    await page.locator(".pm-detail").waitFor();
    const links = await page.locator(".pm-detail a.pm-link").allTextContents();
    assert.deepEqual(links.filter((s) => /npm/i.test(s)), [], "a folder has no npm page");
    assert.deepEqual(errors, []);
  });
}

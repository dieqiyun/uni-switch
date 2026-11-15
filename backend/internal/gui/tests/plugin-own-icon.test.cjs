// Run with Node's test runner and Playwright on the module path; see README.md.
// #1775 (peterroe): a plugin that signs in to several providers gives each
// its own picture in the auth hook, and the plugin one of its own in
// package.json's uni-switch.icon. Its row on Installed, and its page, wear the
// plugin's picture, not its first provider's; the providers keep theirs in
// the Add sheet. One whose package gives none still wears its first
// provider's, as before. Every language, both engines, wide and narrow;
// the API is faked here. A row's picture loads lazily, the page's at once.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const SUITE = "@peterroe/example-suite", PLAIN = "@peterroe/plain";
const state = { bun: true, bunVersion: "1.3.0", picker: false, movable: [], plugins: [
  { spec: SUITE, version: "1.0.0", icon: "file:suite.png", providers: ["Provider A", "Provider B"], moved: [] },
  { spec: PLAIN, version: "1.0.0", providers: ["Provider C"], moved: [] },
] };
const subs = [
  { id: "pa-plugin", pid: "pa", name: "Provider A", icon: "file:pa.png", spec: SUITE, methods: [], signedIn: false, models: 0 },
  { id: "pb-plugin", pid: "pb", name: "Provider B", icon: "file:pb.png", spec: SUITE, methods: [], signedIn: false, models: 0 },
  { id: "pc-plugin", pid: "pc", name: "Provider C", icon: "file:pc.png", spec: PLAIN, methods: [], signedIn: false, models: 0 },
];
// a 1x1 picture
const PNG = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==", "base64");

function server(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: subs });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname === "/api/plugins") return json(state);
    if (url.pathname === "/api/plugins/listings") return json({ listings: [] });
    if (url.pathname === "/api/plugins/market") return json({ listings: [], state });
    if (/^\/api\/icons\/\w+\.png$/.test(url.pathname)) return route.fulfill({ contentType: "image/png", body: PNG });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a plugin's row and page wear its own picture", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) for (const width of [980, 420]) {
      await t.test(lang + " " + width, async () => {
        const page = await (await browser.newContext({ viewport: { width, height: 820 } })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang));
        await page.goto("http://uni-switch.test/?view=plugins");
        const view = page.locator("#view-plugins");
        const installed = await page.evaluate((l) => l === "en" ? "Installed" : I18N[l]["Installed"], lang);
        await view.page().locator(":is(.lib-tabs .opt, .nav-s):visible", { hasText: installed }).click();
        const row = (name) => view.locator(".pm-row").filter({ has: page.locator(".name", { hasText: name }) });
        const suite = row("example-suite");
        await suite.waitFor();
        assert.equal(await suite.locator(".pm-logo img").getAttribute("src"), "/api/icons/suite.png", "the plugin's own, not its first provider's");
        assert.equal(await row("plain").locator(".pm-logo img").getAttribute("src"), "/api/icons/pc.png", "none of its own: its first provider's, as before");
        // a row's picture is asked for as it nears the screen: each SVG an
        // <img> draws is a document of its own in the webview (#909); one
        // on screen still loads
        assert.equal(await suite.locator(".pm-logo img").getAttribute("loading"), "lazy");
        await page.waitForFunction(() => document.querySelector("#view-plugins .pm-row .pm-logo img")?.naturalWidth > 0);
        const box = await suite.boundingBox();
        assert.ok(box.x >= 0 && box.x + box.width <= width, `the row fits: ${JSON.stringify(box)}`);

        // its page too
        await suite.locator(".name").click();
        const dlg = page.locator("#modal .pm-detail");
        await dlg.waitFor();
        assert.equal(await dlg.locator(".pm-dhead .pm-logo img").getAttribute("src"), "/api/icons/suite.png");
        assert.equal(await dlg.locator(".pm-dhead .pm-logo img").getAttribute("loading"), null, "the page's own picture loads at once");
        assert.deepEqual(errors, []);
        await page.context().close();
      });
    }
  });
}

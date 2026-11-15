// Run with Node's test runner and Playwright on the module path; see README.md.
// A Discover card with an update out has Update as its button (Discord:
// 显示更新但是好像没有更新按钮; #1587): the card names the version it goes
// to, the button updates without opening the plugin's page, and goes once
// it's done.
// English and Chinese; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const upgrades = [];
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function server(lang) {
  const installed = [
    { spec: "opencode-copilot-auth", providers: ["GitHub Copilot"], version: "0.0.7", latest: "0.0.9" },
    { spec: "the retired opencode-zed-auth", providers: ["Zed"], version: "0.1.4", latest: "0.1.4",
      autoUpdated: { package: "the retired opencode-zed-auth", from: "0.1.3", to: "0.1.4", at: "2026-09-30T08:00:00Z" } },
  ];
  const waiting = () => installed.filter((e) => e.version !== e.latest).map((e) => ({ spec: e.spec, package: e.spec, version: e.version, latest: e.latest }));
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname === "/api/plugins/updates") return json({ checked: "2026-10-01T00:00:00Z", waiting: waiting(), updated: [] });
    // the page asks for the market in parts (#488)
    if (url.pathname === "/api/plugins/market" || url.pathname === "/api/plugins" || url.pathname === "/api/plugins/listings") { const m = { listings: [{ package: "opencode-copilot-auth", name: "Copilot", icon: "copilot", providers: ["github-copilot"], community: true, summary: { en: "Copilot", zh: "Copilot" }, npm: { version: "0.0.9" } }], state: { bun: true, bunVersion: "1.3.0", plugins: installed } }; return json(url.pathname === "/api/plugins" ? m.state : url.pathname === "/api/plugins/listings" ? { listings: m.listings } : m); }
    if (url.pathname === "/api/plugins/upgrade") { upgrades.push(JSON.parse(route.request().postData() || "{}")); installed[0].version = "0.0.9"; return json({}); }
    if (url.pathname.startsWith("/api/")) return json({});
    if (!/^\/[\w./-]*$/.test(url.pathname) || url.host !== "uni-switch.test") return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const L = {
  en: { button: "Update", title: "Update to v0.0.9" },
  zh: { button: "更新", title: "更新到 v0.0.9" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a card's Update button updates the plugin", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh"]) {
      await t.test(lang, async () => {
        const w = L[lang];
        upgrades.length = 0;
        const page = await (await browser.newContext({ viewport: { width: 980, height: 820 } })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang));
        await page.goto("http://uni-switch.test/?view=plugins");
        const card = page.locator('#view-plugins .pm-card[data-pkg="opencode-copilot-auth"]').first();
        const up = card.locator("button.pm-act.pm-up");
        await up.waitFor();
        assert.equal((await up.textContent()).trim(), w.button);
        assert.equal(await up.getAttribute("title"), w.title);
        assert.equal((await card.locator(".pm-ver").textContent()).trim(), "v0.0.7 → v0.0.9");
        await up.click();
        await page.waitForFunction(() => !document.querySelector('#view-plugins .pm-card[data-pkg="opencode-copilot-auth"] .pm-act.pm-up'));
        assert.equal(upgrades.length, 1);
        assert.equal(upgrades[0].package || upgrades[0].spec || upgrades[0].pkg, "opencode-copilot-auth");
        // it updated, and didn't open the plugin's page
        assert.equal(await page.locator(".pm-detail").count(), 0);
        assert.deepEqual(errors, []);
      });
    }
  });
}

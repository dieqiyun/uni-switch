// Run with Node's test runner and Playwright on the module path; see README.md.
// #4558 (MYCF711): a plugin installed from git whose package.json gives
// itself a name in uni-switch.name ("华为云 HUAWEI") read as its dependency key
// (uni-switch-officeace) on Installed. Bun takes no non-ASCII key, so the
// package's own name is the only readable one it has. Its row and its page
// say that name, with the package beside it on the page; a git one that
// gives none still says its package; a package the community's list names
// is still named by the list, whatever its package.json says. Every
// language, both engines, wide and narrow; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const OFFICE = "github:example/uni-switch-officeace", LOOMY = "github:example/uni-switch-loomy-auth", LISTED = "the retired opencode-qoder-auth";
const state = { bun: true, bunVersion: "1.3.0", picker: false, movable: [], plugins: [
  { spec: OFFICE, package: "uni-switch-officeace", ownName: "华为云 HUAWEI", version: "0.2.4", commit: "c594984", head: "c594984", providers: [], moved: [] },
  { spec: LOOMY, package: "uni-switch-loomy-auth", version: "0.1.0", commit: "8fd2d9a", head: "8fd2d9a", providers: [], moved: [] },
  { spec: LISTED, ownName: "Not the list's", version: "0.3.0", providers: [], moved: [] },
] };
const listings = [{ package: LISTED, name: "Qoder", summary: "Qoder's plan", providers: ["qoder"], community: true, github: { owner: "uni-switch-community", repo: "plugins" } }];

function server(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname === "/api/plugins") return json(state);
    if (url.pathname === "/api/plugins/listings") return json({ listings });
    if (url.pathname === "/api/plugins/market") return json({ listings, state });
    if (url.pathname === "/api/plugins/search") return json({ hits: [] });
    if (url.pathname === "/api/plugins/updates") return json({ waiting: [], updated: [] });
    if (url.pathname === "/api/plugins/git") return json({ heads: { [OFFICE]: "c594984", [LOOMY]: "8fd2d9a" } });
    if (url.pathname === "/api/plugins/page") return json({ readme: "# uni-switch-officeace", updated: "2026-10-01T10:00:00Z" });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a git plugin's row says the name its package.json gives", async (t) => {
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
        await view.locator(".pm-row").nth(2).waitFor();
        const names = await view.locator(".pm-row .name > span:first-child").allTextContents();
        assert.deepEqual([...names].sort(), ["Qoder", "uni-switch-loomy-auth", "华为云 HUAWEI"].sort(), "the package's own name, the package where it gives none, the list's where it lists one");
        const row = view.locator(".pm-row").filter({ has: page.locator(".name", { hasText: "华为云 HUAWEI" }) });
        const box = await row.boundingBox();
        assert.ok(box.x >= 0 && box.x + box.width <= width, `the row fits: ${JSON.stringify(box)}`);

        // its page says it too, with the repository it came from
        await row.locator(".name").click();
        const dlg = page.locator("#modal .pm-detail");
        await dlg.waitFor();
        assert.equal(await dlg.locator(".pm-dhead .pm-name b").textContent(), "华为云 HUAWEI");
        assert.ok((await dlg.locator(".pm-dhead .pm-by").textContent()).includes(OFFICE), "the page names where it came from");
        assert.deepEqual(errors, []);
        await page.context().close();
      });
    }
  });
}

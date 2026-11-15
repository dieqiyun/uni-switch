// Run with Node's test runner and Playwright on the module path; see README.md.
// Plugin versions with a pre-release: the Installed rows say an update is
// out for exactly the plugins update.Newer puts the dot on Plugins for.
// "2.0.0" comes after "2.0.0-beta.1", "1.0.0-beta.10" after "1.0.0-beta.9",
// and build metadata is no update. Before, the rows read only x.y.z, so a
// beta with its release out had the dot and no row. No strings change, so
// English only; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

// the installed plugins, with whether Go's update.Newer(latest, version)
// is true for each (internal/update TestNewer has the same pairs)
const cases = [
  { spec: "opencode-release-out", version: "2.0.0-beta.1", latest: "2.0.0", up: true },
  { spec: "opencode-beta-ten", version: "1.0.0-beta.9", latest: "1.0.0-beta.10", up: true },
  { spec: "opencode-rc-after-beta", version: "1.0.0-beta.11", latest: "1.0.0-rc.1", up: true },
  { spec: "opencode-longer-pre", version: "1.0.0-alpha", latest: "1.0.0-alpha.1", up: true },
  { spec: "opencode-beta-back", version: "1.0.0-beta.10", latest: "1.0.0-beta.9", up: false },
  { spec: "opencode-build-only", version: "1.0.0", latest: "1.0.0+build.2", up: false },
  { spec: "opencode-same", version: "0.3.0", latest: "0.3.0", up: false },
];

function server() {
  const installed = cases.map((c) => ({ spec: c.spec, providers: [], version: c.version, latest: c.latest }));
  const waiting = cases.filter((c) => c.up).map((c) => ({ spec: c.spec, package: c.spec, version: c.version, latest: c.latest }));
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"en",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang: "en", theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname === "/api/plugins/updates") return json({ checked: "2026-10-01T00:00:00Z", waiting, updated: [] });
    if (url.pathname === "/api/plugins/market" || url.pathname === "/api/plugins" || url.pathname === "/api/plugins/listings") { const m = { listings: [], state: { bun: true, bunVersion: "1.3.0", plugins: installed } }; return json(url.pathname === "/api/plugins" ? m.state : url.pathname === "/api/plugins/listings" ? { listings: m.listings } : m); }
    if (url.pathname.startsWith("/api/")) return json({});
    if (!/^\/[\w./-]*$/.test(url.pathname) || url.host !== "uni-switch.test") return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": rows and the Plugins dot agree on pre-releases", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const page = await (await browser.newContext({ viewport: { width: 980, height: 820 } })).newPage();
    page.setDefaultTimeout(5000);
    page.setDefaultNavigationTimeout(60000);
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.route("**/*", server());

    await page.goto("http://uni-switch.test/?view=providers");
    await page.waitForFunction(() => document.querySelector('#nav button[data-view="plugins"]')?.classList.contains("has-dot"));
    await page.locator('#nav button[data-view="plugins"]').click();
    const view = page.locator("#view-plugins");
    await view.page().locator(":is(.lib-tabs .opt, .nav-s):visible", { hasText: "Installed" }).click();
    await view.locator(".pm-row").nth(cases.length - 1).waitFor();

    const rows = await view.locator(".pm-row").evaluateAll((rs) => rs.map((r) => ({
      name: r.querySelector(".name span")?.textContent,
      chip: r.querySelector(".pm-chip.up")?.textContent || null,
      update: [...r.querySelectorAll(".val button")].some((b) => b.textContent === "Update"),
    })));
    for (const c of cases) {
      const r = rows.find((x) => x.name === c.spec);
      assert.ok(r, c.spec + " has a row");
      assert.equal(r.chip, c.up ? `v${c.latest} out` : null, `${c.spec}: ${c.version} → ${c.latest}`);
      assert.equal(r.update, c.up, `${c.spec}: Update button`);
    }
    const n = cases.filter((c) => c.up).length;
    assert.equal(await view.getByRole("button", { name: `Update all (${n})`, exact: true }).count(), 1);
    assert.deepEqual(errors, []);
  });
}

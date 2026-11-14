// Run with Node's test runner and Playwright on the module path; see README.md.
// The window shown again hears visibilitychange and focus together, and
// under focus-follows-mouse focus each time the pointer crosses in. Each
// of them read the whole state (/api/state, the agents' configs, their
// CLIs, the installs) again: two full reads for one return, and one per
// crossing. One read now serves the returns of the same second; a return
// while an older read is still on its way gets one more read after it,
// however many returns came meanwhile. Chromium and WebKit, English and
// Chinese, the window and the narrow panel.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(process.env.UNI_SWITCH_ASSETS || path.join(__dirname, "../assets"));

function serve(lang, seen) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") {
      if (url.searchParams.get("last") === "1") return json({});
      seen.state++;
      if (seen.hold) await seen.hold;
      return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    }
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true } });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    for (const surface of ["window", "panel"]) {
      test(`${engine} ${lang} ${surface}: the window back reads the state once`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const context = await browser.newContext({ viewport: surface === "panel" ? { width: 360, height: 640 } : { width: 1000, height: 760 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const seen = { state: 0, hold: null };
        await page.clock.install();
        await page.route("**/*", serve(lang, seen));
        await page.goto(surface === "panel" ? "http://uni-switch.test/?mode=panel" : "http://uni-switch.test/");
        await page.waitForFunction(() => load.done);
        // reads asked for after one of them must come within a moment of it:
        // what counts is that the second never comes, so the wait for it is
        // a generous one
        const quiet = () => new Promise((r) => setTimeout(r, 1500));
        const reach = async (n) => { for (let i = 0; i < 200 && seen.state < n; i++) await new Promise((r) => setTimeout(r, 25)); };
        const back = () => page.evaluate(() => {
          document.dispatchEvent(new Event("visibilitychange"));
          window.dispatchEvent(new Event("focus"));
        });
        const focus = () => page.evaluate(() => window.dispatchEvent(new Event("focus")));
        await quiet();
        const first = seen.state;

        await back();
        await reach(first + 1);
        await quiet();
        assert.equal(seen.state - first, 1, "shown again: visibilitychange and focus read the state once");
        await page.clock.runFor(1500);
        await focus();
        await reach(first + 2);
        await quiet();
        assert.equal(seen.state - first, 2, "a later return reads it again");

        // a read held on its way: returns a second later each get the one
        // read after it, not one each
        let release;
        seen.hold = new Promise((r) => { release = r; });
        await page.clock.runFor(1500);
        await focus();
        await reach(first + 3);
        assert.equal(seen.state - first, 3);
        await page.clock.runFor(1500);
        await focus();
        await focus();
        await back();
        await quiet();
        assert.equal(seen.state - first, 3, "nothing more is asked while the read is on its way");
        seen.hold = null;
        release();
        await reach(first + 4);
        await quiet();
        assert.equal(seen.state - first, 4, "one read follows the held one, for all the returns while it was out");
        assert.deepEqual(errors, []);
      });
    }
  }
}

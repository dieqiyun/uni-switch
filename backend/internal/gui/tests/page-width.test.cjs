// Run with Node's test runner and Playwright on the module path; see README.md.
// Every page of the window keeps to one width, centred, on a large screen:
// Routing, Usage and Sessions too, not stretched edge to edge while Agents,
// Providers and Settings sit in the middle (面条 on Discord, after #860). A
// narrow window keeps every page's 16px gutter. Masking too, which a
// maximised window stretched edge to edge while the rest kept to the
// middle (refeccd on Discord): the pages are the sidebar's own, so a page
// added later is held to the same width without this list knowing it.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function serve(lang) {
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { url: "http://127.0.0.1:3425", running: true, window: true } });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/usage/quotas")) return json([]);
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file).catch(() => ""), contentType });
  };
}

// where a view's content sits: its box less its own padding, as the page
// lays it out, whatever is in it yet (a page long enough for a scroll bar
// has that bar's width less inside)
const box = (page, v) => page.evaluate((v) => {
  const e = document.querySelector(`#view-${v}`);
  const cs = getComputedStyle(e);
  const r = e.getBoundingClientRect();
  const pl = parseFloat(cs.paddingLeft), pr = parseFloat(cs.paddingRight);
  // pr is the right side's gap with the bar's room, which comes out of the padding
  return { left: Math.round(r.left + pl), width: Math.round(e.clientWidth - pl - pr), pl, pr: pr + e.offsetWidth - e.clientWidth };
}, v);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: every page keeps one width`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const [lang, width] of [["en", 1920], ["zh", 2560], ["zh-TW", 1920], ["ja", 2560], ["de", 1920], ["en", 900], ["en", 420], ["zh", 420], ["zh-TW", 420], ["ja", 420], ["de", 420]]) {
      await t.test(`${lang} at ${width}`, async () => {
        const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        await page.goto("http://uni-switch.test/");
        await page.waitForSelector("#nav button.on");
        // every page the sidebar opens (a community plugin's page draws in a
        // frame of its own, and none is installed here), and Settings
        const views = await page.$$eval("#nav button[data-view]:not([data-plugin-page])", (bs) => bs.map((b) => b.dataset.view));
        views.push("settings");
        assert.ok(views.includes("masking"), `the sidebar's pages: ${views}`);
        const got = {};
        for (const v of views) {
          // Settings is at the sidebar's foot, the others in its list
          const b = page.locator(v === "settings" ? "#prefs" : `#nav button[data-view="${v}"]`);
          if (!(await b.isVisible())) continue;
          await b.click();
          await page.waitForFunction((v) => !document.querySelector(`#view-${v}`).hidden, v);
          got[v] = await box(page, v);
        }
        assert.ok(["sessions", "routing", "usage", "masking", "agents", "settings"].every((v) => got[v]), `the pages reached: ${Object.keys(got)}`);
        const ref = got.agents;
        for (const [v, b] of Object.entries(got)) {
          if (width >= 1600) {
            assert.ok(b.width <= 1200 && b.width >= 1150, `${v}: ${b.width} wide at ${width}`);
            assert.ok(Math.abs(b.pl - b.pr) <= 1, `${v}: centred (${b.pl} / ${b.pr})`);
          } else {
            assert.equal(b.pl, 16, `${v}: the window's gutter`);
          }
          assert.ok(Math.abs(b.left - ref.left) <= 1 && Math.abs(b.pl - ref.pl) <= 1 && Math.abs(b.pr - ref.pr) <= 1, `${v} where Agents is: ${JSON.stringify(b)} vs ${JSON.stringify(ref)}`);
        }
        assert.deepEqual(errors, []);
        await context.close();
      });
    }
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// The window set user-select: none on the whole page, so nothing could be
// selected or copied into a bug report (Outvi V on Discord). What it shows
// can be: here what a vendor said in a request's story on the Routing page,
// dragged across with the mouse. Its controls can't, as in a Mac app:
// a request's row, a button, the navigation, a tab. A field is still a
// field. In both engines, every language, wide and narrow.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const at = (i) => new Date(now.getTime() - (i + 1) * 60e3).toISOString();
const vendor = "Relay: upstream connect error or disconnect/reset before headers";
const relay = { id: "relay", provider: "relay", name: "Relay", kind: "provider", model: "claude-sonnet-4-6" };
const routes = [{
  id: 100, seq: 100, time: at(0), agent: "claude", model: "relay/claude-sonnet-4-6", provider: "relay", order: [relay],
  tries: [{ id: relay.id, model: relay.model, start: at(0), done: true, status: 503, ms: 40, fail: "other", error: vendor, again: 2000, gone: true }],
  done: true, status: 499, error: "the agent canceled the request", ms: 900,
}, {
  id: 99, seq: 99, time: at(1), agent: "claude", model: "relay/claude-sonnet-4-6", provider: "relay", order: [relay],
  tries: [{ id: relay.id, model: relay.model, start: at(1), done: true, status: 200, ms: 500 }], done: true, status: 200, ms: 500,
}];

function serve(lang) {
  const state = { agents: [{ id: "claude", name: "Claude Code", path: "/test/settings.json", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) await new Promise((r) => setTimeout(r, 20e3));
      return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname === "/api/gateway/history") {
      const d = url.searchParams.get("day");
      return json({ cut: false, days: [{ day, requests: routes.length }], routes: d ? routes : [] });
    }
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}


// drag drags the mouse across loc's text, as a reader selecting it does,
// and gives what ended up selected
async function drag(page, loc) {
  await page.evaluate(() => getSelection().removeAllRanges());
  // the story is still opening when it is first drawn: wait till it stays
  let b = await loc.boundingBox();
  for (let i = 0; i < 50; i++) {
    await page.waitForTimeout(60);
    const n = await loc.boundingBox();
    if (n.x === b.x && n.y === b.y && n.width === b.width) break;
    b = n;
  }
  // scrolled to as a reader does, with the wheel, clear of the window's foot
  for (let i = 0; i < 20 && (b.y < 120 || b.y + b.height > 560); i++) {
    await page.mouse.move(10 + b.width / 2, 300);
    await page.mouse.wheel(0, b.y - 300);
    await page.waitForTimeout(120);
    b = await loc.boundingBox();
  }
  const y = b.y + Math.min(b.height / 2, 8);
  await page.mouse.move(b.x + 2, y);
  await page.mouse.down();
  await page.mouse.move(b.x + b.width / 2, y, { steps: 4 });
  await page.mouse.move(b.x + b.width - 2, y, { steps: 4 });
  await page.mouse.up();
  return page.evaluate(() => String(getSelection()));
}
const selectOf = (loc) => loc.evaluate((e) => { const s = getComputedStyle(e); return s.userSelect || s.webkitUserSelect; });

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "ja", "de"]) {
    for (const width of [1100, 420]) {
      test(`${engine} ${lang} ${width}: what a request said can be selected, its controls can't`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const context = await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        t.after(() => browser.close());
        await page.goto("http://uni-switch.test/?view=routing");
        await page.locator(".rt-days .rt-day").nth(1).click();
        await page.locator(".rt-req").nth(routes.length - 1).waitFor();
        const row = page.locator(".rt-req").nth(0);
        await row.click();
        const said = page.locator(".rt-steps li", { hasText: "upstream connect error" });
        await said.waitFor();

        // the vendor's own words, as a bug report quotes them
        assert.equal(await selectOf(said), "text");
        const got = await drag(page, said);
        assert(got.includes("Relay: upstream connect error"), "selected " + JSON.stringify(got));

        // a request's row is a button: a drag across it selects nothing,
        // and it still opens on a click with that text selected elsewhere
        assert.equal(await selectOf(row), "none");
        assert.equal(await drag(page, row.locator(".to")), "");
        await drag(page, said);
        await page.locator(".rt-req").nth(1).evaluate((e) => e.scrollIntoView({ block: "center", behavior: "instant" }));
        await page.locator(".rt-req").nth(1).click();
        await page.waitForFunction(() => document.querySelector(".rt-req[aria-pressed=true]") === document.querySelectorAll(".rt-req")[1]);

        // the page's navigation and every button stay controls; every field
        // can still be typed into and selected
        const st = await page.evaluate(() => {
          const sel = (e) => { const s = getComputedStyle(e); return s.userSelect || s.webkitUserSelect; };
          return {
            controls: [...document.querySelectorAll("nav, nav *, button, button *, [role=tab], .seg, .opt")].map(sel).filter((v) => v !== "none").length,
            fields: [...document.querySelectorAll("input:not([type=checkbox]):not([type=radio]):not([type=range]), textarea")].map(sel).filter((v) => v === "none").length,
          };
        });
        assert.deepEqual(st, { controls: 0, fields: 0 });
        assert.deepEqual(errors, []);
      });
    }
  }
}

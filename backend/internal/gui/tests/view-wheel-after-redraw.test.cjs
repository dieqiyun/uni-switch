// Run with Node's test runner and Playwright on the module path; see README.md.
// issennknight on Discord (macOS 13, v0.1.1621), after view-wheel-after-click:
// the page still stood still and shook back under the wheel right after
// switching the Usage page's tabs (Overview, Requests, Sessions, Context),
// and after an agent's row on the Agents page was opened and then closed.
// macOS 13's WebKit scrolls a view under a wheel it never tells the page of,
// and the view guard gives such scrolls back as the reader's run unless a
// click's hold says the page was redrawn. Both clicks draw what was clicked
// again (the tabs, the agents' list), so the held parts were gone from the
// page, and a part gone read as moved at every scroll: for the hold's 4s
// every scroll was put back. A part that stays in place on the screen as
// the view scrolls (Settings' tabs, sticky at a narrow width) read the same.
// Here the wheel, unheard, goes on right after each such click: the page
// follows it down and back up, in every language, narrow and wide, in
// Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = process.env.UNI_SWITCH_AFTER_REDRAW_ASSETS || path.resolve(__dirname, "../assets");
const LANGS = ["en", "zh", "zh-TW", "ja", "de"];
const model = (id) => ({ key: "model", label: "model", value: id, options: [{ value: id, label: id, ref: "relay/" + id, note: "Relay · via uni-switch" }] });
const AGENTS = Array.from({ length: 14 }, (_, i) => ({
  id: "a" + i, name: "Agent " + i, path: "/t/a" + i + ".json", icon: "generic", wired: true,
  fields: [model("m" + i), { key: "effort", label: "effort", value: "high", options: [{ value: "high", label: "High" }] }],
}));

function server(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: AGENTS, profiles: [], settings: { lang, theme: "light", guideHidden: true } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) return new Promise(() => {});
      return json({ mine: true, now: new Date().toISOString(), seq: 0, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (url.pathname === "/api/window/fit") return route.fulfill({ status: 204 });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file).catch(() => ""), contentType });
  };
}

// the view at rest: its scrollTop the same across a few looks
async function rested(p, view, ms = 3000) {
  let last = NaN, same = 0;
  const until = Date.now() + ms;
  while (Date.now() < until) {
    const t = await view.evaluate((v) => v.scrollTop);
    same = t === last ? same + 1 : 0;
    if (same >= 3) return t;
    last = t;
    await p.waitForTimeout(50);
  }
  return last;
}
const wheels = async (p, n, dy) => { for (let i = 0; i < n; i++) { await p.mouse.wheel(0, dy); await p.waitForTimeout(16); } };
// a page taller than the window whatever a pane draws: the test is about the
// scroll, not the figures. A pseudo-element, which no redraw takes out.
const tall = (view) => view.evaluate((v) => { const s = document.createElement("style"); s.textContent = `#${v.id}::after { content: ""; display: block; flex: none; height: 2400px; min-height: 2400px; }`; document.head.append(s); });
const DEAF = `addEventListener("wheel", (e) => e.stopImmediatePropagation(), { capture: true, passive: true });`;

// after the click, ten wheels down and ten back up go where the wheel takes them
async function follows(p, view, what) {
  const r = await view.boundingBox();
  await p.mouse.move(r.x + r.width / 2, r.y + r.height * 0.6);
  const at = await view.evaluate((v) => v.scrollTop);
  await wheels(p, 10, 30);
  const down = await rested(p, view);
  assert(down >= at + 200, `${what}: ten wheels of 30px right after the click scroll the page: from ${at} to ${down}`);
  await wheels(p, 10, -30);
  const up = await rested(p, view);
  assert(up <= down - 200, `${what}: and back up: from ${down} to ${up}`);
}

const CASES = {
  // each tab in turn, clicked and wheeled at once
  async usage(p) {
    const view = p.locator("#view-usage");
    await p.goto("http://uni-switch.test/?view=usage");
    await p.locator("#usageTab .opt").nth(3).waitFor();
    await tall(view);
    await rested(p, view);
    for (const i of [1, 2, 3, 0]) {
      await view.evaluate((v) => { v.scrollTop = 0; });
      await rested(p, view);
      await p.waitForTimeout(1100); // the last run's second is over
      await p.locator("#usageTab .opt").nth(i).click();
      await p.locator("#usageTab .opt.on").nth(0).waitFor();
      assert.equal(await p.locator("#usageTab .opt").nth(i).evaluate((b) => b.classList.contains("on")), true, "the tab clicked is on");
      await follows(p, view, "Usage tab " + i);
    }
  },
  // an agent's row opened, then closed, wheeled at once after each
  async agents(p) {
    const view = p.locator("#view-agents");
    await p.goto("http://uni-switch.test/?view=agents");
    await p.locator('#agents .row.agent[data-id="a1"] .ag-link').waitFor();
    await tall(view);
    await rested(p, view);
    await p.locator('#agents .row.agent[data-id="a1"] .ag-link').click();
    await p.locator('#agents .row.agent[data-id="a1"] > .ag-exp').waitFor();
    await rested(p, view);
    await follows(p, view, "an agent opened");
    await view.evaluate((v) => { v.scrollTop = 0; });
    await rested(p, view);
    await p.waitForTimeout(1100);
    await p.locator('#agents .row.agent[data-id="a1"] .ag-link').click();
    await p.locator('#agents .row.agent[data-id="a1"] > .ag-exp').waitFor({ state: "detached" });
    await follows(p, view, "an agent closed");
  },
  // Settings' tabs stay at the top as the page scrolls under them (narrow)
  async settings(p) {
    const view = p.locator("#view-settings");
    await p.goto("http://uni-switch.test/?view=settings");
    await p.locator("#setTab-network").waitFor();
    await tall(view);
    await rested(p, view);
    await view.evaluate((v) => { v.scrollTop = 300; });
    await rested(p, view);
    await p.waitForTimeout(1100);
    await p.locator("#setTab-general").click();
    await follows(p, view, "a Settings tab");
  },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a wheel the page never hears scrolls right after a click that redraws", { timeout: 900000 }, async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit : chromium).launch();
    t.after(() => browser.close());
    for (const lang of LANGS) for (const width of [420, 1000]) for (const name of Object.keys(CASES)) {
      if (name === "settings" && width !== 420) continue;
      await t.test(`${name}, ${lang}, ${width}px`, async () => {
        const ctx = await browser.newContext({ viewport: { width, height: 600 }, reducedMotion: "reduce" });
        const p = await ctx.newPage();
        p.setDefaultTimeout(8000);
        p.setDefaultNavigationTimeout(60000);
        const errors = [];
        p.on("pageerror", (e) => { if (!/^ResizeObserver loop/.test(e.message)) errors.push(e.message); });
        await p.addInitScript(DEAF);
        await p.route("**/*", server(lang));
        await CASES[name](p);
        const others = await p.evaluate(() => [document.scrollingElement, ...document.querySelectorAll(".view:not([hidden]) *")].filter((n) => n.scrollTop > 0 && !n.matches(".sess-menu *, .heat-grid, .led-hscroll")).map((n) => n.id || n.className));
        assert.deepEqual(others, [], "nothing but the view scrolls");
        assert.deepEqual(errors, []);
        await ctx.close();
      });
    }
  });
}

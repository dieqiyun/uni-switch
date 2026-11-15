// Run with Node's test runner and Playwright on the module path; see README.md.
// A subscription's windows side by side have their bars at one height when
// one's name wraps and the other's doesn't. The windows were a subgrid of
// three rows of their grid once, the layout that crashed Safari 16's WebKit
// sizing it (#3731, #3955); now each window is a grid of its own and its
// names are as tall as the tallest beside them. No rule of the windows says
// subgrid, and at each width, in both engines and every language, the bars
// of each row of windows start at one y, the names of a window too
// short to wrap still sit above its bar, and a reset line is as far under
// its bar as the subgrid put it. UNI_SWITCH_QUOTA_ASSETS points the
// page at an older asset tree, to watch this go red there.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, process.env.UNI_SWITCH_QUOTA_ASSETS || "../assets");
const soon = new Date(Date.now() + 3 * 3600e3).toISOString();
const later = new Date(Date.now() + 4 * 86400e3).toISOString();
const quotas = [{
  provider: "zcode", name: "ZCode", icon: "zcode", plan: "Pro", user: "a",
  windows: [
    { name: "ZCode 1024 Dev Day · GLM-5.3 and every model it counts", used: 42, resetsAt: soon },
    { name: "5h", used: 12, resetsAt: soon },
    { name: "Week", used: 60, resetsAt: later },
    { name: "Month", used: 7 },
  ],
}];

function serve(lang, quotas) {
  const settings = { lang, theme: "light", quotaLeft: true, currency: "usd" };
  const state = { agents: [], profiles: [], settings };
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/gateway/trace") return url.searchParams.get("wait") ? new Promise(() => {}) : json({ mine: true, now: new Date().toISOString(), seq: 0, totals: {}, routes: [] });
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ models: [], groups: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname === "/api/usage/quotas") return json(quotas);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await r.fulfill({ body: await fs.readFile(file), contentType }); } catch { await r.fulfill({ status: 404, body: "" }); }
  };
}

test("no rule of the windows says subgrid", async () => {
  const css = await fs.readFile(path.join(assets, "app.css"), "utf8");
  const bad = [];
  for (const m of css.replace(/\/\*[\s\S]*?\*\//g, "").matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (/\.quota(-windows)?\b/.test(m[1]) && /subgrid/.test(m[2])) bad.push(m[1].trim());
  }
  assert.deepEqual(bad, []);
});

// each window: its row's y, its name's height, its bar's top, its names' bottom
const windowsOf = (page) => page.evaluate(() => [...document.querySelectorAll(".subscription-card .quota-windows > .quota")].map((q) => ({
  row: Math.round(q.getBoundingClientRect().top),
  name: q.querySelector(".quota-name").getBoundingClientRect().height,
  bar: q.querySelector(".quota-track")?.getBoundingClientRect().top,
  under: q.querySelector(".quota-labels").getBoundingClientRect().bottom,
  reset: q.querySelector(".quota-track + .quota-reset")?.getBoundingClientRect().top,
})));

// the frames a resize takes: the windows' observer, then the frame it
// fits them in (quotaFit)
const frames = (page) => page.evaluate(() => new Promise((r) => { let n = 4; const f = () => (--n ? requestAnimationFrame(f) : r()); requestAnimationFrame(f); }));

// the bars of each row of windows start at one y, and a reset line is as
// far under its bar as it was; and whether some row's names differ in
// height, as the case is about
async function lined(page) {
  await frames(page);
  const ws = await windowsOf(page);
  for (const y of new Set(ws.map((w) => w.row))) {
    const bars = ws.filter((w) => w.row === y && w.bar != null).map((w) => Math.round(w.bar));
    assert.equal(new Set(bars).size, 1, `row at ${y}: bars at ${bars} ${JSON.stringify(ws)}`);
  }
  for (const w of ws) if (w.bar != null) assert.ok(w.under <= w.bar + 0.5, `names past the bar ${JSON.stringify(w)}`);
  for (const w of ws) if (w.reset != null) assert.ok(Math.abs(w.reset - w.bar - 13) < 0.6, `reset ${w.reset - w.bar}px under its bar's top`);
  return [...new Set(ws.map((w) => w.row))].some((y) => new Set(ws.filter((w) => w.row === y).map((w) => Math.round(w.name))).size > 1);
}

for (const [name, engine] of [["chromium", chromium], ["webkit", webkit]]) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${name} ${lang}: windows side by side have their bars at one height`, async () => {
      const browser = await engine.launch();
      try {
        const page = await (await browser.newContext({ viewport: { width: 1100, height: 900 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, quotas));
        await page.goto("http://uni-switch.test/?view=usage");
        await page.locator(".subscription-card .quota-track").nth(2).waitFor();
        let wrapped = false;
        for (const width of [1100, 700, 390, 1100]) {
          await page.setViewportSize({ width, height: 900 });
          wrapped = (await lined(page)) || wrapped;
        }
        assert.ok(wrapped, "no width had one window's name taller than the one beside it");
        assert.deepEqual(errors, []);
      } finally { await browser.close(); }
    });
  }
}

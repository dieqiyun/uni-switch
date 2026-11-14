// Run with Node's test runner and Playwright on the module path; see README.md.
// rtbeilin on X (10-24): ZCode's two windows of one plan, side by side, are
// named "ZCode 1024 Dev Day · GLM-5.3…", wider than their column, and the
// first name ran on under the second window's. A name too wide for its
// column goes on to a second line inside it, never cut (two windows of one
// plan would read alike) and never past it. Chromium and WebKit, every
// language, the panel's width and wider.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const resetsAt = new Date(Date.now() + 44 * 3600e3).toISOString();
// the reporter's card: two buckets of the 1024 Dev Day plan, the plugin's
// byPlan names
const zcode = {
  provider: "zcode", name: "ZCode", icon: "generic", plan: "ZCode 1024 Dev Day", user: "a",
  windows: [
    { name: "ZCode 1024 Dev Day · GLM-5.3", used: 0, display: "0 / 300,000,000", amount: 0, limit: 300000000, unit: "tokens", resetsAt },
    { name: "ZCode 1024 Dev Day · GLM-5.3-Flash", used: 4.8, display: "9,670,287 / 200,000,000", amount: 9670287, limit: 200000000, unit: "tokens", resetsAt },
  ],
};
function serve(lang) {
  const settings = { lang, theme: "dark", currency: "usd" };
  const state = { agents: [], profiles: [], settings };
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"dark",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/gateway/trace") return url.searchParams.get("wait") ? new Promise(() => {}) : json({ mine: true, now: new Date().toISOString(), seq: 0, totals: {}, routes: [] });
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ models: [], groups: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname === "/api/usage/quotas") return json([zcode]);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await r.fulfill({ body: await fs.readFile(file), contentType }); } catch { await r.fulfill({ status: 404, body: "" }); }
  };
}

for (const [name, engine] of [["chromium", chromium], ["webkit", webkit]]) {
  if (process.env.BROWSER && process.env.BROWSER !== name) continue;
  test(`${name}: a window's long name stays in its own column, whole`, async () => {
    const browser = await engine.launch();
    try {
      for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) for (const width of [380, 600, 1100]) {
        const page = await (await browser.newContext({ viewport: { width, height: 820 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        await page.route("**/*", serve(lang));
        await page.goto("http://uni-switch.test/?view=usage");
        await page.locator(".subscription-card .quota-name").nth(1).waitFor();
        await page.waitForTimeout(300);
        const ws = await page.evaluate(() => [...document.querySelectorAll(".quota-windows .quota")].map((q) => {
          const nm = q.querySelector(".quota-name"), b = nm.getBoundingClientRect(), col = q.getBoundingClientRect();
          return { text: nm.textContent, left: b.left - col.left, right: col.right - b.right, cut: nm.scrollWidth > nm.clientWidth + 1 };
        }));
        const where = `${lang} ${width}px`;
        assert.equal(ws.length, 2, where);
        for (const w of ws) {
          assert.ok(w.left >= -1 && w.right >= -1, `${where}: "${w.text}" runs past its column (${w.left}, ${w.right})`);
          assert.equal(w.cut, false, `${where}: "${w.text}" is cut`);
        }
        await page.close();
      }
    } finally { await browser.close(); }
  });
}

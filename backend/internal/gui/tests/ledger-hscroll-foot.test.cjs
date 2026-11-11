// Run with Node's test runner and Playwright on the module path; see README.md.
// The Requests table's stand-in scrollbar (#799) is held at the window's
// foot while the table runs past it. It stood the view's bottom padding
// above the foot, and the table's card showed through a strip under it
// (碳碳双键 on Discord, 10-23): its bottom is the view's visible bottom,
// with nothing of the page under it, at a narrow window too, and at the
// page's end it still follows the table. Chinese, as reported, and English;
// Chromium with classic and with overlay scrollbars, and WebKit. No backend,
// the API is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = Date.now();

const ROWS = Array.from({ length: 40 }, (_, i) => ({
  route_id: 500 + i, t: new Date(now - (i + 1) * 60e3).toISOString(), agent: "trae", agentName: "Trae CN", icon: "generic",
  provider: "deepseek", providerName: "DeepSeek Harness", req: "group/auto-deepseek-v4-1-flash", model: "deepseek-v4-1-flash", served: "deepseek-v4-1-flash",
  in: 1200 + i, out: 80, ms: 2400, ttft_ms: 600, status: 200, rid: "req_" + i, ep: "/v1/chat/completions", cost: 0.001, priced: true,
}));

function server(lang, asked) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data, status) => route.fulfill({ json: data, status });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" }, fx: { rate: 7.2, at: new Date().toISOString() } });
    if (url.pathname === "/api/usage/requests") return json({ period: url.searchParams.get("period"), rows: ROWS, offset: 0, total: ROWS.length, calls: ROWS.length, errors: 0, input: 1, output: 1, cost: 0.04, unpriced: 0, agents: [{ id: "trae", name: "Trae CN", icon: "generic" }] });
    if (url.pathname === "/api/gateway/route") { asked.push(url.searchParams.get("id")); return json({ error: "gone" }, 404); }
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/usage") return json({ calls: 40, errors: 0, input: 1, output: 1, reasoning: 0, unpriced: 0, cost: 0.04, bucket: "day", series: [], agents: [], models: [], path: "" });
    if (url.pathname === "/api/sessions") return json({ sessions: [], dirs: [] });
    if (url.pathname === "/api/sessions/stats") return json({ from: "", to: "", days: [], agents: {} });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const runs = [
  { engine: "chromium", name: "chromium (classic scrollbars)", launch: () => chromium.launch({ channel: "chromium", ignoreDefaultArgs: ["--hide-scrollbars"] }) },
  { engine: "chromium", name: "chromium (overlay scrollbars)", launch: () => chromium.launch({ channel: "chromium" }) },
  { engine: "webkit", name: "webkit", launch: () => webkit.launch() },
];

for (const run of runs.filter((r) => !process.env.BROWSER || r.engine === process.env.BROWSER)) {
  test(run.name + ": nothing under the Requests table's scrollbar", async (t) => {
    const browser = await run.launch();
    t.after(() => browser.close());
    for (const lang of ["zh", "en"]) {
      // 600 as #799's test has it, and a narrow window
      for (const width of [600, 420]) {
        await t.test(lang + " " + width, async () => {
          const errors = [];
          const ctx = await browser.newContext({ viewport: { width, height: 640 }, reducedMotion: "reduce" });
          const p = await ctx.newPage();
          p.setDefaultTimeout(5000);
          p.setDefaultNavigationTimeout(60000);
          p.on("pageerror", (e) => errors.push(e.message));
          await p.route("**/*", server(lang, []));
          await p.goto("http://uni-switch.test/");
          await p.locator('[data-view="usage"]').first().click();
          await p.locator(":is(#usageTab .opt, .nav-s):visible").nth(1).click();
          await p.locator("#ledWrap .led tbody tr").first().waitFor();
          await p.waitForTimeout(200);
          const bar = p.locator("#ledHScroll");
          assert(await bar.isVisible(), "the table is wider than the window and has its bar");

          // the view's visible bottom, the bar's, and what is drawn just
          // above the view's bottom
          const at = () => p.evaluate(() => {
            const v = document.querySelector("#view-usage"), b = document.querySelector("#ledHScroll"), w = document.querySelector("#ledWrap");
            const vr = v.getBoundingClientRect(), br = b.getBoundingClientRect(), wr = w.getBoundingClientRect();
            const foot = vr.top + v.clientTop + v.clientHeight;
            const hit = document.elementFromPoint(br.left + br.width / 2, foot - 1);
            return { foot, bar: br.bottom, barTop: br.top, wrap: wr.bottom, under: hit ? (hit.id || hit.className || hit.tagName) : "", onBar: !!hit && b.contains(hit) };
          });
          // mid-table: the table runs on past the window
          await p.mouse.move(width / 2, 300);
          for (let i = 0; i < 8; i++) await p.mouse.wheel(0, 200);
          await p.waitForTimeout(300);
          let m = await at();
          assert(m.wrap > m.foot + 50, "the table runs past the window: " + JSON.stringify(m));
          assert(Math.abs(m.bar - m.foot) <= 0.5, "the bar's bottom is the view's bottom, no gap under it: " + JSON.stringify(m));
          assert(m.onBar, "the view's last pixel row is the bar, not the page under it: " + JSON.stringify(m));

          // the page's end: the bar follows the table, two pixels under it
          for (let i = 0; i < 30; i++) await p.mouse.wheel(0, 300);
          await p.waitForTimeout(300);
          m = await at();
          assert(m.wrap < m.foot, "the table's foot is in sight: " + JSON.stringify(m));
          assert(Math.abs(m.barTop - m.wrap - 2) <= 0.5, "at the end the bar sits under the table: " + JSON.stringify(m));
          assert.deepEqual(errors, []);
          await ctx.close();
        });
      }
    }
  });
}

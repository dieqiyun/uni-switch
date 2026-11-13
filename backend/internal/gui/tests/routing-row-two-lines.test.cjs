// Run with Node's test runner and Playwright on the module path; see README.md.
// #2288 (xishuchkui-code): on a laptop's screen (about 1300px wide at 150%)
// Routing's request list was about 800px wide, beside the accounts. That is
// wider than 760px, where a row goes onto three lines, but too narrow for one
// line, so the row cut where it went to "Op… · deepseek-v4.1-fla…" and broke
// its metrics over two lines of their own, a different break on each row.
// Where its numbers no longer fit their column on one line, a row now keeps
// where it went whole on its first line and its numbers on one line under
// it; where they fit, one line as before (routing-columns, #860). Both
// engines, every language, at the reporter's list width, at 1300, 1100 and
// 420.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
// the reporter's request: Claude Desktop asking OpenCode Go's DeepSeek V4.1 Flash
const seat = { id: "opencode-go", provider: "opencode-go", name: "OpenCode Go", who: "OpenCode Go", kind: "account", model: "deepseek-v4.1-flash" };
function req(id, extra = {}) {
  const time = new Date(now.getTime() - (20 - id) * 60e3).toISOString();
  return { id, seq: id, time, agent: "claude-desktop", session: "s1", model: "opencode-go/deepseek-v4.1-flash", provider: "opencode-go",
    effort: "high", order: [seat], tries: [{ id: seat.id, model: seat.model, effort: "high", start: time, done: true, status: 200, ms: 5800, ttft: 5000 }],
    done: true, status: 200, ms: 5800, ttft: 5000, tokens: 178700, out: 170,
    usage: [{ provider: "opencode-go", model: seat.model, in: 100, out: 170, cache_read: 178500, cache_write: 0 }], priced: true, cost: 0.0007, ...extra };
}
const at = now.toISOString();
const fixtures = [req(12), req(11, { ms: 9400, ttft: 9200, tries: [{ id: seat.id, start: at, done: true, status: 429, fail: "rate", ms: 200 },
  { id: seat.id, model: seat.model, effort: "high", start: at, done: true, status: 200, ms: 9200, ttft: 9200 }] }), req(10), req(9), req(8)];

function serve(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"dark"};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [{ id: "claude-desktop", name: "Claude Desktop", fields: [] }], profiles: [], settings: { lang, theme: "dark" } });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.has("wait")) return new Promise(() => {}); // nothing more comes
      return json({ mine: true, now: now.toISOString(), seq: 12, totals: { requests: fixtures.length, rerouted: 0, errors: 0 }, routes: fixtures });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    return route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${engine} ${lang}: a request's row too narrow for one line keeps where it went whole and its metrics on one line under it`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await browser.newPage({ viewport: { width: 1500, height: 900 }, reducedMotion: "reduce" });
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      await page.goto("http://uni-switch.test/?view=routing");
      const rows = page.locator(".rt-req");
      await rows.nth(fixtures.length - 1).waitFor();
      // 1500: the list ~840px beside the accounts, the reporter's; 1300 and
      // 1100: ~1050 and ~850 on its own; 420: a phone's three lines
      for (const width of [1500, 1300, 1100, 420]) {
        await page.setViewportSize({ width, height: 900 });
        await page.waitForTimeout(200);
        const list = await page.locator(".rt-reqs").evaluate((e) => e.getBoundingClientRect().width);
        const seen = await rows.evaluateAll((rs) => rs.map((row) => {
          const box = (e) => e.getBoundingClientRect();
          // the text's own width, unrounded: WebKit cuts a box a fraction
          // narrower than its text with an ellipsis scrollWidth hides
          const inked = (e) => { const r = document.createRange(); r.selectNodeContents(e); return r.getBoundingClientRect().width; };
          const cut = (e) => !e || e.scrollWidth > e.clientWidth + 1 || inked(e) > box(e).width + 0.01;
          const said = row.querySelector(".to .said");
          return { text: said.textContent, who: cut(said.querySelector(".who")), mdl: cut(said.querySelector(".mdl")),
            to: box(row.querySelector(".to")), meta: box(row.querySelector(".meta")), row: box(row),
            metrics: [...row.querySelectorAll(".meta > *")].map((e) => ({ clipped: e.scrollWidth > e.clientWidth + 1, top: Math.round(box(e).top), l: box(e).left, r: box(e).right })) };
        }));
        for (const [i, r] of seen.entries()) {
          for (const m of r.metrics) {
            assert(!m.clipped, `${width}px row ${i}: a metric is clipped`);
            assert(m.l >= r.row.left - 0.5 && m.r <= r.row.right + 0.5, `${width}px row ${i}: a metric is outside its row`);
          }
          assert(r.meta.left >= r.to.right - 1 || r.meta.top >= r.to.bottom - 1, `${width}px row ${i}: the metrics overlap where it went`);
          if (list <= 760) continue; // three lines there, as before
          assert.equal(r.text, "OpenCode Go · deepseek-v4.1-flash");
          assert(!r.who && !r.mdl, `${width}px (list ${Math.round(list)}px) row ${i}: where it went is cut`);
          const lines = new Set(r.metrics.map((m) => m.top)).size;
          assert.equal(lines, 1, `${width}px (list ${Math.round(list)}px) row ${i}: the metrics break over ${lines} lines`);
        }
        const over = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
        assert(over <= 0, `${width}px: sideways scroll ${over}`);
      }
      assert.deepEqual(errors, []);
    });
  }
}

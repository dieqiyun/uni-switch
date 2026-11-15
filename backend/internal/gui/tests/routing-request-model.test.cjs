// Run with Node's test runner and Playwright on the module path; see README.md.
// #1926 (hibaliqya-yonma): with Routing kept to 1200px (61a0a69b) and the
// accounts beside the requests, a request's row gave the room to its
// metrics and the reasoning level, and the model that answered shrank to
// "· hi…": the metric column took the widest row's whole width for every
// row, and the model gave way before the level. The model that answered
// keeps a readable width now; the metrics wrap in their own column rather
// than take it. Both engines, every language, at the reporter's two-column
// width, at 1100 and at 420.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const seat = { id: "zcode", provider: "zcode", name: "ZCode", who: "someone@example.com", kind: "account", model: "glm-5.1-turbo" };
function req(id, extra = {}) {
  const time = new Date(now.getTime() - (20 - id) * 60e3).toISOString();
  return { id, seq: id, time, agent: "codex", session: "s" + (id % 3), model: id % 2 ? "group/auto-gpt-6-1-sol" : "group/group-sol-swe2", provider: "zcode",
    effort: "high", order: [seat], tries: [{ id: seat.id, model: seat.model, effort: "high", start: time, done: true, status: 200, ms: 14000, ttft: 9600 }],
    done: true, status: 200, ms: 14000, ttft: 9600, tokens: 396900, out: 670, usage: [{ provider: "zcode", model: seat.model, in: 1000, out: 670, cache_read: 395000, cache_write: 230 }],
    priced: true, cost: 0.088, ...extra };
}
const fixtures = [req(12), req(11, { kind: "agent_created_title" }), req(10, { kind: "subagent" }), req(9), req(8, { cost: 0.075, ms: 4200, ttft: 4200 })];

function serve(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"dark"};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [{ id: "codex", name: "Codex", fields: [] }], profiles: [], settings: { lang, theme: "dark" } });
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
    test(`${engine} ${lang}: a request's row keeps the model that answered readable beside its metrics`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await browser.newPage({ viewport: { width: 1320, height: 900 }, reducedMotion: "reduce" });
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      await page.goto("http://uni-switch.test/?view=routing");
      const rows = page.locator(".rt-req");
      await rows.nth(fixtures.length - 1).waitFor();
      for (const width of [1920, 1320, 1240, 1100, 900, 420]) {
        await page.setViewportSize({ width, height: 900 });
        await page.waitForTimeout(150);
        const seen = await rows.evaluateAll((rs) => rs.map((row) => {
          const box = (e) => e.getBoundingClientRect();
          // the text's own width, unrounded: WebKit cut a box a fraction
          // narrower than its text with an ellipsis that scrollWidth hides
          const inked = (e) => { const r = document.createRange(); r.selectNodeContents(e); return r.getBoundingClientRect().width; };
          const said = row.querySelector(".to .said"), ef = row.querySelector(".to .ef"), mdl = said.querySelector(".mdl");
          return { mdl: mdl && mdl.textContent, mdlCut: !mdl || mdl.scrollWidth > mdl.clientWidth + 1 || inked(mdl) > box(mdl).width + 0.01, said: said.clientWidth, full: said.scrollWidth, text: said.textContent, ef: ef ? ef.clientWidth : 0,
            to: box(row.querySelector(".to")), meta: box(row.querySelector(".meta")), row: box(row),
            metrics: [...row.querySelectorAll(".rt-metric")].map((e) => ({ clipped: e.scrollWidth > e.clientWidth + 1, l: box(e).left, r: box(e).right })) };
        }));
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.locator(".rt-reqs").screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}.png`) });
        }
        for (const [i, r] of seen.entries()) {
          // its provider and the start of its model, at the least; whole when it fits
          assert(r.said >= Math.min(r.full, 110), `${width}px row ${i}: the model that answered is ${r.said}px of ${r.full} ("${r.text}"), the level ${r.ef}px`);
          // and the model itself whole, the account giving way first
          assert.equal(r.text, "someone@example.com · glm-5.1-turbo");
          assert(!r.mdlCut && r.mdl === " · glm-5.1-turbo", `${width}px row ${i}: the model is cut (${r.said}px of ${r.full})`);
          for (const m of r.metrics) {
            assert(!m.clipped, `${width}px row ${i}: a metric is clipped`);
            assert(m.l >= r.row.left - 0.5 && m.r <= r.row.right + 0.5, `${width}px row ${i}: a metric is outside its row`);
          }
          assert(r.meta.width === 0 || r.meta.left >= r.to.right - 1 || r.meta.top >= r.to.bottom - 1, `${width}px row ${i}: the metrics overlap the model`);
        }
        const over = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
        assert(over <= 0, `${width}px: sideways scroll ${over}`);
      }
      assert.deepEqual(errors, []);
    });
  }
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// The Routing stage shows at most four agents (staged), but the poll plays
// every request under way. A request of a fifth agent was played against an
// agent the stage hadn't drawn, and play threw "Cannot read properties of
// undefined (reading 'node')" for each one, ~85 a minute under mixed
// traffic. Such a request is listed and not flown.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const relay = { id: "relay:team", provider: "relay", name: "Relay", kind: "key", who: "team", model: "gpt-6-sol" };
const other = { id: "other:solo", provider: "other", name: "Other", kind: "key", who: "solo", model: "gpt-6-sol" };
// six agents take turns, more than the stage's four
const AGENTS = ["codex", "claude", "gemini", "opencode", "kimi", "qwen"];
const route = (id, done) => ({
  id, seq: id, time: new Date().toISOString(), agent: AGENTS[id % AGENTS.length], model: `relay/m-${id}`, provider: id % 3 ? "relay" : "other",
  order: id % 3 ? [relay, other] : [other, relay],
  tries: [{ id: (id % 3 ? relay : other).id, model: "gpt-6-sol", start: new Date().toISOString(), done, status: done ? 200 : 0, ms: done ? 900 : 0 }],
  done, status: done ? 200 : 0, ms: done ? 900 : 0,
});

function serve(lang) {
  const state = {
    agents: AGENTS.map((id) => ({ id, name: id, path: `/test/${id}.json`, fields: [] })),
    profiles: [], settings: { lang, theme: "light" },
  };
  return async (r) => {
    const req = r.request(), url = new URL(req.url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/gateway/trace") {
      // a request every 80 ms, the one before it done: each under way while the next comes
      const after = Number(url.searchParams.get("after"));
      const now = new Date().toISOString();
      if (!after) return json({ mine: true, now, seq: 2, totals: { requests: 2 }, routes: [route(1, true), route(2, true)] });
      await new Promise((res) => setTimeout(res, 80));
      return json({ mine: true, now, seq: after + 1, totals: { requests: after + 1 }, routes: [route(after, true), route(after + 1, false)] });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/usage" && url.searchParams.get("period") === "today") return json({ calls: 2, input: 1000, output: 200, series: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/window/")) return r.fulfill({ status: 204 });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await r.fulfill({ body: await fs.readFile(file), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a request of an agent the stage leaves out plays without an error`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const ctx = await browser.newContext({ viewport: { width: 1200, height: 800 }, reducedMotion: "no-preference" });
      const page = await ctx.newPage();
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      page.setDefaultTimeout(8000);
      page.setDefaultNavigationTimeout(60000);
      await page.route("**/*", serve(lang));
      await page.goto("http://uni-switch.test/?view=routing");
      await page.locator("#view-routing .rt-srcs .rt-node").first().waitFor();
      let heard = 0;
      page.on("request", (q) => { if (q.url().includes("/api/gateway/trace")) heard++; });
      for (let i = 0; i < 80 && heard < 30; i++) await page.waitForTimeout(100);
      assert(heard >= 30, `the trace was read ${heard} times`);
      await page.waitForTimeout(500);
      assert(await page.locator("#view-routing .rt-srcs .rt-node").count() <= 4);
      assert.deepEqual(errors, []);
    });
  }
}

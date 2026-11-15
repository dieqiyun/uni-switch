// Run with Node's test runner and Playwright on the module path; see README.md.
// Routing, the request list: a request in the middle of the list that
// finishes has its row made again, and only that row goes in and out. The
// list put every row after it in again in front of the old one, ~40 rows
// moved and laid out on each update a busy gateway sends, and with two
// groups of hundreds of keys open WebKit dropped to a frame a second for a
// moment at each (#3164). In Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = Date.now();
const iso = (ms) => new Date(ms).toISOString();
const seat = { id: "codex@me", provider: "codex", name: "ChatGPT", who: "me@example.com", kind: "account", model: "gpt-6.1-sol", known: true, plan: "Pro" };

// request id, under way or done; the middle one (110) is still under way
function req(id, done, seq = id) {
  const t0 = now - (200 - id) * 1000;
  return {
    id, seq, time: iso(t0), agent: "codex", model: "gpt-6.1-sol", provider: "codex",
    order: [seat], tries: [{ id: seat.id, model: seat.model, start: iso(t0), done, ...(done ? { status: 200, ms: 4000 } : {}) }],
    done, ...(done ? { status: 200, ms: 4000, tokens: 2000, cost: 0.05, priced: true, usage: [{ in: 3000, cache_read: 8000, out: 400 }] } : {}),
  };
}
const first = Array.from({ length: 30 }, (_, i) => req(101 + i, 101 + i !== 110));

function serve(feed) {
  const state = { agents: [{ id: "codex", name: "Codex", path: "/test/codex", fields: [] }], profiles: [], settings: { lang: "en", theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: 'window.bootPrefs = {lang:"en",theme:"light",web:true};' });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/gateway/trace") {
      const trace = (routes) => json({ mine: true, now: new Date().toISOString(), seq: routes.length ? routes[routes.length - 1].seq : Number(url.searchParams.get("after")), totals: { requests: 0, rerouted: 0, errors: 0 }, routes });
      if (!url.searchParams.get("wait")) return trace(first);
      const r = await new Promise((res) => { feed.next = res; });
      return trace([r]);
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file).catch(() => ""), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: a request done in the middle of the list swaps its own row only`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const page = await browser.newPage({ viewport: { width: 1100, height: 900 } });
    page.setDefaultTimeout(5000);
    page.setDefaultNavigationTimeout(60000);
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    const feed = { next: null };
    await page.route("**/*", serve(feed));
    await page.goto("http://uni-switch.test/?view=routing");
    await page.locator(".rt-req").nth(first.length - 1).waitFor();
    await page.waitForTimeout(800);
    await page.evaluate(() => {
      const list = document.querySelector(".rt-reqs");
      window.__was = [...list.querySelectorAll(".rt-req:not(.probe)")];
      window.__moved = { added: 0, removed: 0 };
      window.__mo = new MutationObserver((ms) => {
        for (const m of ms) for (const [k, ns] of [["added", m.addedNodes], ["removed", m.removedNodes]])
          for (const n of ns) if (n.classList?.contains("rt-req") && !n.classList.contains("probe")) window.__moved[k]++;
      });
      window.__mo.observe(list, { childList: true });
    });
    for (let i = 0; i < 60 && !feed.next; i++) await page.waitForTimeout(50);
    assert(feed.next, "the page asks for the next trace");
    feed.next(req(110, true, 200));
    await page.waitForFunction(() => document.querySelectorAll(".rt-req.ok, .rt-req:not(.wait)").length && ![...document.querySelectorAll(".rt-req:not(.probe)")].every((r) => window.__was.includes(r)));
    await page.waitForTimeout(300);
    const got = await page.evaluate(() => {
      window.__mo.disconnect();
      const rows = [...document.querySelectorAll(".rt-reqs .rt-req:not(.probe)")];
      return { rows: rows.length, kept: window.__was.filter((r) => rows.includes(r)).length, moved: window.__moved };
    });
    assert.equal(got.rows, 30);
    assert.equal(got.kept, 29, "every other row is the one it was");
    assert.deepEqual(got.moved, { added: 1, removed: 1 }, "only the finished request's row goes out and in");
    assert.deepEqual(errors, []);
  });
}

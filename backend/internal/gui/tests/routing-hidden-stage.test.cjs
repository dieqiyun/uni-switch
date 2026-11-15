// Run with Node's test runner and Playwright on the module path; see README.md.
// The Routing page out of sight — another page picked, or the window hidden
// — builds nothing for the requests that come meanwhile (#2515, #2513). Each
// trace update put up the stage again (its agents, rows and wires) and the
// request list, out of sight on every page and in the tray panel. In a
// hidden window no frame is drawn, so the entrance and live animations on
// the nodes it replaced stayed on the document's timeline and held each one:
// WebView2 grew to GBs over a day. Out of sight the page is left as it was,
// and back in sight it is drawn at once with the newest request. Out of
// sight it doesn't ask for the trace either: a long-poll held open there was
// answered on every change, 4 to 8 times a second under traffic, for
// nothing it drew. The window's hidden state is faked, as headless pages
// are never hidden.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const relay = { id: "relay:team", provider: "relay", name: "Relay", kind: "key", who: "team", model: "gpt-6-sol" };
const other = { id: "other:solo", provider: "other", name: "Other", kind: "key", who: "solo", model: "gpt-6-sol" };
// agents and accounts take turns, so each update changes the stage
const route = (id, done) => ({
  id, seq: id, time: new Date().toISOString(), agent: id % 2 ? "codex" : "claude", model: `relay/m-${id}`, provider: id % 3 ? "relay" : "other",
  order: id % 3 ? [relay, other] : [other, relay],
  tries: [{ id: (id % 3 ? relay : other).id, model: "gpt-6-sol", start: new Date().toISOString(), done, status: done ? 200 : 0, ms: done ? 900 : 0 }],
  done, status: done ? 200 : 0, ms: done ? 900 : 0,
});

function serve(lang) {
  // a request every 80 ms, and the one before it done, whether or not the
  // page asks: the gateway's trace goes on without it
  const t0 = Date.now();
  const seqNow = () => 2 + Math.floor((Date.now() - t0) / 80);
  const state = {
    agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }, { id: "claude", name: "Claude Code", path: "/test/settings.json", fields: [] }],
    profiles: [], settings: { lang, theme: "light" },
  };
  return async (r) => {
    const req = r.request(), url = new URL(req.url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/gateway/trace") {
      // as the gateway does: what changed since after, at once when
      // something did, or once the next request comes
      const after = Number(url.searchParams.get("after"));
      if (after) while (seqNow() <= after) await new Promise((res) => setTimeout(res, 20));
      const seq = seqNow(), now = new Date().toISOString();
      const routes = [];
      for (let id = Math.max(1, after, seq - 4); id <= seq; id++) routes.push(route(id, id < seq));
      const ids = [];
      for (let id = Math.max(1, seq - 59); id <= seq; id++) ids.push(id);
      return json({ mine: true, now, seq, totals: { requests: seq }, routes, ids });
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

// the newest request the list shows
const newestListed = () => Math.max(0, ...[...document.querySelectorAll("#view-routing .rt-req .m")].map((m) => Number(m.textContent.replace(/\D+/g, "")) || 0));

const ways = {
  "another page picked": {
    hide: (page) => page.locator('#nav button[data-view="agents"]').click(),
    show: (page) => page.locator('#nav button[data-view="routing"]').click(),
  },
  "the window hidden": {
    hide: (page) => page.evaluate(() => window.__hide(true)),
    show: (page) => page.evaluate(() => window.__hide(false)),
  },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    for (const [way, how] of Object.entries(ways)) {
      test(`${engine} ${lang}: out of sight (${way}), the Routing page builds nothing for each request`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const ctx = await browser.newContext({ viewport: { width: 1200, height: 800 }, reducedMotion: "no-preference" });
        await ctx.addInitScript(() => {
          window.__hid = false;
          Object.defineProperty(Document.prototype, "hidden", { configurable: true, get: () => window.__hid });
          Object.defineProperty(Document.prototype, "visibilityState", { configurable: true, get: () => (window.__hid ? "hidden" : "visible") });
          window.__hide = (h) => { window.__hid = h; document.dispatchEvent(new Event("visibilitychange")); };
        });
        const page = await ctx.newPage();
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        await page.route("**/*", serve(lang));
        await page.goto("http://uni-switch.test/?view=routing");
        await page.locator("#view-routing .rt-srcs .rt-node").first().waitFor();
        // in sight, the list keeps up
        await page.waitForFunction(`(${newestListed})() >= 6`);

        await how.hide(page);
        // a request flying as the page went is let finish its step first
        await page.waitForTimeout(300);
        const seqAt = await page.evaluate(() => {
          window.__added = {};
          new MutationObserver((ms) => {
            for (const m of ms) for (const n of m.addedNodes) {
              if (n.nodeType !== 1) continue;
              const k = n.tagName.toLowerCase() + (n.classList.length ? "." + n.classList[0] : "");
              window.__added[k] = (window.__added[k] || 0) + 1;
            }
          }).observe(document.querySelector("#view-routing"), { childList: true, subtree: true });
          return Math.max(0, ...[...document.querySelectorAll("#view-routing .rt-req .m")].map((m) => Number(m.textContent.replace(/\D+/g, "")) || 0));
        });
        let heard = 0;
        page.on("request", (q) => { if (q.url().includes("/api/gateway/trace")) heard++; });
        // 2 s out of sight: 25 requests come, and the page asks for none
        await page.waitForTimeout(2000);
        assert(heard <= 1, `the trace was read ${heard} times out of sight`);
        // a stage's node, a row, a wire, a list's request: none of them put up
        const added = await page.evaluate(() => window.__added);
        assert.deepEqual(added, {}, `out of sight, the page added ${JSON.stringify(added)}`);

        // back in sight: drawn at once, with the newest request
        await how.show(page);
        await page.waitForFunction(`(${newestListed})() >= ${seqAt + 20}`);
        assert(await page.locator("#view-routing .rt-srcs .rt-node").first().isVisible());
        assert.deepEqual(errors, []);
      });
    }
  }
}

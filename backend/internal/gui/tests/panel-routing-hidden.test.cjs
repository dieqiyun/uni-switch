// Run with Node's test runner and Playwright on the module path; see README.md.
// The tray panel is hidden most of the time, and its Routing tab is not
// drawn again for every request while it is (#2515): a hidden page draws no
// frames, so the infinite animations on each replaced tab (the live dot, a
// waiting row's arrow) stayed on the document's timeline and held every old
// tab. On Windows the hidden panel grew to 265k nodes in 11 minutes and GBs
// over a day. Hidden, the tab is drawn again only when its shape (what the
// panel's height is fitted to) changes, no request's dot flies, and once
// the panel shows it is drawn in full, newest request first. Hidden, it
// doesn't ask for the trace either: shown again, it asks once for all that
// came meanwhile. The page's hidden state is faked here, as headless pages
// are never hidden.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const relay = { id: "relay:team", provider: "relay", name: "Relay", kind: "key", who: "team", model: "gpt-6-sol" };
const route = (id, done) => ({
  id, seq: id, time: new Date().toISOString(), agent: "codex", model: `relay/m-${id}`, provider: "relay", order: [relay],
  tries: [{ id: relay.id, model: "gpt-6-sol", start: new Date().toISOString(), done, status: done ? 200 : 0, ms: done ? 900 : 0 }],
  done, status: done ? 200 : 0, ms: done ? 900 : 0,
});

function serve(lang) {
  // a request every 80 ms, under way, whether or not the panel asks
  const t0 = Date.now();
  const seqNow = () => 2 + Math.floor((Date.now() - t0) / 80);
  const state = { agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  return async (r) => {
    const req = r.request(), url = new URL(req.url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/gateway/trace") {
      // as the gateway does: what changed since after, at once when
      // something did, or once the next request comes. Each is a tab drawn
      // again
      const after = Number(url.searchParams.get("after"));
      if (!after) return json({ mine: true, now: new Date().toISOString(), seq: 2, totals: { requests: 2 }, routes: [route(1, true), route(2, true)] });
      while (seqNow() <= after) await new Promise((res) => setTimeout(res, 20));
      const seq = seqNow(), routes = [];
      for (let id = Math.max(after + 1, seq - 9); id <= seq; id++) routes.push(route(id, false));
      return json({ mine: true, now: new Date().toISOString(), seq, totals: { requests: seq }, routes });
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
    test(`${engine} ${lang}: the hidden tray panel doesn't draw its Routing tab again for every request`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const ctx = await browser.newContext({ viewport: { width: 360, height: 560 }, reducedMotion: "no-preference" });
      await ctx.addInitScript(() => {
        try { localStorage.setItem("uni-switch.panelTab", "routing"); } catch {}
        // the panel's window put away, as WebView2 tells a hidden page
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
      await page.goto("http://uni-switch.test/?mode=panel");
      await page.locator("#panelRouting .pr-req").first().waitFor();
      assert(await page.locator("#panelRouting").isVisible());
      // shown, the tab keeps up: its newest request is the one heard last
      await page.waitForFunction(() => document.querySelectorAll("#panelRouting .pr-req").length === 6);

      // hidden: requests keep coming, the tab is (nearly) never drawn again
      const seqAt = await page.evaluate(() => {
        window.__hide(true);
        window.__draws = 0;
        window.__dots = 0;
        // each draw puts up a new head
        new MutationObserver((ms) => {
          for (const m of ms) for (const n of m.addedNodes) {
            if (n.classList?.contains("pr-head")) window.__draws++;
            if (n.classList?.contains("ps-dot")) window.__dots++;
          }
        }).observe(document.querySelector("#panelRouting"), { childList: true, subtree: true });
        return Number(document.querySelector("#panelRouting .pr-req .pr-a .m").textContent.replace(/\D+/g, ""));
      });
      let heard = 0;
      page.on("request", (q) => { if (q.url().includes("/api/gateway/trace")) heard++; });
      // 2 s hidden: 25 requests come, and the panel asks for none
      await page.waitForTimeout(2000);
      assert(heard <= 1, `the trace was read ${heard} times while hidden`);
      const hidden = await page.evaluate(() => ({ draws: window.__draws, dots: window.__dots, shown: Number(document.querySelector("#panelRouting .pr-req .pr-a .m").textContent.replace(/\D+/g, "")) }));
      assert.equal(hidden.draws, 0, `the hidden tab was drawn ${hidden.draws} times over ${heard} requests`);
      assert.equal(hidden.dots, 0, "no request's dot flies in a hidden panel");
      assert.equal(hidden.shown, seqAt, "what the tab shows is left as it was");

      // shown again: it asks once for what came meanwhile, and draws the
      // newest request first
      await page.evaluate(() => window.__hide(false));
      await page.waitForFunction((at) => Number(document.querySelector("#panelRouting .pr-req .pr-a .m").textContent.replace(/\D+/g, "")) >= at + 20, seqAt);
      assert(heard >= 1, "shown again, the trace is asked for");
      assert.equal(await page.locator("#panelRouting .pr-req").count(), 6);
      assert.deepEqual(errors, []);
    });
  }
}

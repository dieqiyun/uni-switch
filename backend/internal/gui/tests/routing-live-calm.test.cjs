// Run with Node's test runner and Playwright on the module path; see README.md.
// The Routing page holds still while requests come (#3731, gpboyer2: under
// traffic the stage, the caption and the request list kept being made
// again, so nothing could be read). A request still going is answered by
// the trace 4 to 8 times a second, each saying a little more of it; its
// row in the list is the same row throughout, patched in place, so its
// pulse never starts over, while what it says does change. With nothing
// coming, the countdowns' second writes nothing on the page at all.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = Date.now();
const iso = (ms) => new Date(ms).toISOString();
const acct = (n) => ({ id: `acct-${n}`, provider: `prov${n}`, name: `Provider ${n}`, who: `user${n}@example.com`, kind: "account", model: `model-${n}`, known: true, used: 10 * n, plan: "Pro" });
function req(id, agent, done) {
  const t0 = now - (done ? 30000 : 3000) - (200 - id) * 10;
  const order = [1, 2, 3].map(acct);
  const tries = [{ id: order[0].id, model: order[0].model, start: iso(t0), done, ...(done ? { ms: 800, status: 200 } : {}) }];
  return { id, seq: id, time: iso(t0), agent, model: order[0].model, provider: order[0].provider, order, tries, done, status: done ? 200 : 0, ms: done ? 900 : 0, tokens: 1500 };
}
const first = Array.from({ length: 12 }, (_, i) => req(100 + i, i % 2 ? "claude" : "codex", true));
const live = req(200, "codex", false);
const state = { agents: ["codex", "claude"].map((id) => ({ id, name: id, path: `/test/${id}`, fields: [] })), profiles: [], settings: { lang: "en", theme: "light" } };

function serve(feed) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: 'window.bootPrefs = {lang:"en",theme:"light",web:true};' });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/gateway/trace") {
      const trace = (routes, seq) => json({ mine: true, now: new Date().toISOString(), seq, totals: { requests: 0, rerouted: 0, errors: 0 }, routes });
      if (!url.searchParams.get("wait")) return trace(first, 111);
      // the long poll: held until the test turns the traffic on, then
      // answered every 150ms with the same request, its tokens counting up
      if (!feed.on) await new Promise((res) => { feed.wake = res; });
      if (feed.closed) return route.abort().catch(() => {});
      await new Promise((r) => setTimeout(r, 150));
      feed.n++;
      return trace([{ ...live, seq: 200 + feed.n, tokens: 100 * feed.n }], 200 + feed.n).catch(() => {});
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ models: [], groups: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: the Routing page holds still while requests come`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const page = await (await browser.newContext({ viewport: { width: 1100, height: 900 } })).newPage();
    page.setDefaultTimeout(5000);
    page.setDefaultNavigationTimeout(60000);
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    const feed = { on: false, n: 0 };
    await page.route("**/*", serve(feed));
    t.after(async () => { feed.closed = true; feed.wake?.(); await browser.close(); });
    await page.goto("http://uni-switch.test/?view=routing");
    await page.locator(".rt-req").nth(first.length - 1).waitFor();
    await page.waitForTimeout(2500); // the first request's play

    // what the page writes: nodes put in or taken out, attributes, text;
    // and each animation that starts
    const watch = () => page.evaluate(() => {
      const m = window.__m = { nodes: [], attrs: 0, text: 0, anims: [] };
      window.__mo = new MutationObserver((ms) => { for (const x of ms) {
        if (x.type === "childList") for (const n of [...x.addedNodes, ...x.removedNodes]) m.nodes.push(`${x.target.getAttribute?.("class") || x.target.nodeName} ⊃ ${n.getAttribute?.("class") || n.nodeName}`);
        else if (x.type === "attributes") m.attrs++;
        else m.text++;
      } });
      window.__mo.observe(document.querySelector("#view-routing"), { subtree: true, childList: true, attributes: true, characterData: true });
      window.__an = (e) => m.anims.push(e.animationName);
      document.addEventListener("animationstart", window.__an, true);
    });
    const read = () => page.evaluate(() => { window.__mo.disconnect(); document.removeEventListener("animationstart", window.__an, true); return window.__m; });

    // nothing coming: three of the countdowns' seconds write nothing
    await watch();
    await page.waitForTimeout(3200);
    const idle = await read();
    assert.deepEqual(idle, { nodes: [], attrs: 0, text: 0, anims: [] }, "with nothing coming the page was written to");

    // a request still going, answered again and again
    feed.on = true;
    feed.wake?.();
    const row = page.locator(".rt-req.wait");
    await row.waitFor();
    await page.waitForTimeout(600);
    await row.evaluate((b) => { window.__row = b; });
    const said = await row.textContent();
    await watch();
    const n0 = feed.n;
    await page.waitForTimeout(2500);
    const busy = await read();
    assert(feed.n - n0 >= 8, `the trace must have answered again and again (${feed.n - n0})`);
    assert.equal(await row.evaluate((b) => b === window.__row), true, "the request's row was made again");
    assert.notEqual(await row.textContent(), said, "the row must say what the trace says now");
    assert.deepEqual(busy.anims.filter((a) => a === "rtlive"), [], "the row's pulse started over");
    // (the sizer's probes aside too: unseen, a request's numbers taking
    // another shape is a probe of that shape, measured and never drawn)
    assert.deepEqual(busy.nodes.filter((n) => !/^rt-sky |^rt-sizer |\bpkt\b|rt-flier|#text/.test(n)), [], "nodes were put in afresh while the same request came (the packet in flight aside)");
    assert.deepEqual(errors, []);
  });
}

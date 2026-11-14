// Run with Node's test runner and Playwright on the module path; see README.md.
// A busy gateway doesn't make the Routing page redo what it shows (#308,
// luw2007: with trace updates coming often the page grew sluggish — each
// one rebuilt the request list, its rows and the story whole, and did so
// even with the page out of sight). As more requests come and go, the row
// of a request that hasn't changed stays the very same row, and clicking it
// still picks it; with another tab open, the list isn't touched at all, and
// coming back shows every request that came meanwhile.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const iso = (ms) => new Date(ms).toISOString();
const acct = (n) => ({ id: `acct-${n}`, provider: `prov${n}`, name: `Provider ${n}`, who: `user${n}@example.com`, kind: "account", model: `model-${n}`, known: true, used: 10 * n, plan: "Pro" });
const agentsOf = ["codex", "claude", "opencode", "kimi"];

function req(id) {
  const t0 = Date.now() - 60e3 + id * 100;
  const order = [acct(id % 5), acct((id + 1) % 5)];
  const tries = [{ id: order[0].id, model: order[0].model, start: iso(t0), done: true, ms: 300, status: 200 }];
  return { id, seq: id, time: iso(t0), agent: agentsOf[id % agentsOf.length], model: order[0].model, provider: order[0].provider, order, tries, done: true, status: 200, ms: 400, tokens: 900 };
}
const first = [1, 2, 3, 4, 5].map(req);

function serve(lang, feed) {
  const state = { agents: agentsOf.map((id) => ({ id, name: id[0].toUpperCase() + id.slice(1), path: `/test/${id}`, fields: [] })), profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/gateway/trace") {
      const trace = (routes) => json({ mine: true, now: new Date().toISOString(), seq: routes.length ? routes[routes.length - 1].seq : Number(url.searchParams.get("after")), totals: { requests: 0, rerouted: 0, errors: 0 }, routes });
      if (!url.searchParams.get("wait")) return trace(first);
      while (!feed.q.length && !feed.closed) await new Promise((r) => setTimeout(r, 20));
      return trace(feed.q.splice(0)).catch(() => {}); // every change since, as the gateway answers
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

const words = { en: "Requests", zh: "请求" };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: trace updates keep the rows that haven't changed, and redraw nothing out of sight`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const context = await browser.newContext({ viewport: { width: 1100, height: 760 } });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const feed = { q: [], closed: false };
      await page.route("**/*", serve(lang, feed));
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-busy.png`) });
        }
        feed.closed = true;
        await browser.close();
      });
      const drain = async () => { for (let i = 0; i < 100 && feed.q.length; i++) await page.waitForTimeout(50); assert.equal(feed.q.length, 0); };
      await page.goto("http://uni-switch.test/?view=routing");
      await page.locator(".rt-req").nth(first.length - 1).waitFor();
      await page.waitForTimeout(2500);
      assert.equal(await page.getByText(words[lang], { exact: true }).first().isVisible(), true);

      // the rows as they are, marked
      const n0 = await page.evaluate(() => { const rs = [...document.querySelectorAll(".rt-req")]; rs.forEach((r) => { r.__kept = true; }); return rs.length; });
      assert.equal(n0, first.length);

      // more requests come, one update each
      for (let i = 0; i < 8; i++) feed.q.push(req(10 + i));
      await drain();
      await page.waitForFunction((n) => document.querySelectorAll(".rt-req").length === n, first.length + 8);
      await page.waitForTimeout(2500); // their plays end
      const kept = await page.evaluate(() => [...document.querySelectorAll(".rt-req")].filter((r) => r.__kept).length);
      assert.equal(kept, first.length, "the rows of the requests that didn't change must be the same rows");
      // and a kept row still picks its request
      const at = await page.evaluate(() => [...document.querySelectorAll(".rt-req")].findIndex((r) => r.__kept && r.getAttribute("aria-pressed") !== "true"));
      await page.locator(".rt-req").nth(at).click();
      await page.waitForFunction((at) => document.querySelectorAll(".rt-req")[at].getAttribute("aria-pressed") === "true", at);
      assert.equal(await page.locator(".rt-req").nth(at).evaluate((r) => r.__kept === true), true, "the row picked must be a kept one");

      // another tab open: updates come, and the list isn't touched
      await page.locator('#nav button[data-view="agents"]').click();
      await page.evaluate(() => {
        window.__touched = 0;
        window.__mo = new MutationObserver((ms) => { window.__touched += ms.length; });
        window.__mo.observe(document.querySelector(".rt-reqs"), { childList: true, subtree: true, attributes: true, characterData: true });
      });
      for (let i = 0; i < 6; i++) feed.q.push(req(30 + i));
      await drain();
      await page.waitForTimeout(1200);
      const touched = await page.evaluate(() => window.__touched);
      assert.equal(touched, 0, `the list was redrawn ${touched} times out of sight`);
      // back: all of them listed
      await page.locator('#nav button[data-view="routing"]').click();
      await page.waitForFunction((n) => document.querySelectorAll(".rt-req").length === n, first.length + 8 + 6);
      assert.deepEqual(errors, []);
    });
  }
}

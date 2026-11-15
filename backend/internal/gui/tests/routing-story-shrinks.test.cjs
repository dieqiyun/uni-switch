// Run with Node's test runner and Playwright on the module path; see README.md.
// Routing, live: the story of the last request ("How the last request was
// routed") is as tall as the last few requests' stories, not as tall as the
// tallest one seen (夜松云 on Discord: the box grew when a request was
// handed on through many members and never shrank back). A request handed
// on four times is told in many lines; the box keeps that height while the
// next two short requests come in, so nothing jumps from one request to the
// next (routing-ctx-still), and shrinks to the short ones' at the third.
// English and Chinese, Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = Date.now();
const iso = (ms) => new Date(ms).toISOString();
const seat = (n) => ({ id: "codex@me" + n, provider: "codex", name: "ChatGPT " + n, who: "me" + n + "@example.com", kind: "account", model: "gpt-6.1-sol", known: true, plan: "Pro" });
// a request answered by its first member, or handed on through `hops`
// members that refused it before the last answered
function req(id, hops, step = "done") {
  const t0 = now - (200 - id) * 1000, done = step === "done";
  const order = Array.from({ length: hops + 1 }, (_, i) => seat(i));
  const tries = order.map((s, i) => i < hops
    ? { id: s.id, model: s.model, start: iso(t0 + i * 100), done: true, status: 429, ms: 90, err: "rate limited: try again in 30s" }
    : { id: s.id, model: s.model, start: iso(t0 + i * 100), done, ...(done ? { status: 200, ms: 4000 } : {}) });
  return {
    id, seq: id * 10 + (done ? 2 : 0), time: iso(t0), agent: "codex", session: "019a-session", model: "gpt-6.1-sol", provider: "codex",
    order, tries, done, ...(done ? { status: 200, ms: 4000 + hops * 100, tokens: 2000, cost: 0.05, priced: true, usage: [{ in: 3000, cache_read: 87000, out: 400 }] } : {}),
  };
}
const first = [req(101, 0), req(102, 0)];

function serve(lang, feed) {
  const state = { agents: [{ id: "codex", name: "Codex", path: "/test/codex", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/gateway/trace") {
      const trace = (routes) => json({ mine: true, now: new Date().toISOString(), seq: routes.length ? routes[routes.length - 1].seq : Number(url.searchParams.get("after")), totals: { requests: 0, rerouted: 0, errors: 0 }, routes });
      if (!url.searchParams.get("wait")) return trace(first);
      const r = await new Promise((res) => { feed.next = res; });
      return trace([].concat(r));
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file).catch(() => ""), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: the last request's story shrinks back after a long one`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const [lang, width] of [["en", 1100], ["zh", 420]]) {
      await t.test(`${lang} ${width}px`, async () => {
        const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const feed = { next: null };
        await page.route("**/*", serve(lang, feed));
        await page.goto("http://uni-switch.test/?view=routing");
        await page.locator(".rt-req").nth(first.length - 1).waitFor();
        await page.locator(".rt-log .rt-steps li").first().waitFor();
        await page.waitForTimeout(800);
        const send = async (r) => {
          for (let i = 0; i < 60 && !feed.next; i++) await page.waitForTimeout(50);
          const next = feed.next;
          assert(next, "the page asks for the next trace");
          feed.next = null;
          next(r);
          await page.waitForTimeout(400);
        };
        const story = () => page.evaluate(() => {
          const s = document.querySelector(".rt-log .rt-steps");
          const lines = [...s.children].reduce((b, li) => Math.max(b, li.getBoundingClientRect().bottom), s.getBoundingClientRect().top);
          return { h: s.getBoundingClientRect().height, content: lines - s.getBoundingClientRect().top, held: s.style.minHeight };
        });
        const short = await story();

        await send(req(103, 4));
        const tall = await story();
        assert(tall.h > short.h + 40, `the long request is told in more lines: ${tall.h} vs ${short.h}`);

        // under way after it, and the two answered after it: held, so
        // nothing jumps from one request to the next
        await send(req(104, 0, "start"));
        assert((await story()).h >= tall.h - 0.5, "held while under way");
        for (const id of [104, 105]) {
          await send(req(id, 0));
          const h = (await story()).h;
          assert(h >= tall.h - 0.5, `${id} held: ${h} vs ${tall.h}`);
        }

        // the third short one answered: the long one has gone out of the
        // hold, and the box is as tall as its own lines
        await send(req(106, 0));
        const after = await story();
        assert(after.h < tall.h - 40, `the box shrank back after the long request: ${after.h} vs ${tall.h} (min-height ${after.held || "none"})`);
        assert(Math.abs(after.h - short.h) <= 1, `as tall as a short request: ${after.h} vs ${short.h}`);
        assert(after.h - after.content <= 24, `no empty room under its lines: ${after.h} tall, lines to ${after.content}`);

        assert.deepEqual(errors, []);
        feed.next?.(req(999, 0));
        await context.close();
      });

      // A busy gateway (shensmart on Discord, a remote uni-switch in a local
      // browser: the room under the story grew and never shrank back):
      // each request is answered as the next one starts, so the two come
      // in one trace and the newest is always under way. None of them is
      // ever drawn answered, yet the long one stops holding the box once
      // as many requests have been told after it.
      await t.test(`${lang} ${width}px, the newest always under way`, async () => {
        const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const feed = { next: null };
        await page.route("**/*", serve(lang, feed));
        await page.goto("http://uni-switch.test/?view=routing");
        await page.locator(".rt-req").nth(first.length - 1).waitFor();
        await page.locator(".rt-log .rt-steps li").first().waitFor();
        await page.waitForTimeout(800);
        const send = async (r) => {
          for (let i = 0; i < 60 && !feed.next; i++) await page.waitForTimeout(50);
          const next = feed.next;
          assert(next, "the page asks for the next trace");
          feed.next = null;
          next(r);
          await page.waitForTimeout(400);
        };
        const story = () => page.evaluate(() => {
          const s = document.querySelector(".rt-log .rt-steps");
          const lines = [...s.children].reduce((b, li) => Math.max(b, li.getBoundingClientRect().bottom), s.getBoundingClientRect().top);
          return { h: s.getBoundingClientRect().height, content: lines - s.getBoundingClientRect().top, held: s.style.minHeight };
        });

        await send(req(103, 4));
        const tall = await story();
        await send([req(104, 0, "start")]);
        const going = await story();
        assert(going.h >= tall.h - 0.5, `held while the next is under way: ${going.h} vs ${tall.h}`);
        for (let id = 105; id <= 110; id++) await send([req(id - 1, 0), req(id, 0, "start")]);
        const after = await story();
        assert(after.h < tall.h - 40, `the box shrank back with the newest always under way: ${after.h} vs ${tall.h} (min-height ${after.held || "none"})`);
        assert(after.h - after.content <= 24, `no empty room under its lines: ${after.h} tall, lines to ${after.content}`);

        assert.deepEqual(errors, []);
        feed.next?.(req(999, 0));
        await context.close();
      });
    }
  });
}

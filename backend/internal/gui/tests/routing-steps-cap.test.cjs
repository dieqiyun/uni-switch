// Run with Node's test runner and Playwright on the module path; see README.md.
// A request tried on many keys keeps its steps in a box of their own
// (#3764, hheei; jayswu's patch): its story used to grow a line or two a
// key, so 40 keys pushed the request list off the screen, and clicking
// between it and a one-key request moved the list by a screen each time.
// Now the list moves by no more than the box, the long story scrolls in
// it, and another request's story starts at its top.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = Date.now();
const iso = (ms) => new Date(ms).toISOString();
const key = (n) => ({ id: `key-${n}`, provider: "gem", name: `Gemini key ${n}`, kind: "key", model: "gemini-3-pro", known: true });
function req(id, keys) {
  const t0 = now - 60000 + id * 1000;
  const order = Array.from({ length: keys }, (_, i) => key(i + 1));
  const tries = order.map((k, i) => i < keys - 1
    ? { id: k.id, model: k.model, start: iso(t0 + i * 20), done: true, ms: 20, status: 429, error: "429 RESOURCE_EXHAUSTED: Quota exceeded for this key" }
    : { id: k.id, model: k.model, start: iso(t0 + i * 20), done: true, ms: 800, status: 200 });
  return { id, seq: id, time: iso(t0), agent: "codex", model: "gemini-3-pro", provider: "gem", order, tries, done: true, status: 200, ms: 900, tokens: 1500 };
}
const routes = [req(1, 1), req(2, 40), req(3, 1), req(4, 40)];
const state = { agents: [{ id: "codex", name: "codex", path: "/test/codex", fields: [] }], profiles: [], settings: { lang: "en", theme: "light" } };

async function serve(route) {
  const url = new URL(route.request().url());
  const json = (data) => route.fulfill({ json: data });
  if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: 'window.bootPrefs = {lang:"en",theme:"light",web:true};' });
  if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
  if (url.pathname === "/api/state") return json(state);
  if (url.pathname === "/api/gateway/trace") {
    if (url.searchParams.get("wait")) return new Promise(() => {});
    return json({ mine: true, now: new Date().toISOString(), seq: 4, totals: { requests: 0, rerouted: 0, errors: 0 }, routes });
  }
  if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
  if (url.pathname === "/api/groups") return json({ models: [], groups: [], pools: [] });
  if (url.pathname === "/api/providers") return json({ providers: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
  if (url.pathname.startsWith("/api/")) return json({});
  const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
  const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
  await route.fulfill({ body: await fs.readFile(file), contentType });
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: a request tried on many keys keeps its steps in a box, and the list stays near`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const page = await (await browser.newContext({ viewport: { width: 1100, height: 900 } })).newPage();
    page.setDefaultTimeout(5000);
    page.setDefaultNavigationTimeout(60000);
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.route("**/*", serve);
    t.after(() => browser.close());
    await page.goto("http://uni-switch.test/?view=routing");
    const rows = page.locator(".rt-req");
    await rows.nth(routes.length - 1).waitFor();
    const steps = page.locator(".rt-steps");
    const pickTo = async (n) => {
      await rows.nth(routes.length - n).click(); // newest first
      await page.waitForFunction((n) => document.querySelectorAll(".rt-steps li").length > (n % 2 ? 0 : 40), n);
    };
    const shape = () => steps.evaluate((s) => ({ h: s.getBoundingClientRect().height, sh: s.scrollHeight, top: s.scrollTop, under: document.querySelector(".rt-req").getBoundingClientRect().top }));

    await pickTo(1);
    const short = await shape();
    await pickTo(2);
    const long = await shape();
    assert(long.sh > 600, `40 keys' story must be long (${long.sh}px)`);
    assert(long.h <= 201, `the story's box grew to ${long.h}px`);
    assert(long.under - short.under <= 201, `the request list moved ${long.under - short.under}px between a one-key and a 40-key request`);

    // scrolled down in one story, another request's starts at its top
    await steps.evaluate((s) => { s.scrollTop = 400; });
    assert((await shape()).top > 0);
    // the story is drawn once the click's play is over
    await pickTo(4);
    await page.waitForFunction(() => document.querySelector(".rt-steps").scrollTop === 0).catch(() => {});
    assert.equal((await shape()).top, 0, "another request's story opened scrolled down");
    assert.deepEqual(errors, []);
  });
}

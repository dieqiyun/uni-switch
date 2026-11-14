// Run with Node's test runner and Playwright on the module path; see README.md.
// A plugin's page the reader left (pluginpages.js): a frame in a hidden view
// isn't hidden to itself, so the page is told as the reader leaves and
// comes back (uni-switch.onVisibility, uni-switch.visible), and its frame, with its
// scripts, timers and connections, is let go after 10 minutes away. Back
// sooner, it is the same frame; opened after, it loads again. A page whose
// package says uni-switch.page.keepAlive keeps its frame however long. A clock
// stands in for time, so nothing waits; the API is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const bridge = path.resolve(__dirname, "../pagebridge.js");

const IDLE = "e".repeat(32), KEEP = "f".repeat(32);
const entry = (tok, name, more = {}) => ({
  spec: "/x/" + name, name, key: "plugin-" + name, title: name, reads: [],
  url: `/_uni-switch/plugin/${tok}/index.html`, ...more,
});
const pages = [entry(IDLE, "idle"), entry(KEEP, "keep", { keepAlive: true })];
// what the page heard, kept where the test reads it
const PAGE = `<!doctype html><meta charset="utf-8"><script src="_uni-switch/bridge.js"></script>
<script>window.heard = []; uni-switch.onVisibility((v) => heard.push(v));</script><p>page</p>`;

async function server(route) {
  const url = new URL(route.request().url());
  const json = (data) => route.fulfill({ json: data });
  if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"en",theme:"light",web:false};` });
  if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
  if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang: "en", theme: "light" } });
  if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
  if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
  if (url.pathname === "/api/plugin-pages") return json(pages);
  if (url.pathname === "/api/plugin-panels") return json([]);
  if (url.pathname.startsWith("/api/")) return json({});
  const m = url.pathname.match(/^\/_uni-switch\/plugin\/([0-9a-f]{32})\/(.+)$/);
  if (m) {
    const headers = { "X-Content-Type-Options": "nosniff", "Cache-Control": "no-cache" };
    if (m[2] === "_uni-switch/bridge.js") return route.fulfill({ headers: { ...headers, "Content-Type": "text/javascript" }, body: await fs.readFile(bridge) });
    if (m[2] === "index.html") return route.fulfill({ headers: { ...headers, "Content-Type": "text/html" }, body: PAGE });
    return route.fulfill({ status: 404, body: "" });
  }
  if (url.host !== "uni-switch.test") return route.fulfill({ status: 404, body: "" });
  const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
  const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
  try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
}

const MIN = 60e3;

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: a page the reader left is told, and let go after a while unless it keeps itself`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const context = await browser.newContext({ viewport: { width: 1100, height: 700 }, reducedMotion: "reduce" });
    const page = await context.newPage();
    page.setDefaultTimeout(8000);
    page.setDefaultNavigationTimeout(60000);
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.clock.install({ time: Date.now() });
    await page.route("**/*", server);
    await page.goto("http://uni-switch.test/");
    await page.locator("#nav button[data-plugin-page]").nth(1).waitFor({ state: "attached" });

    const frames = (key) => page.locator(`#view-plugin-${key} iframe.plugin-frame`).count();
    // the page's frame once it has said hello, marked so a new one shows
    async function opened(key, tok) {
      await page.evaluate((k) => show("plugin-" + k), key);
      await page.waitForFunction((tok) => [...document.querySelectorAll("iframe.plugin-frame")].some((f) => f.src.includes(tok)), tok);
      let f;
      for (let i = 0; i < 100 && !(f = page.frames().find((x) => x.url().includes(tok))); i++) await page.waitForTimeout(50);
      await f.waitForFunction(() => window.uniSwitch?.theme);
      return f;
    }
    const pass = async (ms) => { await page.clock.runFor(ms); await page.waitForTimeout(100); };

    const idle = await opened("idle", IDLE);
    await idle.evaluate(() => { window.mark = 1; });
    assert.deepEqual(await idle.evaluate(() => [uniSwitch.visible, heard]), [true, []], "open: seen, nothing heard yet");

    // left: told at once, and its frame still there short of 10 minutes
    await page.evaluate(() => show("agents"));
    await idle.waitForFunction(() => heard.length === 1);
    assert.deepEqual(await idle.evaluate(() => [uniSwitch.visible, heard]), [false, [false]], "left: told it is hidden");
    await pass(9 * MIN);
    assert.equal(await frames("idle"), 1, "kept 9 minutes away");

    // back before then: the same frame, told it is seen, its timer done
    await page.evaluate(() => show("plugin-idle"));
    await idle.waitForFunction(() => heard.length === 2);
    assert.deepEqual(await idle.evaluate(() => [uniSwitch.visible, heard, window.mark]), [true, [false, true], 1], "back: the same frame, seen");
    await pass(11 * MIN);
    assert.equal(await frames("idle"), 1, "a page the reader is on is never let go");

    // a page that keeps itself, left beside it
    const keep = await opened("keep", KEEP);
    await keep.evaluate(() => { window.mark = 2; });
    await page.evaluate(() => show("agents"));
    await keep.waitForFunction(() => heard.length === 1);
    await pass(10 * MIN + 1000);
    assert.equal(await frames("idle"), 0, "let go 10 minutes after it was left");
    assert.equal(await frames("keep"), 1, "keepAlive: kept");
    assert.deepEqual(await keep.evaluate(() => [uniSwitch.visible, heard, window.mark]), [false, [false], 2], "keepAlive: told, still running");
    await pass(60 * MIN);
    assert.equal(await frames("keep"), 1, "keepAlive: kept an hour on");

    // opened again: it loads again, and is seen
    const again = await opened("idle", IDLE);
    assert.deepEqual(await again.evaluate(() => [uniSwitch.visible, heard, window.mark]), [true, [], undefined], "opened after: a new frame");
    assert.equal(await frames("idle"), 1);
    // and nothing the window held for the one let go says it broke
    await pass(11e3);
    assert.equal(await page.locator("#view-plugin-idle .slot-error").count(), 0, "no 'stopped working'");
    assert.deepEqual(errors, []);
  });
}

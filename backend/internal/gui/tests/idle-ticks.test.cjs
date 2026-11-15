// Run with Node's test runner and Playwright on the module path; see README.md.
// A page out of sight ticks nothing (#3185): Routing's countdowns, Usage's
// look at its refresh and the lanes' read each woke the web content process
// every second or two from wherever the window was, drawing nothing. Each runs
// only while its page, or a lane pill, is in sight, and starts again when it
// is. A clock stands in for time here, so nothing waits; no backend, the API
// is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = Date.now();

async function server(route) {
  const url = new URL(route.request().url());
  const json = (data) => route.fulfill({ json: data });
  if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"en",theme:"light",web:false};` });
  if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
  if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang: "en", theme: "light" }, fx: { rate: 7.2, at: new Date().toISOString() } });
  if (url.pathname === "/api/usage/requests") return json({ period: "30d", rows: [], offset: 0, total: 0, calls: 0, errors: 0, input: 0, output: 0, cache_read: 0, cache_write: 0, reasoning: 0, cost: 0, unpriced: 0, bucket: "day", series: [], by: { provider: [], agent: [], model: [] }, agents: [], providers: [] });
  if (url.pathname === "/api/usage") return json({ calls: 0, errors: 0, input: 0, output: 0, cache_read: 0, cache_write: 0, reasoning: 0, unpriced: 0, cost: 0, bucket: "day", series: [], agents: [], models: [], path: "~/.config/uni-switch/usage.jsonl" });
  if (url.pathname === "/api/usage/quotas") return json([]);
  if (url.pathname === "/api/sessions") return json({ sessions: [], dirs: [] });
  if (url.pathname === "/api/sessions/stats") return json({ from: "", to: "", days: [], agents: {} });
  if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
  if (url.pathname.startsWith("/api/")) return json({});
  const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
  const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
  await route.fulfill({ body: await fs.readFile(file), contentType });
}

// every interval's ticks, by the script that set it and its period
const census = () => {
  window.ticks = {};
  const set = window.setInterval;
  window.setInterval = function (fn, ms, ...rest) {
    const at = (new Error().stack || "").split("\n").map((l) => /uni-switch\.test\/((?:plugins\/\w+\/)?[\w-]+\.js):\d+/.exec(l)).find(Boolean);
    const key = `${at ? at[1] : "?"} ${ms}`;
    return set.call(window, function () { window.ticks[key] = (window.ticks[key] || 0) + 1; return typeof fn === "function" ? fn.apply(this, arguments) : undefined; }, ms, ...rest);
  };
};

const WATCHED = [["plugins/routing/client.js", 1000], ["plugins/usage/page.js", 1000], ["app.js", 2000]];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: pages out of sight tick nothing, and tick again once shown`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const errors = [];
    const context = await browser.newContext({ viewport: { width: 1180, height: 700 }, reducedMotion: "reduce" });
    const p = await context.newPage();
    p.setDefaultTimeout(5000);
    p.setDefaultNavigationTimeout(60000);
    p.on("pageerror", (e) => errors.push(e.message));
    await p.clock.install({ time: now });
    await p.addInitScript(census);
    await p.route("**/*", server);
    await p.goto("http://uni-switch.test/");
    await p.locator('[data-view="usage"]').first().waitFor();
    const ticks = async () => p.evaluate(() => ({ ...window.ticks }));
    const of = (all, [file, ms]) => all[`${file} ${ms}`] || 0;
    const pass = async (ms) => { await p.clock.runFor(ms); await p.waitForTimeout(150); };

    // on Providers, with no lane pill: none of the three ticks
    await pass(1000);
    let before = await ticks();
    await pass(10_000);
    let after = await ticks();
    for (const w of WATCHED) assert.equal(of(after, w) - of(before, w), 0, `${w[0]} ticked out of sight`);

    // Usage shown: its look ticks each second; Routing's still doesn't
    await p.locator('[data-view="usage"]').first().click();
    await pass(1000);
    before = await ticks();
    await pass(5000);
    after = await ticks();
    assert.ok(of(after, WATCHED[1]) - of(before, WATCHED[1]) >= 4, `Usage's look ticks while it is shown: ${JSON.stringify(after)}`);
    assert.equal(of(after, WATCHED[0]) - of(before, WATCHED[0]), 0, "Routing's countdown ticked on Usage");

    // Routing shown: its countdown ticks; Usage's has stopped
    await p.locator('[data-view="routing"]').first().click();
    await pass(1000);
    before = await ticks();
    await pass(5000);
    after = await ticks();
    assert.ok(of(after, WATCHED[0]) - of(before, WATCHED[0]) >= 4, `Routing's countdown ticks while it is shown: ${JSON.stringify(after)}`);
    assert.equal(of(after, WATCHED[1]) - of(before, WATCHED[1]), 0, "Usage's look ticked on Routing");
    assert.deepEqual(errors, []);
  });
}

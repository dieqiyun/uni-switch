// Run with Node's test runner and Playwright on the module path; see README.md.
// #2591 (Usage › Sessions): the token totals count the cache the way the
// Overview's have since #2378 (uni-switch.usageWithCache), once the reader has
// asked for it there: the tokens tile with its label pop-up, a session's row
// in the list, the projects and models bars, the day chart and the top
// sessions (whose order follows the count shown); a pick made on the
// Sessions tab refreshes that tab, not the Overview, and is kept across a
// reload. The default stays without the cache, as #2378 left it.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const to = new Date(2026, 9, 17);
const back = (i) => { const d = new Date(to); d.setDate(d.getDate() - i); return d; };
const iso = (d) => [d.getFullYear(), d.getMonth() + 1, d.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const days = [back(1), back(0)].map((d, i) => ({
  date: iso(d),
  usage: i ? [
    // 31K with the cache, 11K without
    { agent: "claude", cwd: "/work/alpha", model: "claude-opus-4", input: 10000, output: 1000, cache_read: 20000, cache_write: 0, cost: 1.5, priced: true },
  ] : [
    // 125K with the cache, 46K with the codex row, 34K without it
    { agent: "claude", cwd: "/work/alpha", model: "claude-opus-4", input: 30000, output: 4000, cache_read: 90000, cache_write: 1000, cost: 1.5, priced: true },
    { agent: "codex", cwd: "/work/beta", model: "gpt-5", input: 10000, output: 2000, cache_read: 0, cache_write: 0, cost: 0.4, priced: true },
  ],
  active: [{ agent: "claude", cwd: "/work/alpha", seconds: 600, hours: Object.assign(new Array(24).fill(0), { 9: 600 }) }],
}));
// the sessions in the list are the top sessions: the first ranked by tokens
// in and out, the second below it there but above it once the cache counts
const spent = (key, title, input, output, cache_read) => ({
  key, agent: "claude", id: key.split(":")[1], title, cwd: "/work/alpha", last: to.toISOString(),
  input, output, cache_read, cache_write: 0, cost: 12, priced: true, active: 600, models: ["claude-opus-4"],
});
const TOPS = [spent("claude:s0", "Latest cache", 900000, 5000, 50000), spent("claude:s1", "Plain small", 400000, 4000, 1000000)];
const full = (key) => ({
  key, agent: "claude", name: "Claude Code", icon: "claude", id: key.split(":")[1], cwd: "/work/alpha", title: "Latest", start: back(1).toISOString(), last: to.toISOString(),
  input: 900000, output: 5000, cache_read: 50000, cache_write: 0, cost: 12, priced: true,
  models: [{ model: "claude-opus-4", input: 900000, output: 5000, cost: 12, priced: true }],
});
const sessions = TOPS.map((t) => ({ ...full(t.key), title: t.title, input: t.input, output: t.output, cache_read: t.cache_read }));

function serve(lang) {
  const state = { agents: [{ id: "claude", name: "Claude Code", path: "/test/settings.json", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/sessions/progress") return json({ indexing: false });
    if (url.pathname === "/api/sessions") return json({ sessions, dirs: ["/test/sessions"] });
    if (url.pathname === "/api/sessions/stats") return json({ from: days[0].date, to: iso(to), days, agents: { claude: "Claude Code", codex: "Codex" } });
    if (url.pathname === "/api/sessions/overview") {
      return json({
        count: 2, median: 654000, p90: 905000, days: days.map((d) => d.usage.length ? 1 : 0),
        top: { tokens: TOPS, cost: TOPS, active: TOPS },
        messages: days.map((d) => d.usage.length ? 10 : 0), output: days.map(() => 0),
        shape: {
          messages: { edges: [1, 6, 16, 31, 61, 121], counts: [2], total: 2 },
          minutes: { edges: [1, 6, 16, 31, 61, 121], counts: [2], total: 2 },
          autonomy: { edges: [0, 1, 3, 6, 11, 21], counts: [2], total: 2 },
        },
        tools: {
          calls: 10, sessions: 2,
          top: [["Bash", "Bash", 8, 2], ["Read", "Read", 2, 1]].map(([name, category, calls, sessions]) => ({ name, category, calls, sessions })),
          categories: [["Bash", 8], ["Read", 2]].map(([name, calls]) => ({ name, calls, sessions: 1 })),
          weeks: [iso(back(7))].map((start) => ({ start, calls: { Bash: 8, Read: 2 } })),
        },
        skills: { calls: 0, count: 0, top: [] },
      });
    }
    if (url.pathname === "/api/sessions/one") return json(full(new URL(route.request().url()).searchParams.get("key")));
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/usage/quotas") return json([]); // an array, as the Go side writes it
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    const body = await fs.readFile(file).catch(() => null);
    await (body ? route.fulfill({ body, contentType }) : route.fulfill({ status: 404, body: "" }));
  };
}

// the words the reader sees on the tile's label and in its pop-up
const want = {
  en: { tokens: "tokens", withCache: "tokens with cache", without: "Without cache", with: "With cache", head: "Token totals" },
  zh: { tokens: "token", withCache: "token（含缓存）", without: "不含缓存", with: "含缓存", head: "token 汇总" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: the Sessions tab's token totals count the cache once the Overview's pick says so`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const context = await browser.newContext({ viewport: { width: 1100, height: 760 }, reducedMotion: "reduce", timezoneId: "Asia/Shanghai" });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      // the Overview's pick, made before the page loads, is the only thing
      // that changes the Sessions tab's counts
      await page.addInitScript(() => {
        localStorage.setItem("uni-switch.usageTab", "sessions");
        localStorage.setItem("uni-switch.sessRange", "all");
        if (!localStorage.getItem("uni-switch.usageWithCache")) localStorage.setItem("uni-switch.usageWithCache", "1");
      });
      t.after(async () => await browser.close());
      await page.goto("http://uni-switch.test/?view=usage");
      await page.locator("#sessGrid:not([hidden])").waitFor();
      await page.waitForFunction(() => !document.querySelector("#sessStats .kpi.stale") && document.querySelectorAll("#sessTop .sess-top").length === 2);
      const fmt = (n) => page.evaluate((n) => fmtN(n), n);

      // the tokens tile: with the cache, and said so
      const tile = page.locator("#sessStats .kpi").nth(1);
      const pick = tile.locator(".kpi-pick");
      assert.equal((await tile.locator("b").textContent()).trim(), await fmt(168000), "the tile's tokens");
      assert.equal((await pick.textContent()).trim(), want[lang].withCache, "the tile's label");
      assert.equal(await tile.locator("small.parts > span").count(), 3, "in, out and the cache");

      // a session's row, the projects bar and the day chart count it too
      const row = page.locator("#sessList .row.sess", { hasText: "Latest cache" });
      assert.equal((await row.locator(".num b").textContent()).trim(), await fmt(955000), "the list row's tokens");
      const bar = page.locator("#sessProjects .sess-bar", { hasText: "alpha" });
      assert.equal((await bar.locator(".v").textContent()).trim(), await fmt(156000), "the project bar's tokens");
      const day = await page.locator("#sessChart .bars .bar").first().getAttribute("title");
      assert.match(day, new RegExp((await fmt(137000)).replace(/[.]/g, "\\.")), "the day's tokens count the cache");

      // the top sessions read as the count they are ranked by
      const top = page.locator("#sessTop .sess-top").first();
      assert.equal(await top.locator(".name").textContent(), "Plain small", "ranked by the count shown");
      assert.equal((await top.locator(".v").textContent()).trim(), await fmt(1404000));

      // picked back to without the cache, on the Sessions tab: the tab
      // follows and stays
      await pick.scrollIntoViewIfNeeded();
      await pick.click();
      const menu = page.locator(".proto-menu:visible");
      await menu.waitFor();
      assert.equal((await menu.locator(".pm-head").textContent()).trim(), want[lang].head);
      assert.deepEqual(await menu.locator(".pm-name").allTextContents(), [want[lang].without, want[lang].with]);
      await menu.locator(".pm-item").first().click();
      await page.waitForTimeout(150);
      assert.equal(await page.evaluate(() => localStorage.getItem("uni-switch.usageWithCache")), "0");
      assert(await page.locator("#sessStats").isVisible(), "still on the Sessions tab");
      assert.equal((await tile.locator("b").textContent()).trim(), await fmt(57000), "the tile's tokens");
      assert.equal((await tile.locator(".kpi-pick").textContent()).trim(), want[lang].tokens, "the tile's label");
      assert.equal(await tile.locator("small.parts > span").count(), 2, "in and out");
      assert.equal((await page.locator("#sessList .row.sess", { hasText: "Latest cache" }).locator(".num b").textContent()).trim(), await fmt(905000), "the list row's tokens");
      assert.equal((await page.locator("#sessProjects .sess-bar", { hasText: "alpha" }).locator(".v").textContent()).trim(), await fmt(45000), "the project bar's tokens");
      assert.equal(await page.locator("#sessTop .sess-top").first().locator(".name").textContent(), "Latest cache", "ranked by the count shown");
      assert.equal((await page.locator("#sessTop .sess-top").first().locator(".v").textContent()).trim(), await fmt(905000));
      const dayOff = await page.locator("#sessChart .bars .bar").first().getAttribute("title");
      assert.match(dayOff, new RegExp((await fmt(46000)).replace(/[.]/g, "\\.")), "the day's tokens");

      // kept across a reload
      await page.reload();
      await page.locator("#sessGrid:not([hidden])").waitFor();
      await page.waitForFunction(() => !document.querySelector("#sessStats .kpi.stale") && document.querySelectorAll("#sessTop .sess-top").length === 2);
      assert.equal((await page.locator("#sessStats .kpi").nth(1).locator("b").textContent()).trim(), await fmt(57000), "the choice is kept");

      assert.deepEqual(errors, []);
    });
  }
}

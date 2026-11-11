// Run with Node's test runner and Playwright on the module path; see README.md.
// Opening the allowances reads them now (#1588, qq666666qq): the Usage page
// from the nav, the window brought back on it, the tray panel's Allowances
// tab picked or the panel brought back on it, and a menu bar cell each load
// usage/quotas?asked=1, as Refresh now does. The vendors aren't asked again
// for an opening within 30s of the last asked read (switching tabs and
// back, focus and visibility both firing); Refresh now always asks, and the
// timed refresh never does. The fake backend answers an unasked read with
// the reading it kept, an asked one with the new reading, as AskUsage has
// the backend do. Chromium and WebKit, English and Chinese, the panel at
// its narrow width.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(process.env.UNI_SWITCH_ASSETS || path.join(__dirname, "../assets"));

function serve(lang, acct) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light", quotaReads: "asked" } });
    if (url.pathname === "/api/usage/quotas") {
      const asked = url.searchParams.get("asked") === "1";
      acct.reads.push(asked ? "asked" : "kept");
      if (acct.hold) await acct.hold;
      if (asked) acct.kept = acct.vendor;
      return json([{ provider: "codex", name: "Codex", user: "a@example.com", plan: "plus",
        windows: [{ name: "5 hours", used: acct.kept, resetsAt: "2030-01-01T00:00:00Z" }] }]);
    }
    if (url.pathname === "/api/usage/quotas/history") { acct.hist++; return json([]); }
    if (url.pathname === "/api/usage") return json({ calls: 1, input: 100, output: 10, cost: 0.01, series: [], agents: [], models: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

async function open(browser, t, surface, lang) {
  const acct = { reads: [], kept: 10, vendor: 40, hold: null, hist: 0 };
  const errors = [];
  const context = await browser.newContext({ viewport: surface === "panel" ? { width: 360, height: 640 } : { width: 1000, height: 760 }, reducedMotion: "reduce" });
  t.after(() => context.close());
  const page = await context.newPage();
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  page.on("pageerror", (e) => errors.push(e.message));
  await page.clock.install();
  await page.route("**/*", serve(lang, acct));
  await page.goto(surface === "panel" ? "http://uni-switch.test/?mode=panel" : "http://uni-switch.test/");
  const card = surface === "panel" ? ".pq-card" : ".subscription-card";
  // what the page shows once the reads on their way are in
  const shows = (n) => page.waitForFunction(([s, n]) => !quotasLoading && document.querySelector(s)?.textContent.includes(n + "%"), [card, n]);
  const settle = () => page.waitForFunction(() => !quotasLoading);
  const asked = () => acct.reads.filter((r) => r === "asked").length;
  return { page, acct, errors, shows, settle, asked };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: opening the Usage page reads the allowances, once in 30s`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const { page, acct, errors, shows, settle, asked } = await open(browser, t, "window", lang);
      const nav = (v) => page.locator(`#nav [data-view="${v}"]`).click();
      await page.locator('#nav [data-view="usage"]').waitFor();
      await settle();
      assert.equal(asked(), 0, "the Agents page asks nothing");

      await nav("usage");
      await shows(40);
      assert.equal(asked(), 1, `opening Usage asks: ${acct.reads}`);

      // back and forth, and the window's focus and visibility both firing,
      // within the gap: the vendors aren't asked again
      acct.vendor = 55;
      await nav("agents");
      await nav("usage");
      await page.evaluate(() => { window.dispatchEvent(new Event("focus")); document.dispatchEvent(new Event("visibilitychange")); });
      await settle();
      assert.equal(asked(), 1, `reopened within 30s, nothing asked: ${acct.reads}`);
      await shows(40);

      // the window brought back to the front on Usage later asks, once,
      // though focus and visibility both fire
      await page.clock.fastForward(31e3);
      await settle();
      const n = asked();
      await page.evaluate(() => { window.dispatchEvent(new Event("focus")); document.dispatchEvent(new Event("visibilitychange")); });
      await shows(55);
      assert.equal(asked(), n + 1, `the window back on Usage asks once: ${acct.reads}`);

      // Refresh now asks whatever the gap
      acct.vendor = 70;
      await page.locator("#usageReload").click();
      await shows(70);
      assert.equal(asked(), n + 2, `Refresh now asks: ${acct.reads}`);

      // the Requests tab doesn't show allowances, the Overview picked again does
      await page.clock.fastForward(31e3);
      await settle();
      const m = asked();
      await page.locator(":is(#usageTab .opt, .nav-s):visible").nth(1).click();
      await page.evaluate(() => window.dispatchEvent(new Event("focus")));
      await settle();
      assert.equal(asked(), m, `the Requests tab brought back asks nothing: ${acct.reads}`);
      acct.vendor = 80;
      await page.locator(":is(#usageTab .opt, .nav-s):visible").first().click();
      await shows(80);
      assert.equal(asked(), m + 1, `the Overview picked asks: ${acct.reads}`);

      // the page opened from the nav on the Requests tab asks nothing, nor
      // reads the allowances' history (#4064)
      await page.locator(":is(#usageTab .opt, .nav-s):visible").nth(1).click();
      await nav("agents");
      await page.clock.fastForward(31e3);
      await settle();
      const r = asked(), hist = acct.hist;
      await nav("usage");
      await page.locator("#ledgerPane").waitFor();
      await settle();
      assert.equal(asked(), r, `Usage opened on Requests asks nothing: ${acct.reads}`);
      assert.equal(acct.hist, hist, "nor reads the allowances' history");
      acct.vendor = 85;
      await page.locator(":is(#usageTab .opt, .nav-s):visible").first().click();
      await shows(85);
      assert.equal(asked(), r + 1, `then the Overview picked asks: ${acct.reads}`);

      // the timed refresh, left alone for minutes, never asks
      const k = asked();
      for (let i = 0; i < 4; i++) { await page.clock.fastForward(61e3); await settle(); }
      assert(acct.reads.length > 0);
      assert.equal(asked(), k, `the timer asks nothing: ${acct.reads}`);
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), "the page fits");
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: opening the tray panel's Allowances reads them, once in 30s`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const { page, acct, errors, shows, settle, asked } = await open(browser, t, "panel", lang);
      const tab = (v) => page.locator(`#ptabs [data-ptab="${v}"]`).click();
      await shows(10);
      assert.equal(asked(), 0, "the panel on Agents asks nothing");
      // on Agents, the panel brought back asks nothing either
      await page.evaluate(() => window.dispatchEvent(new Event("focus")));
      await settle();
      assert.equal(asked(), 0, "the panel back on Agents asks nothing");

      await tab("usage");
      await shows(40);
      assert.equal(asked(), 1, `Allowances picked asks: ${acct.reads}`);

      acct.vendor = 55;
      await tab("agents");
      await tab("usage");
      await tab("usage");
      await page.evaluate(() => { window.dispatchEvent(new Event("focus")); document.dispatchEvent(new Event("visibilitychange")); panelQuotaFocus("codex|a@example.com"); });
      await settle();
      assert.equal(asked(), 1, `reopened within 30s, nothing asked: ${acct.reads}`);

      // the panel brought back up on Allowances later asks, once
      await page.clock.fastForward(31e3);
      await settle();
      let n = asked();
      await page.evaluate(() => { window.dispatchEvent(new Event("focus")); document.dispatchEvent(new Event("visibilitychange")); });
      await shows(55);
      assert.equal(asked(), n + 1, `the panel back on Allowances asks once: ${acct.reads}`);

      // a menu bar cell clicked later asks too
      await page.clock.fastForward(31e3);
      await settle();
      n = asked();
      acct.vendor = 60;
      await page.evaluate(() => panelQuotaFocus("codex|a@example.com"));
      await shows(60);
      assert.equal(asked(), n + 1, `a menu bar cell asks: ${acct.reads}`);

      // an ordinary read on its way doesn't swallow the opening: the asked
      // one goes after it, one at a time
      await page.clock.fastForward(31e3);
      await settle();
      await tab("agents");
      let release;
      acct.hold = new Promise((r) => (release = r));
      const before = acct.reads.length;
      await page.evaluate(() => { loadQuotas(); });
      await page.waitForFunction(() => !!quotasLoading);
      acct.vendor = 65;
      await tab("usage");
      acct.hold = null;
      release();
      await shows(65);
      assert.deepEqual(acct.reads.slice(before), ["kept", "asked"], "the asked read went after the one on its way");

      // the card fits the narrow panel
      assert(await page.locator(".pq-card").first().evaluate((e) => e.scrollWidth <= e.clientWidth + 1), "the card fits");
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), "the panel fits");
      assert.deepEqual(errors, []);
    });
  }
}

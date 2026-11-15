// Run with Node's test runner and Playwright on the module path; see README.md.
// Dates and times read in the GUI's language, not the system's (#1843): a
// Mac in Chinese with uni-switch set to English showed Routing's day pills as
// "10月11日 周日", since they were formatted with no locale and so followed
// the system. Every language is tried on a system in another one (English
// on zh-CN, the rest on en-US), and English on an en-GB system keeps the
// system's own English. Routing's day pills and a request's time are read.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const ymd = (d) => [d.getFullYear(), d.getMonth() + 1, d.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
// days before yesterday, so none reads "today" or "yesterday"
const days = Array.from({ length: 3 }, (_, i) => ({ day: ymd(new Date(now.getTime() - (i + 3) * 864e5)), requests: 10 + i }));
// 14:05:09 local, so a 12-hour clock shows PM and a 24-hour one 14
const reqAt = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 14, 5, 9);
const who = { id: "fixture-key", provider: "fixture", name: "Fixture", who: "key-1", kind: "key", model: "model-a", routing: "", used: 0 };
const route = {
  id: 7, seq: 7, time: reqAt.toISOString(), agent: "fixture", model: "model-a", provider: "fixture",
  order: [who], tries: [{ id: who.id, model: "model-a", start: reqAt.toISOString(), done: true, status: 200, ms: 900 }],
  done: true, status: 200, ms: 900, tokens: 1200,
};

function serve(lang) {
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [{ id: "fixture", name: "Fixture", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) await new Promise((res) => setTimeout(res, 20e3));
      return json({ mine: true, now: now.toISOString(), seq: 7, totals: { requests: 1, rerouted: 0, errors: 0 }, routes: [route] });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days, routes: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await r.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// [GUI language, the system's locale, the tag its dates must read in]
const cases = [
  ["en", "zh-CN", "en"],
  ["en", "en-GB", "en-GB"],
  ["zh", "en-US", "zh-CN"],
  ["zh-TW", "en-US", "zh-TW"],
  ["ja", "en-US", "ja-JP"],
  ["de", "en-US", "de-DE"],
];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const [lang, sys, tag] of cases) {
    test(`${engine}: GUI in ${lang} on a ${sys} system reads dates in ${tag}`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      for (const width of [1100, 390]) {
        const context = await browser.newContext({ viewport: { width, height: 760 }, locale: sys, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        await page.goto("http://uni-switch.test/?view=routing");
        await page.locator(".rt-days .rt-day").nth(1).waitFor({ state: "attached" });
        await page.locator(".rt-req .at").first().waitFor();
        await page.waitForTimeout(200);
        assert.equal(await page.evaluate(() => navigator.language), sys, "the browser stands in for the system's locale");

        const [want, other] = await page.evaluate(([ds, tag, sys]) => [tag, sys].map((loc) => ds.map((d) =>
          new Date(d + "T12:00:00").toLocaleDateString(loc, { month: "short", day: "numeric", weekday: "short" }))), [days.map((d) => d.day), tag, sys]);
        // the days as shown: the pills in the bar, then those past its width
        // in the menu under its last pill (#961)
        const pills = await page.locator(".rt-days .rt-day:not(.rt-day-more)").evaluateAll((xs) => xs.slice(1).filter((x) => !x.hidden && x.getBoundingClientRect().width > 0).map((x) => x.querySelector("span").textContent));
        const shown = [...pills];
        if (pills.length < days.length) {
          // (dispatched: at 390 the bar sits below the fold, and the page
          // keeps its scroll on a click)
          await page.locator(".rt-days .rt-day-more").dispatchEvent("click");
          const items = page.locator(".proto-menu .pm-item");
          await items.first().waitFor();
          const texts = await items.allInnerTexts();
          for (let i = pills.length; i < days.length; i++) {
            const hit = texts.find((s) => s.includes(want[i]));
            shown.push(hit ? want[i] : texts.join(" | "));
          }
          await page.keyboard.press("Escape");
        }
        assert.deepEqual(shown, want, `${width}px: the days read in ${tag}`);
        if (tag !== sys) assert.notDeepEqual(shown, other, `${width}px: the days read in the system's ${sys}`);
        if (lang === "en") assert.doesNotMatch(shown.join(" "), /[　-鿿]/, `${width}px: English days hold CJK: ${shown}`);

        const at = await page.locator(".rt-req .at").first().textContent();
        const wantAt = await page.evaluate(([at, tag]) => new Date(at).toLocaleTimeString(tag, { hour: "2-digit", minute: "2-digit", second: "2-digit" }), [route.time, tag]);
        assert.equal(at, wantAt, `${width}px: the request's time reads in ${tag}`);
        assert.deepEqual(errors, []);
        await context.close();
      }
    });
  }
}

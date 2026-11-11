// Run with Node's test runner and Playwright on the module path; see README.md.
// A rest lifted outside the Routing page (zoecats on Discord): a key rate
// limited (429) and another sent to the back of the order, then Try now
// pressed in the provider's editor. Before, Routing kept saying 429 and
// "at the back" on its stage and under Accounts and keys until uni-switch
// restarted, as the requests it keeps still say so. Now the trace tells the
// page what was lifted and when (lifted), and the page shows neither
// resting, with no request since. In every language, at a wide and a
// narrow window, in Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const start = new Date(now.getTime() - 60e3).toISOString();
const key = (k, extra = {}) => ({ id: "relay#" + k, provider: "relay", name: "Relay", kind: "key", who: "key " + k, model: "glm-5", ...extra });
const rest = { why: "rate", status: 429, until: new Date(now.getTime() + 10 * 60e3).toISOString(), key: "relay#a" };
const a = key("a", { rest }), b = key("b"), c = key("c", { sunk: start });
const routes = [{
  id: 1, seq: 1, time: start, agent: "codex", model: "relay/glm-5", provider: "relay",
  order: [b, a, c],
  tries: [
    { id: a.id, model: a.model, start, done: true, status: 429, ms: 300, fail: "rate", rest, error: "Relay: Too many requests" },
    { id: b.id, model: b.model, start, done: true, status: 200, ms: 900 },
  ],
  done: true, status: 200, ms: 1300,
}];

function serve(lang, lift) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/gateway/trace") {
      if (!url.searchParams.has("wait")) return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 1, rerouted: 1, errors: 0 }, ids: [1], routes });
      // the page waits; Try now in the provider's editor lifts both, and
      // the trace tells it with no route of its own
      if (lift.told) return; // the next wait waits on
      lift.told = true;
      await lift.when;
      const t = new Date().toISOString();
      return json({ mine: true, now: t, seq: 2, totals: { requests: 1, rerouted: 1, errors: 0 }, ids: [1], routes: [], lifted: { "relay#a": t, "relay#c": t } });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file).catch(() => ""), contentType });
  };
}

const sideways = (page) => page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [1100, 420]) {
      test(`${engine} ${lang} ${width}px: a rest lifted in the provider's editor stops showing on Routing`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        let go;
        const lift = { told: false, when: new Promise((r) => { go = r; }) };
        await page.route("**/*", serve(lang, lift));
        t.after(async () => {
          go();
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-lifted-elsewhere.png`), fullPage: true });
          }
          await browser.close();
        });
        await page.goto("http://uni-switch.test/?view=routing");
        await page.locator(".rt-act").first().waitFor();

        // the three keys fold under their provider; the fold says one rests
        const fold = page.locator("li.rt-fold").first();
        await fold.waitFor();
        const foldSaid = (await fold.locator("em").textContent()).trim();
        await fold.click();
        const stage = (who) => page.locator("li", { has: page.locator(".who", { hasText: new RegExp(`^${who}$`) }) }).filter({ has: page.locator("code.mdl") });
        await stage("key a").first().waitFor({ state: "attached" });
        assert.equal(await stage("key a").count(), 1);
        assert.equal(await stage("key c").count(), 1);
        // before: a rests, c is at the back
        assert(await stage("key a").evaluate((e) => e.classList.contains("rest")), "key a doesn't rest before the lift");
        // Accounts and keys folds the three too: one of them rests
        const actSaid = (await page.locator(".rt-act .st").first().textContent()).trim();
        const sunkSaid = (await stage("key c").locator("em").textContent()).trim();
        assert(sunkSaid, "key c says nothing before the lift");

        go();
        await page.waitForFunction(() => {
          const li = [...document.querySelectorAll("li")].find((l) => l.querySelector("code.mdl") && l.querySelector(".who")?.textContent === "key a");
          return li && !li.classList.contains("rest");
        });
        assert.notEqual((await page.locator(".rt-act .st").first().textContent()).trim(), actSaid, "Accounts and keys still counts key a resting");
        assert.notEqual((await stage("key c").locator("em").textContent()).trim(), sunkSaid, "key c still at the back after the lift");
        await fold.click(); // folded again, it counts none resting
        assert.notEqual((await fold.locator("em").textContent()).trim(), foldSaid, "the fold still counts key a resting");
        assert(!(await sideways(page)), "the page scrolls sideways");
        assert.deepEqual(errors, []);
      });
    }
  }
}

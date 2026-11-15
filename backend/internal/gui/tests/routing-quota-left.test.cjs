// Run with Node's test runner and Playwright on the module path; see README.md.
// #602: with Settings → Allowance display on Left, the Routing page's
// accounts say how much is left, as the Usage page and the menu bar do —
// on the stage and in the list of what each account did, the bars filling
// with what is left; on Used they say how much is used. In English and
// Chinese.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = Date.now();
const iso = (ms) => new Date(ms).toISOString();
const accts = [
  { id: "acct-1", provider: "codex", name: "Codex", who: "work@example.com", kind: "account", model: "gpt-6", known: true, used: 30, plan: "Plus" },
  { id: "acct-2", provider: "codex", name: "Codex", who: "spare@example.com", kind: "account", model: "gpt-6", known: true, used: 95, plan: "Pro" },
];
const route = {
  id: 7, seq: 7, time: iso(now - 2000), agent: "codex", model: "gpt-6", provider: "codex", done: true, status: 200, ms: 900, tokens: 1500,
  order: accts, tries: [{ id: "acct-1", model: "gpt-6", start: iso(now - 2000), done: true, ms: 800, status: 200 }],
};

function serve(lang, left) {
  const state = { agents: [{ id: "codex", name: "Codex", path: "/test/codex", fields: [] }], profiles: [], settings: { lang, theme: "light", quotaLeft: left } };
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) return new Promise(() => {}); // nothing more comes
      return json({ mine: true, now: new Date().toISOString(), seq: 7, totals: { requests: 1, rerouted: 0, errors: 0 }, routes: [route] });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ models: [], groups: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await r.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { left: { work: "70% left", spare: "5% left · kept for last" }, used: { work: "30% used", spare: "95% used · kept for last" } },
  zh: { left: { work: "剩余 70%", spare: "剩余 5% · 留到最后" }, used: { work: "已用 30%", spare: "已用 95% · 留到最后" } },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    for (const left of [true, false]) {
      const w = words[lang][left ? "left" : "used"];
      test(`${engine} ${lang}: the Routing page's accounts say how much is ${left ? "left" : "used"}`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await (await browser.newContext({ viewport: { width: 1100, height: 760 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, left));
        await page.goto("http://uni-switch.test/?view=routing");
        const stage = (who) => page.locator("li", { has: page.locator(".who", { hasText: who }) }).first();
        await stage("spare@example.com").locator("em").filter({ hasText: w.spare }).waitFor();
        assert.equal((await stage("spare@example.com").locator("em").textContent()).trim(), w.spare);
        // the account that answered says so; its bar is what it has
        assert.equal(await stage("work@example.com").locator(".bar i").evaluate((i) => i.style.width), left ? "70%" : "30%");
        assert.equal(await stage("spare@example.com").locator(".bar i").evaluate((i) => i.style.width), left ? "5%" : "95%");
        // what each account did: the same word, as of the request
        const act = page.locator(".rt-act", { hasText: "work@example.com" }).first();
        await act.waitFor();
        assert.ok((await act.locator(".st").textContent()).startsWith(w.work), await act.locator(".st").textContent());
        assert.equal(await act.locator(".bar i").evaluate((i) => i.style.width), left ? "70%" : "30%");
        assert.deepEqual(errors, []);
      });
    }
  }
}

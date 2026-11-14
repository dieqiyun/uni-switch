// Run with Node's test runner and Playwright on the module path; see README.md.
// #659: an account whose allowance is counted in credits (WorkBuddy) says
// the credits beside the share — on the Routing page's stage and in its
// list of what each account did, and on the Usage page's card: how many
// are used when the share says used, how many are left when it says left.
// An account counted in shares alone says the share only. English and
// Chinese; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = Date.now();
const iso = (ms) => new Date(ms).toISOString();
const accts = [
  { id: "wb-1", provider: "workbuddy", name: "WorkBuddy", who: "李雷", kind: "account", model: "deepseek-v4.1-flash", known: true, used: 71, amount: 3550, limit: 5000, unit: "credits", plan: "Free" },
  { id: "wb-2", provider: "workbuddy", name: "WorkBuddy", who: "韩梅梅", kind: "account", model: "deepseek-v4.1-flash", known: true, used: 0, plan: "Free" },
];
const route = {
  id: 7, seq: 7, time: iso(now - 2000), agent: "pi", model: "deepseek-v4.1-flash", provider: "workbuddy", done: true, status: 200, ms: 900, tokens: 1500,
  order: accts, tries: [{ id: "wb-2", model: "deepseek-v4.1-flash", start: iso(now - 2000), done: true, ms: 800, status: 200 }],
};
const quotas = [
  { provider: "workbuddy", name: "WorkBuddy", icon: "workbuddy-color", plan: "Free", user: "李雷", windows: [{ name: "Credits", used: 71, display: "3550 / 5000", amount: 3550, limit: 5000, unit: "credits" }] },
  { provider: "codex", name: "Codex", icon: "openai", plan: "Plus", user: "me@example.com", windows: [{ name: "5 hours", used: 20 }] },
];

function serve(lang, left) {
  const settings = { lang, theme: "light", quotaLeft: left, currency: "usd" };
  const state = { agents: [{ id: "pi", name: "Pi", path: "/test/pi", fields: [] }], profiles: [], settings };
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) return new Promise(() => {});
      return json({ mine: true, now: new Date().toISOString(), seq: 7, totals: { requests: 1, rerouted: 0, errors: 0 }, routes: [route] });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ models: [], groups: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname === "/api/usage/quotas") return json(quotas);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await r.fulfill({ body: await fs.readFile(file), contentType }); } catch { await r.fulfill({ status: 404, body: "" }); }
  };
}

const words = {
  en: {
    used: { stage: "3,550 / 5,000 credits · 71% used", plain: "0% used", card: "3,550 / 5,000 credits · 71% used", codex: "20% used" },
    left: { stage: "1,450 / 5,000 credits · 29% left", plain: "100% left", card: "1,450 / 5,000 credits · 29% left", codex: "80% left" },
  },
  zh: {
    used: { stage: "3,550 / 5,000 积分 · 已用 71%", plain: "已用 0%", card: "3,550 / 5,000 积分 · 已用 71%", codex: "已用 20%" },
    left: { stage: "1,450 / 5,000 积分 · 剩余 29%", plain: "剩余 100%", card: "1,450 / 5,000 积分 · 剩余 29%", codex: "剩余 80%" },
  },
};
const squash = (s) => s.replace(/\s+/g, " ").trim();

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    for (const left of [false, true]) {
      const w = words[lang][left ? "left" : "used"];
      test(`${engine} ${lang}: WorkBuddy's credits ${left ? "left" : "used"} beside the share`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        let page;
        t.after(async () => {
          if (process.env.ARTIFACT_DIR && page) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${left ? "left" : "used"}-credits.png`) });
          }
          await browser.close();
        });
        page = await (await browser.newContext({ viewport: { width: 1100, height: 760 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, left));

        // the Routing page: the stage, and what each account did
        await page.goto("http://uni-switch.test/?view=routing");
        const stage = (who) => page.locator("li", { has: page.locator(".who", { hasText: who }) }).first();
        await stage("李雷").locator("em").filter({ hasText: w.stage }).waitFor();
        assert.equal(squash(await stage("李雷").locator("em").textContent()), w.stage);
        assert.equal(await stage("李雷").locator(".bar i").evaluate((i) => i.style.width), left ? "29%" : "71%");
        const act = page.locator(".rt-act", { hasText: "李雷" }).first();
        await act.waitFor();
        assert.ok(squash(await act.locator(".st").textContent()).startsWith(w.stage), await act.locator(".st").textContent());
        // an account counted in shares alone: the share only
        const plain = page.locator(".rt-act", { hasText: "韩梅梅" }).first();
        assert.ok(squash(await plain.locator(".st").textContent()).startsWith(w.plain), await plain.locator(".st").textContent());

        // the Usage page's card
        await page.goto("http://uni-switch.test/?view=usage");
        const card = page.locator(".subscription-card", { hasText: "WorkBuddy" });
        const n = card.locator(".quota-n").first();
        await n.waitFor();
        assert.equal(squash(await n.textContent()), w.card);
        assert.equal(squash(await page.locator(".subscription-card", { hasText: "Codex" }).locator(".quota-n").first().textContent()), w.codex);
        assert.deepEqual(errors, []);
      });
    }
  }
}

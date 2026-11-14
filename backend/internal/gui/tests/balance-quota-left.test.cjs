// Run with Node's test runner and Playwright on the module path; see README.md.
// A balance part asked as a percent reads as every window does (#3193, にとり):
// with Allowance display on Left its figure says what is left of it and its
// bar fills with that, on Used the share used; the nearly-full colour keeps
// to the used share either way; an amount without a percent (money) is as it
// was. The tray panel's balance card and the Usage page's balance row alike.
// No backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

const quotas = [
  // the reporter's: a custom provider whose balance path reads a daily
  // allowance as a percent used
  { provider: "custom", name: "Custom", icon: "generic", user: "me@example.com", balance: "Daily 8.9%",
    balanceParts: [{ label: "Daily", text: "8.9%", percent: 8.9 }], windows: [] },
  // credits beside windows, nearly full: the bar's colour stays keyed to
  // the used share
  { provider: "cc", name: "Command Code", icon: "generic", user: "cc@example.com", balance: "Credits 95%",
    balanceParts: [{ label: "Credits", text: "95%", percent: 95 }], windows: [{ name: "5-hour", used: 30 }] },
  // money: no percent, the switch leaves it as it is
  { provider: "money", name: "Money", icon: "generic", user: "m@example.com", balance: "$12.00",
    balanceParts: [{ label: "Credit", text: "$12.00" }], windows: [] },
];

function serve(lang, left) {
  const settings = { theme: "light", lang, tray: "panel", quotaLeft: left, currency: "usd" };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (body) => route.fulfill({ json: body });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") return json(quotas);
    if (url.pathname === "/api/usage/quotas/history") return json([]);
    if (url.pathname === "/api/usage") return json({ days: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

// said: the figure a part shows, en and zh, by the display and the share used
const said = {
  en: {
    true: { daily: "91.1% left", credits: "5% left" },
    false: { daily: "8.9%", credits: "95%" },
  },
  zh: {
    true: { daily: "剩余 91.1%", credits: "剩余 5%" },
    false: { daily: "8.9%", credits: "95%" },
  },
};

const width = {
  true: { daily: "91.1%", credits: "5%" },
  false: { daily: "8.9%", credits: "95%" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    for (const left of [true, false]) {
      test(`${engine} ${lang}: a balance part asked as a percent reads ${left ? "left" : "used"}`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const errors = [];
        const part = (page, card, word) => page.locator(card, { hasText: word }).locator(".bal-part", { hasText: word.split(" ")[0] }).first();
        const figure = (row) => row.locator("b").textContent();
        const fill = (row) => row.locator(".quota-track i").evaluate((i) => i.style.width);
        const nearlyFull = (row) => row.locator(".quota-track").evaluate((track) => track.classList.contains("full"));

        // the Usage page: every balance row
        const page = await (await browser.newContext({ viewport: { width: 900, height: 800 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, left));
        await page.goto("http://uni-switch.test/?view=usage&tab=usage");
        await page.waitForFunction(() => document.querySelectorAll("#subscriptionUsage > [data-key]").length >= 3);
        assert.equal(await figure(part(page, ".subscription-card", said[lang][left].daily)), said[lang][left].daily);
        assert.equal(await fill(part(page, ".subscription-card", said[lang][left].daily)), width[left].daily);
        assert.equal(await figure(part(page, ".subscription-card", said[lang][left].credits)), said[lang][left].credits);
        assert.equal(await fill(part(page, ".subscription-card", said[lang][left].credits)), width[left].credits);
        // nearly full by the used share, whichever way the meter reads
        assert.equal(await nearlyFull(part(page, ".subscription-card", said[lang][left].credits)), true);
        const money = page.locator(".subscription-card", { hasText: "Money" }).locator(".bal-part", { hasText: "Credit" });
        assert.equal(await figure(money), "$12.00");
        assert.equal(await money.locator(".quota-track").count(), 0, "money takes no meter");

        // the tray panel's Allowances tab: the same words
        const context = await browser.newContext({ viewport: { width: 380, height: 640 }, reducedMotion: "reduce" });
        await context.addInitScript(() => { try { localStorage.setItem("uni-switch.panelTab", "usage"); } catch {} });
        const panel = await context.newPage();
        panel.setDefaultTimeout(5000);
        panel.setDefaultNavigationTimeout(60000);
        panel.on("pageerror", (e) => errors.push(e.message));
        await panel.route("**/*", serve(lang, left));
        await panel.goto("http://uni-switch.test/?mode=panel");
        const daily = panel.locator(".pq-card.bal", { hasText: "Custom" });
        await daily.waitFor();
        assert.equal(await figure(daily.locator(".pq-lead")), said[lang][left].daily);
        assert.equal(await fill(daily), width[left].daily);
        // the panel's card shows the account, its name only in the tooltip
        const credits = panel.locator(".pq-card", { hasText: "cc@example.com" }).locator(".quota-balance .bal-part", { hasText: "Credits" });
        await credits.waitFor();
        assert.equal(await figure(credits), said[lang][left].credits);
        assert.equal(await fill(credits), width[left].credits);
        const cash = panel.locator(".pq-card.bal", { hasText: "Money" });
        assert.equal(await cash.locator(".pq-lead b").textContent(), "$12.00");
        assert.equal(await cash.locator(".quota-track").count(), 0, "money takes no meter");
        assert.deepEqual(errors, []);
        await context.close();
      });
    }
  }
}

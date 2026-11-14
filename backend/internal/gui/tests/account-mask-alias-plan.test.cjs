// Run with Node's test runner and Playwright on the module path; see README.md.
// Hide accounts hides accounts, not the labels around them (#3656,
// faketrader): the name a user gave an account (#1515) stayed blurred,
// so several accounts couldn't be told apart, and an Antigravity account
// whose value is "Google" blurred "Google" of its plan "Google AI Pro" on
// Usage and Providers. Now the user's own name stays in sight while the
// account beside it is hidden; a plugin's name for an account (a masked
// phone, #4416) is the account's and is hidden; a plan, a model and a
// card's title hide only an address, so one written in a plan still is;
// the account "Google" is hidden in its own rows, sentences and tooltips,
// in the window and the tray panel. Every language, wide and narrow.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const RAW = "raw@example.com", ALIAS = "Work laptop", IAM = "iam::42:user:ops", PHONE = "138****5678", BOSS = "boss@corp.example";
const PLAN = "Google AI Pro";
const base = { icon: "generic", chat: "", responses: "", anthropic: "", catalog: "", agents: [], fallback: [], headers: {}, keyList: [], proxy: "", models: [{ id: "gemini-3-pro", name: "", on: true }] };
const providers = () => ({ providers: [
  { ...base, id: "antigravity", name: "Antigravity", icon: "antigravity-color",
    account: { agent: "antigravity", agentName: "Antigravity", user: "Google", plan: PLAN,
      logins: [{ user: "Google", plan: PLAN, active: true, on: true }, { user: RAW, plan: PLAN, on: true }] },
    accountNames: { [RAW]: ALIAS } },
  { ...base, id: "fakeco", name: "FakeCo", account: { agent: "fakeco", agentName: "FakeCo", user: IAM, plan: "Team", logins: [{ user: IAM, plan: "Team", active: true, on: true }] },
    accountUsers: { [IAM]: PHONE } },
], presets: [], excluded: [], gateway: { running: true, window: true } });
const quotas = () => [
  { provider: "antigravity", name: "Antigravity", icon: "antigravity-color", plan: PLAN, user: "Google", windows: [{ name: "Gemini", used: 5 }] },
  { provider: "antigravity", name: "Antigravity", icon: "antigravity-color", plan: PLAN, user: RAW, alias: ALIAS, named: true, windows: [{ name: "Gemini", used: 30 }] },
  // the plugin's name for its account: the account's, not the user's own
  { provider: "fakeco", name: "FakeCo", plan: "Team of " + BOSS, user: IAM, alias: PHONE, windows: [{ name: "Credits", used: 10 }] },
];

function serve(lang, panel) {
  const settings = { theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd" };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:${!panel}};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json(providers());
    if (url.pathname === "/api/usage/quotas") return json(quotas());
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/login/usage") return json({});
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

// what of an element is in sight: its text and every tooltip in it
const seen = (loc) => loc.evaluate((e) => [e.textContent, ...[e, ...e.querySelectorAll("[title]")].map((x) => x.title || "")].join(" | "));
const raws = (loc) => loc.evaluate((e) => [...e.querySelectorAll(".pii")].map((p) => p.dataset.raw));

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [1100, 420]) {
      test(`${engine} ${lang} ${width}: Hide accounts hides the accounts, not their names, plans or models`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const context = await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" });
        const errors = [];
        const open = async (url) => {
          const page = await context.newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", serve(lang, url.includes("mode=panel")));
          await page.goto(url);
          return page;
        };

        // Usage
        const page = await open("http://uni-switch.test/?view=usage");
        await page.locator(".subscription-account .quota-alias").first().waitFor();
        await page.evaluate(() => {
          const d = document.createElement("div");
          d.id = "sample";
          d.textContent = "Google answered · Work laptop waited · gemini-3-pro";
          d.title = "Google · raw@example.com";
          document.querySelector("#view-usage").append(d);
          const m = document.createElement("span");
          m.id = "model";
          m.className = "model-value";
          m.textContent = "Google Gemini via Google";
          document.querySelector("#view-usage").append(m);
        });
        await page.evaluate(() => hideAccounts.set(true, true));
        await page.waitForFunction(() => document.querySelector("#sample .pii"));
        const acct = (u) => page.locator(".subscription-account", { has: page.locator(`.pii[data-raw="${u}"]`) }).first();
        // the user's own name, in sight; the account beside it, hidden
        const mine = acct(RAW);
        assert.equal(await mine.locator(".quota-alias").textContent(), ALIAS);
        assert.deepEqual(await raws(mine.locator(".quota-alias")), []);
        assert.ok(!(await seen(mine)).includes(RAW), "the address is hidden, its tooltips too: " + await seen(mine));
        // the account named Google: hidden in its row, its plan in sight
        const google = acct("Google");
        assert.equal(await google.locator(".plan").textContent(), PLAN);
        assert.equal(await mine.locator(".plan").textContent(), PLAN);
        // the plugin's name for an account is the account
        const fake = acct(PHONE);
        assert.ok(!(await seen(fake)).includes(PHONE), "the plugin's name is hidden: " + await seen(fake));
        // an address in a plan is still hidden
        assert.deepEqual(await raws(fake.locator(".plan")), [BOSS]);
        assert.ok(!(await fake.locator(".plan").textContent()).includes(BOSS));
        // elsewhere: the account in a sentence and a tooltip, never the name
        assert.deepEqual(await raws(page.locator("#sample")), ["Google"]);
        assert.match(await page.locator("#sample").textContent(), /^\S+ answered · Work laptop waited · gemini-3-pro$/);
        assert.equal(await page.locator("#sample").getAttribute("title"), "•••••• · •••@•••••••.•••");
        // a model's name is a label
        assert.equal(await page.locator("#model").textContent(), "Google Gemini via Google");
        assert.ok(!(await page.locator("#view-usage").textContent()).includes(RAW));
        await page.evaluate(() => hideAccounts.set(false, true));
        await page.waitForFunction(() => !document.querySelector("#view-usage .pii"));
        assert.equal(await page.locator("#sample").textContent(), "Google answered · Work laptop waited · gemini-3-pro");
        await page.evaluate(() => hideAccounts.set(true, true));

        // Providers: the row's plan in sight, its account hidden
        await page.evaluate(() => show("providers"));
        const row = page.locator(".row.provider", { hasText: "Antigravity" }).first();
        await row.waitFor();
        await page.waitForFunction(() => document.querySelector("#view-providers .pii"));
        assert.equal(await row.locator(".acct-plan").textContent(), PLAN);
        assert.deepEqual(await raws(row.locator(".sub")), ["Google"]);
        await row.click();
        const login = (id) => page.locator(`.editor .accts .acc[data-account-id="${id}"]`);
        await login(RAW).locator(".acct-who").waitFor();
        await page.waitForFunction((raw) => document.querySelector(`.editor .acc[data-account-id="${raw}"] .acct-user .pii`), RAW);
        // the user's own name in sight, the address and its tooltip hidden
        assert.equal(await login(RAW).locator(".rename").textContent(), ALIAS);
        assert.ok(!(await seen(login(RAW))).includes(RAW), "the address is hidden in the row: " + await seen(login(RAW)));
        // an account with no name of the user's is shown by its own: hidden
        assert.deepEqual(await raws(login("Google").locator(".rename")), ["Google"]);
        for (const id of ["Google", RAW]) assert.equal(await login(id).locator(".plan").textContent(), PLAN);

        // the tray panel follows the window's switch
        const panel = await open("http://uni-switch.test/?mode=panel");
        await panel.evaluate(() => setPanelTab("usage"));
        await panel.locator("#panelQuota .pq-user").first().waitFor();
        const more = panel.locator('#panelQuota .pq-more:not(.pq-arrange)[aria-expanded="false"]');
        for (let i = 0; i < 4 && await more.count(); i++) await more.first().click();
        await panel.waitForFunction(() => document.querySelectorAll("#panelQuota .pq-card").length === 3);
        await panel.evaluate(() => hideAccounts.set(true));
        await panel.waitForFunction(() => document.querySelector("#panelQuota .pq-user .pii"));
        const pq = await panel.evaluate(() => [...document.querySelectorAll("#panelQuota .pq-card")].map((c) => ({
          user: c.querySelector(".pq-user").textContent, raw: c.querySelector(".pq-user .pii")?.dataset.raw ?? null, title: c.title })));
        const own = pq.find((c) => c.user === ALIAS);
        assert.ok(own, "the user's own name is in sight: " + JSON.stringify(pq));
        assert.ok(!own.title.includes(RAW), "its tooltip hides the address: " + own.title);
        assert.deepEqual(pq.map((c) => c.raw).filter(Boolean).sort(), ["138****5678", "Google"].sort());
        assert.ok(pq.every((c) => !c.title.includes(PHONE) && !c.title.includes(RAW) && !c.title.includes(BOSS)), JSON.stringify(pq));

        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth), 0, `no sideways scroll at ${width}px`);
        assert.deepEqual(errors, []);
        await context.close();
      });
    }
  }
}

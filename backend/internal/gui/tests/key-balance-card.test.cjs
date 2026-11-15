// Run with Node's test runner and Playwright on the module path; see README.md.
// A key told both its plan's windows and its balance (Command Code's) is one
// card, the balance a row under the windows, on the tray panel's Allowances
// tab and the Usage page alike (#1658, MizuIro-H: "COMMAND CODE" with its
// 5-hour and weekly rings, and under Balances a second "Command Code",
// $63.78). Two keys' plans each take their own key's balance; a balance
// that failed or is a stale stand-in stays a card of its own, which says
// so. A menu bar cell for the balance still finds the card. APIs are mocked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const soon = new Date(Date.now() + 2 * 3600e3).toISOString();
// a reset more than a week out is written as a date, and Sept. 28 is about
// the widest one ("28. Sept. 02:37" in German), so the narrow-window checks
// below see the longest line every day of the year, not only on days when
// three days ahead falls in next week (#2443)
const week = (() => {
  const y = new Date().getFullYear(), at = (y) => new Date(y, 8, 28, 2, 37);
  return (at(y) - Date.now() > 8 * 86400e3 ? at(y) : at(y + 1)).toISOString();
})();

// the reporter's (uni-switch quota's lines): one Command Code key with a plan
// and credits, and other providers' balances
const reporter = {
  quotas: [
    { provider: "codex", name: "Codex", icon: "codex-color", user: "x@y.z", plan: "plus", windows: [{ name: "5 hours", used: 25, resetsAt: soon }, { name: "7 days", used: 4, resetsAt: week }] },
    { provider: "commandcode", name: "Command Code", icon: "commandcode", windows: [{ name: "5 hours", used: 1, resetsAt: soon }, { name: "Weekly", used: 6, resetsAt: week }] },
    { provider: "sudocode", name: "SudoCode", icon: "generic", user: "sk-su…a1b2", windows: [], balance: "¥9.85" },
    { provider: "deepseek", name: "DeepSeek", icon: "deepseek-color", user: "sk-de…c3d4", windows: [], balance: "¥14.64" },
    { provider: "commandcode", name: "Command Code", icon: "commandcode", user: "user_…9f3a", windows: [], balance: "$63.78" },
  ],
  // the cards each place holds, by provider, and the Command Code card's balance
  panel: [["codex"], ["commandcode"], ["sudocode", "deepseek"]],
  usage: ["codex", "commandcode", "sudocode", "deepseek"],
  balances: { commandcode: ["$63.78"] },
  focus: "commandcode|user_…9f3a",
};
// two keys, each its plan and balance; a third provider's plan whose
// balance failed, and a fourth's standing in for a failed read: those two
// balances stay their own
const keys = {
  quotas: [
    { provider: "commandcode", name: "Command Code", icon: "commandcode", user: "Work", windows: [{ name: "5 hours", used: 10, resetsAt: soon }] },
    { provider: "commandcode", name: "Command Code", icon: "commandcode", user: "Home", windows: [{ name: "5 hours", used: 50, resetsAt: soon }] },
    { provider: "commandcode", name: "Command Code", icon: "commandcode", user: "Home", windows: [], balance: "$2.00" },
    { provider: "commandcode", name: "Command Code", icon: "commandcode", user: "Work", windows: [], balance: "$40.00" },
    { provider: "minimax", name: "MiniMax", icon: "minimax-color", windows: [{ name: "5 hours", used: 30, resetsAt: soon }] },
    { provider: "minimax", name: "MiniMax", icon: "minimax-color", user: "sk-cp…77", windows: [], error: "minimax: 503" },
    { provider: "kimi-code", name: "Kimi Code", icon: "kimi", windows: [{ name: "Weekly", used: 30, resetsAt: week }] },
    { provider: "kimi-code", name: "Kimi Code", icon: "kimi", user: "sk-ki…88", windows: [], balance: "¥3.00", asOf: new Date(Date.now() - 3600e3).toISOString() },
  ],
  panel: null, // the panel shows one key of several in full; checked by the Usage page
  // the failed and the stale balance: each its own card beside the plan, as before
  usage: ["commandcode", "minimax", "minimax", "kimi-code", "kimi-code"],
  balances: { commandcode: ["$40.00", "$2.00"] },
  apart: ["minimax|sk-cp…77", "kimi-code|sk-ki…88"],
  focus: "commandcode|Home",
};

function serve(lang, c) {
  const settings = { theme: "light", lang, tray: "panel", quotaLeft: true, currency: "usd", usageOrder: [], panelUsageHidden: [] };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") return json(c.quotas);
    if (url.pathname === "/api/usage") return json({ days: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

// what runs off sideways: the page, or a card past the window's edge
const sideways = () => {
  const out = [];
  if (document.scrollingElement.scrollWidth > innerWidth) out.push("page " + document.scrollingElement.scrollWidth);
  for (const box of ["#panelQuota", "#subscriptionUsage"]) {
    const v = document.querySelector(box)?.closest(".view");
    if (v && v.offsetParent && v.scrollWidth > v.clientWidth) out.push(v.id + " " + v.scrollWidth + ">" + v.clientWidth);
  }
  for (const c of document.querySelectorAll("#panelQuota .pq-card, #subscriptionUsage > [data-key]")) {
    const r = c.getBoundingClientRect();
    if (r.width && r.right > innerWidth + 0.5) out.push((c.dataset.card || c.dataset.key) + " " + Math.round(r.right));
  }
  return out;
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const [name, c] of [["the reporter's key", reporter], ["two keys, a failed and a stale balance", keys]]) {
      test(`${engine} ${lang}: a key's balance is on its plan's card, ${name}`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(async () => { await browser.close(); });
        const errors = [];
        const context = await browser.newContext({ viewport: { width: 380, height: 640 }, reducedMotion: "reduce" });
        await context.addInitScript(() => { try { localStorage.setItem("uni-switch.panelTab", "usage"); } catch {} });
        const balanceWord = async (p) => p.evaluate(() => t("Balance"));

        if (c.panel) {
          const page = await context.newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", serve(lang, c));
          await page.goto("http://uni-switch.test/?mode=panel");
          await page.waitForFunction((n) => document.querySelectorAll("#panelQuota .pq-card[data-card]").length >= n, c.panel.flat().length);
          const groups = await page.evaluate(() => [...document.querySelectorAll("#panelQuota > .pq-group")]
            .map((g) => [...new Set([...g.querySelectorAll(".pq-card[data-card]")].map((x) => x.dataset.card.split("|")[0]))]));
          assert.deepEqual(groups, c.panel, "one Command Code card, not a second under Balances");
          // the plan's card: its rings, and the balance under them
          const cc = await page.evaluate(() => {
            const card = document.querySelector('#panelQuota .pq-card[data-card="commandcode"]');
            return { rings: card.querySelectorAll(".pq-ring").length, bal: card.querySelector(".quota-balance")?.textContent, of: card.dataset.balCard };
          });
          assert.equal(cc.rings, 2);
          assert.equal(cc.bal, (await balanceWord(page)) + "$63.78");
          assert.equal(cc.of, c.focus);
          assert.equal(await page.evaluate(() => [...document.querySelectorAll("#panelQuota .pq-gn")].filter((x) => /command code/i.test(x.textContent)).length), 1, "Command Code is named once");
          // a menu bar cell for the balance opens the plan's card. Watched
          // as it happens, not polled for: under reduced motion the flash
          // lasts .01ms, its class gone at the next frame, and a poll on a
          // busy WebKit missed it (a timeout, most runs at --test-concurrency=4)
          await page.evaluate(() => {
            window.flashed = false;
            new MutationObserver(() => {
              if (document.querySelector('#panelQuota .pq-card[data-card="commandcode"]')?.classList.contains("flash")) window.flashed = true;
            }).observe(document.querySelector("#panelQuota"), { subtree: true, attributes: true, attributeFilter: ["class"] });
          });
          await page.evaluate((id) => panelQuotaFocus(id), c.focus);
          await page.waitForFunction(() => window.flashed);
          assert.deepEqual(await page.evaluate(sideways), [], "nothing runs off sideways at 380px");
          await page.close();
        }

        // the window, at its narrowest (windowMinW)
        const winContext = await browser.newContext({ viewport: { width: 560, height: 640 }, reducedMotion: "reduce" });
        const win = await winContext.newPage();
        win.setDefaultTimeout(5000);
        win.setDefaultNavigationTimeout(60000);
        win.on("pageerror", (e) => errors.push(e.message));
        await win.route("**/*", serve(lang, c));
        await win.goto("http://uni-switch.test/?view=usage&tab=usage");
        await win.waitForFunction((n) => document.querySelectorAll("#subscriptionUsage > [data-key]").length >= n, c.usage.length);
        assert.deepEqual(await win.evaluate(() => [...document.querySelectorAll("#subscriptionUsage > [data-key]")].map((x) => x.dataset.key)), c.usage, "the balance on its plan's card, not a card of its own");
        for (const [provider, want] of Object.entries(c.balances)) {
          const got = await win.evaluate((p) => [...document.querySelectorAll(`#subscriptionUsage > [data-key="${p}"] .quota-balance b`)].map((b) => b.textContent), provider);
          assert.deepEqual(got, want, "each plan's own key's balance, under its windows");
        }
        // a balance that failed or stands in for one keeps its own row, saying so
        for (const id of c.apart || []) {
          assert(await win.evaluate((id) => !!document.querySelector(`#subscriptionUsage [data-card="${CSS.escape(id)}"]`), id), id + " stays its own row");
        }
        if (c.apart) assert.equal(await win.evaluate(() => document.querySelectorAll('#subscriptionUsage > [data-card="kimi-code"] .quota-balance, #subscriptionUsage > [data-card="kimi-code"][data-bal-card]').length), 0, "a stale balance isn't put under the windows");
        assert(await win.evaluate((id) => !!document.querySelector(`#subscriptionUsage [data-bal-card="${CSS.escape(id)}"]`), c.focus), "the balance's id is on the card it moved to");
        assert.deepEqual(await win.evaluate(sideways), [], "nothing runs off sideways at 560px");
        assert.deepEqual(errors, []);
      });
    }
  }
}

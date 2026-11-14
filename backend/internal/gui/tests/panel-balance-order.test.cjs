// Run with Node's test runner and Playwright on the module path; see README.md.
// The tray panel's Allowances tab lists its cards in the order the Usage page
// shares (#1510, JokerQyou): a balance-only card (DeepSeek) stands where that
// order puts it, not gathered after every plan. Balances next to each other
// share one grid. The Usage page lists the same order. APIs are mocked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

// the reporter's: Codex, DeepSeek, Copilot shown; Gemini CLI and Kimi hidden
// (#1510's screenshots), in the order uni-switch itself lists them
const reporter = {
  quotas: [
    { provider: "codex", name: "Codex", icon: "codex-color", user: "x@y.z", plan: "plus", windows: [{ name: "7d", used: 71 }] },
    { provider: "copilot", name: "Copilot", icon: "githubcopilot", user: "gh", plan: "Free", windows: [{ name: "Chat requests", used: 0 }, { name: "Completions", used: 0 }] },
    { provider: "deepseek", name: "DeepSeek", icon: "deepseek-color", windows: [], balance: "¥121.61 · $0.00" },
    { provider: "gemini-cli", name: "Gemini CLI", icon: "gemini-color", error: "can't read the quota now" },
    { provider: "kimi-code", name: "Kimi For Coding", icon: "kimi-color", windows: [{ name: "Weekly", used: 30 }] },
  ],
  settings: { usageOrder: ["codex", "deepseek", "copilot", "gemini-cli", "kimi-code"], panelUsageHidden: ["gemini-cli", "kimi-code"] },
  panel: [["codex"], ["deepseek"], ["copilot"]],
  usage: ["codex", "deepseek", "copilot", "gemini-cli", "kimi-code"],
};
// a balance first, two balances side by side, a plan between and after
const mixed = {
  quotas: [
    { provider: "codex", name: "Codex", icon: "codex-color", user: "x@y.z", windows: [{ name: "Weekly", used: 25 }] },
    { provider: "claude", name: "Claude Code", icon: "claude-color", user: "a@b.c", windows: [{ name: "5-hour", used: 40 }] },
    { provider: "deepseek", name: "DeepSeek", icon: "deepseek-color", windows: [], balance: "¥12.30" },
    { provider: "moonshot", name: "Moonshot", icon: "moonshot", windows: [], balance: "¥5.00" },
    { provider: "siliconflow", name: "SiliconFlow", icon: "siliconcloud-color", windows: [], balance: "¥1.00" },
  ],
  settings: { usageOrder: ["moonshot", "codex", "deepseek", "siliconflow", "claude"], panelUsageHidden: [] },
  panel: [["moonshot"], ["codex"], ["deepseek", "siliconflow"], ["claude"]],
  usage: ["moonshot", "codex", "deepseek", "siliconflow", "claude"],
};

function serve(lang, c) {
  const settings = { theme: "light", lang, tray: "panel", quotaLeft: true, currency: "usd", ...c.settings };
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

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "ja", "de"]) {
    for (const [name, c] of [["the reporter's order", reporter], ["balances first and side by side", mixed]]) {
      test(`${engine} ${lang}: the panel's Allowances tab keeps the Usage page's order, ${name}`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(async () => { await browser.close(); });
        const errors = [];
        const context = await browser.newContext({ viewport: { width: 380, height: 640 }, reducedMotion: "reduce" });
        await context.addInitScript(() => { try { localStorage.setItem("uni-switch.panelTab", "usage"); } catch {} });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, c));
        await page.goto("http://uni-switch.test/?mode=panel");
        const want = c.panel.flat().length;
        await page.waitForFunction((n) => document.querySelectorAll("#panelQuota .pq-card[data-card]").length >= n, want);

        // each group in turn, the providers of its cards
        const groups = await page.evaluate(() => [...document.querySelectorAll("#panelQuota > .pq-group")]
          .map((g) => [...new Set([...g.querySelectorAll(".pq-card[data-card]")].map((x) => x.dataset.card.split("|")[0]))]));
        assert.deepEqual(groups, c.panel, "the panel's cards follow the Usage page's order");
        // a balance's group is still headed as balances, in the reader's language
        const balances = await page.evaluate(() => t("Balances"));
        for (const g of await page.evaluate(() => [...document.querySelectorAll("#panelQuota > .pq-group")]
          .filter((g) => g.querySelector(".pq-card.bal")).map((g) => [g.querySelector(".pq-gn")?.textContent, g.querySelectorAll(".pq-card:not(.bal)").length]))) {
          assert.deepEqual(g, [balances, 0], "a balances group holds balances only, under its heading");
        }
        // nothing runs off sideways at a narrow panel
        assert(await page.evaluate(() => {
          const v = document.querySelector("#view-agents");
          return v.scrollWidth <= v.clientWidth && document.scrollingElement.scrollWidth <= innerWidth
            && [...document.querySelectorAll("#panelQuota .pq-card")].every((x) => x.getBoundingClientRect().right <= innerWidth);
        }), "nothing runs off sideways at 380px");

        // the Usage page: the same order
        const win = await context.newPage();
        win.on("pageerror", (e) => errors.push(e.message));
        await win.route("**/*", serve(lang, c));
        await win.goto("http://uni-switch.test/?view=usage&tab=usage");
        await win.waitForFunction((n) => document.querySelectorAll("#subscriptionUsage > [data-key]").length >= n, c.usage.length);
        assert.deepEqual(await win.evaluate(() => [...new Set([...document.querySelectorAll("#subscriptionUsage > [data-key]")].map((x) => x.dataset.key))]), c.usage);
        assert.deepEqual(errors, []);
      });
    }
  }
}

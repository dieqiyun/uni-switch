// Run with Node's test runner and Playwright on the module path; see README.md.
// Gemini 4 Argon has a quota group of its own beside Gemini's and Claude &
// GPT's, with only a week (agy 1.4.0's /usage: "Gemini 4 Argon", bucket
// gemini-argon-wtu-weekly; CodexBar #4585). 01huadalang on Discord: 反重力
// 有 Gemini 4 的条子是7天…有 Gemini 4 的条子怎么 claude 的条子没有了. The
// account rows showed two pools and the panel four rings, Gemini's two
// pools first, so Claude & GPT's went out. Now every pool shows, Claude &
// GPT before the model's own pool, which reads "Gemini 4" on its ring, not
// a second "Gemini". English and Chinese, Chromium and WebKit; the API is
// faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const at = (h) => new Date(Date.now() + h * 36e5).toISOString();
const model = (name, family, pool, used, h) => ({ name, family, pool, used, resetsAt: at(h) });
const pool = (pool, name, used, h) => ({ name, pool, used, resetsAt: at(h) });
// as uni-switch reports it: the models, then each group's windows as Google
// lists the groups, Argon's last with its week alone
const ag = (user) => ({
  provider: "antigravity", name: "Antigravity", icon: "antigravity-color", user, plan: "Google AI Pro",
  windows: [
    model("Claude Sonnet 4.6", "Claude", "Claude & GPT", 30, 3),
    model("Gemini 3 Flash", "Gemini", "Gemini", 0, 2),
    model("Gemini 4 Argon", "Gemini", "Gemini 4 Argon", 47, 150),
    pool("Gemini", "7 days", 1, 100), pool("Gemini", "5 hours", 0, 2),
    pool("Claude & GPT", "7 days", 40, 90), pool("Claude & GPT", "5 hours", 30, 3),
    pool("Gemini 4 Argon", "7 days", 47, 150),
  ],
});
const quotas = [ag("ada@example.com")];
const logins = [{ user: "ada@example.com", plan: "Google AI Pro", active: true, on: true }];
const antigravity = {
  id: "antigravity", name: "Antigravity", icon: "antigravity-color", chat: "", responses: "", anthropic: "", catalog: "",
  models: [{ id: "gemini-3-flash", name: "Gemini 3 Flash", on: true }], agents: [], fallback: [], headers: {}, keyList: [],
  account: { agent: "antigravity", agentName: "Antigravity", user: logins[0].user, plan: logins[0].plan, logins },
};

function serve(lang, panel) {
  const settings = { theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd" };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:${!panel}};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json({ providers: [antigravity], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") return json(quotas);
    if (url.pathname === "/api/login/usage") return json(Object.fromEntries(quotas.map((q) => [q.user, q])));
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const words = {
  en: { five: "5 hours", week: "7 days" },
  zh: { five: "5 小时", week: "7 天" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    const w = words[lang];
    test(`${engine} ${lang}: a model's own quota group shows beside Gemini's and Claude's`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const errors = [];
      const open = async (url, viewport) => {
        const page = await (await browser.newContext({ viewport, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, url.includes("mode=panel")));
        await page.goto(url);
        return page;
      };
      const fill = (loc) => loc.evaluateAll((es) => es.map((e) => Math.round(parseFloat(e.style.width))));

      // the Usage page: every group's windows, Claude & GPT before Argon
      const page = await open("http://uni-switch.test/?view=usage", { width: 900, height: 700 });
      const box = page.locator(".subscription-card", { hasText: "Antigravity" }).locator(".quota-windows").first();
      await box.waitFor();
      assert.deepEqual(await box.locator(".quota-labels > span:first-child").allTextContents(),
        [`Gemini · ${w.five}`, `Gemini · ${w.week}`, `Claude & GPT · ${w.five}`, `Claude & GPT · ${w.week}`, `Gemini 4 Argon · ${w.week}`]);
      const tip = await box.locator(".quota").nth(1).getAttribute("title");
      assert(tip.includes("Gemini 3 Flash") && !tip.includes("Gemini 4 Argon"), "Gemini's pool is the other Gemini models': " + tip);

      // the menu bar panel: five rings, Claude's among them, Argon's named for it
      const panel = await open("http://uni-switch.test/?mode=panel", { width: 440, height: 640 });
      await panel.locator('#ptabs [data-ptab="usage"]').click();
      const card = panel.locator(".pq-group", { hasText: "Antigravity" }).locator(".pq-card").first();
      await card.locator(".pq-ring").first().waitFor();
      assert.deepEqual(await card.locator(".pq-rn").evaluateAll((es) => es.map((e) => [...e.children].map((c) => c.textContent).join(" "))),
        ["Gemini 5h", "Gemini 7d", "Claude 5h", "Claude 7d", "Gemini 4 7d"]);
      assert.deepEqual(await card.locator(".pq-dial b").allTextContents(), ["0%", "1%", "30%", "40%", "47%"]);
      assert(await card.locator(".pq-rn > span").evaluateAll((es) => es.every((e) => e.scrollWidth <= e.clientWidth)), "the ring labels fit");
      const fits = await card.evaluate((c) => { const r = c.getBoundingClientRect(); return [...c.querySelectorAll(".pq-ring")].every((x) => x.getBoundingClientRect().right <= r.right + 0.5); });
      assert(fits, "the five rings fit the card");
      assert(await card.locator(".pq-user").evaluate((e) => e.getBoundingClientRect().width >= 80), "the account's name keeps room");

      // the provider's account row: every pool, Claude & GPT's included
      const prov = await open("http://uni-switch.test/?view=providers", { width: 900, height: 700 });
      await prov.locator(".row.provider", { hasText: "Antigravity" }).click();
      const row = prov.locator(".accts .acc", { hasText: "ada@example.com" });
      await row.locator(".aq-w").first().waitFor();
      assert.deepEqual(await row.locator(".aq-n").allTextContents(), ["Gemini", "Claude & GPT", "Gemini 4 Argon"]);
      assert.deepEqual(await row.locator(".aq-k").allTextContents(), ["5h", "7d", "5h", "7d", "7d"]);
      assert.deepEqual(await fill(row.locator(".aq-track i")), [0, 1, 30, 40, 47]);
      assert.deepEqual(errors, []);
    });
  }
}

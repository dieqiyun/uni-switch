// Run with Node's test runner and Playwright on the module path; see README.md.
// WorkBuddy's credits a day (#568): under the account's Credits meter, the
// Usage page says how many credits it used in the period, and, over 7 or
// 30 days, a bar a day, each day's figure in its tooltip; a day before
// uni-switch began counting is an empty slot saying so, not a zero, and the
// card says since when it counts. A card with no days counted has none of
// it. English and Chinese; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const dayKey = (back) => {
  const d = new Date();
  d.setHours(12, 0, 0, 0);
  d.setDate(d.getDate() - back);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
};
const quotas = () => [
  { provider: "workbuddy", name: "WorkBuddy", icon: "workbuddy-color", plan: "Free", user: "李雷", windows: [{ name: "Credits", used: 8.9, display: "500.52 / 5600" }],
    daily: { since: dayKey(4), days: [{ day: dayKey(4), used: 30.25 }, { day: dayKey(2), used: 60 }, { day: dayKey(0), used: 12.5 }] } },
  { provider: "codex", name: "Codex", icon: "openai", plan: "Plus", windows: [{ name: "5 hours", used: 20 }] },
];

function serve(lang) {
  const settings = { theme: "light", lang, quotaLeft: false, currency: "usd" };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") return json(quotas());
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const words = {
  en: { head: "Credits used per day", today: "12.5 today", week: "102.75 in 7 days", day7: "7 days", day1: "Today", bar: /: 60 credits$/, unknown: /: not counted — uni-switch began counting on /, since: /^Counted since / },
  zh: { head: "每天用掉的积分", today: "今天 12.5", week: "7 天共 102.75", day7: "7 天", day1: "今天", bar: /：60 积分$/, unknown: /：未统计，uni-switch 从 .+ 开始统计$/, since: /^从 .+ 开始统计$/ },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    const w = words[lang];
    test(`${engine} ${lang}: WorkBuddy's credits a day`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-credit-days.png`) });
        }
        await browser.close();
      });
      const page = await (await browser.newContext({ viewport: { width: 900, height: 760 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      await page.goto("http://uni-switch.test/?view=usage");
      const card = page.locator(".subscription-card", { hasText: "WorkBuddy" });
      await page.locator("#period .opt", { hasText: new RegExp("^" + w.day1 + "$") }).click();
      const box = card.locator(".credit-days");
      await box.waitFor();
      assert.equal(await box.locator(".cd-head span").textContent(), w.head);
      assert.equal(await box.locator(".cd-head b").textContent(), w.today);
      assert.equal(await box.locator(".cd-day").count(), 0, "one day: its figure, no bars");
      assert.equal(await page.locator(".subscription-card", { hasText: "Codex" }).locator(".credit-days").count(), 0);

      // 7 days: a bar a day, oldest first; the days before counting began
      // empty, and the card saying since when
      const y = await page.evaluate(() => document.querySelector("#view-usage").scrollTop);
      await page.locator("#period .opt", { hasText: new RegExp("^" + w.day7 + "$") }).click();
      await page.waitForFunction(() => document.querySelectorAll(".credit-days .cd-day").length === 7);
      assert.equal(await page.evaluate(() => document.querySelector("#view-usage").scrollTop), y, "a click doesn't scroll");
      assert.equal(await box.locator(".cd-head b").textContent(), w.week);
      const days = await box.locator(".cd-day").evaluateAll((ds) => ds.map((d) => ({ unknown: d.classList.contains("unknown"), h: parseFloat(d.querySelector("i").style.height), title: d.title })));
      assert.deepEqual(days.map((d) => d.unknown), [true, true, false, false, false, false, false]);
      assert.deepEqual(days.map((d) => d.h), [0, 0, 50.4, 0, 100, 0, 20.8]);
      assert.match(days[4].title, w.bar);
      assert.match(days[0].title, w.unknown);
      assert.match(await box.locator(".cd-note").textContent(), w.since);
      // no coloured stripe down a side
      const sides = await box.evaluate((b) => { const s = getComputedStyle(b); return [s.borderLeftWidth, s.borderRightWidth]; });
      assert.deepEqual(sides, ["0px", "0px"]);
      assert.deepEqual(errors, []);
    });
  }
}

// Every credit-based provider (#3321), not WorkBuddy alone: a plugin's card
// (Trae CN) gets the same bars; a card with two pools (Qoder's Credits and
// Add-on credits) shows each on its own, named; a meter in money (Cursor's
// On-demand) is spent, in its currency, not credits. Every language, at a
// phone's width.
const pools = () => [
  { provider: "trae-cn", name: "Trae CN", plan: "Pro", user: "trae@x", windows: [{ name: "Credits", used: 10, amount: 100, limit: 1000, unit: "credits" }],
    daily: { since: dayKey(2), days: [{ day: dayKey(0), used: 7 }], window: "Credits" } },
  { provider: "qoder", name: "Qoder", plan: "Pro", user: "q@x", windows: [{ name: "Credits", used: 10, display: "300 / 3000 credits" }, { name: "Add-on credits", used: 50, display: "50 / 100 credits" }],
    daily: { since: dayKey(2), days: [{ day: dayKey(0), used: 4 }], window: "Credits" },
    moreDaily: [{ since: dayKey(2), days: [{ day: dayKey(0), used: 9 }], window: "Add-on credits" }] },
  { provider: "cursor", name: "Cursor", plan: "Pro", user: "c@x", windows: [{ name: "On-demand", used: 25, display: "$5 / $20" }],
    daily: { since: dayKey(2), days: [{ day: dayKey(0), used: 2.5 }], window: "On-demand", unit: "usd" } },
];
const poolWords = {
  en: { head: "Credits used per day", spent: "Spent per day", day1: "Today" },
  zh: { head: "每天用掉的积分", spent: "每天花掉的金额", day1: "今天" },
  "zh-TW": { head: "每天用掉的積分", spent: "每天花掉的金額", day1: "今天" },
  ja: { head: "1 日あたりのクレジット使用量", spent: "1 日あたりの支出", day1: "今日" },
  de: { head: "Verbrauchte Credits pro Tag", spent: "Ausgaben pro Tag", day1: "Heute" },
};
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(poolWords)) {
    const w = poolWords[lang];
    test(`${engine} ${lang}: every provider's credits a day, pool by pool`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 360, height: 760 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", async (route) => {
        if (new URL(route.request().url()).pathname === "/api/usage/quotas") return route.fulfill({ json: pools() });
        return serve(lang)(route);
      });
      await page.goto("http://uni-switch.test/?view=usage");
      const today = page.locator("#period .opt").filter({ hasText: new RegExp("^" + w.day1 + "$") });
      // a phone picks the days from the menu over the strip, which already says Today
      if (await today.count() && await today.first().isVisible()) await today.first().click();
      else await page.waitForFunction((s) => document.querySelector("#usageRangeMenu")?.textContent.trim() === s, w.day1);
      const heads = (name) => page.locator(".subscription-card", { hasText: name }).locator(".credit-days .cd-head span").allTextContents();
      await page.locator(".subscription-card", { hasText: "Cursor" }).locator(".credit-days").waitFor();
      assert.deepEqual(await heads("Trae CN"), [w.head]);
      const q = await heads("Qoder");
      assert.equal(q.length, 2);
      assert.ok(q[0].startsWith(w.head + " · ") && q[1].startsWith(w.head + " · ") && q[0] !== q[1], q.join(" | "));
      assert.deepEqual(await heads("Cursor"), [w.spent]);
      const spent = await page.locator(".subscription-card", { hasText: "Cursor" }).locator(".credit-days .cd-head b").textContent();
      assert.match(spent, /2[.,]50/);
      assert.match(spent, /\$/);
      const wide = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      assert.ok(wide <= 0, `no page scroll sideways (${wide}px)`);
      assert.deepEqual(errors, []);
    });
  }
}

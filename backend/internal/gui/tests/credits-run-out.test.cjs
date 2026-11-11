// Run with Node's test runner and Playwright on the module path; see README.md.
// cdredfox on X: WorkBuddy's credits come in packages that each end on
// their own day, and the Credits window has no reset. The plugin says when
// the soonest credits left run out (runsOutAt, runsOut), and the Usage
// card says so under the window, "232.4 credits expire tomorrow 00:39 ·
// in 21h", in every language, at a phone's width too, and in its tooltip.
// A window without it, or whose credits ran out already, says nothing of
// it.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const soon = (h) => new Date(Date.now() + h * 3600e3).toISOString();
const credits = (runsOutAt) => ({ name: "Credits", used: 100 * 17.6 / 2250, display: "17.6 / 2250", amount: 17.6, limit: 2250, unit: "credits", ...(runsOutAt ? { runsOutAt, runsOut: 232.4 } : {}) });
const quotas = [
  { provider: "workbuddy-ai", name: "WorkBuddy AI", icon: "workbuddy-color", plan: "Pro", user: "a@example.com", windows: [credits(soon(30))] },
  { provider: "workbuddy-ai", name: "WorkBuddy AI", icon: "workbuddy-color", plan: "Pro", user: "b@example.com", windows: [credits("")] },
  { provider: "workbuddy-ai", name: "WorkBuddy AI", icon: "workbuddy-color", plan: "Pro", user: "c@example.com", windows: [credits(soon(-1))] },
];
function serve(lang) {
  const settings = { lang, theme: "light", currency: "usd", chineseUnits: lang.startsWith("zh") };
  const state = { agents: [], profiles: [], settings };
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/gateway/trace") return url.searchParams.get("wait") ? new Promise(() => {}) : json({ mine: true, now: new Date().toISOString(), seq: 0, totals: {}, routes: [] });
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

const want = {
  en: /^232\.4 credits expire .+ · in (29|30)h$/,
  zh: /^232\.4 积分 将于 .+ 过期 · (29|30) 小时后$/,
  "zh-TW": /^232\.4 積分 將於 .+ 過期 · (29|30) 小時後$/,
  ja: /^232\.4 クレジットは .+ に失効 · (29|30) ?時間後$/,
  de: /^232\.4 Credits verfallen .+ · in (29|30) Std\.?$/,
};

for (const [name, engine] of [["webkit", webkit], ["chromium", chromium]]) {
  test(`${name}: credits that run out say when, under their window, at 360 and 440px in every language`, async () => {
    const browser = await engine.launch();
    try {
      for (const lang of Object.keys(want)) {
        for (const width of [360, 440, 1000]) {
          const page = await (await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" })).newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          await page.route("**/*", serve(lang));
          await page.goto("http://uni-switch.test/?view=usage");
          await page.locator(".subscription-card .quota").nth(2).waitFor();
          const got = await page.evaluate(() => [...document.querySelectorAll(".subscription-card .quota")].map((q) => {
            const card = q.closest(".subscription-card").getBoundingClientRect();
            const r = q.querySelector(".quota-reset");
            const b = r?.getBoundingClientRect();
            return { text: r?.textContent.replace(/\s+/g, " ").trim() ?? "", title: q.title,
              inCard: !b || (b.left >= card.left - 1 && b.right <= card.right + 1),
              cut: !!r && [...r.children].some((s) => s.scrollWidth > s.clientWidth + 1) };
          }).concat([{ pageScroll: document.documentElement.scrollWidth > document.documentElement.clientWidth + 1 }]));
          const at = `${lang} ${width}px`;
          const { pageScroll } = got.pop();
          assert.equal(got.length, 3, at);
          const [a, b, c] = got;
          assert.match(a.text, want[lang], at);
          assert.ok(a.title.includes("232.4"), `${at}: tooltip ${a.title}`);
          assert.ok(a.inCard && !a.cut, `${at}: "${a.text}" is cut or past its card`);
          assert.equal(b.text, "", `${at}: not known says nothing`);
          assert.equal(c.text, "", `${at}: run out already says nothing`);
          assert.equal(pageScroll, false, `${at}: the page scrolls sideways`);
          await page.close();
        }
      }
    } finally { await browser.close(); }
  });
}

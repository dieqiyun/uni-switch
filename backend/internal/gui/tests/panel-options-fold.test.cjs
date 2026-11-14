// Run with Node's test runner and Playwright on the module path; see README.md.
// A Codex card's options in the menu bar panel, Auto-use resets and Use
// credits, fold to one line saying how each is set (#2356, detail47: Codex
// 占用过多界面空间，导致其他供应商被遮挡). Open until folded; folded, the card is shorter and the line
// reads each option's setting, and the fold is kept, by the card's id,
// over a reload too. Unfolded, the switches are back and still work. A
// click moves nothing, the line fits the card at narrow widths, and no
// left-border accent. English, Chinese (both), Japanese and German,
// Chromium and WebKit; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const later = new Date(Date.now() + 3 * 864e5).toISOString();
const quotas = [
  { provider: "codex", name: "Codex", icon: "codex-color", user: "me@example.com", plan: "Plus",
    windows: [{ name: "5 hours", used: 30, resetsAt: later }, { name: "7 days", used: 89, resetsAt: later }],
    resets: { count: 3, until: later } },
];

function serve(lang, posts) {
  let off = [];
  const settings = () => ({ theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd", trayUsages: ["codex"], trayUsageEvery: 3,
    codexAutoReset: ["me@example.com"], codexAutoResetOnly: { "me@example.com": "week" }, codexNoCredits: off });
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: settings() });
    if (url.pathname === "/api/settings") return json(settings());
    if (url.pathname === "/api/settings/codex-credits") {
      const body = req.postDataJSON();
      posts.push(body);
      off = off.filter((u) => u !== body.user.toLowerCase());
      if (!body.on) off.push(body.user.toLowerCase());
      return json(settings());
    }
    if (url.pathname === "/api/usage/quotas") return json(quotas);
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) await new Promise((r) => setTimeout(r, 20e3));
      return json({ mine: true, now: new Date().toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// the folded line: the one case on, then credits on, then off
const words = {
  en: { me: "Auto-use resets: when its week runs out · Use credits: On", meOff: "Auto-use resets: when its week runs out · Use credits: Off", fold: "Fold", unfold: "Unfold" },
  zh: { me: "自动使用重置卡： 周额度用完时 · 使用积分： 开启", meOff: "自动使用重置卡： 周额度用完时 · 使用积分： 关闭", fold: "收起", unfold: "展开" },
  "zh-TW": { me: "自動使用重置卡： 週額度用完時 · 使用積分： 開啟", meOff: "自動使用重置卡： 週額度用完時 · 使用積分： 關閉", fold: "收起", unfold: "展開" },
  ja: { me: "リセットを自動使用： 週の枠を使い切ったとき · クレジットを使用： オン", meOff: "リセットを自動使用： 週の枠を使い切ったとき · クレジットを使用： オフ", fold: "折りたたむ", unfold: "展開" },
  de: { me: "Resets automatisch einlösen: wenn die Woche aufgebraucht ist · Credits nutzen: An", meOff: "Resets automatisch einlösen: wenn die Woche aufgebraucht ist · Credits nutzen: Aus", fold: "Einklappen", unfold: "Aufklappen" },
};
const keys = ["Fold", "Unfold", "On", "Off", "Auto-use resets:", "Use credits:", "when its week runs out", "before one expires"];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    const w = words[lang];
    test(`${engine} ${lang}: a Codex card's options in the panel fold to one line, kept per card`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      for (const width of [440, 360]) {
        const posts = [];
        const errors = [];
        const ctx = await browser.newContext({ viewport: { width, height: 640 }, reducedMotion: "reduce" });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, posts));
        const show = async () => {
          await page.goto("http://uni-switch.test/?mode=panel");
          await page.locator('#ptabs [data-ptab="usage"]').click();
          await page.locator(".pq-card .pq-opts-fold").first().waitFor();
          await page.waitForFunction(() => !quotasLoading);
        };
        await show();
        const me = '#panelQuota .pq-card[data-card="codex|me@example.com"]';
        const text = (sel) => page.locator(sel).evaluate((e) => e.textContent.replace(/\s+/g, " ").trim());
        const spot = () => page.evaluate(() => [document.scrollingElement.scrollTop, ...[...document.querySelectorAll("#panelQuota, #panelQuota *")].filter((e) => e.scrollHeight > e.clientHeight).map((e) => e.scrollTop)]);

        // open until folded: both rows with their switches
        const fold = page.locator(me + " .pq-opts-fold");
        assert.equal(await fold.getAttribute("aria-expanded"), "true");
        assert.equal(await fold.getAttribute("title"), w.fold);
        assert.equal(await page.locator(me + " .pq-autoreset .ar-case").count(), 2);
        assert.equal(await page.locator(me + " .pq-credits .use-credits").count(), 1);
        const tall = await page.locator(me).evaluate((e) => e.getBoundingClientRect().height);
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-panel-options-open.png`) });
        }

        // folded: one line saying how each is set, the card shorter, the
        // other card untouched, and nothing moved
        const was = await spot();
        await fold.click();
        await page.locator(me + " .pq-opts-brief").waitFor();
        assert.deepEqual(await spot(), was, "the click moved the page");
        assert.equal(await page.locator(me + " .pq-autoreset:not(.pq-opts-brief), " + me + " .pq-credits").count(), 0);
        assert.equal(await text(me + " .pq-opts-brief"), w.me);
        const f = page.locator(me + " .pq-opts-fold");
        assert.equal(await f.getAttribute("aria-expanded"), "false");
        assert.equal(await f.getAttribute("title"), w.unfold);
        assert(await f.evaluate((e) => document.activeElement === e), "the fold keeps the focus");
        const short = await page.locator(me).evaluate((e) => e.getBoundingClientRect().height);
        assert(short < tall - 15, `folded is shorter: ${short} vs ${tall}`);
        const fits = await page.locator(me + " .pq-opts-brief").evaluate((r) => {
          const c = r.closest(".pq-card").getBoundingClientRect(), b = r.getBoundingClientRect(), s = r.querySelector(".ar-say");
          return b.right <= c.right + 0.5 && r.querySelector(".pq-opts-fold").getBoundingClientRect().right <= c.right + 0.5 && s.scrollWidth <= s.clientWidth + 1;
        });
        assert(fits, `the line fits the card at ${width}px`);

        // kept over a reload
        await show();
        assert.equal(await page.locator(me + " .pq-opts-brief").count(), 1, "kept over a reload");

        // unfolded: the switches are back and still work
        await page.locator(me + " .pq-opts-fold").click();
        await page.locator(me + " .pq-credits .use-credits").waitFor();
        assert.equal(await page.locator(me + " .pq-opts-fold").getAttribute("aria-expanded"), "true");
        await page.locator(me + " .pq-credits .use-credits").click();
        for (let i = 0; i < 60 && !posts.length; i++) await page.waitForTimeout(50);
        assert.deepEqual(posts, [{ user: "me@example.com", on: false }]);
        await page.locator(me + ' .use-credits[aria-checked="false"]').waitFor();
        await page.locator(me + " .pq-opts-fold").click();
        await page.locator(me + " .pq-opts-brief").waitFor();
        assert.equal(await text(me + " .pq-opts-brief"), w.meOff);

        const border = await page.evaluate(() => [...document.querySelectorAll(".pq-opts-brief, .pq-opts-brief *, .pq-opts-fold")].map((e) => getComputedStyle(e).borderLeftWidth).filter((b) => parseFloat(b) > 1));
        assert.deepEqual(border, [], "no left-border accent");
        if (lang !== "en") {
          const missing = await page.evaluate(([keys, lang]) => keys.filter((k) => !I18N[lang][k]), [keys, lang]);
          assert.deepEqual(missing, [], `every string has its ${lang}`);
        }
        assert.deepEqual(errors, []);
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-panel-options-folded.png`) });
        }
        await ctx.close();
      }
    });
  }
}

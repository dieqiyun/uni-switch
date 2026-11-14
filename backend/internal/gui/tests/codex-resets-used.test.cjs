// Run with Node's test runner and Playwright on the module path; see README.md.
// The resets uni-switch used (foobra12 on Discord: "uni-switch会自动使用codex重置卡吗
// 我发现刚才好像不小心自动给我使用了一张"): a reset gone from a Codex
// account's card read the same whether uni-switch used it by itself, the user
// did, or it ran out. The card's auto-use row now says the last one uni-switch
// used, when and why, on a line of its own, with every kept one in its
// title; the menu bar panel's card the same, and an account in brief in its
// switch's title. An account uni-switch used none of has no such line. Each
// language, Chromium and WebKit, at 1100 and 420; no backend, the API is
// faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const later = new Date(Date.now() + 3 * 864e5).toISOString();
const now = new Date().toISOString();
const usedAt = "2026-10-12T08:05:00Z", handAt = "2026-10-10T20:30:00Z";
const quotas = [
  { provider: "codex", name: "Codex", icon: "codex-color", user: "Me@example.com", plan: "Plus", lastServedAt: now,
    windows: [{ name: "5 hours", used: 100, resetsAt: later }, { name: "7 days", used: 100, resetsAt: later }],
    resets: { count: 1, until: later },
    resetsUsed: [{ user: "me@example.com", at: usedAt, why: "expiring", windows: 2 }, { user: "me@example.com", at: handAt, why: "hand", windows: 1 }] },
  // none used: no line
  { provider: "codex", name: "Codex", icon: "codex-color", user: "two@example.com", plan: "Plus",
    windows: [{ name: "5 hours", used: 10, resetsAt: later }, { name: "7 days", used: 20, resetsAt: later }] },
];

function serve(lang) {
  const settings = () => ({ theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd", trayUsages: ["codex"], trayUsageEvery: 3, codexAutoReset: ["me@example.com"] });
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: settings() });
    if (url.pathname === "/api/settings") return json(settings());
    if (url.pathname === "/api/usage/quotas") return json(quotas);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { last: /^Last reset used: .+ · by itself, before it expired$/, hand: "by hand" },
  zh: { last: /^上次用卡：.+ · 自动，重置卡到期前$/, hand: "手动" },
  "zh-TW": { last: /^上次用卡：.+ · 自動，重置卡到期前$/, hand: "手動" },
  ja: { last: /^前回のリセット使用：.+ · 自動（期限切れの前）$/, hand: "手動" },
  de: { last: /^Zuletzt Reset eingelöst: .+ · automatisch, vor dem Ablauf$/, hand: "von Hand" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const [lang, w] of Object.entries(words)) {
    test(`${engine} ${lang}: a Codex account's card says the last reset uni-switch used, when and why`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const pages = [];
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          for (const [i, p] of pages.entries()) await p.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-codex-resets-used-${i}.png`) });
        }
        await browser.close();
      });
      const errors = [];
      const open = async (url, viewport) => {
        const page = await (await browser.newContext({ viewport, reducedMotion: "reduce" })).newPage();
        pages.push(page);
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        await page.goto(url);
        return page;
      };
      const text = (loc) => loc.evaluate((e) => e.textContent.replace(/\s+/g, " ").trim());
      // the line fits its card, nothing cut off or wider, once the card has
      // settled: the page lays the cards out again as their readings land
      const fits = async (page, sel, card) => {
        for (let i = 0; i < 40; i++) {
          const ok = await page.locator(sel).evaluate((e, card) => {
            const c = e.closest(card).getBoundingClientRect(), b = e.getBoundingClientRect();
            return b.width > 0 && b.left >= c.left - 0.5 && b.right <= c.right + 0.5 && e.scrollWidth <= e.clientWidth + 1;
          }, card);
          if (ok) return true;
          await page.waitForTimeout(50);
        }
        return false;
      };

      for (const width of [1100, 420]) {
        const page = await open("http://uni-switch.test/?view=usage", { width, height: 800 });
        const me = '.quota-autoreset[data-user="Me@example.com"]';
        await page.locator(me).waitFor();
        const line = page.locator(me + " .ar-used");
        assert.equal(await line.count(), 1, `${width}: the line under the say`);
        assert.match(await text(line), w.last);
        // every kept one in its title, newest first
        const title = (await line.getAttribute("title")).split("\n");
        assert.equal(title.length, 2);
        assert.match(title[1], new RegExp(" · " + w.hand + "$"));
        assert(await fits(page, me + " .ar-used", ".subscription-card"), `${width}: the line fits the card`);
        // the account uni-switch used none of: no line, in full or in brief
        const two = '.subscription-account[data-card="codex|two@example.com"]';
        assert.equal(await page.locator(two + " .ar-used").count(), 0);
        const briefTitle = await page.locator(two + " .acct-auto").getAttribute("title");
        assert.doesNotMatch(briefTitle || "", w.last);
        // Me in brief: its switch's title says it
        await page.locator('.subscription-account[data-card="codex|Me@example.com"] .quota-acct-fold').click();
        const meBrief = '.subscription-account.brief[data-card="codex|Me@example.com"] .acct-auto';
        await page.locator(meBrief).waitFor();
        assert.match((await page.locator(meBrief).getAttribute("title")).split("\n").at(-1), w.last);
        const border = await page.evaluate(() => [...document.querySelectorAll(".ar-used")].map((e) => getComputedStyle(e).borderLeftWidth).filter((b) => parseFloat(b) > 1));
        assert.deepEqual(border, [], "no left-border accent");
      }

      // the menu bar panel's card
      const panel = await open("http://uni-switch.test/?mode=panel", { width: 440, height: 600 });
      const psel = '.pq-autoreset[data-user="Me@example.com"]';
      await panel.locator('#ptabs [data-ptab="usage"]').click();
      await panel.locator(psel).waitFor();
      // measured on the cards drawn from the tab's own read (#2360)
      await panel.waitForFunction(() => !quotasLoading);
      assert.match(await text(panel.locator(psel + " .ar-used")), w.last);
      assert(await fits(panel, psel + " .ar-used", ".pq-card"), "the line fits the panel's card");
      assert.deepEqual(errors, []);
    });
  }
}

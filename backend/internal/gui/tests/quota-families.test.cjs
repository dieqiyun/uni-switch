// Run with Node's test runner and Playwright on the module path; see README.md.
// Antigravity's allowance one figure a model family (01huadalang on
// Discord: with several accounts signed in, every level of every model —
// Gemini 3.1 Pro (High), (Low), Gemini 3.7 Flash (Low), (Medium), (High)… —
// read as bloat). Windows that name a family show as one a family, Gemini
// and Claude first, each the most used of its models, on the Usage page's
// card, the menu bar panel's rings and the provider's account rows; each
// family's tooltip lists its models level by level, and "Every model"
// turns an account's card to each window and back, in place, the page not
// moving. Windows naming no family (Claude Code's 5-hour and weekly) are as
// they were. No left-border accent. English and Chinese, Chromium and
// WebKit; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const at = (h) => new Date(Date.now() + h * 36e5).toISOString();
const win = (name, family, used, h) => ({ name, family, used, ...(h ? { resetsAt: at(h) } : {}) });
// as uni-switch reports an Antigravity account: a window a model, by id
const ag = (user, [opus, sonnet, flash, proHigh, proLow, f37High, f37Low, f37Med, oss]) => ({
  provider: "antigravity", name: "Antigravity", icon: "antigravity-color", user, plan: "Google AI Pro",
  windows: [
    win("Claude Opus 4.6 (Thinking)", "Claude", opus, 3), win("Claude Sonnet 4.6", "Claude", sonnet, 3),
    win("Gemini 3 Flash", "Gemini", flash, 5), win("Gemini 3.1 Pro (High)", "Gemini", proHigh, 2),
    win("Gemini 3.1 Pro (Low)", "Gemini", proLow, 2), win("Gemini 3.7 Flash (High)", "Gemini", f37High, 5),
    win("Gemini 3.7 Flash (Low)", "Gemini", f37Low, 5), win("Gemini 3.7 Flash (Medium)", "Gemini", f37Med, 5),
    win("GPT-OSS 120B (Medium)", "GPT-OSS", oss, 5),
  ],
});
const quotas = [
  { provider: "claude", name: "Claude Code", icon: "claude-color", plan: "Max 5x",
    windows: [{ name: "5 hours", used: 30, resetsAt: at(2) }, { name: "7 days", used: 12, resetsAt: at(90) }] },
  ag("ada@example.com", [25, 40, 0, 60, 10, 20, 0, 5, 0]),
  ag("bob@example.com", [100, 100, 0, 30, 0, 0, 0, 0, 15]),
];
// what each account's families read: Gemini, Claude, GPT-OSS
const families = { "ada@example.com": [60, 40, 0], "bob@example.com": [30, 100, 15] };
const logins = quotas.slice(1).map((q, i) => ({ user: q.user, plan: q.plan, active: i === 0, on: true }));
const antigravity = {
  id: "antigravity", name: "Antigravity", icon: "antigravity-color", chat: "", responses: "", anthropic: "", catalog: "",
  models: [{ id: "gemini-3.1-pro", name: "Gemini 3.1 Pro", on: true }], agents: [], fallback: [], headers: {}, keyList: [],
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
    if (url.pathname === "/api/login/usage") return json(Object.fromEntries(quotas.slice(1).map((q) => [q.user, q])));
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const words = {
  en: { every: "Every model (9)", back: "By family", five: "5 hours", most: "Gemini: the most used of its models" },
  zh: { every: "全部模型（9）", back: "按系列", five: "5 小时", most: "Gemini：取其中用得最多的模型" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    const w = words[lang];
    test(`${engine} ${lang}: Antigravity's allowance one figure a model family`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const pages = [];
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          for (const [name, p] of pages) await p.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-quota-families-${name}.png`) });
        }
        await browser.close();
      });
      const errors = [];
      const open = async (name, url, viewport) => {
        const page = await (await browser.newContext({ viewport, reducedMotion: "reduce" })).newPage();
        pages.push([name, page]);
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, url.includes("mode=panel")));
        await page.goto(url);
        return page;
      };
      const fill = (loc) => loc.evaluateAll((es) => es.map((e) => Math.round(parseFloat(e.style.width))));

      // the Usage page: an account's families, Gemini and Claude first
      const page = await open("usage", "http://uni-switch.test/?view=usage", { width: 900, height: 700 });
      const card = page.locator(".subscription-card", { hasText: "Antigravity" });
      await card.locator(".quota-windows").first().waitFor();
      const boxes = card.locator(".quota-windows");
      assert.equal(await boxes.count(), 2, "one meter box an account");
      for (const [i, user] of ["ada@example.com", "bob@example.com"].entries()) {
        const box = boxes.nth(i);
        assert.deepEqual(await box.locator(".quota-labels > span:first-child").allTextContents(), ["Gemini", "Claude", "GPT-OSS"], `${user}: one meter a family`);
        assert.deepEqual(await fill(box.locator(".quota-track i")), families[user], `${user}: each family its most used model's`);
      }
      const gemini = boxes.first().locator(".quota").first();
      const tip = await gemini.getAttribute("title");
      assert(tip.startsWith(w.most), "the family says how its figure is taken: " + tip);
      for (const m of ["Gemini 3 Flash", "Gemini 3.1 Pro (High)", "Gemini 3.1 Pro (Low)", "Gemini 3.7 Flash (High)", "Gemini 3.7 Flash (Low)", "Gemini 3.7 Flash (Medium)"])
        assert(tip.includes(m), `the tooltip names ${m}`);
      assert(!tip.includes("Claude"), "only its own family's models");
      // Claude Code's windows name no family: as they were, no toggle
      const cc = page.locator(".subscription-card", { hasText: "Claude Code" });
      assert.deepEqual(await cc.locator(".quota-labels > span:first-child").allTextContents(), [w.five, lang === "zh" ? "7 天" : "7 days"]);
      assert.equal(await cc.locator(".quota-every").count(), 0);
      assert.equal(await card.locator("button.quota-every").count(), 2, "one toggle an account");

      // Every model, and back, in place
      const toggle = card.locator(".subscription-account", { hasText: "ada@example.com" }).locator("button.quota-every");
      assert.equal((await toggle.textContent()).trim(), w.every);
      assert.equal(await toggle.getAttribute("aria-expanded"), "false");
      const where = () => toggle.evaluate((e) => [e.getBoundingClientRect().top, document.scrollingElement.scrollTop, ...[...document.querySelectorAll(".view")].map((v) => v.scrollTop)]);
      await toggle.scrollIntoViewIfNeeded();
      await page.waitForTimeout(150);
      const before = await where();
      await toggle.click();
      await page.waitForTimeout(200);
      assert.equal(await boxes.first().locator(".quota").count(), 9, "every model's window");
      assert.equal(await boxes.nth(1).locator(".quota").count(), 3, "the other account stays by family");
      assert.equal((await toggle.textContent()).trim(), w.back);
      assert.equal(await toggle.getAttribute("aria-expanded"), "true");
      assert.deepEqual(await where(), before, "the click moved the page");
      await toggle.click();
      await page.waitForTimeout(200);
      assert.equal(await boxes.first().locator(".quota").count(), 3, "by family again");
      assert.deepEqual(await where(), before, "the click moved the page");
      const border = await page.evaluate(() => [...document.querySelectorAll(".subscription-card, .subscription-card *")].map((e) => getComputedStyle(e).borderLeftWidth).filter((b) => parseFloat(b) > 1));
      assert.deepEqual(border, [], "no left-border accent");

      // the menu bar panel: three rings an account, the families
      const panel = await open("panel", "http://uni-switch.test/?mode=panel", { width: 440, height: 640 });
      await panel.locator('#ptabs [data-ptab="usage"]').click();
      // its second account is behind the button, as several accounts are
      await panel.locator(".pq-group", { hasText: "Antigravity" }).locator(".pq-more").click();
      const pcards = panel.locator(".pq-group", { hasText: "Antigravity" }).locator(".pq-card");
      await pcards.first().locator(".pq-ring").first().waitFor();
      assert.equal(await pcards.count(), 2);
      for (let i = 0; i < 2; i++)
        assert.deepEqual(await pcards.nth(i).locator(".pq-rn").allTextContents(), ["Gemini", "Claude", "GPT-OSS"]);
      assert.deepEqual(await pcards.first().locator(".pq-dial b").allTextContents(), ["60%", "40%", "0%"]);
      const ring = await pcards.first().locator(".pq-ring").nth(1).getAttribute("title");
      assert(ring.includes("Claude Opus 4.6 (Thinking)") && ring.includes("Claude Sonnet 4.6"), "the ring's tooltip lists its models: " + ring);

      // the provider's accounts: Gemini and Claude on each row
      const prov = await open("accounts", "http://uni-switch.test/?view=providers", { width: 900, height: 700 });
      await prov.locator(".row.provider", { hasText: "Antigravity" }).click();
      await prov.locator(".accts .acc .aq-w").first().waitFor();
      for (const user of ["ada@example.com", "bob@example.com"]) {
        const row = prov.locator(".accts .acc", { hasText: user });
        assert.deepEqual(await row.locator(".aq-n").allTextContents(), ["Gemini", "Claude"], user);
      }
      const aq = await prov.locator(".accts .acc", { hasText: "bob@example.com" }).locator(".aq-w").nth(1).getAttribute("title");
      assert(aq.includes("Claude Opus 4.6 (Thinking)"), "the meter's tooltip lists its models: " + aq);

      const missing = await page.evaluate(() => [
        "Every model ({n})", "By family", "One figure a model family, its most used model's", "Each model's allowance, level by level",
        "{family}: the most used of its models",
      ].filter((k) => !I18N.zh[k]));
      assert.deepEqual(missing, [], "every string has its Chinese");
      assert.deepEqual(errors, []);
    });
  }
}

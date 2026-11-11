// Run with Node's test runner and Playwright on the module path; see README.md.
// #3569 (Eronwin; kekao on Discord, v0.1.1486): in Chinese a tray card's
// line "重置于 10月31日 01:43 · 按此速度，预计 10月26日 16:19 用完" ran past
// the card, and the card's own refresh, which sat inside that line's
// ellipsis, was cut off with it: no icon, nothing to click. English fit.
// The words now shorten and the refresh keeps a slot of its own at their
// end. Checked on each tray line that carries the refresh: the deadline
// with its pace, a long error and an unlimited plan, in every language,
// Chromium and WebKit, at the tray's widths. No backend: the API is faked
// here, and a click reads that account alone.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = process.env.UNI_SWITCH_QUOTA_ASSETS || path.resolve(__dirname, "../assets");
const LANGS = ["en", "zh", "zh-TW", "ja", "de"];
const H = 3600e3;
const iso = (ms) => new Date(ms).toISOString();

// a weekly window used faster than an even burn, read every 6 hours, so
// the deadline's line also says when it runs out at this pace
function fixtures(now) {
  const start = now - 84 * H, reset = now + 84 * H;
  const points = [];
  for (let h = 0; h <= 84; h += 6) points.push({ at: iso(start + h * H), left: 100 - (70 * h) / 84, start: iso(start), resetsAt: iso(reset) });
  return {
    quotas: [
      { provider: "codex", name: "Codex", icon: "openai", user: "pace@example.com", plan: "Pro", readAt: iso(now - 60e3),
        windows: [{ name: "Weekly", used: 70, startsAt: iso(start), resetsAt: iso(reset) }] },
      { provider: "kimi", name: "Kimi", error: "this account has no GLM Coding Plan, and ZCode's Start Plan has ended or was never started", windows: [] },
      { provider: "unlimited", name: "Unlimited plan", readAt: iso(now - 60e3), windows: [{ name: "Requests", unlimited: true }] },
    ],
    history: [{ provider: "codex", user: "pace@example.com", lines: [{ name: "Weekly", points }] }],
    posts: [],
  };
}

function serve(lang, data) {
  const settings = { theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd" };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (body) => route.fulfill({ json: body });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = ${JSON.stringify({ lang, theme: "light", web: true })};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") return json(data.quotas);
    if (url.pathname === "/api/usage/quotas/history") return json(data.history);
    if (url.pathname === "/api/usage/quotas/refresh") { data.posts.push(url.search); return json(data.quotas); }
    if (url.pathname === "/api/usage") return json({ calls: 0, input: 0, output: 0, cost: 0, agents: [], accounts: [], models: [], series: [], days: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

// where the open refresh is, against the card it is in, and
// what the pointer finds at its middle
const place = (b) => b.evaluate((x) => {
  const r = x.getBoundingClientRect(), card = x.closest(".pq-card").getBoundingClientRect();
  const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
  return { width: r.width, opacity: getComputedStyle(x).opacity, insideCard: r.left >= card.left && r.right <= card.right, hit: !!hit && x.contains(hit) };
});

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of LANGS) {
    for (const width of [486, 420, 360]) {
      test(`${engine} ${lang} ${width}px: a tray card's refresh stays in sight and clickable beside a long line`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await browser.newPage({ viewport: { width, height: 720 }, reducedMotion: "reduce" });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], data = fixtures(Date.now());
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, data));
        await page.goto("http://uni-switch.test/?mode=panel");
        await page.locator('#ptabs [data-ptab="usage"]').click();
        const pace = page.locator('.pq-card[data-card="codex|pace@example.com"]');
        await pace.locator(".pq-pace").waitFor();
        await pace.locator("> .pq-sub").first().waitFor({ state: "visible" });

        // the reported case: in Chinese the deadline and its pace are longer
        // than the line, at the tray's narrow width
        if (lang === "zh" && width === 360) {
          // waited for: a redraw can hold the card unlaid out for a moment
          const over = await (await page.waitForFunction(() => {
            const s = document.querySelector('.pq-card[data-card="codex|pace@example.com"] > .pq-sub'), p = s?.querySelector(".pq-pace");
            const w = s?.getBoundingClientRect().width;
            if (!p || !w) return null;
            const r = document.createRange(); r.selectNodeContents(p.parentElement);
            return [r.getBoundingClientRect().width, w];
          })).jsonValue();
          assert.ok(over[0] > over[1] + 1, `the zh deadline's words are wider than their line (the case of #3569): ${over}`);
        }
        for (const id of ["codex|pace@example.com", "kimi", "unlimited"]) {
          const card = page.locator(`.pq-card[data-card="${id}"]`);
          const button = card.locator(".quota-refresh");
          assert.equal(await button.count(), 1, `${id} has its own refresh`);
          await card.hover();
          await page.waitForFunction((b) => b.getBoundingClientRect().width >= 12, await button.elementHandle());
          const at = await place(button);
          assert.ok(at.insideCard, `${id}'s refresh sits inside its card: ${JSON.stringify(at)}`);
          assert.ok(at.hit, `${id}'s refresh is what the pointer finds at its middle: ${JSON.stringify(at)}`);
          assert.equal(at.opacity, "1");
        }
        // the deadline's words are what shorten, not the refresh
        const line = pace.locator("> .pq-sub").first();
        assert.ok((await line.textContent()).includes((await pace.locator(".pq-pace").textContent()).trim()), "the pace is said in the deadline's line");

        // and the click reads that account
        await pace.hover();
        await pace.locator(".quota-refresh").click();
        await page.waitForFunction(() => !document.querySelector(".pq-card .quota-refresh.busy"));
        assert.deepEqual(data.posts, ["?provider=codex&user=pace%40example.com"]);

        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "nothing wider than the panel");
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await pace.hover();
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `panel-refresh-long-${engine}-${lang}-${width}.png`) });
        }
        assert.deepEqual(errors, []);
      });
    }
  }
}

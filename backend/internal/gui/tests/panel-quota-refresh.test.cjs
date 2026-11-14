// #840 (hhh2210): the tray's Allowances tab names a card's reset deadline
// ("Resets 14:26", not a bare "↻ 14:26" that read as a button to click),
// and each card has the Usage page's own refresh: opened as the card is
// hovered or the key reaches it, reading that account alone, the page not
// moving and the focus kept. Real page, isolated responses: no vendor,
// account or installed app is used.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const assets = process.env.UNI_SWITCH_QUOTA_ASSETS || path.resolve(__dirname, "../assets");
const iso = n => new Date(n).toISOString();
const words = {
  en: { reset: "Resets", label: "Refresh this card" },
  zh: { reset: "重置于", label: "刷新这张卡片" },
  "zh-TW": { reset: "重置於", label: "重新整理這張卡片" },
  ja: { reset: "にリセット", label: "このカードを更新" },
  de: { reset: "Zurückgesetzt", label: "Diese Karte aktualisieren" },
};
function fixtures() {
  const now = Date.now(), readAt = iso(now - 40 * 60000), resetsAt = iso(now + 2 * 3600000);
  return [
    { provider: "claude", name: "Claude Code", icon: "claude-color", user: "a@example.com", plan: "Pro", readAt, asOf: readAt, readDeferred: true, windows: [{ name: "5 hours", used: 28, resetsAt }] },
    { provider: "codex", name: "Codex", icon: "openai", user: "b@example.com", plan: "Pro", readAt, windows: [{ name: "5 hours", used: 50, resetsAt }] },
    { provider: "kimi", name: "Kimi", error: "HTTP 503", windows: [] },
    { provider: "relay", name: "Relay", balance: "$2.00", readAt, windows: [] },
    { provider: "unlimited", name: "Unlimited plan", readAt, windows: [{ name: "Requests", unlimited: true }] },
  ];
}
function serve(lang, data) {
  const settings = { theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd" };
  return async route => {
    const url = new URL(route.request().url()), json = body => route.fulfill({ json: body });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = ${JSON.stringify({ lang, theme: "light", web: true })};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") { data.gets++; return json(data.quotas); }
    if (url.pathname === "/api/usage/quotas/refresh") {
      assert.equal(route.request().method(), "POST");
      data.posts.push(url.search);
      await data.gate;
      if (data.behavior === "fail") return route.fulfill({ status: 503, body: "fixture offline" });
      const q = data.quotas.find(q => q.provider === url.searchParams.get("provider") && (q.user || "") === (url.searchParams.get("user") || ""));
      assert.ok(q, "refresh addressed a different account");
      if (data.behavior !== "cached") {
        delete q.error; delete q.asOf; delete q.readDeferred;
        q.readAt = iso(Date.now());
        if (q.balance) q.balance = "$3.00";
        else if (!q.windows.length) q.windows = [{ name: "Week", used: 10 }];
        else if (!q.windows[0].unlimited) q.windows[0].used = 61;
      }
      return json(data.quotas);
    }
    if (url.pathname === "/api/usage/quotas/history") return json([]);
    if (url.pathname === "/api/usage") return json({ calls: 0, cost: 0, days: [], agents: [], models: [], keys: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); }
    catch { await route.fulfill({ status: 404, body: "" }); }
  };
}
async function open(t, engine, lang, width = 440) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  t.after(() => browser.close());
  const page = await browser.newPage({ viewport: { width, height: 640 }, reducedMotion: "reduce" });
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [], data = { quotas: fixtures(), gets: 0, posts: [], behavior: "fresh" };
  page.on("pageerror", e => errors.push(e.message));
  await page.route("**/*", serve(lang, data));
  await page.goto("http://uni-switch.test/?mode=panel");
  await page.locator('#ptabs [data-ptab="usage"]').click();
  await page.locator('.pq-card[data-card="claude|a@example.com"]').waitFor();
  await page.evaluate(() => quotasLoading);
  return { page, data, errors };
}
const shown = (b) => b.evaluate((x) => x.getBoundingClientRect().width >= 12 && getComputedStyle(x).opacity === "1");
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const [lang, w] of Object.entries(words)) {
    for (const width of [440, 360]) {
      test(`${engine} ${lang} ${width}px: a tray card's reset deadline is named, and its own refresh reads that account alone`, async (t) => {
        const { page, data, errors } = await open(t, engine, lang, width);
        await page.mouse.move(1, 1);
        const card = page.locator('.pq-card[data-card="codex|b@example.com"]');
        const deadline = card.locator("> .pq-sub").first();
        const text = await deadline.textContent();
        assert.ok(text.includes(w.reset), `the deadline says ${w.reset}, got ${text}`);
        assert.ok(!text.includes("↻"), "no bare ↻ standing for a deadline");

        // every kind of tray card has the one refresh, closed at rest
        for (const id of ["claude|a@example.com", "codex|b@example.com", "kimi", "relay", "unlimited"]) {
          const b = page.locator(`.pq-card[data-card="${id}"] .quota-refresh`);
          assert.equal(await b.count(), 1, `${id} has its own refresh`);
          assert.equal(await b.getAttribute("aria-label"), w.label);
          assert.equal(await shown(b), false, `${id}'s refresh is closed until hovered`);
          // closed, it takes no room: the card and its line's text sit where they did
          const [was, without] = await b.evaluate((x) => {
            const at = () => { const c = x.closest(".pq-card").getBoundingClientRect(), n = x.nextSibling, r = document.createRange(); r.selectNodeContents(n || x.parentElement); return [c.height, Math.round(r.getBoundingClientRect().left)]; };
            const a = at(); x.style.display = "none"; const z = at(); x.style.display = ""; return [a, z];
          });
          assert.deepEqual(was, without, `${id}'s closed refresh moves nothing`);
        }
        const button = card.locator(".quota-refresh");
        await card.hover();
        await page.waitForFunction((b) => b.getBoundingClientRect().width >= 12, await button.elementHandle());
        assert.ok(await shown(button), "hovering the card opens its refresh");
        await page.mouse.move(1, 1);

        // the key reaches it (macOS WebKit tabs to buttons with Option-Tab)
        const tab = engine === "webkit" && process.platform === "darwin" ? "Alt+Tab" : "Tab";
        for (let i = 0; i < 60 && !(await button.evaluate((b) => b === document.activeElement)); i++) await page.keyboard.press(tab);
        assert.ok(await button.evaluate((b) => b === document.activeElement), "the refresh is reachable by keyboard");
        assert.ok(await shown(button), "focused, the refresh is in sight");

        const scroll = () => page.evaluate(() => [scrollY, document.querySelector("#panelQuota").closest(".view").scrollTop]);
        const y = await scroll(), gets = data.gets;
        let release; data.gate = new Promise((r) => { release = r; }); t.after(() => release());
        await page.keyboard.press("Enter");
        await card.locator(".quota-refresh.busy").waitFor();
        await page.keyboard.press("Enter"); // a second press while reading asks nothing more
        release(); data.gate = null;
        await card.locator(".pq-dial b", { hasText: "61%" }).waitFor();
        assert.deepEqual(data.posts, ["?provider=codex&user=b%40example.com"], "that account alone, once");
        assert.equal(data.gets, gets, "no read of every account");
        assert.equal(await page.locator('.pq-card[data-card="claude|a@example.com"] .pq-dial b').first().textContent(), "28%", "another card left as it was");
        assert.ok(await page.locator('.pq-card[data-card="codex|b@example.com"] .quota-refresh').evaluate((b) => b === document.activeElement), "the redraw keeps the focus");
        assert.deepEqual(await scroll(), y, "a refresh never moves the page");

        // a failed read: the figures stay, the failure is said
        data.behavior = "fail";
        await page.keyboard.press("Enter");
        await page.locator("#status.err").waitFor({ state: "visible" });
        assert.equal(await card.locator(".pq-dial b").first().textContent(), "61%", "a failure keeps what was read");

        // an error card and a balance read again from the tray too
        data.behavior = "fresh";
        await page.locator('.pq-card[data-card="kimi"] .quota-refresh').focus();
        await page.keyboard.press("Enter");
        await page.locator('.pq-card[data-card="kimi"] .pq-dial b', { hasText: "10%" }).waitFor();
        await page.locator('.pq-card[data-card="relay"] .quota-refresh').focus();
        await page.keyboard.press("Enter");
        await page.locator('.pq-card[data-card="relay"] .pq-amt', { hasText: "$3.00" }).waitFor();
        assert.deepEqual(data.posts.slice(-2), ["?provider=kimi", "?provider=relay"]);

        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "nothing wider than the panel");
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await card.hover();
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `panel-refresh-${engine}-${lang}-${width}.png`), fullPage: true });
        }
        assert.deepEqual(errors, []);
      });
    }
  }
}

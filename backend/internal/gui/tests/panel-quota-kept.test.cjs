// #3160 (yiliang114), #3134 (dakango): the tray panel's Allowances tab
// stood on skeletons until the whole state had been read and then every
// vendor's read had come. The allowances last read are kept in the panel's
// storage and drawn at once, each card's refresh turning until the read
// lands, and the read is asked for beside the state, not after it. Real
// page, isolated responses: no vendor, account or installed app is used.
// UNI_SWITCH_QUOTA_ASSETS points at older assets to see it fail.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const assets = process.env.UNI_SWITCH_QUOTA_ASSETS || path.resolve(__dirname, "../assets");
const iso = n => new Date(n).toISOString();
function fixtures(used) {
  const now = Date.now(), readAt = iso(now - 60000), resetsAt = iso(now + 2 * 3600000);
  return [
    { provider: "codex", name: "Codex", icon: "openai", user: "b@example.com", plan: "Pro", readAt, windows: [{ name: "5 hours", used, resetsAt }] },
    { provider: "relay", name: "Relay", balance: "$2.00", readAt, windows: [] },
  ];
}
function serve(lang, data) {
  const settings = { theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd" };
  return async route => {
    const url = new URL(route.request().url()), json = body => route.fulfill({ json: body });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = ${JSON.stringify({ lang, theme: "light", web: true })};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state" && url.searchParams.get("last")) return json({ agents: [], profiles: [], settings, last: true });
    if (url.pathname === "/api/state") { data.states++; await data.state; return json({ agents: [], profiles: [], settings }); }
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") { data.gets++; await data.quotasHeld; return json(data.quotas); }
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
const card = '#panelQuota .pq-card[data-card="codex|b@example.com"]';
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [440, 360]) {
      test(`${engine} ${lang} ${width}px: the panel draws the allowances last read at once, asks beside the state, and puts the read in their place`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await browser.newPage({ viewport: { width, height: 640 }, reducedMotion: "reduce" });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        let giveState, giveQuotas;
        const data = {
          quotas: fixtures(75), gets: 0, states: 0,
          state: new Promise(r => { giveState = r; }), quotasHeld: new Promise(r => { giveQuotas = r; }),
        };
        const errors = [];
        page.on("pageerror", e => errors.push(e.message));
        await page.addInitScript(kept => {
          try {
            if (sessionStorage.getItem("seeded")) return;
            sessionStorage.setItem("seeded", "1");
            localStorage.setItem("uni-switch.panelTab", "usage");
            localStorage.setItem("uni-switch.panelQuotas", JSON.stringify(kept));
          } catch {}
        }, fixtures(50));
        await page.route("**/*", serve(lang, data));
        await page.goto("http://uni-switch.test/?mode=panel");

        // the state and the read both held: the kept card is drawn, turning
        await page.locator(`${card} .pq-dial b`, { hasText: "50%" }).waitFor();
        assert.equal(await page.locator("#panelQuota .skeleton").count(), 0, "no skeleton over the kept cards");
        assert.ok(await page.locator(`${card} .quota-refresh.reading`).count() > 0, "the kept card's refresh turns");
        assert.ok(data.gets > 0, "the allowances were asked for before the state came");

        // the state comes, then the read: the read is drawn and stops turning
        giveState();
        giveQuotas();
        await page.locator(`${card} .pq-dial b`, { hasText: "75%" }).waitFor();
        await page.locator("#panelQuota .quota-refresh.reading").waitFor({ state: "detached" });
        const kept = await page.evaluate(() => JSON.parse(localStorage.getItem("uni-switch.panelQuotas")));
        assert.equal(kept[0].windows[0].used, 75, "the read is kept for the next open");
        assert.deepEqual(errors, []);
      });
    }
  }
}

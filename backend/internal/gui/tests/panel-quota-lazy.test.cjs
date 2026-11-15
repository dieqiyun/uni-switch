// #3192 (parkavenue9639): the tray panel's Allowances tab showed its
// skeleton until the allowances' 45-day history came, though the cards
// need only the allowances; the window coming back rebuilt cards that had
// not changed (and restarted the skeleton's shimmer while loading); and
// each draw also built the Usage page's cards and curves, which the panel
// never shows. Real page, isolated responses: no vendor, account or
// installed app is used. UNI_SWITCH_QUOTA_ASSETS points at older assets to see
// it fail.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const assets = process.env.UNI_SWITCH_QUOTA_ASSETS || path.resolve(__dirname, "../assets");
const iso = n => new Date(n).toISOString();
function fixtures() {
  const now = Date.now(), readAt = iso(now - 60000), resetsAt = iso(now + 2 * 3600000);
  return [
    { provider: "codex", name: "Codex", icon: "openai", user: "b@example.com", plan: "Pro", readAt, windows: [{ name: "5 hours", used: 50, resetsAt }] },
    { provider: "relay", name: "Relay", balance: "$2.00", readAt, windows: [] },
  ];
}
function serve(lang, data) {
  const settings = { theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd" };
  return async route => {
    const url = new URL(route.request().url()), json = body => route.fulfill({ json: body });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = ${JSON.stringify({ lang, theme: "light", web: true })};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") { data.states++; return json({ agents: [], profiles: [], settings }); }
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") { data.gets++; return json(data.quotas); }
    if (url.pathname === "/api/usage/quotas/history") { await data.history; return json([]); }
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
async function open(t, engine, lang, width) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  t.after(() => browser.close());
  const page = await browser.newPage({ viewport: { width, height: 640 }, reducedMotion: "reduce" });
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  let release;
  const data = { quotas: fixtures(), gets: 0, states: 0, history: new Promise(r => { release = r; }) };
  const errors = [];
  page.on("pageerror", e => errors.push(e.message));
  await page.route("**/*", serve(lang, data));
  await page.goto("http://uni-switch.test/?mode=panel");
  await page.locator('#ptabs [data-ptab="usage"]').click();
  return { page, data, errors, release };
}
const card = '#panelQuota .pq-card[data-card="codex|b@example.com"]';
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [440, 360]) {
      test(`${engine} ${lang} ${width}px: the panel's allowances show without their history, and stay as drawn when nothing changed`, async (t) => {
        const { page, data, errors, release } = await open(t, engine, lang, width);
        // the history held: the allowances are drawn all the same
        await page.locator(card).waitFor();
        assert.equal(await page.locator("#panelQuota .skeleton").count(), 0, "no skeleton left once the allowances came");
        release();
        await page.evaluate(() => quotasLoading);

        // the Usage page is never drawn in the panel
        assert.equal(await page.locator("#subscriptionUsage > *").count(), 0, "the panel draws no Usage page cards");

        // the window coming back with the same answers keeps the cards
        await page.evaluate(() => { document.querySelector("#panelQuota .pq-card").kept = true; document.querySelector("#panelQuota .pq-group").kept = true; });
        const states = data.states;
        await page.evaluate(() => loadBack());
        await page.evaluate(() => quotasLoading);
        await page.evaluate(() => new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r))));
        assert.ok(data.states > states, "the state was read again");
        assert.equal(await page.locator(card).evaluate(c => document.querySelector("#panelQuota .pq-card").kept === true && c.isConnected), true, "the card is the one drawn before");
        assert.equal(await page.evaluate(() => document.querySelector("#panelQuota .pq-group").kept), true, "the group is the one drawn before");

        // a change is drawn
        data.quotas[0].windows[0].used = 75;
        await page.evaluate(() => loadQuotas(true));
        await page.locator(`${card} .pq-dial b`, { hasText: "75%" }).waitFor();
        assert.deepEqual(errors, []);
      });
    }
  }
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// A card with its balance read but not the rest (#79, AkaChou): a ClinePass
// card whose limits failed to read for a reason other than no ClinePass
// shows its balance and, under it, that the limits couldn't be read, the
// reason in the tooltip. A balance alone with no error is as it was, and a
// card with nothing read still shows its error alone. The built-in key card
// (clineLimitsUnread) and the Cline plugin send the same words. No backend,
// the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const REASON = "ClinePass limits couldn't be read: Cline answered 503 Service Unavailable: try again later";

const quotas = [
  { provider: "clinepass", name: "ClinePass", icon: "cline", user: "pass", balance: "$12.00", error: REASON, windows: [] },
  { provider: "relay", name: "Relay", icon: "generic", balance: "$3.00", readAt: new Date().toISOString(), windows: [] },
  { provider: "kimi", name: "Kimi", icon: "kimi-color", error: "HTTP 503", windows: [] },
];

function serve(lang) {
  const settings = { theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd" };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (body) => route.fulfill({ json: body });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") return json(quotas);
    if (url.pathname === "/api/usage/quotas/history") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const words = {
  en: "ClinePass limits couldn't be read — hover for why",
  zh: "ClinePass 额度没能读到 — 悬停查看原因",
  "zh-TW": "ClinePass 額度沒能讀到 — 懸停查看原因",
  ja: "ClinePass の上限を読み取れませんでした — ホバーで理由を表示",
  de: "ClinePass-Limits konnten nicht gelesen werden — für den Grund darüberfahren",
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const [lang, said] of Object.entries(words)) {
    for (const width of [900, 420]) {
      test(`${engine} ${lang} ${width}px: a card with its balance but not its limits shows both`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const errors = [];
        const page = await (await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        await page.goto("http://uni-switch.test/?view=usage");

        const pass = page.locator(".subscription-card", { hasText: "ClinePass" });
        await pass.locator(".quota-balance").waitFor();
        assert.match(await pass.locator(".quota-balance").textContent(), /\$12\.00/);
        const err = pass.locator(".subscription-error");
        assert.equal(await err.count(), 1, "the limits' failure is said beside the balance");
        assert.equal((await err.textContent()).trim(), said);
        assert.equal(await err.getAttribute("title"), REASON, "the reason in the tooltip");
        assert.equal(await pass.locator(".quota-refresh").count(), 1, "one refresh on the card");
        const [b, e] = [await pass.locator(".quota-balance").boundingBox(), await err.boundingBox()];
        assert.ok(e.y >= b.y + b.height - 1, "the error under the balance");
        assert.ok(e.x + e.width <= width, "the error fits the page");

        const relay = page.locator(".subscription-card", { hasText: "Relay" });
        assert.match(await relay.locator(".quota-balance").textContent(), /\$3\.00/);
        assert.equal(await relay.locator(".subscription-error").count(), 0, "a balance alone says no error");

        const kimi = page.locator(".subscription-card", { hasText: "Kimi" });
        assert.equal(await kimi.locator(".subscription-error").count(), 1);
        assert.equal(await kimi.locator(".quota-balance").count(), 0);
        assert.deepEqual(errors, []);
      });
    }
  }
}

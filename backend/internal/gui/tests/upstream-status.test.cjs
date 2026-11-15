// Run with Node's test runner and Playwright on the module path; see README.md.
// #971 (emo172): when a vendor's own status page says its API is degraded or
// down, that vendor's providers say so — a badge on the provider's row and
// on its usage card, beside the name — so an outage isn't taken for a
// sign-in or quota problem. A click opens the incident (or the status page)
// and doesn't open the row. A relay serving the same models isn't marked,
// nor a vendor whose page says all is well, nor one whose page couldn't be
// read (unknown, not an outage). At 440px the badge is whole and nothing
// spills. English and Chinese, Chromium and WebKit; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const base = { chat: "", responses: "", anthropic: "", catalog: "", routing: "smart", models: [{ id: "m", name: "M", on: true }], agents: [], fallback: [], headers: {}, keyList: [] };
const list = [
  { ...base, id: "anthropic", name: "Anthropic", icon: "anthropic", anthropic: "https://api.anthropic.com", host: "api.anthropic.com", key: { set: true, masked: "sk-ant-…abcd" } },
  { ...base, id: "relay", name: "Claude Relay", icon: "generic", anthropic: "https://relay.example.com", host: "relay.example.com", key: { set: true, masked: "sk-…wxyz" } },
  { ...base, id: "claude", name: "Claude Code", icon: "claudecode-color", anthropic: "https://api.anthropic.com", key: { set: false },
    account: { agent: "claude", agentName: "Claude Code", user: "a-rather-long-address@example.com", plan: "max", logins: [{ user: "a-rather-long-address@example.com", plan: "max", active: true, on: true }] } },
  { ...base, id: "codex", name: "Codex", icon: "codex-color", responses: "https://chatgpt.com/backend-api/codex", key: { set: false },
    account: { agent: "codex", agentName: "Codex", user: "c@example.com", plan: "PLUS", logins: [{ user: "c@example.com", plan: "PLUS", active: true, on: true }] } },
  { ...base, id: "kimi", name: "Moonshot", icon: "moonshot", chat: "https://api.moonshot.cn/v1", host: "api.moonshot.cn", key: { set: true, masked: "sk-…kimi" } },
];
const quotas = [
  { provider: "claude", name: "Claude Code", kind: "subscription", icon: "claudecode-color", plan: "Max 20x", windows: [{ name: "5 hours", used: 30 }] },
  { provider: "codex", name: "Codex", kind: "subscription", icon: "codex-color", plan: "Plus", windows: [{ name: "5 hours", used: 10 }] },
];
const providersOf = { anthropic: "anthropic", claude: "anthropic", codex: "openai", kimi: "moonshot" };
const outage = {
  providers: providersOf,
  vendors: [
    { vendor: "anthropic", name: "Anthropic", page: "https://status.claude.com", level: "major", parts: [{ name: "Claude API (api.anthropic.com)", status: "major_outage" }], incidents: [{ name: "Elevated errors on Claude API", impact: "critical", url: "https://stspg.io/abc" }] },
    { vendor: "openai", name: "OpenAI", page: "https://status.openai.com", level: "ok" },
    // read and failed: unknown, never marked
    { vendor: "moonshot", name: "Moonshot AI", page: "https://status.moonshot.cn", level: "", error: "answered 503" },
  ],
};
const allWell = { providers: providersOf, vendors: outage.vendors.map((v) => ({ vendor: v.vendor, name: v.name, page: v.page, level: v.vendor === "moonshot" ? "" : "ok" })) };

function serve(lang, upstream) {
  const settings = { theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd" };
  const providers = { providers: list, presets: [], excluded: [], gateway: { running: true, window: true } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/upstream") return json(upstream.now);
    if (url.pathname === "/api/usage/quotas") return json(quotas);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const words = {
  en: { outage: "API outage", says: "Anthropic's status page says its API is affected", part: "major outage", open: "Click to open the status page" },
  zh: { outage: "API 故障", says: "Anthropic 的状态页显示其 API 受影响", part: "严重中断", open: "点击打开状态页" },
};

// the badges drawn, by row or card, and whether each is whole and inside
const badges = (page, sel, box) => page.evaluate(([sel, box]) => {
  const out = {};
  for (const r of document.querySelectorAll(sel)) {
    const b = r.querySelector(".badge.upstream");
    const id = r.dataset.id || r.dataset.provider;
    if (!b) { out[id] = null; continue; }
    const at = b.getBoundingClientRect(), clip = b.closest(box).getBoundingClientRect(), name = b.parentElement.getBoundingClientRect();
    const whole = at.width > 0 && at.right <= clip.right + 0.5 && at.left >= clip.left - 0.5
      && (!b.parentElement.matches(".name") || at.right <= name.right + 0.5) && b.scrollWidth <= b.clientWidth + 0.5;
    out[id] = { text: b.textContent, title: b.title, whole, after: b.previousElementSibling?.tagName };
  }
  out.sideways = document.documentElement.scrollWidth > innerWidth;
  return out;
}, [sel, box]);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    const w = words[lang];
    for (const width of [900, 440]) {
      test(`${engine} ${lang} ${width}px: a vendor's API outage is marked on its providers and usage cards, nothing else`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await (await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const upstream = { now: outage };
        await page.route("**/*", serve(lang, upstream));
        await page.addInitScript(() => { window.open = (u) => { window.opened = u; return null; }; });

        await page.goto("http://uni-switch.test/?view=providers");
        await page.waitForSelector("#providers .row.provider[data-id=anthropic] .badge.upstream");
        let got = await badges(page, "#providers .row.provider[data-id]", ".row");
        assert.equal(got.anthropic.text, w.outage);
        assert.equal(got.claude.text, w.outage, "the Claude subscription calls the same API");
        assert.equal(got.relay, null, "a relay isn't the vendor");
        assert.equal(got.codex, null, "OpenAI's page says all is well");
        assert.equal(got.kimi, null, "a page that couldn't be read is unknown, not an outage");
        assert.ok(got.anthropic.title.includes(w.says) && got.anthropic.title.includes("Elevated errors on Claude API")
          && got.anthropic.title.includes(w.part) && got.anthropic.title.includes(w.open), got.anthropic.title);
        assert.ok(got.anthropic.whole && got.claude.whole, "the badge is whole: " + JSON.stringify(got));
        assert.equal(got.sideways, false);

        // a click opens the incident, not the row
        await page.click("#providers .row.provider[data-id=anthropic] .badge.upstream");
        await page.waitForFunction(() => window.opened);
        assert.equal(await page.evaluate(() => window.opened), "https://stspg.io/abc");
        assert.equal(await page.locator("#providers .row.provider.selected").count(), 0, "the row didn't open");
        assert.equal(await page.locator(".modal:visible, dialog[open]").count(), 0, "no editor");

        // the Usage page: on the subscription's card, after its name
        await page.goto("http://uni-switch.test/?view=usage");
        await page.waitForSelector("#subscriptionUsage > .subscription-card[data-provider=claude] .badge.upstream");
        got = await badges(page, "#subscriptionUsage > .subscription-card[data-provider]", ".subscription-card");
        assert.equal(got.claude.text, w.outage);
        assert.equal(got.claude.after, "B", "beside the card's name");
        assert.ok(got.claude.whole, "the badge is whole: " + JSON.stringify(got.claude));
        assert.equal(got.codex, null);
        assert.equal(got.sideways, false);

        // all well again: nothing is marked
        upstream.now = allWell;
        await page.goto("http://uni-switch.test/?view=providers");
        await page.waitForSelector("#providers .row.provider[data-id=anthropic]");
        await page.waitForTimeout(300);
        assert.equal(await page.locator(".badge.upstream").count(), 0);
        assert.deepEqual(errors, []);
      });
    }
  }
}

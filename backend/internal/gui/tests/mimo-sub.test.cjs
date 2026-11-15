// Run with Node's test runner and Playwright on the module path; see README.md.
// A Xiaomi MiMo subscription (#249: support MiMo Desktop): the add sheet
// lists Xiaomi MiMo under Subscriptions with MiMo's logo, and starting its
// sign-in first says what Xiaomi may do about it, in Xiaomi's words rather
// than Zed's or Antigravity's; nothing is asked of the backend until "Sign in
// anyway", which asks for {agent:"mimo-app"}. A signed-in account reads as
// "Xiaomi MiMo" and its plan. English and Chinese; no backend, the API is
// faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function server(lang, asked) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [{ id: "openai", name: "OpenAI", icon: "openai", preset: "openai", models: [], agents: [], key: { set: true, masked: "sk-…ab12" } }], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname === "/api/signin") {
      asked.push(route.request().postDataJSON());
      return json({ id: "s1", agent: "mimo-app", state: "waiting", url: "https://account.xiaomi.com/longPolling/login?ticket=1" });
    }
    if (url.pathname.startsWith("/api/signin/")) return json({ id: "s1", agent: "mimo-app", state: "waiting" });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const L = {
  en: { title: "Xiaomi MiMo accounts can be suspended", note: /Xiaomi serves these models to its own MiMo app/, anyway: "Sign in anyway", plan: "Xiaomi MiMo Free" },
  zh: { title: "Xiaomi MiMo 账号可能被封禁", note: /小米只向自己的 MiMo 应用提供这些模型/, anyway: "仍然登录", plan: "Xiaomi MiMo 免费" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a Xiaomi MiMo subscription", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh"]) {
      await t.test(lang, async () => {
        const w = L[lang];
        const page = await (await browser.newContext({ viewport: { width: 900, height: 800 } })).newPage();
        const errors = [], asked = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, asked));
        await page.goto("http://uni-switch.test/?view=providers");
        await page.locator("#addProvider").click();
        const sheet = page.locator("#addSheet");
        const row = sheet.locator('.tile[data-pick="Xiaomi MiMo"]');
        await row.waitFor();
        assert.match(await row.getAttribute("title"), /Free · Starter · Plus · Pro · Ultra/);
        // MiMo's own logo, its SVG as the mask
        assert.equal(await row.locator(".ic").getAttribute("data-icon"), "mimocode");
        assert.match(await row.locator(".ic .mask").evaluate((m) => m.style.getPropertyValue("--i")), /icons\/mimocode\.svg/);
        const logo = await page.evaluate(async () => { const r = await fetch("icons/mimocode.svg"); return [r.status, await r.text()]; });
        assert.equal(logo[0], 200);
        assert.match(logo[1], /<svg/);

        // a signed-in account's plan, in the page's language
        assert.equal(await page.evaluate(() => accountPlan({ agent: "mimo-app", plan: "Free" })), w.plan);

        const top = await page.evaluate(() => window.scrollY);
        await row.click();
        const box = sheet.locator(".signing");
        await box.waitFor();
        assert.equal(await box.locator(".n").textContent(), w.title);
        assert.match(await box.locator(".s").textContent(), w.note);
        assert.doesNotMatch(await box.textContent(), /Antigravity|Google|Zed/);
        assert.equal(asked.length, 0, "nothing asked before it is said");
        assert.equal(await page.evaluate(() => window.scrollY), top, "the click scrolls nothing");

        await box.locator("button", { hasText: w.anyway }).click();
        await page.waitForFunction(() => document.querySelector("#addSheet .signing")?.textContent.length > 0);
        await assert.doesNotReject(async () => { for (let i = 0; i < 50 && !asked.length; i++) await page.waitForTimeout(50); });
        assert.deepEqual(asked, [{ agent: "mimo-app" }]);
        assert.deepEqual(errors, []);
      });
    }
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// A Zed subscription (yosocoli5 on X: bought Zed, can it be added?): the add
// sheet lists Zed under Subscriptions with its own logo, and starting its
// sign-in first says what Zed may do about it, in Zed's words rather than
// Antigravity's; nothing is asked of the backend until "Sign in anyway".
// English and Chinese; no backend, the API is faked here.
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
      return json({ id: "s1", agent: "zed", state: "waiting", url: "https://zed.dev/native_app_signin?native_app_port=1" });
    }
    if (url.pathname.startsWith("/api/signin/")) return json({ id: "s1", agent: "zed", state: "waiting" });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const L = {
  en: { title: "Zed accounts can be suspended", note: /Zed serves these models to its own editor/, anyway: "Sign in anyway" },
  zh: { title: "Zed 账号可能被封禁", note: /Zed 只向自己的编辑器提供这些模型/, anyway: "仍然登录" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a Zed subscription", async (t) => {
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
        const row = sheet.locator('.tile[data-pick="Zed"]');
        await row.waitFor();
        assert.match(await row.getAttribute("title"), /Pro · Student · Business/);
        // Zed's own logo, its SVG as the mask
        assert.equal(await row.locator(".ic").getAttribute("data-icon"), "zed");
        assert.match(await row.locator(".ic .mask").evaluate((m) => m.style.getPropertyValue("--i")), /icons\/zed\.svg/);
        const logo = await page.evaluate(async () => { const r = await fetch("icons/zed.svg"); return [r.status, await r.text()]; });
        assert.equal(logo[0], 200);
        assert.match(logo[1], /<svg[^>]*viewBox="0 0 96 96"/);

        await row.click();
        const box = sheet.locator(".signing");
        await box.waitFor();
        assert.equal(await box.locator(".n").textContent(), w.title);
        assert.match(await box.locator(".s").textContent(), w.note);
        assert.doesNotMatch(await box.textContent(), /Antigravity|Google/);
        assert.equal(asked.length, 0, "nothing asked before it is said");

        await box.locator("button", { hasText: w.anyway }).click();
        await page.waitForFunction(() => document.querySelector("#addSheet .signing")?.textContent.length > 0);
        await assert.doesNotReject(async () => { for (let i = 0; i < 50 && !asked.length; i++) await page.waitForTimeout(50); });
        assert.deepEqual(asked, [{ agent: "zed" }]);
        assert.deepEqual(errors, []);
      });
    }
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// A Factory subscription (#242: add Factory's Droid account): the add sheet
// lists Factory under Subscriptions with its own logo, and starting its
// sign-in first says what Factory may do about it; nothing is asked of the
// backend until "Sign in anyway". Then the device code WorkOS gave is shown
// to be checked on Factory's page, not GitHub's. Nothing scrolls.
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
    const signing = { id: "s1", agent: "factory", state: "waiting", url: "https://factory.example/device?code=WDJB-MJHT", code: "WDJB-MJHT" };
    if (url.pathname === "/api/signin") {
      asked.push(route.request().postDataJSON());
      return json(signing);
    }
    if (url.pathname.startsWith("/api/signin/")) return json(signing);
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const L = {
  en: { title: "Factory accounts can be suspended", note: /Factory serves these models to its own Droid CLI/, anyway: "Sign in anyway", device: /Factory's sign-in page/ },
  zh: { title: "Factory 账号可能被封禁", note: /Factory 只向自己的 Droid CLI 提供这些模型/, anyway: "仍然登录", device: /Factory 登录页/ },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a Factory subscription", async (t) => {
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
        const row = sheet.locator('.tile[data-pick="Factory"]');
        await row.waitFor();
        assert.match(await row.getAttribute("title"), /Pro · Plus · Max/);
        // Factory's own logo, its SVG as the mask
        assert.equal(await row.locator(".ic").getAttribute("data-icon"), "factory");
        assert.match(await row.locator(".ic .mask").evaluate((m) => m.style.getPropertyValue("--i")), /icons\/factory\.svg/);
        const logo = await page.evaluate(async () => { const r = await fetch("icons/factory.svg"); return r.status; });
        assert.equal(logo, 200);

        const scrolls = () => page.evaluate(() => [window.scrollY, ...[...document.querySelectorAll("#addSheet, #addSheet *")].filter((e) => e.scrollTop).map((e) => e.scrollTop)]);
        const before = await scrolls();
        await row.click();
        const box = sheet.locator(".signing");
        await box.waitFor();
        assert.equal(await box.locator(".n").textContent(), w.title);
        assert.match(await box.locator(".s").textContent(), w.note);
        assert.equal(asked.length, 0, "nothing asked before it is said");

        await box.locator("button", { hasText: w.anyway }).click();
        const code = box.locator(".devcode code");
        await code.waitFor();
        assert.deepEqual(asked, [{ agent: "factory" }]);
        assert.equal(await code.textContent(), "WDJB-MJHT");
        assert.match(await box.locator(".s").first().textContent(), w.device);
        assert.doesNotMatch(await box.textContent(), /GitHub/);
        assert.deepEqual(await scrolls(), before, "nothing scrolled");
        assert.deepEqual(errors, []);
      });
    }
  });
}

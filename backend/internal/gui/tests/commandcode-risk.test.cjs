// Run with Node's test runner and Playwright on the module path; see README.md.
// Command Code's subscription (Discord: Command Code said on X that its
// private protocol used from other tools may get an account banned): starting
// its sign-in first says so, as Antigravity's and Zed's do — a Go plan goes
// through Command Code's private interface, Pro/Max through its Provider API —
// and nothing is asked of the backend until "Sign in anyway". The click moves
// nothing on the page. English and Chinese; no backend, the API is faked here.
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
      return json({ id: "s1", agent: "commandcode-plan", state: "waiting", url: "https://commandcode.ai/login" });
    }
    if (url.pathname.startsWith("/api/signin/")) return json({ id: "s1", agent: "commandcode-plan", state: "waiting" });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const L = {
  en: { title: "Command Code accounts can be suspended", note: /^A Go plan account is used through Command Code's private interface, which Command Code may treat as a breach of its terms and ban the account for\. Pro, Max and the other plans use its Provider API\./, anyway: "Sign in anyway", cancel: "Cancel" },
  zh: { title: "Command Code 账号可能被封禁", note: /^Go 套餐账号经 Command Code 的私有接口使用，Command Code 可能视为违反条款并封禁该账号。Pro、Max 等其他套餐走其 Provider API。/, anyway: "仍然登录", cancel: "取消" },
};

// where everything that can scroll stands
const scrolls = (page) => page.evaluate(() => [window.scrollX, window.scrollY, ...[...document.querySelectorAll("*")].filter((e) => e.scrollTop || e.scrollLeft).map((e) => e.scrollTop + "," + e.scrollLeft)].join("|"));

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": Command Code's sign-in warns of a ban first", async (t) => {
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
        const row = sheet.locator('.tile[data-pick="Command Code"]');
        await row.waitFor();
        assert.match(await row.getAttribute("title"), /Go · Pro · GOAT · Max · Ultra/);

        // Cancel leaves it unasked
        await row.click();
        let box = sheet.locator(".signing");
        await box.waitFor();
        await box.locator("button", { hasText: w.cancel }).click();
        await box.waitFor({ state: "detached" });
        assert.equal(asked.length, 0);

        const before = await scrolls(page);
        await row.click();
        box = sheet.locator(".signing");
        await box.waitFor();
        assert.equal(await scrolls(page), before, "the click moved nothing");
        assert.equal(await box.locator(".n").textContent(), w.title);
        assert.match(await box.locator(".s").textContent(), w.note);
        assert.doesNotMatch(await box.textContent(), /Antigravity|Google/);
        // a dot marks it, not a coloured stripe down its left side
        const edge = await box.evaluate((b) => { const s = getComputedStyle(b); return [s.borderLeftWidth, s.borderLeftColor, s.borderRightColor]; });
        assert.ok(edge[0] === "0px" || edge[1] === edge[2], "no left-border accent: " + edge);
        assert.equal(asked.length, 0, "nothing asked before it is said");

        await box.locator("button", { hasText: w.anyway }).click();
        for (let i = 0; i < 50 && !asked.length; i++) await page.waitForTimeout(50);
        assert.deepEqual(asked, [{ agent: "commandcode-plan" }]);
        assert.deepEqual(errors, []);
      });
    }
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// #933: OpenAI's Sign in with ChatGPT is a subscription of its own in the
// add sheet, "ChatGPT API" beside Codex's "ChatGPT" — two rows that read
// apart — its title saying what it is, and its row starts that sign-in
// (agent chatgpt-api) and none of Codex's. Its account is named by its plan
// as a ChatGPT one is. English and Chinese; no backend, the API is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const anthropic = { id: "anthropic", name: "Anthropic", icon: "claude", preset: "anthropic", models: [], agents: [], key: { set: true, masked: "sk-…ab12" } };

function server(lang, list, posted) {
  return async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: list, presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname === "/api/signin") {
      posted.push(JSON.parse(req.postData() || "{}"));
      return json({ id: "s1", agent: "chatgpt-api", state: "waiting", url: "https://auth.openai.com/api/accounts/authorize?client_id=dynamic_agent_client" });
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const L = {
  en: { hint: /your plan on OpenAI's own API, with your agent's own instructions rather than Codex's/ },
  zh: { hint: /通过 OpenAI 官方 API 使用你的套餐/ },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": Sign in with ChatGPT is its own subscription", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh"]) {
      await t.test(lang, async () => {
        const posted = [];
        const page = await (await browser.newContext({ viewport: { width: 900, height: 800 } })).newPage();
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, [anthropic], posted));
        await page.goto("http://uni-switch.test/?view=providers");
        await page.locator("#addProvider").click();
        const sheet = page.locator("#addSheet");
        await sheet.locator(".tile").first().waitFor();
        const row = (name) => sheet.locator(".tile").filter({ has: page.locator(".n", { hasText: new RegExp("^" + name + "$") }) });
        assert.equal(await row("ChatGPT").count(), 1, "Codex's row");
        assert.equal(await row("ChatGPT API").count(), 1, "Sign in with ChatGPT's row");
        assert.match(await row("ChatGPT API").getAttribute("title"), L[lang].hint);
        assert.doesNotMatch(await row("ChatGPT").getAttribute("title"), L[lang].hint);
        const y = await page.evaluate(() => scrollY);
        await row("ChatGPT API").click();
        for (let i = 0; i < 50 && !posted.length; i++) await page.waitForTimeout(20);
        assert.deepEqual(posted, [{ agent: "chatgpt-api" }]);
        assert.equal(await page.evaluate(() => scrollY), y, "the click doesn't scroll");
        assert.deepEqual(errors, []);
        await page.context().close();
      });
    }
    // its account named by its plan, as a ChatGPT one is
    await t.test("account name", async () => {
      const list = [{ id: "chatgpt-api", name: "ChatGPT API", icon: "openai", models: [], agents: [],
        account: { agent: "chatgpt-api", user: "me@example.com", plan: "plus", logins: [{ user: "me@example.com", plan: "plus", active: true, on: true }] } }];
      const page = await (await browser.newContext({ viewport: { width: 900, height: 800 } })).newPage();
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server("en", list, []));
      await page.goto("http://uni-switch.test/?view=providers");
      const r = page.locator('#providers .row[data-id="chatgpt-api"]');
      await r.waitFor();
      assert.match(await r.textContent(), /ChatGPT Plus/);
      assert.deepEqual(errors, []);
      await page.context().close();
    });
  });
}

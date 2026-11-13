// Run with Node's test runner and Playwright on the module path; see README.md.
// A ChatGPT account whose sign-in OpenAI revoked (#1793, ysicing: 401
// token_revoked on every request, the account asked again each time) reads
// as signed out on its own row once uni-switch marks it (signin_revoked.go):
// "Sign-in required", the reason on its allowance line in the user's
// language, with Sign in again, which runs Codex's sign-in for that
// account, and Remove. The card's pill says Signed out once every account
// in use is. An account its vendor still serves has neither.
// Chromium and WebKit, 420 and 1100 wide, in every language.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const lapse = (user) => `${user}'s sign-in was revoked; sign in again`;

const codex = (logins) => ({
  id: "codex", name: "Codex", icon: "openai", chat: "", responses: "https://chatgpt.com/backend-api/codex", anthropic: "", catalog: "",
  models: [{ id: "gpt-5.5", name: "GPT-5.5", on: true }], agents: [], fallback: [], headers: {}, keyList: [], key: {},
  account: { agent: "codex", agentName: "Codex", agentIcon: "openai", builtin: true, user: logins[0].user, plan: "Plus", logins },
});

function serve(lang, logins, posts) {
  const providers = () => ({ providers: [codex(logins)], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [], onPlugins: [] });
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers());
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/login/usage") return json({});
    if (route.request().method() === "POST") {
      const body = route.request().postDataJSON();
      posts.push({ path: url.pathname, body });
      if (url.pathname === "/api/signin") return json({ id: "s1", agent: "codex", state: "waiting", url: "https://auth.openai.test/authorize" });
      return json(providers());
    }
    if (url.pathname.startsWith("/api/signin/")) return json({ id: "s1", agent: "codex", state: "waiting", url: "https://auth.openai.test/authorize" });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const W = {
  en: { required: "Sign-in required", again: "Sign in again", remove: "Remove", out: "Signed out", reason: "The vendor revoked this sign-in; sign in again" },
  zh: { required: "需要重新登录", again: "重新登录", remove: "移除", out: "已退出登录", reason: "服务商已撤销这个登录，请重新登录" },
  "zh-TW": { required: "需要重新登入", again: "重新登入", remove: "移除", out: "已退出登入", reason: "服務商已撤銷這個登入，請重新登入" },
  ja: { required: "サインインが必要です", again: "再度サインイン", remove: "削除", out: "サインアウトしました", reason: "ベンダーがこのサインインを取り消しました。再度サインインしてください" },
  de: { required: "Anmeldung erforderlich", again: "Erneut anmelden", remove: "Entfernen", out: "Abgemeldet", reason: "Der Anbieter hat diese Anmeldung widerrufen; bitte erneut anmelden" },
};

const cases = [
  { name: "the only account (the report)", user: "me@example.com", logins: [{ user: "me@example.com", plan: "Plus", active: true, on: true, lapsed: lapse("me@example.com") }], pill: true },
  { name: "one of two in use", user: "spare@example.com", logins: [{ user: "me@example.com", plan: "Plus", active: true, on: true }, { user: "spare@example.com", plan: "Pro", on: true, lapsed: lapse("spare@example.com") }], pill: false },
];

const overlap = (a, b) => a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height;

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": an account whose sign-in its vendor revoked reads as signed out and signs in again", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of Object.keys(W)) {
      const w = W[lang];
      for (const width of [420, 1100]) {
        const open = async (logins, posts) => {
          const page = await (await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" })).newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          const errors = [];
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", serve(lang, logins, posts));
          await page.goto("http://uni-switch.test/?view=providers");
          const card = page.locator(".row.provider", { hasText: "Codex" });
          await card.waitFor();
          return { page, errors, card };
        };
        for (const c of cases) {
          await t.test(`${lang} ${width}: ${c.name}`, async () => {
            const posts = [];
            const { page, errors, card } = await open(c.logins, posts);
            const pill = await card.locator(".key").innerText();
            if (c.pill) assert.equal(pill, w.out, "every account in use revoked: the card says signed out");
            else assert.notEqual(pill, w.out, "another account still in use: the card isn't signed out");
            await card.click();
            const row = page.locator(".accts .acc", { hasText: c.user }).filter({ has: page.locator(".using", { hasText: w.required }) });
            await row.waitFor();
            const again = row.getByRole("button", { name: w.again, exact: true });
            const rm = row.getByRole("button", { name: w.remove, exact: true });
            assert.equal(await again.count(), 1, "Sign in again is on the row");
            assert.equal(await rm.count(), 1, "Remove is on the row");
            const aq = row.locator(".aq .aq-none");
            await aq.waitFor();
            assert.equal(await aq.innerText(), w.reason);
            const box = await row.boundingBox(), line = await aq.boundingBox();
            for (const b of [again, rm]) {
              await b.scrollIntoViewIfNeeded();
              assert.ok(await b.isVisible());
              const r = await b.boundingBox();
              assert.ok(!overlap(r, line), `${await b.innerText()} is under the reason`);
              assert.ok(r.x >= box.x - 1 && r.x + r.width <= box.x + box.width + 1, `${await b.innerText()} runs out of the row`);
            }
            const docW = await page.evaluate(() => document.documentElement.scrollWidth);
            assert.ok(docW <= width, `the page scrolls sideways (${docW} > ${width})`);
            if (process.env.ARTIFACT_DIR) await row.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `revoked-signin-${engine}-${lang}-${width}-${c.user.replace(/[@.]/g, "_")}.png`) });

            // Sign in again is Codex's sign-in of this account
            const y = await page.evaluate(() => document.scrollingElement.scrollTop);
            await again.click();
            for (let i = 0; i < 50 && !posts.some((p) => p.path === "/api/signin"); i++) await page.waitForTimeout(50);
            assert.deepEqual(posts.filter((p) => p.path === "/api/signin").map((p) => { const { proxy, ...b } = p.body; return b; }), [{ agent: "codex", user: c.user }]);
            assert.equal(await page.evaluate(() => document.scrollingElement.scrollTop), y, "the click scrolled the page");
            assert.deepEqual(errors, []);
            await page.context().close();
          });
        }

        await t.test(`${lang} ${width}: an account its vendor serves has no Sign in again`, async () => {
          const posts = [];
          const { page, errors, card } = await open([{ user: "me@example.com", plan: "Plus", active: true, on: true }], posts);
          assert.notEqual(await card.locator(".key").innerText(), w.out);
          await card.click();
          const row = page.locator(".accts .acc", { hasText: "me@example.com" });
          await row.waitFor();
          assert.equal(await row.getByRole("button", { name: w.again, exact: true }).count(), 0);
          assert.equal(await row.locator(".using", { hasText: w.required }).count(), 0);
          assert.deepEqual(errors, []);
          await page.context().close();
        });
      }
    }
  });
}

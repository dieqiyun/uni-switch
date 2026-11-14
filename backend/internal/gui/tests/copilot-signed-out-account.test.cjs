// Run with Node's test runner and Playwright on the module path; see README.md.
// A built-in Copilot account whose GitHub token GitHub refused (echo_ts on
// Discord: a revoked token, every request "Copilot is signed out …; sign in
// to Copilot again", the account to be removed and added anew) reads as
// signed out on its own row — "Sign-in required", the reason on its
// allowance line in the user's language — with Sign in again, which runs
// Copilot's own sign-in for that account (on its enterprise's host for a
// GHE.com one), and Remove. The card's pill says Signed out once every
// account in use is. A Copilot account GitHub still takes has neither.
// Chromium and WebKit, 420 and 1100 wide, in every language.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const LAPSE = "GitHub refused this Copilot sign-in; sign in again";

const copilot = (logins) => ({
  id: "copilot", name: "GitHub Copilot", icon: "githubcopilot", chat: "https://api.githubcopilot.com", responses: "", anthropic: "", catalog: "",
  models: [{ id: "gpt-4.1", name: "GPT-4.1", on: true }], agents: [], fallback: [], headers: {}, keyList: [], key: {},
  account: { agent: "copilot", agentName: "Copilot", agentIcon: "githubcopilot", builtin: true, user: logins[0].user, plan: "Pro", logins },
});

function serve(lang, logins, posts) {
  const providers = () => ({ providers: [copilot(logins)], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [], onPlugins: [] });
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
      if (url.pathname === "/api/signin") return json({ id: "s1", agent: "copilot", state: "waiting", url: "https://github.test/login/device", code: "ABCD-1234" });
      return json(providers());
    }
    if (url.pathname.startsWith("/api/signin/")) return json({ id: "s1", agent: "copilot", state: "waiting", url: "https://github.test/login/device", code: "ABCD-1234" });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const W = {
  en: { required: "Sign-in required", again: "Sign in again", remove: "Remove", out: "Signed out", reason: LAPSE },
  zh: { required: "需要重新登录", again: "重新登录", remove: "移除", out: "已退出登录", reason: "GitHub 拒绝了这个 Copilot 登录，请重新登录" },
  "zh-TW": { required: "需要重新登入", again: "重新登入", remove: "移除", out: "已退出登入", reason: "GitHub 拒絕了這個 Copilot 登入，請重新登入" },
  ja: { required: "サインインが必要です", again: "再度サインイン", remove: "削除", out: "サインアウトしました", reason: "GitHub がこの Copilot のサインインを拒否しました。再度サインインしてください" },
  de: { required: "Anmeldung erforderlich", again: "Erneut anmelden", remove: "Entfernen", out: "Abgemeldet", reason: "GitHub hat diese Copilot-Anmeldung abgelehnt; bitte erneut anmelden" },
};

const cases = [
  { name: "the only account, on github.com (the report)", user: "octocat", logins: [{ user: "octocat", plan: "Pro", active: true, on: true, lapsed: LAPSE }], body: { agent: "copilot", user: "octocat" }, pill: true },
  { name: "one of two, on an enterprise's GHE.com", user: "octo@acme.ghe.com", logins: [{ user: "hubot", plan: "Pro", active: true, on: true }, { user: "octo@acme.ghe.com", plan: "Business", on: true, lapsed: LAPSE }], body: { agent: "copilot", site: "acme.ghe.com", user: "octo@acme.ghe.com" }, pill: false },
  { name: "the editors' own", user: "octocat", logins: [{ user: "octocat", plan: "Pro", active: true, on: true, own: true, lapsed: LAPSE }], body: { agent: "copilot", user: "octocat" }, pill: true },
];

const overlap = (a, b) => a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height;

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a Copilot account GitHub refused reads as signed out and signs in again", async (t) => {
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
          const card = page.locator(".row.provider", { hasText: "GitHub Copilot" });
          await card.waitFor();
          return { page, errors, card };
        };
        for (const c of cases) {
          await t.test(`${lang} ${width}: ${c.name}`, async () => {
            const posts = [];
            const { page, errors, card } = await open(c.logins, posts);
            const pill = await card.locator(".key").innerText();
            if (c.pill) assert.equal(pill, w.out, "every account in use refused: the card says signed out");
            else assert.notEqual(pill, w.out, "another account still in use: the card isn't signed out");
            await card.click();
            const row = page.locator(".accts .acc", { hasText: c.user }).filter({ has: page.locator(".using", { hasText: w.required }) });
            await row.waitFor();
            const again = row.getByRole("button", { name: w.again, exact: true });
            const rm = row.getByRole("button", { name: w.remove, exact: true });
            assert.equal(await again.count(), 1, "Sign in again is on the row");
            assert.equal(await rm.count(), 1, "Remove is on the row");
            // the reason, in the user's language, below the controls
            const aq = row.locator(".aq .aq-none");
            await aq.waitFor();
            assert.equal(await aq.innerText(), w.reason);
            // read once the row has stopped being drawn again: what comes in
            // after the editor opens draws the accounts again, and a row
            // looked at across that is gone (null boxes, "not attached")
            for (let i = 0; i < 20; i++) {
              await row.evaluate((e) => { e.dataset.seen = "1"; });
              await page.waitForTimeout(250);
              if (await row.evaluate((e) => e.dataset.seen === "1").catch(() => false)) break;
            }
            const boxes = async (b) => {
              for (let i = 0; i < 40; i++) {
                const got = [await row.boundingBox(), await aq.boundingBox(), await b.boundingBox()];
                if (got.every(Boolean)) return got;
                await page.waitForTimeout(50);
              }
              assert.fail(`${await b.innerText()}, its row or the reason never laid out`);
            };
            for (const b of [again, rm]) {
              await b.scrollIntoViewIfNeeded();
              assert.ok(await b.isVisible());
              const [box, line, r] = await boxes(b);
              assert.ok(!overlap(r, line), `${await b.innerText()} is under the reason`);
              assert.ok(r.x >= box.x - 1 && r.x + r.width <= box.x + box.width + 1, `${await b.innerText()} runs out of the row`);
            }
            const docW = await page.evaluate(() => document.documentElement.scrollWidth);
            assert.ok(docW <= width, `the page scrolls sideways (${docW} > ${width})`);
            if (process.env.ARTIFACT_DIR) await row.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `copilot-signed-out-${engine}-${lang}-${width}-${c.user.replace(/[@.]/g, "_")}${c.logins[0].own ? "-own" : ""}.png`) });

            // Sign in again is Copilot's own sign-in of this account, on its host
            const y = await page.evaluate(() => document.scrollingElement.scrollTop);
            await again.click();
            for (let i = 0; i < 50 && !posts.some((p) => p.path === "/api/signin"); i++) await page.waitForTimeout(50);
            assert.deepEqual(posts.filter((p) => p.path === "/api/signin").map((p) => { const { proxy, ...b } = p.body; return b; }), [c.body]);
            await page.locator(".signing").first().waitFor();
            assert.equal(await page.evaluate(() => document.scrollingElement.scrollTop), y, "the click scrolled the page");
            assert.deepEqual(errors, []);
            await page.context().close();
          });
        }

        await t.test(`${lang} ${width}: an account GitHub takes has no Sign in again`, async () => {
          const posts = [];
          const { page, errors, card } = await open([{ user: "octocat", plan: "Pro", active: true, on: true }], posts);
          assert.notEqual(await card.locator(".key").innerText(), w.out);
          await card.click();
          const row = page.locator(".accts .acc", { hasText: "octocat" });
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

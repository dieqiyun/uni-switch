// Run with Node's test runner and Playwright on the module path; see README.md.
// The owner, on a provider editor's accounts: 在这里也要加上手动刷新用量的按钮。
// Each signed-in account has a refresh after its plan, shown, not on hover
// only: it reads that account's card again (provider and user, as the
// Usage page's refresh does), and its allowance line then shows the new
// reading. One that can't be read has it too; a signed-out one has none.
// It is on the account's own line, which never wraps: at the end of the
// readings it added a line where the meters filled it (window-cap, WebKit
// 1100), and WebKit drew that line over the next row until a click.
// In every language, wide, narrow and in a phone's browser, both engines.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(process.env.UNI_SWITCH_GUI_ASSETS || path.resolve(__dirname, "../assets"));
const claude = () => ({
  id: "claude", name: "Claude Code", icon: "claude-color", chat: "", responses: "", anthropic: "", catalog: "", routing: "",
  models: [{ id: "claude-opus-5-5", name: "Claude Opus 5.5", on: true }], agents: [], fallback: [], headers: {}, keyList: [],
  account: {
    agent: "claude", agentName: "Claude Code", user: "first@example.com", plan: "team",
    logins: [
      { user: "first@example.com", plan: "team", active: true, on: true },
      { user: "spare@example.com", plan: "max", on: true },
      { user: "gone@example.com", plan: "pro", on: true, lapsed: "Sign-in has expired" },
    ],
  },
});
const serve = (lang, web, ctl) => async (route) => {
  const url = new URL(route.request().url());
  const json = (data) => route.fulfill({ json: data });
  if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:${web}};` });
  if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
  if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
  if (url.pathname === "/api/providers") return json({ providers: [claude()], presets: [], excluded: [], gateway: { running: true, window: true } });
  if (url.pathname === "/api/groups") return json({ groups: [] });
  if (url.pathname === "/api/plugins") return json({ plugins: [] });
  if (url.pathname === "/api/login/usage") return json({
    "first@example.com": { windows: [{ name: "5h", used: ctl.used, limit: 100 }] },
    "spare@example.com": { error: "unavailable" },
  });
  if (url.pathname === "/api/usage/quotas/refresh") {
    ctl.asked.push(url.searchParams.get("provider") + " " + url.searchParams.get("user"));
    ctl.used = 64;
    return json([{ provider: "claude", user: "first@example.com", windows: [{ name: "5h", used: 64, limit: 100 }] }]);
  }
  if (url.pathname.startsWith("/api/")) return json({});
  const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
  const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
  try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
};

for (const [engine, browserType] of [["chromium", chromium], ["webkit", webkit]]) {
  test(engine + ": each account's allowance has a refresh that reads it again", async (t) => {
    const browser = await browserType.launch();
    t.after(() => browser.close());
    for (const [web, width] of [[false, 1000], [false, 560], [true, 390]]) for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      const at = `${lang} ${web ? "web " : ""}${width}`;
      const ctl = { used: 12, asked: [] };
      const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, web, ctl));
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator(".row.provider", { hasText: "Claude Code" }).first().click();
      const row = (u) => page.locator(`.editor .accts .acc[data-account-id="${u}"]`);
      await row("first@example.com").locator(".aq-w").waitFor();
      await page.mouse.move(1, 1);

      // shown without a hover, after the plan on the account's line, inside the row
      const tip = await page.evaluate((lang) => lang === "en" ? "Read this account's usage again" : I18N[lang]?.["Read this account's usage again"], lang);
      assert.ok(tip, `${at}: the refresh's tip has no ${lang}`);
      for (const u of ["first@example.com", "spare@example.com"]) {
        const b = row(u).locator(".acc-line > .plan + .quota-refresh");
        assert.equal(await b.count(), 1, `${at}: ${u} has no refresh`);
        assert.equal(await b.getAttribute("title"), tip);
        const box = await b.boundingBox(), aq = await row(u).locator(".acc-line").boundingBox(), r = await row(u).boundingBox();
        assert.ok(box && box.width >= 10 && +(await b.evaluate((x) => getComputedStyle(x).opacity)) === 1, `${at}: ${u}'s refresh is not shown`);
        assert.ok(box.y >= aq.y - 1 && box.y + box.height <= aq.y + aq.height + 1, `${at}: ${u}'s refresh is off its line`);
        assert.ok(box.x + box.width <= r.x + r.width + 1, `${at}: ${u}'s refresh is out of its row`);
        // beside the plan
        const plan = await row(u).locator(".acc-line > .plan").boundingBox();
        assert.ok(box.y < plan.y + plan.height && box.y + box.height > plan.y && box.x >= plan.x + plan.width && box.x - plan.x - plan.width <= 8, `${at}: ${u}'s refresh is not beside its plan`);
      }
      assert.equal(await row("gone@example.com").locator(".quota-refresh").count(), 0, `${at}: a signed-out account has a refresh`);

      // it reads that account's card again, and the line shows the new reading
      await row("first@example.com").locator(".quota-refresh").click();
      await page.waitForFunction(() => document.querySelector('.editor .acc[data-account-id="first@example.com"] .aq-w b')?.textContent.includes("64"));
      assert.deepEqual(ctl.asked, ["claude first@example.com"]);
      await page.waitForFunction(() => !document.querySelector(".editor .quota-refresh.busy"));
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1), true, `${at}: the page scrolls sideways`);
      await page.screenshot({ path: path.join(process.env.SHOTS || "/tmp", `account-refresh-${engine}-${lang}-${width}.png`) }).catch(() => {});
      assert.deepEqual(errors, []);
      await context.close();
    }
  });
}

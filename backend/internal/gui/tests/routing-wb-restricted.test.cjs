// Run with Node's test runner and Playwright on the module path; see README.md.
// WorkBuddy's 403 11140 "request illegal" for an account it restricted
// (#1849): the Routing page tells the try as the account restricted, not
// "failed", says it rests two hours because only the vendor lifting the
// restriction brings it back, after an appeal; gives uni-switch's hint apart
// from the vendor's words; and the resting account's row lifts its rest by
// hand once the appeal went through — in every language the page speaks.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { reach } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const start = new Date(now.getTime() - 60e3).toISOString();
const hint = "WorkBuddy restricted this account for unusual activity; it stays out until WorkBuddy lifts that, which an appeal (Submit feedback in WorkBuddy) asks for";
const vendor = "WorkBuddy AI: request illegal";
const a = { id: "workbuddy-ai#a", provider: "workbuddy-ai", name: "WorkBuddy AI", kind: "account", who: "a@x", model: "deepseek-v4.1-flash" };
const b = { id: "workbuddy-ai#b", provider: "workbuddy-ai", name: "WorkBuddy AI", kind: "account", who: "b@x", model: "deepseek-v4.1-flash" };
const rest = { why: "restricted", by: "restricted", status: 403, key: "workbuddy-ai@a", until: new Date(now.getTime() + 2 * 3600e3 - 60e3).toISOString() };
const routes = [{
  id: 100, seq: 100, time: start, agent: "codex", model: "workbuddy-ai/deepseek-v4.1-flash", provider: "workbuddy-ai",
  order: [a, b],
  tries: [
    { id: a.id, model: a.model, start, done: true, status: 403, ms: 900, fail: "restricted", rest, error: vendor + " — " + hint },
    { id: b.id, model: b.model, start, done: true, status: 200, ms: 1200 },
  ],
  done: true, status: 200, ms: 2100,
}];

function serve(lang, lifted) {
  const state = { agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) await new Promise((r) => setTimeout(r, 20e3));
      return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname === "/api/gateway/history") {
      const d = url.searchParams.get("day");
      return json({ cut: false, days: [{ day, requests: routes.length }], routes: d ? routes : [] });
    }
    if (url.pathname === "/api/gateway/unrest") {
      lifted.push(JSON.parse(route.request().postData() || "{}"));
      return json({ lifted: true });
    }
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const want = {
  en: { label: "account restricted by the vendor", why: "only the vendor lifting the restriction brings it back. Appeal with the vendor", hint, lift: "The restriction is lifted — try it again" },
  zh: { label: "账号被服务商限制", why: "只有服务商解除限制才能让它恢复。请向服务商申诉", hint: "WorkBuddy 因异常行为限制了这个账号；在 WorkBuddy 解除之前它不会再被使用，可在 WorkBuddy 中提交反馈申诉", lift: "限制已解除，重新使用" },
  "zh-TW": { label: "帳號被服務商限制", why: "只有服務商解除限制才能讓它恢復。請向服務商申訴", hint: "WorkBuddy 因異常行為限制了這個帳號；在 WorkBuddy 解除之前它不會再被使用，可在 WorkBuddy 中提交回饋申訴", lift: "限制已解除，重新使用" },
  ja: { label: "ベンダーがアカウントを制限", why: "制限を解除できるのはベンダーだけなので", hint: "WorkBuddy が不審なアクティビティを理由にこのアカウントを制限しました。WorkBuddy が解除するまで使われません。解除は WorkBuddy のフィードバック送信から申し立てられます", lift: "制限が解除された — もう一度試す" },
  de: { label: "Konto vom Anbieter gesperrt", why: "denn nur die Aufhebung der Sperre durch den Anbieter bringt es zurück. Legen Sie beim Anbieter Einspruch ein", hint: "WorkBuddy hat dieses Konto wegen ungewöhnlicher Aktivität gesperrt; es bleibt außen vor, bis WorkBuddy die Sperre aufhebt, was ein Einspruch (Feedback senden in WorkBuddy) beantragt", lift: "Die Sperre ist aufgehoben – erneut versuchen" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(want)) {
    for (const width of [1100, 420]) {
      test(`${engine} ${lang} ${width}px: a restricted WorkBuddy account is told as one, with the way back`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], lifted = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, lifted));
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-wb-restricted.png`), fullPage: true });
          }
          await browser.close();
        });
        await page.goto("http://uni-switch.test/?view=routing");
        await page.locator(".rt-days .rt-day").nth(1).click();
        await page.locator(".rt-req").first().waitFor();
        await page.locator(".rt-req").first().click();
        await page.locator(".rt-steps li").first().waitFor();
        await page.waitForTimeout(300);
        const w = want[lang];
        const steps = await page.locator(".rt-steps li").evaluateAll((ls) => ls.map((l) => [l.className, l.textContent]));
        // the try: the label, and why it rests as long as it does
        const told = steps.map(([, s]) => s).filter((s) => s.includes(w.label));
        assert.equal(told.length, 1, JSON.stringify(steps));
        assert(told[0].includes(w.why), told[0]);
        // the vendor's words as they were, and uni-switch's hint on its own line
        assert.equal(steps.filter(([c]) => c === "aside said").length, 1, JSON.stringify(steps));
        assert(steps.find(([c]) => c === "aside said")[1].endsWith(vendor), JSON.stringify(steps));
        assert.deepEqual(steps.filter(([, s]) => s === w.hint).map(([c]) => c), ["aside"]);
        if (lang !== "en") assert(!steps.some(([, s]) => s.includes("unusual activity")), JSON.stringify(steps));

        // the account's row: resting, restricted, and a way to lift it
        const row = page.locator(".rt-act").filter({ hasText: w.label }).first();
        await row.waitFor();
        assert((await row.locator(".st.rest").textContent()).includes(w.label));
        const go = row.locator(".fix button", { hasText: w.lift });
        await go.waitFor();
        // nothing of it runs past the page's width
        assert(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1), "the page scrolls sideways");
        // scrolled to where a reader sees it, it is the button under the
        // pointer, not the footer
        // a reader scrolls down to it with the wheel
        await reach(page, go);
        await go.click();
        // the lift once it is posted: a fixed pause read it before the
        // post reached the fake on a busy machine
        for (let i = 0; i < 100 && !lifted.length; i++) await new Promise((r) => setTimeout(r, 50));
        assert.deepEqual(lifted, [{ key: rest.key }]);
        assert.deepEqual(errors, []);
      });
    }
  }
}

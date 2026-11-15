// Run with Node's test runner and Playwright on the module path; see README.md.
// Copilot's Auto (#256, infinitr0us on v0.1.916, a Student account): the
// route logged each refused try as "auto", so which model Copilot refused,
// and whether /auto or /models/session picked it, wasn't told. Each try's
// line now names the model Auto picked, and a line under it says where the
// pick came from, why not /auto when it didn't, and Copilot's refusal; and
// (v0.1.921's report) which API it went to and whether Auto's session token
// went with it.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const at = (i) => new Date(now.getTime() - (i + 1) * 60e3).toISOString();
const refused = "Copilot: The requested model is not supported.";
const key = { id: "copilot", provider: "copilot", name: "GitHub Copilot", kind: "provider", model: "auto" };
const routes = [{
  id: 100, seq: 100, time: at(0), agent: "opencode", model: "copilot/auto", provider: "copilot", order: [key],
  tries: [
    { id: key.id, model: "auto", start: at(0), done: true, status: 400, ms: 300, error: refused,
      auto: [{ model: "gpt-5.3-codex", via: "/auto", refused, api: "/responses", session: true }] },
    { id: key.id, model: "auto", start: at(0), done: true, status: 200, ms: 400,
      auto: [{ model: "gpt-4.1", via: "fallback", skipped: "picked gpt-5.3-codex, which the account was refused", api: "/chat/completions" }] },
  ],
  done: true, status: 200, ms: 700,
}];

function serve(lang) {
  const state = { agents: [{ id: "opencode", name: "OpenCode", path: "/test/opencode.json", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
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
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const want = {
  en: [
    "Copilot's Auto picked gpt-5.3-codex (/auto) · sent to /responses with Auto's session token · Copilot refused it: " + refused,
    "Copilot's Auto picked gpt-4.1 (the model the account may pick by hand) · not from /auto: picked gpt-5.3-codex, which the account was refused · sent to /chat/completions",
  ],
  zh: [
    "Copilot 的 Auto 选了 gpt-5.3-codex（/auto） · 发往 /responses，带 Auto 的会话令牌 · Copilot 拒绝了它：" + refused,
    "Copilot 的 Auto 选了 gpt-4.1（账号可手动选的模型） · 未取自 /auto：picked gpt-5.3-codex, which the account was refused · 发往 /chat/completions",
  ],
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: Copilot Auto's picks are told on each try`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const context = await browser.newContext({ viewport: { width: 1100, height: 760 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(15000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-copilot-auto.png`), fullPage: true });
        }
        await browser.close();
      });
      await page.goto("http://uni-switch.test/?view=routing");
      await page.locator(".rt-days .rt-day").nth(1).click();
      await page.locator(".rt-req").first().click();
      await page.waitForTimeout(400);
      const got = await page.locator(".rt-steps li").evaluateAll((ls) => ls.map((l) => [l.className, l.textContent]));
      assert.deepEqual(got.filter(([, s]) => s.startsWith(want[lang][0].slice(0, 12))).map(([c, s]) => [c, s]), want[lang].map((s) => ["aside", s]), JSON.stringify(got));
      // each try's line names the model it went as
      const tries = got.filter(([c]) => c === "ok" || c === "bad").map(([, s]) => s);
      assert(tries[0].includes("auto → gpt-5.3-codex") && tries[1].includes("auto → gpt-4.1"), JSON.stringify(tries));
      assert.deepEqual(errors, []);
    });
  }
}

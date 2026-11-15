// Run with Node's test runner and Playwright on the module path; see README.md.
// Smart routing and the account Claude Code (or Codex) is signed in to are
// two things (#209): the Routing field of a Claude subscription with two
// accounts on says the routing picks for what comes through uni-switch, and
// that the sign-in moves on to the next ticked account once it is 98%
// used; in English and Chinese.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const claude = {
  id: "claude", name: "Claude", icon: "anthropic", chat: "", responses: "", anthropic: "", catalog: "",
  models: [{ id: "claude-opus-5-5", name: "Claude Opus 5.5", on: true }], agents: [], fallback: [], headers: {}, keyList: [],
  account: { agent: "claude", agentName: "Claude Code", user: "a@example.com", plan: "max", logins: [
    { user: "a@example.com", plan: "max", active: true, on: true },
    { user: "b@example.com", plan: "max", on: true },
  ] },
};

function serve(lang) {
  const providers = { providers: [claude], presets: [], excluded: [], gateway: { running: true, window: true } };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const want = {
  en: /Claude Code on its own uses the one it is signed in to, which uni-switch moves to the next ticked account with room once it is 98% used/,
  zh: /Claude Code 直连时用它登录的账号，该账号用到 98% 时/,
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: the Routing field says the sign-in follows Smart`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const context = await browser.newContext({ viewport: { width: 900, height: 760 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      t.after(() => browser.close());
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator(".row.provider", { hasText: "Claude" }).click();
      const hint = page.locator(".editor .hint", { hasText: "98%" });
      await hint.waitFor();
      assert.match(await hint.textContent(), want[lang]);
      assert.deepEqual(errors, []);
    });
  }
}

// Routing says a conversation stays on the account that compacted it
// natively (#1099), not that the account is the only one on for the model,
// which read as uni-switch not moving to the others (#2639).
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const at = (minutes) => new Date(now.getTime() - minutes * 60e3).toISOString();
const account = {
  id: "codex@biz@example.com", provider: "codex", name: "Codex", who: "biz@example.com",
  kind: "account", agent: "codex", model: "gpt-6-astra", routing: "pace",
  known: true, used: 45, pace: 1, due: new Date(now.getTime() + 3 * 36e5).toISOString(),
};
const route = (id, fields) => {
  const r = {
    id, seq: id, time: at(1), agent: "codex", model: "codex/gpt-6-astra", provider: "codex",
    order: [account], done: true, status: 200, ms: 900, tokens: 1200, ...fields,
  };
  r.tries = [{ id: account.id, model: account.model, start: at(1), done: true, status: 200, ms: 900 }];
  return r;
};
const routes = [
  route(102, { compaction: account.id }),
  route(101, {}),
];

function serve(lang) {
  return async (request) => {
    const url = new URL(request.request().url());
    const json = (data) => request.fulfill({ json: data });
    if (url.pathname === "/boot.js") return request.fulfill({
      contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};`,
    });
    if (url.pathname === "/wails/runtime.js") return request.fulfill({
      contentType: "text/javascript", body: "export const Window = {};",
    });
    if (url.pathname === "/api/state") return json({
      agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }],
      profiles: [], settings: { lang, theme: "light" },
    });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) await new Promise((resolve) => setTimeout(resolve, 20e3));
      return json({ mine: true, now: now.toISOString(), seq: 102, totals: { requests: routes.length }, routes });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await request.fulfill({ body: await fs.readFile(file), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${engine} ${lang}: a natively compacted conversation says why it stays`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await browser.newPage({ viewport: { width: 1100, height: 1200 }, reducedMotion: "reduce" });
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (error) => errors.push(error.message));
      await page.route("**/*", serve(lang));
      await page.goto("http://uni-switch.test/?view=routing");
      await page.locator(".rt-req").nth(routes.length - 1).waitFor();
      const pinned = { en: /compacted this conversation natively/, zh: /原生压缩了这条对话/, "zh-TW": /原生壓縮了這條對話/, ja: /ネイティブに圧縮/, de: /nativ komprimiert/ }[lang];
      const only = { en: /only account on for/, zh: /唯一启用的账号/, "zh-TW": /唯一啟用的帳號/, ja: /有効なアカウントは/, de: /einzige/ }[lang];
      const why = () => page.locator(".rt-steps li.why").first().textContent();

      await page.locator(".rt-req").first().click();
      await page.waitForFunction(() => document.querySelector(".rt-steps li.why"));
      assert.match(await why(), pinned);
      assert.match(await why(), /biz@example\.com/);
      assert.doesNotMatch(await why(), only);

      await page.locator(".rt-req").nth(1).click();
      await page.waitForFunction((pattern) => !new RegExp(pattern).test(document.querySelector(".rt-steps li.why").textContent), pinned.source);
      assert.doesNotMatch(await why(), pinned);
      assert.deepEqual(errors, []);
    });
  }
}

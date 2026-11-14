// Run with Node's test runner and Playwright on the module path; see README.md.
// Every window on a provider editor's account row says when it resets, not
// only one nearly used up (rosarobertist on X: 能在这儿加个重置时间吗？这样
// 就能做到心中有数了). The row read "5 hours 100% used resets in 1h · 7 days
// 23% used" — the week's reset was left to the tooltip below 80%. Claude
// Code and Codex accounts, every language, Chromium and WebKit, at a desk's
// width, the window's narrowest and a phone's: the reset stays in the row
// and the sheet doesn't scroll sideways. UNI_SWITCH_ACC_ASSETS runs it on older
// assets.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(process.env.UNI_SWITCH_ACC_ASSETS || path.resolve(__dirname, "../assets"));
const soon = new Date(Date.now() + 2 * 36e5).toISOString();
const week = new Date(Date.now() + (6 * 24 + 1) * 36e5).toISOString();
const provider = (id, name, icon, user) => ({
  id, name, icon, chat: "", responses: "", anthropic: "", catalog: "", routing: "",
  models: [{ id: "m", name: "", on: true }], agents: [], fallback: [], headers: {}, keyList: [],
  account: { agent: id, agentName: name, user, plan: "pro", logins: [{ user, plan: "pro", active: true, on: true }] },
});
const providers = [provider("claude", "Claude Code", "claude-color", "me@example.com"), provider("codex", "Codex", "codex-color", "you@example.com")];
const windows = () => [{ name: "5 hours", used: 100, resetsAt: soon }, { name: "7 days", used: 23, resetsAt: week }];
const serve = (lang, web) => async (route) => {
  const url = new URL(route.request().url());
  const json = (data) => route.fulfill({ json: data });
  if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:${web}};` });
  if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
  if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light", quotaLeft: false } });
  if (url.pathname === "/api/providers") return json({ providers, presets: [], excluded: [], gateway: { running: true, window: true } });
  if (url.pathname === "/api/groups") return json({ groups: [] });
  if (url.pathname === "/api/plugins") return json({ plugins: [] });
  if (url.pathname === "/api/login/usage") {
    const agent = url.searchParams.get("agent");
    const p = providers.find((x) => x.id === agent);
    return json(p ? { [p.account.user]: { provider: agent, windows: windows(), asOf: new Date(Date.now() - 6e4).toISOString(), readDeferred: true } } : {});
  }
  if (url.pathname.startsWith("/api/")) return json({});
  const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
  const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
  try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
};

const langs = ["en", "zh", "zh-TW", "ja", "de"];
for (const [engine, browserType] of [["chromium", chromium], ["webkit", webkit]]) {
  test(engine + ": every window on an account row says when it resets", async (t) => {
    const browser = await browserType.launch();
    t.after(() => browser.close());
    for (const [web, width] of [[false, 1000], [false, 560], [true, 390]]) for (const lang of langs) for (const p of providers) {
      const at = `${p.name} ${lang} ${web ? "web " : ""}${width}`;
      const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, web));
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator(".row.provider", { hasText: p.name }).first().click();
      const row = page.locator(`.editor .acc[data-account-id="${p.account.user}"]`);
      await row.locator(".aq-w").nth(1).waitFor();
      // the words the row should say, in its language
      const want = await page.evaluate((lang) => {
        const tr = (k, v) => Object.entries(v).reduce((s, [n, x]) => s.replace("{" + n + "}", x), (lang === "en" ? k : I18N[lang][k]) || k);
        return [tr("resets {in}", { in: tr("in {n}h", { n: 2 }) }), tr("resets {in}", { in: tr("in {n}d", { n: 6 }) })];
      }, lang);
      const got = await row.locator(".aq-w").evaluateAll((ms) => ms.map((m) => m.querySelector(".aq-r")?.textContent || ""));
      assert.deepEqual(got, want, `${at}: the 5 hours and the 7 days each say when they reset`);
      // in the row, and the sheet not scrolling sideways
      const fit = await row.evaluate((r) => {
        const rb = r.getBoundingClientRect(), body = document.querySelector(".modal .ebody");
        return { out: [...r.querySelectorAll(".aq-w")].map((m) => Math.round(m.getBoundingClientRect().right - rb.right)), wide: body ? body.scrollWidth - body.clientWidth : 0 };
      });
      for (const o of fit.out) assert.ok(o <= 0, `${at}: a window's reset stays in its row (${o}px past it)`);
      assert.ok(fit.wide <= 0, `${at}: the sheet doesn't scroll sideways (${fit.wide}px)`);
      assert.deepEqual(errors, [], at);
      await context.close();
    }
  });
}

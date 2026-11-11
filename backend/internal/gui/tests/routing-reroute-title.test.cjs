// Run with Node's test runner and Playwright on the module path; see README.md.
// A rerouted request's row says, in its title too, where it went (#337: 请求
// 改道后，悬停 title 仍是原来的路径): the first try failed and another took it,
// the row names the one that took it, and hovering it said the model and
// provider the request resolved to first. The title now names the one that
// took it, then where it was rerouted from, in the window's Requests list and
// the tray panel's Routing tab alike. A request not rerouted keeps its title.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const at = (min) => new Date(now.getTime() - min * 60e3).toISOString();
const plan = { id: "claude:a", provider: "claude", name: "Claude", kind: "account", who: "ann@example.com", model: "claude-opus-5" };
const relay = { id: "relay", provider: "relay", name: "Relay", kind: "provider", model: "gpt-6-sol" };
const routes = [
  // the plan's account rate limited, the relay took it
  { id: 102, seq: 102, time: at(2), agent: "codex", model: "group/best", provider: "claude", order: [plan, relay],
    tries: [{ id: plan.id, model: plan.model, start: at(2), done: true, status: 429, ms: 300, fail: "rate" },
      { id: relay.id, model: relay.model, start: at(2), done: true, status: 200, ms: 1200 }],
    done: true, status: 200, ms: 1500, tokens: 900 },
  // answered on the first try
  { id: 101, seq: 101, time: at(4), agent: "codex", model: "group/best", provider: "claude", order: [plan, relay],
    tries: [{ id: plan.id, model: plan.model, start: at(4), done: true, status: 200, ms: 800 }],
    done: true, status: 200, ms: 800, tokens: 500 },
];

function serve(lang) {
  const state = { agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) await new Promise((r) => setTimeout(r, 20e3));
      return json({ mine: true, now: now.toISOString(), seq: 102, totals: { requests: 2, rerouted: 1, errors: 0 }, routes });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/window/main" || url.pathname === "/api/window/fit") return route.fulfill({ status: 204 });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const want = {
  en: ["Codex · group/best → Relay · gpt-6-sol\nrerouted from Claude · ann@example.com · claude-opus-5", "Codex · group/best → claude"],
  zh: ["Codex · group/best → Relay · gpt-6-sol\n改道自 Claude · ann@example.com · claude-opus-5", "Codex · group/best → claude"],
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a rerouted request's title names the one that took it`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const errors = [], pages = [];
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          for (const [i, p] of pages.entries()) await p.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-reroute-title-${i}.png`) });
        }
        await browser.close();
      });
      const open = async (url, viewport) => {
        const page = await (await browser.newContext({ viewport, reducedMotion: "reduce" })).newPage();
        pages.push(page);
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        await page.goto(url);
        return page;
      };

      // the window's Requests list
      const win = await open("http://uni-switch.test/?view=routing", { width: 1100, height: 760 });
      await win.locator(".rt-req").nth(routes.length - 1).waitFor();
      const rows = await win.locator(".rt-req").evaluateAll((rs) => rs.map((r) => ({ title: r.title, to: r.querySelector(".to").textContent })));
      assert.deepEqual(rows.map((r) => r.title), want[lang]);
      assert.match(rows[0].to, /Relay · gpt-6-sol/, "the row itself names the one that took it");

      // the tray panel's Routing tab
      const panel = await open("http://uni-switch.test/?mode=panel", { width: 440, height: 600 });
      await panel.locator('[data-ptab="routing"]').click();
      await panel.locator(".pr-req").nth(routes.length - 1).waitFor();
      const prows = await panel.locator(".pr-req").evaluateAll((rs) => rs.map((r) => ({ title: r.title, where: r.querySelector(".pr-where").textContent })));
      assert.deepEqual(prows.map((r) => r.title), want[lang]);
      assert.equal(prows[0].where, "Relay");
      assert.deepEqual(errors, []);
    });
  }
}

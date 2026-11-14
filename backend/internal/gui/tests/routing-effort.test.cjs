// Run with Node's test runner and Playwright on the module path; see README.md.
// A request's row in the Routing page shows the reasoning its model was
// sent at, after the level the agent asked for when that was another
// (xhigh → max), so a level the agent didn't pick reads as the agent's or
// as uni-switch's at a glance (呆滞 on X: Pi 里面选择是 xhigh 但是 uni-switch 里面显示的是 max).
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const at = (i) => new Date(now.getTime() - (i + 1) * 60e3).toISOString();
const seat = { id: "codex", provider: "openai", name: "OpenAI", who: "Codex's own sign-in", kind: "account", agent: "pi", model: "gpt-6.1-sol" };
const cases = [
  { asked: "xhigh", sent: "max", want: "xhigh → max" },
  { asked: "max", sent: "max", want: "max" },
  { asked: "xhigh", sent: "xhigh", want: "xhigh" },
  { asked: "", sent: "", want: null },
];
const routes = cases.map((c, i) => ({
  id: 100 - i, seq: 100 - i, time: at(i), agent: "pi", model: "codex/gpt-6.1-sol", provider: "openai", ...(c.asked ? { effort: c.asked } : {}),
  order: [seat], tries: [{ id: seat.id, model: "gpt-6.1-sol", start: at(i), done: true, status: 200, ms: 900, ...(c.sent ? { effort: c.sent } : {}) }],
  done: true, status: 200, ms: 900, tokens: 1200,
}));

function serve(lang) {
  const state = { agents: [{ id: "pi", name: "Pi", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
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

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a row shows the level asked for before the one sent when they differ`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const context = await browser.newContext({ viewport: { width: 1100, height: 760 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-effort.png`), fullPage: true });
        }
        await browser.close();
      });
      await page.goto("http://uni-switch.test/?view=routing");
      await page.locator(".rt-days .rt-day").nth(1).click();
      await page.locator(".rt-req").nth(cases.length - 1).waitFor();

      const rows = await page.locator(".rt-req").evaluateAll((rs) => rs.map((r) => {
        const ef = r.querySelector(".to .ef");
        if (!ef) return null;
        const b = ef.getBoundingClientRect(), to = r.querySelector(".to").getBoundingClientRect();
        return { text: ef.textContent, title: ef.title, inside: b.right <= to.right + 0.5 && ef.clientWidth >= ef.scrollWidth };
      }));
      assert.deepEqual(rows.map((r) => r && r.text), cases.map((c) => c.want));
      for (const r of rows.filter(Boolean)) assert(r.inside, JSON.stringify(r));
      // the title still says how it came to be
      assert.match(rows[0].title, lang === "zh" ? /xhigh/ : /nearest to the xhigh Pi asked for/);
      assert.match(rows[1].title, lang === "zh" ? /max/ : /as Pi asked/);
      assert.deepEqual(errors, []);
    });
  }
}

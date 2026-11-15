// Run with Node's test runner and Playwright on the module path; see README.md.
// A request still being answered shows a breathing dot whose glow reaches
// 6px out of it; the row's answer cell clips its overflow, and with the dot
// at its very edge the glow's left side was cut flat (#2553, dumplings).
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
const cases = [{ asked: "", sent: "" }, { asked: "", sent: "" }];
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
    for (const width of [1100, 420]) {
      test(`${engine} ${lang} ${width}px: a waiting row's dot keeps its whole glow`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const context = await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-wait-dot.png`), fullPage: true });
          }
          await browser.close();
        });
        await page.goto("http://uni-switch.test/?view=routing");
        await page.locator(".rt-days .rt-day").nth(1).click();
        await page.locator(".rt-req").nth(cases.length - 1).waitFor();

        const r = await page.locator(".rt-req").first().evaluate((row) => {
          const to = row.querySelector(".to"), dot = to.querySelector("i");
          const before = { dot: dot.getBoundingClientRect(), row: row.getBoundingClientRect() };
          row.classList.add("wait");
          // the glow reaches 6px out of the dot (rtlive); overflow clips at
          // .to's padding box
          const b = to.getBoundingClientRect(), s = getComputedStyle(to), d = dot.getBoundingClientRect();
          const clip = { left: b.left + parseFloat(s.borderLeftWidth), top: b.top + parseFloat(s.borderTopWidth), bottom: b.bottom - parseFloat(s.borderBottomWidth) };
          return {
            left: d.left - clip.left, top: d.top - clip.top, bottom: clip.bottom - d.bottom,
            moved: Math.abs(d.left - before.dot.left) + Math.abs(d.top - before.dot.top),
            dotFromRow: before.dot.left - before.row.left,
          };
        });
        assert.ok(r.left >= 6, `left room ${r.left}`);
        assert.ok(r.top >= 6, `top room ${r.top}`);
        assert.ok(r.bottom >= 6, `bottom room ${r.bottom}`);
        assert.ok(r.moved < 0.5, `the dot moved ${r.moved}px when the row started waiting`);
        assert.deepEqual(errors, []);
      });
    }
  }
}

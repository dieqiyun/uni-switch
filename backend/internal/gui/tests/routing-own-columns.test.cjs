// Run with Node's test runner and Playwright on the module path; see README.md.
// The requests' rows have columns of their own, not a subgrid of the list
// (#3731, gpboyer2: on macOS 13, Safari 16's WebKit recursed sizing the
// list's fit-content() columns through the rows' subgrid until its
// WebContent process was killed, and the page reloaded up to 93 times a
// minute under traffic). No rule of the list's rows says subgrid, and the
// rows still line up as they did: each column starts at one x down the
// list, as wide as its widest row's, in every layout the list takes.
// UNI_SWITCH_ROUTING_ASSETS points the page at an older asset tree, to watch
// this go red there.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assetsDir = path.resolve(process.env.UNI_SWITCH_ROUTING_ASSETS || path.join(__dirname, "../assets"));
const now = new Date();
const at = (i) => new Date(now.getTime() - (i + 1) * 60e3).toISOString();
const renew = new Date(now.getTime() + 3 * 864e5).toISOString();
const W = {
  aihub: { id: "aihubmix", provider: "aihubmix", name: "AiHubMix", kind: "provider", model: "glm-5.3-flash", icon: "aihubmix-color", routing: "smart" },
  c1: { id: "codex:a", provider: "codex", name: "Codex", who: "ann@example.com", plan: "plus", kind: "account", model: "gpt-5.6-terra", known: true, used: 9, renews: [renew], icon: "codex-color", routing: "smart" },
  c2: { id: "codex:b", provider: "codex", name: "Codex", who: "bob@example.com", plan: "pro", kind: "account", model: "gpt-5.6-terra", known: true, used: 4, renews: [renew], icon: "codex-color", routing: "smart" },
  cop: { id: "copilot:a", provider: "copilot", name: "Copilot", who: "ann", kind: "account", model: "gpt-6-sol", known: true, used: 1, renews: [renew], icon: "githubcopilot", routing: "smart" },
  ds: { id: "deepseek", provider: "deepseek", name: "DeepSeek", kind: "provider", model: "deepseek-flash", icon: "deepseek-color", routing: "smart" },
  or: { id: "openrouter", provider: "openrouter", name: "OpenRouter", kind: "provider", model: "openai/gpt-6-sol", icon: "openrouter", routing: "smart" },
  wb: { id: "wb:a", provider: "workbuddy", name: "WorkBuddy", who: "ann", kind: "account", model: "glm", known: false, icon: "generic", routing: "smart" },
};
const mk = (i, w, model, extra = {}) => ({
  id: 200 - i, seq: 200 - i, time: at(i), agent: i % 2 ? "codex" : "curl", model, provider: w.provider,
  order: Object.values(W).filter((x) => x.provider === w.provider), done: true, status: 200, ms: 5000 + i * 900, tokens: 90000 + i * 700,
  tries: [{ id: w.id, model: w.model || model.split("/").pop(), start: at(i), done: true, status: 200, ms: 5000 }], ...extra,
});
const routes = [
  mk(0, W.ds, "deepseek/deepseek-flash"), mk(1, W.ds, "deepseek/deepseek-flash"),
  ...[2, 3, 4, 5].map((i) => ({ ...mk(i, W.c1, "codex/gpt-5.6-terra"), tries: [{ id: W.c1.id, model: "gpt-5.6-terra", start: at(i), done: true, status: 200, ms: 8000, effort: "medium" }] })),
  { ...mk(6, W.c1, "codex/gpt-6-luna"), tries: [{ id: W.c1.id, model: "gpt-6-luna", start: at(6), done: true, status: 200, ms: 8000 }] },
  { ...mk(7, W.c1, "codex/gpt-5.6-luna"), tries: [{ id: W.c1.id, model: "gpt-5.6-luna", start: at(7), done: true, status: 200, ms: 8000 }] },
  mk(8, W.cop, "copilot/gpt-6-sol-with-a-much-longer-model-name"), mk(9, W.aihub, "aihubmix/glm-5.3-flash"), mk(10, W.or, "openrouter/openai/gpt-6-sol"), mk(11, W.wb, "workbuddy/glm"),
];
function serve(lang) {
  const state = { agents: [{ id: "codex", name: "Codex", path: "/t", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  const assets = assetsDir;
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) await new Promise((r) => setTimeout(r, 20e3));
      return json({ mine: true, now: now.toISOString(), seq: 200, totals: { requests: routes.length, rerouted: 0, errors: 0 }, routes });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file).catch(() => ""), contentType });
  };
}


test("no rule of the request rows says subgrid", async () => {
  const css = await fs.readFile(path.join(assetsDir, "plugins/routing/routing.css"), "utf8");
  const bad = [];
  for (const m of css.replace(/\/\*[\s\S]*?\*\//g, "").matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    if (/\.rt-req\b/.test(m[1]) && /subgrid/.test(m[2])) bad.push(m[1].trim());
  }
  assert.deepEqual(bad, []);
});

// each drawn row's cells: where each starts
const rowsOf = (page) => page.evaluate(() => [...document.querySelectorAll(".rt-reqs > .rt-req")].map((r) => ({
  model: r.querySelector(".m").textContent,
  x: [".at", ".asked", ".to", ".meta"].map((s) => { const b = r.querySelector(s)?.getBoundingClientRect(); return b && b.width ? Math.round(b.left * 2) / 2 : null; }),
  under: document.querySelector(".rt-col").className,
})));

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: the requests' columns line up without a subgrid`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const page = await browser.newPage({ viewport: { width: 1440, height: 1400 } });
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      t.after(() => browser.close());
      await page.goto("http://uni-switch.test/?view=routing");
      await page.locator(".rt-reqs > .rt-req").first().waitFor();
      for (const width of [1440, 1100, 940, 700]) {
        await page.setViewportSize({ width, height: 1400 });
        // the columns are measured a frame or two after the width changes:
        // waited for until they line up, or two seconds
        const starts = (rows) => [0, 1, 2, 3].map((c) => [...new Set(rows.map((r) => r.x[c]).filter((x) => x !== null))]);
        let rows = await rowsOf(page);
        for (const until = Date.now() + 2000; Date.now() < until && starts(rows).some((xs) => xs.length > 1);) {
          await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => r())));
          rows = await rowsOf(page);
        }
        assert.equal(rows.length, routes.length, `${width}px: every request has its row`);
        starts(rows).forEach((xs, c) => assert(xs.length <= 1, `${width}px (${rows[0].under}): column ${c + 1} starts at ${xs.join(", ")}`));
      }
      assert.deepEqual(errors, []);
    });
  }
}

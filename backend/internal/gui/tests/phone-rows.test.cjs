// Run with Node's test runner and Playwright on the module path; see README.md.
// A row on a phone (zaidu259434 on X: 移动端适配真的希望优化下). At 360px a
// provider's row squeezed its name to a letter or two beside its key pill
// and agent icons, and pushed its switch past the card; a gateway key's row
// cut its name and its buttons; a model's badges, a call's model and path,
// and a routed request's reasoning beside its upstream were cut to "m…".
// On a phone the trailing parts go to a line of their own: nothing on the
// page runs off the side, no text in a row is cut to a sliver, and every
// control in a row is inside it. Both engines, every language; the API is
// faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const at = (i) => new Date(now.getTime() - (i + 1) * 60e3).toISOString();

const agents = [
  { id: "codex", name: "Codex", icon: "codex-color", model: "deepseek-chat", current: true },
  { id: "claude", name: "Claude Code", icon: "claudecode-color", model: "deepseek-chat", current: true },
  { id: "opencode", name: "OpenCode", icon: "opencode", model: "deepseek-reasoner", current: true },
];
const models = [
  { id: "deepseek-chat", name: "DeepSeek V3.2 Chat", on: true, efforts: ["low", "medium", "high"], images: true, context: 128000 },
  { id: "deepseek-reasoner", name: "DeepSeek V3.2 Reasoner", on: true, efforts: ["low", "medium", "high"], context: 128000 },
];
const provider = (id, name, more) => ({
  id, name, icon: "generic", host: "api." + id + ".example.com", models, agents, ready: true,
  key: { set: true, masked: "sk-a…33f1" }, ...more,
});
const providers = [
  provider("deepseek", "DeepSeek"),
  provider("openrouter", "OpenRouter International"),
  provider("siliconflow", "SiliconFlow 硅基流动", { off: true, agents: [] }),
];
const calls = [0, 1, 2].map((i) => ({
  time: at(i), agent: "Claude Code", model: "openrouter/anthropic/claude-opus-4.5", from: "OpenRouter International", to: "OpenRouter International",
  status: 200, ms: 6900, ttft: 6800,
}));
const keys = [
  { id: "laptop", name: "MacBook Pro", masked: "sk-uni-switch-key-…111111" },
  { id: "phone", name: "iPhone", masked: "sk-uni-switch-key-…222222" },
];
const or = { id: "openrouter", provider: "openrouter", name: "OpenRouter International", kind: "provider", model: "anthropic/claude-opus-4.5" };
const routes = [0, 1, 2].map((i) => ({
  id: 100 - i, seq: 100 - i, time: at(i), agent: "claude", model: "openrouter/anthropic/claude-opus-4.5", provider: "openrouter", effort: "medium",
  order: [or], tries: [{ id: or.id, model: or.model, start: at(i), done: true, status: 200, ms: 6900, served: or.model, effort: "medium", upstream: "Claude Platform on AWS" }],
  done: true, status: 200, ms: 6900, tokens: 24700, served: or.model, upstream: "Claude Platform on AWS",
}));

function serve(lang) {
  const state = { agents: agents.map((a) => ({ id: a.id, name: a.name, path: "/test/" + a.id, fields: [] })), profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/favicon.ico") return route.fulfill({ status: 204 });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json({
      providers, presets: [], excluded: [],
      gateway: { running: true, window: true, mine: true, url: "http://uni-switch.example.ts.net:3425", lan: true, lanURLs: ["http://192.168.1.10:3425"], calls, groups: [] },
    });
    if (url.pathname === "/api/settings") return json({ lang, theme: "light", lan: true, lanURLs: ["http://192.168.1.10:3425"] });
    if (url.pathname === "/api/caller-keys") return json({ keys });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) await new Promise((r) => setTimeout(r, 20e3));
      return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [{ day, requests: routes.length }], routes: url.searchParams.get("day") ? routes : [] });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// what a phone's reader would call broken in the rows matched by sel
function broken(sel) {
  const seen = (e) => {
    const s = getComputedStyle(e);
    return e.getClientRects().length && s.visibility !== "hidden" && s.opacity !== "0";
  };
  const out = { wide: document.documentElement.scrollWidth - innerWidth, rows: 0, cut: [], outside: [] };
  for (const row of document.querySelectorAll(sel)) {
    if (!seen(row)) continue;
    out.rows++;
    const r = row.getBoundingClientRect();
    for (const e of row.querySelectorAll("*")) {
      if (!seen(e)) continue;
      const text = [...e.childNodes].some((n) => n.nodeType === 3 && n.textContent.trim());
      // a text cut to a sliver: under 40px wide for over twice that much
      if (text && e.clientWidth < 40 && e.scrollWidth > 2 * e.clientWidth) out.cut.push(e.textContent.trim());
      if (e.matches("button, [role=switch], a, input")) {
        const b = e.getBoundingClientRect();
        // a control past its row's edge, or hidden by a box that clips it
        let clipped = b.right > r.right + 1 || b.left < r.left - 1;
        for (let p = e.parentElement; p && p !== row && !clipped; p = p.parentElement) {
          const s = getComputedStyle(p);
          if (s.overflowX === "visible") continue;
          const pb = p.getBoundingClientRect();
          if (b.right > pb.right + 1 || b.left < pb.left - 1) clipped = true;
        }
        if (clipped) out.outside.push(e.className || e.textContent.trim() || e.tagName);
      }
    }
  }
  return out;
}

const pages = [
  { view: "providers", rows: ".row.provider", wait: "#view-providers .row.provider" },
  { view: "gateway", rows: ".row.gw, #gwModels .row.model, #activity .call, #gatewayKeys .acc[data-key]", wait: "#gatewayKeys .acc[data-key]" },
  // the requests are today's in the history: open the day
  { view: "routing", rows: ".rt-req", wait: ".rt-day >> nth=1", open: (page) => page.locator(".rt-day").nth(1).click(), then: ".rt-req .upstream" },
];
const engines = process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"];
const langs = process.env.LANGS ? process.env.LANGS.split(",") : ["en", "zh", "zh-TW", "ja", "de"];

for (const engine of engines) {
  for (const lang of langs) {
    test(`${engine} ${lang}: a row at 360px keeps its text and its controls`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const bad = {};
      for (const p of pages) {
        const context = await browser.newContext({ viewport: { width: 360, height: 760 }, isMobile: engine === "chromium", hasTouch: true, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(6000);
        page.setDefaultNavigationTimeout(60000);
        await page.route("**/*", serve(lang));
        await page.goto("http://uni-switch.test/?view=" + p.view);
        await page.waitForSelector(p.wait, { timeout: 15000 });
        if (p.open) { await p.open(page); await page.waitForSelector(p.then); }
        if (p.view === "gateway") await page.waitForSelector("#gwModels .row.model");
        await page.waitForTimeout(200);
        const got = await page.evaluate(broken, p.rows);
        assert.ok(got.rows > 0, `${p.view}: rows drawn`);
        // every page is looked at before one fails, so a run names them all
        if (got.wide) bad[p.view + ": runs off the side by"] = got.wide;
        if (got.cut.length) bad[p.view + ": text cut to a sliver"] = got.cut;
        if (got.outside.length) bad[p.view + ": controls outside their row"] = got.outside;
        await context.close();
      }
      assert.deepEqual(bad, {});
    });
  }
}

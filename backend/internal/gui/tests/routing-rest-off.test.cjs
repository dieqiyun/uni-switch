// Run with Node's test runner and Playwright on the module path; see README.md.
// A provider set never to rest after an unknown failure (`uni-switch provider
// rest-cap <id> off`; Kismet Alan on Discord: a relay's 5xx comes now and
// then and the next request goes through): the Routing page tells its 502
// as not resting, the request gone on to the next, and its row isn't shown
// resting — in every language the page speaks.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const start = new Date(now.getTime() - 60e3).toISOString();
const a = { id: "relay#a", provider: "relay", name: "Relay", kind: "key", who: "key a", model: "glm-5" };
const b = { id: "relay#b", provider: "relay", name: "Relay", kind: "key", who: "key b", model: "glm-5" };
const rest = { why: "other", by: "off", status: 502, until: new Date(now.getTime() - 59e3).toISOString() };
const routes = [{
  id: 100, seq: 100, time: start, agent: "codex", model: "relay/glm-5", provider: "relay",
  order: [a, b],
  tries: [
    { id: a.id, model: a.model, start, done: true, status: 502, ms: 900, fail: "other", rest, error: "Relay: upstream connect error" },
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
  en: "It doesn't rest: its provider is set to ask it again after an error like this",
  zh: "它不休息：它的提供方设为遇到这类错误后照常再问它",
  "zh-TW": "它不休息：它的提供方設為遇到這類錯誤後照常再問它",
  ja: "休止しません：このプロバイダーは、この種のエラーの後も次にそのまま尋ねる設定です",
  de: "Es pausiert nicht: Sein Anbieter ist so eingestellt, dass es nach einem solchen Fehler wieder gefragt wird",
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(want)) {
    for (const width of [1100, 420]) {
      test(`${engine} ${lang} ${width}px: a key set never to rest after an unknown failure is told as not resting`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, []));
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-rest-off.png`), fullPage: true });
          }
          await browser.close();
        });
        await page.goto("http://uni-switch.test/?view=routing");
        await page.locator(".rt-days .rt-day").nth(1).click();
        await page.locator(".rt-req").first().waitFor();
        await page.locator(".rt-req").first().click();
        await page.locator(".rt-steps li").first().waitFor();
        await page.waitForTimeout(300);
        const steps = await page.locator(".rt-steps li").evaluateAll((ls) => ls.map((l) => l.textContent));
        assert.equal(steps.filter((s) => s.includes(want[lang])).length, 1, JSON.stringify(steps));
        if (lang !== "en") assert(!steps.some((s) => s.includes("doesn't rest")), JSON.stringify(steps));
        // nobody's row is shown resting
        assert.equal(await page.locator(".rt-act .st.rest").count(), 0);
        assert(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1), "the page scrolls sideways");
        assert.deepEqual(errors, []);
      });
    }
  }
}

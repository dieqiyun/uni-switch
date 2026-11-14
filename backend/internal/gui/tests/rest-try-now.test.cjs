// Run with Node's test runner and Playwright on the module path; see README.md.
// Any rest can be lifted by hand (#1869, wuwei0727 on #1598): a key or
// account resting for its quota, a backoff, a Retry-After or a moment's
// trouble has "Try now" on its Routing row and beside "Resting · … back
// at" in the provider's key list. A click posts gateway/unrest with the
// key it rests by, the key list drops the note, and the click doesn't
// scroll. Before, only a verification (#152) or a restriction (#1849)
// could be lifted. In every language, at a wide and a narrow window.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const start = new Date(now.getTime() - 60e3).toISOString();
const until = new Date(now.getTime() + 40 * 60e3).toISOString();
// the issue's own row: zx's key ww#ec03c1b4b9, out of quota by retry-after
const a = { id: "ww#ec03c1b4b9", provider: "ww", name: "WW", kind: "key", who: "zx", model: "m" };
const b = { id: "ww2", provider: "ww2", name: "WW2", kind: "provider", model: "m" };
const rest = { why: "quota", status: 429, until, by: "retry-after", key: "ww#ec03c1b4b9" };
const routes = [{
  id: 100, seq: 100, time: start, agent: "codex", model: "group/g", provider: "ww",
  order: [a, b],
  tries: [
    { id: a.id, model: a.model, start, done: true, status: 429, ms: 300, fail: "quota", rest, error: "WW: quota exceeded" },
    { id: b.id, model: b.model, start, done: true, status: 200, ms: 900 },
  ],
  done: true, status: 200, ms: 1300,
}];
const relay = () => ({
  id: "relay", name: "Relay", icon: "generic", chat: "https://relay.example.com/v1", responses: "", anthropic: "", catalog: "",
  models: [{ id: "big", name: "", on: true }], agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-…one" }, balanceToken: { takes: false, set: false }, proxy: "",
  keyList: [
    { id: "aaaaaaaaaa", name: "", masked: "sk-…one", on: true, active: true },
    { id: "bbbbbbbbbb", name: "Team", masked: "sk-…two", on: true, rest: { why: "rate", status: 429, until, key: "relay#bbbbbbbbbb" } },
  ],
});

function serve(lang, lifted) {
  const state = { agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  const providers = { providers: [relay()], presets: [], excluded: [], gateway: { running: true, window: true } };
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
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const want = {
  en: { go: "Try now", note: "Team is asked again from the next request", remove: "Remove", first: "Make first" },
  zh: { go: "立即重试", note: "Team 从下一个请求起重新使用", remove: "移除", first: "设为首选" },
  "zh-TW": { go: "立即重試", note: "Team 從下一個請求起重新使用", remove: "移除", first: "設為首選" },
  ja: { go: "今すぐ試す", note: "Team は次のリクエストから再び使われます", remove: "削除", first: "最初にする" },
  de: { go: "Jetzt versuchen", note: "Team wird ab der nächsten Anfrage wieder gefragt", remove: "Entfernen", first: "An erste Stelle setzen" },
};

const posted = async (lifted) => {
  for (let i = 0; i < 60 && !lifted.length; i++) await new Promise((r) => setTimeout(r, 50));
  return lifted;
};
const scrolled = (page) => page.evaluate(() => [window.scrollY, ...[...document.querySelectorAll("*")].filter((e) => e.scrollTop > 0).map((e) => `${e.className}:${e.scrollTop}`)].join(" "));
const sideways = (page) => page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1);
// a reader wheels down to it, and it is the button under the pointer
async function reach(page, go) {
  await page.mouse.move(200, 300);
  for (let i = 0; i < 30 && !(await go.evaluate((e) => e.getBoundingClientRect().bottom < innerHeight - 80)); i++) await page.mouse.wheel(0, 200);
  await page.waitForTimeout(200);
  assert(await go.evaluate((e) => { const r = e.getBoundingClientRect(); return e.contains(document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2)); }), "the button can't be reached");
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(want)) {
    for (const width of [1100, 420]) {
      const open = async (t, name, view) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const context = await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], lifted = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, lifted));
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-${name}.png`), fullPage: true });
          }
          await browser.close();
        });
        await page.goto("http://uni-switch.test/?view=" + view);
        return { page, errors, lifted };
      };

      test(`${engine} ${lang} ${width}px: a key resting for its quota is tried again from Routing`, async (t) => {
        const { page, errors, lifted } = await open(t, "routing-try-now", "routing");
        await page.locator(".rt-days .rt-day").nth(1).click();
        await page.locator(".rt-req").first().waitFor();
        await page.locator(".rt-req").first().click();
        const row = page.locator(".rt-act").filter({ hasText: "zx" }).first();
        await row.locator(".st.rest").waitFor();
        const go = row.locator(".fix button", { hasText: want[lang].go });
        await go.waitFor();
        assert.equal(await row.locator(".fix button").count(), 1, "a quota's rest has nothing to verify");
        assert(!(await sideways(page)), "the page scrolls sideways");
        await reach(page, go);
        const sc = await scrolled(page);
        await go.click();
        assert.deepEqual(await posted(lifted), [{ key: rest.key }]);
        // the row no longer says it rests, though the kept request does
        await row.locator(".st.rest").waitFor({ state: "detached" });
        assert.equal(await row.locator(".fix").count(), 0);
        assert.equal(await scrolled(page), sc, "the click scrolled");
        assert.deepEqual(errors, []);
      });

      test(`${engine} ${lang} ${width}px: a resting key in the provider's list is tried again`, async (t) => {
        const { page, errors, lifted } = await open(t, "keys-try-now", "providers");
        await page.locator(".row.provider", { hasText: "Relay" }).first().click();
        const row = page.locator('.acc[data-account-id="bbbbbbbbbb"]');
        await row.locator(".key-rest").waitFor();
        assert.equal(await page.locator('.acc[data-account-id="aaaaaaaaaa"] .key-unrest').count(), 0, "a key not resting offers it");
        const go = row.getByRole("button", { name: want[lang].go, exact: true });
        await go.waitFor();
        assert(!(await sideways(page)), "the page scrolls sideways");
        await reach(page, go);
        // the note and Try now don't push the row's own buttons out of
        // reach (they did in German at 1100px before they took a line)
        await row.hover();
        for (const b of [want[lang].remove, want[lang].first]) {
          const btn = row.getByRole("button", { name: b, exact: true });
          assert(await btn.evaluate((e) => { const r = e.getBoundingClientRect(); return r.width > 0 && e.contains(document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2)); }), `${b} can't be reached`);
        }
        const sc = await scrolled(page);
        await go.click();
        assert.deepEqual(await posted(lifted), [{ key: "relay#bbbbbbbbbb" }]);
        await page.locator('.acc[data-account-id="bbbbbbbbbb"] .key-rest').waitFor({ state: "detached" });
        assert.equal(await page.locator('.acc[data-account-id="bbbbbbbbbb"] .key-unrest').count(), 0);
        assert.equal((await page.locator("#status").textContent()).trim(), want[lang].note);
        assert.equal(await scrolled(page), sc, "the click scrolled");
        assert.deepEqual(errors, []);
      });
    }
  }
}

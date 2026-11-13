// Run with Node's test runner and Playwright on the module path; see README.md.
// #3749 (MYCF711): a member whose model's context window the request is
// longer than isn't asked (roomy), and its row used to go from the Routing
// page with no word of why. The trace now lists it in the route's left,
// with window set to the window it lacked: its row stays, says the request
// is past its window and that it was passed over, faded as a seat held out
// is, and the why list says so rather than "its plan doesn't list". In
// every language, at a window's width and at 420px, in both engines.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = Date.now();
const iso = (ms) => new Date(ms).toISOString();
const big = { id: "sub@a@example.com", provider: "sub", name: "Sub", who: "a@example.com", kind: "account", model: "deepseek-v4-flash", routing: "", known: true, used: 20 };
const small = { id: "zen", provider: "zen", name: "OpenCode Zen", kind: "provider", model: "deepseek-v4-flash-free", routing: "", window: 200000 };
const group = { id: "flash", name: "Flash", routing: "order", members: ["zen/deepseek-v4-flash-free", "sub/deepseek-v4-flash"] };
const route = {
  id: 7, seq: 7, time: iso(now - 2000), agent: "claude", model: "group/flash", provider: "sub", group, done: true, status: 200, ms: 900, tokens: 250000,
  order: [big], left: [small], tries: [{ id: big.id, model: big.model, start: iso(now - 2000), done: true, ms: 800, status: 200 }],
};

function serve(lang) {
  const state = { agents: [{ id: "claude", name: "Claude Code", path: "/test/claude", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) return new Promise(() => {}); // nothing more comes
      return json({ mine: true, now: new Date().toISOString(), seq: 7, totals: { requests: 1, rerouted: 0, errors: 0 }, routes: [route] });
    }
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/usage/quotas/history") return json([]);
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ models: [], groups: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await r.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: ["request longer than its 200K window · passed over", "OpenCode Zen is left out: this request is longer than its 200K context window, so it would only refuse it. It's asked only once every one with room is out of its allowance."],
  zh: ["请求超出其 200K 上下文 · 已跳过", "OpenCode Zen 不参与：这个请求超出它 200K 的上下文窗口，发给它只会被拒绝。只有能容下的都用完额度时才会问它。"],
  "zh-TW": ["請求超出其 200K 上下文 · 已略過", "OpenCode Zen 不參與：這個請求超出它 200K 的上下文視窗，送給它只會被拒絕。只有容得下的都用完額度時才會問它。"],
  ja: ["リクエストが 200K のコンテキストを超過 · スキップ", "OpenCode Zen は除外：このリクエストは 200K のコンテキストウィンドウを超えており、送っても拒否されるだけです。余裕のあるものがすべて枠を使い切ったときだけ使われます。"],
  de: ["Anfrage länger als sein 200K-Fenster · übergangen", "OpenCode Zen wird übergangen: Diese Anfrage ist länger als sein Kontextfenster von 200K, es würde sie nur ablehnen. Es wird erst gefragt, wenn alle mit genug Platz ihr Kontingent aufgebraucht haben."],
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: a member the request is too long for keeps its row and says why`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of Object.keys(words)) {
      for (const width of [1100, 420]) {
        await t.test(`${lang} at ${width}px`, async () => {
          const ctx = await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" });
          try {
            const page = await ctx.newPage();
            page.setDefaultTimeout(5000);
            page.setDefaultNavigationTimeout(60000);
            const errors = [];
            page.on("pageerror", (e) => errors.push(e.message));
            await page.route("**/*", serve(lang));
            await page.goto("http://uni-switch.test/?view=routing");
            const row = page.locator(`.rt-accts > li[title^="${small.id}"]`).first();
            await row.locator("em").filter({ hasText: /\S/ }).waitFor();
            const [rowWord, why] = words[lang];
            assert.equal(await row.locator("em").textContent(), rowWord);
            assert.ok(await row.evaluate((li) => li.classList.contains("left")), "faded as a seat held out is");
            // the one that answered is still drawn as it was
            await page.locator(`.rt-accts > li[title^="${big.id}"]`).first().waitFor();
            // the why list names it and its window, not its plan
            assert.ok((await page.locator("body").textContent()).includes(why), "the why list says why");
            const over = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
            assert.ok(over <= 1, `${lang} at ${width}: ${over}px past`);
            assert.deepEqual(errors, []);
          } finally {
            await ctx.close();
          }
        });
      }
    }
  });
}

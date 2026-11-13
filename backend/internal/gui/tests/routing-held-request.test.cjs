// Run with Node's test runner and Playwright on the module path; see README.md.
// Jason on Discord: "多个账号提示路由失败，保留一个账号也会出现这种情况". A
// Codex turn uni-switch held while it was masked, before routing it, showed
// on the Routing page as "uni-switch · 0 on" over the one-account hint ("only
// one account or key is on") and "Nothing could take ." — a held request
// has no one weighed for it yet, and the card read it as a provider with
// nothing on. Held, it says it is being masked (or routed) and that who
// takes it isn't picked yet; one nothing took says why. Neither shows a
// count of 0 or the one-account hint. In every language, both engines.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const ago = (minutes) => new Date(now.getTime() - minutes * 60e3).toISOString();

const routes = {
  // held before routing while masked: what the gateway's watch shows
  masking: { id: 1, seq: 1, time: ago(0.3), agent: "AirbuildEnvironment", model: "gpt-6-astra", provider: "uni-switch", phase: "redact", done: false, order: [], tries: [] },
  // held, not masking: still read
  routing: { id: 1, seq: 1, time: ago(0.3), agent: "AirbuildEnvironment", model: "", provider: "uni-switch", done: false, order: [], tries: [] },
  // given up on with nobody weighed: its error says why
  ended: { id: 1, seq: 1, time: ago(1), agent: "AirbuildEnvironment", model: "gpt-6-astra", provider: "uni-switch", done: true, status: 504, ms: 120000,
    error: "uni-switch held this request for 2m0s without routing it to any provider", order: [], tries: [] },
};

function serve(lang, scene) {
  return async (request) => {
    const url = new URL(request.request().url());
    const json = (data) => request.fulfill({ json: data });
    if (url.pathname === "/boot.js") return request.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return request.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) return new Promise(() => {}); // nothing more comes
      return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 0 }, routes: [routes[scene]], ids: [1] });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ models: [], groups: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await request.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// the words, as each language says them
const said = {
  masking: {
    en: "uni-switch is masking this request before routing it (Settings › Privacy), so who takes it isn't picked yet.",
    zh: "uni-switch 正在给这个请求脱敏（设置 › 隐私），脱敏完才路由，所以还没选出由谁处理。",
    "zh-TW": "uni-switch 正在為這個請求脫敏（設定 › 隱私），脫敏完才路由，所以還沒選出由誰處理。",
    ja: "uni-switch はルーティングの前にこのリクエストをマスクしています（設定 › プライバシー）。誰が処理するかはまだ決まっていません。",
    de: "uni-switch maskiert diese Anfrage vor der Weiterleitung (Einstellungen › Datenschutz), daher steht noch nicht fest, wer sie übernimmt.",
  },
  routing: {
    en: "uni-switch hasn't routed this request yet, so who takes it isn't picked yet.",
    zh: "uni-switch 还没开始路由这个请求，所以还没选出由谁处理。",
    "zh-TW": "uni-switch 還沒開始路由這個請求，所以還沒選出由誰處理。",
    ja: "uni-switch はまだこのリクエストをルーティングしていないため、誰が処理するかはまだ決まっていません。",
    de: "uni-switch hat diese Anfrage noch nicht weitergeleitet, daher steht noch nicht fest, wer sie übernimmt.",
  },
  ended: {
    en: "Nothing took this request: " + routes.ended.error,
    zh: "没有谁处理这个请求：" + routes.ended.error,
    "zh-TW": "沒有誰處理這個請求：" + routes.ended.error,
    ja: "このリクエストを処理したものはありません: " + routes.ended.error,
    de: "Niemand hat diese Anfrage übernommen: " + routes.ended.error,
  },
};
// what the head says after the name while it is held
const wait = {
  masking: { en: "masking…", zh: "脱敏中…", "zh-TW": "脫敏中…", ja: "マスク中…", de: "maskiert…" },
  routing: { en: "routing…", zh: "路由中…", "zh-TW": "路由中…", ja: "ルーティング中…", de: "leitet weiter…" },
};
// the words the card must not say of a request nobody is weighed for
const zeroOn = { en: "0 on", zh: "启用 0 个", "zh-TW": "啟用 0 個", ja: "0 個有効", de: "0 aktiv" };
const lone = {
  en: "Only one account or key is on for this",
  zh: "这里只开了一个账号或 Key",
  "zh-TW": "這裡只開了一個帳號或 Key",
  ja: "有効なアカウントやキーが 1 つだけ",
  de: "Hier ist nur ein Konto oder Schlüssel aktiv",
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const scene of ["masking", "routing", "ended"]) {
      for (const width of [1100, 420]) {
        test(`${engine} ${lang} ${width}px ${scene}: a request nobody is weighed for says why, not "0 on" and one account`, async (t) => {
          const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
          t.after(() => browser.close());
          const page = await browser.newPage({ viewport: { width, height: 700 }, reducedMotion: "reduce" });
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          const errors = [];
          page.on("pageerror", (error) => errors.push(error.message));
          await page.route("**/*", serve(lang, scene));
          await page.goto("http://uni-switch.test/?view=routing");
          await page.locator(".rt-req").first().waitFor();
          const want = said[scene][lang];
          await page.waitForFunction((s) => document.querySelector(".rt-mode")?.textContent.trim() === s, want);
          const head = (await page.locator(".rt-what").textContent()).trim();
          assert.ok(!head.includes(zeroOn[lang]), `the head counts none on: ${head}`);
          if (wait[scene]) assert.ok(head.endsWith(wait[scene][lang]), `the head says it is held: ${head}`);
          assert.equal(await page.locator(".rt-hub i").isHidden(), true, "the chip names no routing");
          // the story says the same, where it said "Nothing could take ."
          const why = page.locator(".rt-steps li.why").first();
          await why.waitFor();
          assert.equal((await why.textContent()).trim(), want);
          const body = await page.locator("#view-routing").innerText();
          assert.ok(!body.includes(lone[lang]), "the one-account hint is not shown");
          assert.deepEqual(errors, []);
        });
      }
    }
  }
}

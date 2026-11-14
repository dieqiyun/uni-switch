// Run with Node's test runner and Playwright on the module path; see README.md.
// A web search uni-switch ran for a model that can't search by itself, after
// the searcher picked in Settings › Models › Web search failed or found
// nothing, says so in its Routing row: which searcher was asked first, for
// whom, why it didn't search, and which one searched in its place
// (Gearclickist on Discord: Grok picked, and Codex's model searched with
// nothing saying why). In every language, in both engines, at a narrow
// width too.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const words = {
  en: {
    failed: /grok\/grok-4\.3, the searcher picked in Settings › Models › Web search, didn't search for Codex's zai\/glm-5\.2: 502 upstream closed\. So uni-switch had codex\/gpt-6\.1-sol, the next searcher it can use, search in its place\. Not a turn of the conversation\.$/,
    nothing: /grok\/grok-4\.3, the searcher uni-switch picked, didn't search for Codex's zai\/glm-5\.2: its reply named no page it found\. So uni-switch had codex\/gpt-6\.1-sol/,
    notSearched: /grok\/grok-4\.3, the searcher uni-switch picked, didn't search for Codex's zai\/glm-5\.2: it answered without searching the web, so its account or plan may not offer web search\. So uni-switch had codex\/gpt-6\.1-sol/,
  },
  zh: {
    failed: /grok\/grok-4\.3 是在 设置 › 模型 › 联网搜索 里选的代搜模型，但它没能为 Codex 的 zai\/glm-5\.2 搜索：502 upstream closed。所以 uni-switch 改让下一个可用的代搜模型 codex\/gpt-6\.1-sol 来搜。不属于对话轮次。$/,
    nothing: /grok\/grok-4\.3 是 uni-switch 自动选的代搜模型，但它没能为 Codex 的 zai\/glm-5\.2 搜索：它的回复里没有搜到的网页。/,
    notSearched: /grok\/grok-4\.3 是 uni-switch 自动选的代搜模型，但它没能为 Codex 的 zai\/glm-5\.2 搜索：它没有联网搜索就直接回答了，它的账号或套餐可能不支持联网搜索。/,
  },
  "zh-TW": {
    failed: /grok\/grok-4\.3 是在 設定 › 模型 › 聯網搜尋 裡選的代搜模型，但它沒能為 Codex 的 zai\/glm-5\.2 搜尋：502 upstream closed。所以 uni-switch 改讓下一個可用的代搜模型 codex\/gpt-6\.1-sol 來搜。不屬於對話輪次。$/,
    nothing: /grok\/grok-4\.3 是 uni-switch 自動選的代搜模型，但它沒能為 Codex 的 zai\/glm-5\.2 搜尋：它的回覆裡沒有搜到的網頁。/,
    notSearched: /grok\/grok-4\.3 是 uni-switch 自動選的代搜模型，但它沒能為 Codex 的 zai\/glm-5\.2 搜尋：它沒有聯網搜尋就直接回答了，它的帳號或方案可能不支援聯網搜尋。/,
  },
  ja: {
    failed: /設定 › モデル › ウェブ検索 で選んだ検索モデル grok\/grok-4\.3 は Codex の zai\/glm-5\.2 の代わりに検索できませんでした：502 upstream closed。そのため uni-switch は次に使える検索モデル codex\/gpt-6\.1-sol に代わりに検索させました。会話のターンではありません。$/,
    nothing: /uni-switch が自動で選んだ検索モデル grok\/grok-4\.3 は Codex の zai\/glm-5\.2 の代わりに検索できませんでした：返答に見つけたページがありませんでした。/,
    notSearched: /uni-switch が自動で選んだ検索モデル grok\/grok-4\.3 は Codex の zai\/glm-5\.2 の代わりに検索できませんでした：ウェブ検索をせずに回答しました。アカウントやプランがウェブ検索に対応していない可能性があります。/,
  },
  de: {
    failed: /grok\/grok-4\.3, der unter Einstellungen › Modelle › Websuche gewählte Sucher, hat nicht für zai\/glm-5\.2 von Codex gesucht: 502 upstream closed\. Daher ließ uni-switch codex\/gpt-6\.1-sol, den nächsten nutzbaren Sucher, an seiner Stelle suchen\. Keine Runde des Gesprächs\.$/,
    nothing: /grok\/grok-4\.3, der von uni-switch gewählte Sucher, hat nicht für zai\/glm-5\.2 von Codex gesucht: seine Antwort nannte keine gefundene Seite\./,
    notSearched: /grok\/grok-4\.3, der von uni-switch gewählte Sucher, hat nicht für zai\/glm-5\.2 von Codex gesucht: es hat ohne Websuche geantwortet, daher bietet sein Konto oder Tarif möglicherweise keine Websuche\./,
  },
};

const now = new Date();
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const route = (id, f) => ({
  id, seq: id, time: new Date(now.getTime() - id * 60e3).toISOString(), agent: "uni-switch", model: "codex/gpt-6.1-sol", provider: "codex",
  kind: "web_search", for: { agent: "codex", model: "zai/glm-5.2", instead: "grok/grok-4.3", ...f },
  order: [{ id: "codex", provider: "codex", name: "Codex", who: "a@b.c", kind: "account", model: "gpt-6.1-sol" }],
  tries: [{ id: "codex", model: "gpt-6.1-sol", start: new Date(now.getTime() - id * 60e3).toISOString(), done: true, status: 200, ms: 900 }],
  done: true, status: 200, ms: 900, tokens: 1200,
});
// newest first: the picked searcher that failed (a row kept from before a
// picked searcher stopped being handed on), uni-switch's own pick that found
// nothing, and its pick that answered without searching
const routes = [route(1, { picked: true, why: "502 upstream closed" }), route(2, { nothingFound: true }), route(3, { notSearched: true })];

function serve(lang) {
  const settings = { lang, theme: "light", searcher: "grok/grok-4.3", gateway: "http://127.0.0.1:3999", port: 3999 };
  const state = {
    agents: [], profiles: [], settings,
    clients: [{ id: "codex", name: "Codex", icon: "codex" }, { id: "uni-switch", name: "uni-switch", icon: "uni-switch" }],
  };
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) await new Promise((res) => setTimeout(res, 20e3));
      return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname === "/api/gateway/history") {
      return json({ cut: false, days: [{ day, requests: routes.length }], routes: url.searchParams.get("day") ? routes : [] });
    }
    if (url.pathname === "/api/groups") return json({ models: [], groups: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await r.fulfill({ body: await fs.readFile(file), contentType }); } catch { await r.fulfill({ status: 404, body: "" }); }
  };
}

async function open(t, engine, lang, width) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  t.after(() => browser.close());
  const page = await (await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" })).newPage();
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/*", serve(lang));
  await page.goto("http://uni-switch.test/?view=routing");
  return { page, errors };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(words)) {
    const w = words[lang];
    test(`${engine} ${lang}: a search in place of the searcher asked first says which and why`, async (t) => {
      for (const width of [1100, 520]) {
        const { page, errors } = await open(t, engine, lang, width);
        await page.locator(".rt-days .rt-day").nth(1).click();
        // each row's story, read once its own is drawn
        const said = [];
        for (const i of [0, 1, 2]) {
          const req = page.locator(".rt-req").nth(i);
          await req.waitFor();
          await req.click();
          const h = await page.waitForFunction((prev) => {
            const s = document.querySelector(".rt-steps li.kind")?.textContent.trim();
            return s && s !== prev && s;
          }, said.at(-1) || "");
          said.push(await h.jsonValue());
        }
        for (const want of [w.failed, w.nothing, w.notSearched]) {
          assert(said.some((s) => want.test(s)), `${width}px: ${want} in\n${said.join("\n")}`);
        }
        assert(await page.evaluate(() => document.scrollingElement.scrollWidth <= innerWidth + 1), `no sideways scroll at ${width}px`);
        assert.deepEqual(errors, []);
      }
    });
  }
}

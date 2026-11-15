// Run with Node's test runner and Playwright on the module path; see README.md.
// An image described for a model that can't see, after the model asked
// first to describe it failed, says so in its Routing row: which model was
// asked first, for whom, why it couldn't, and which one described it in its
// place (kelvinjason11 on X: one describer failing failed every turn with
// an image). In every language, in both engines, at a narrow width too.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const words = {
  en: {
    picked: /probe\/eye, the Image recognition model picked in Settings › Models, couldn't describe an image for Codex's zai\/glm-5\.2: 502 upstream closed\. So uni-switch had other\/bigeye, the next model it can use that sees, describe it in its place\. Not a turn of the conversation\.$/,
    auto: /probe\/eye, the model uni-switch picked to describe images, couldn't describe an image for Codex's call to uni-switch's analyze_image MCP tool: it failed\. So uni-switch had other\/bigeye/,
  },
  zh: {
    picked: /probe\/eye 是在 设置 › 模型 里选的识图模型，但它没能为 Codex 的 zai\/glm-5\.2 描述图片：502 upstream closed。所以 uni-switch 改让下一个可用的识图模型 other\/bigeye 来描述。不属于对话轮次。$/,
    auto: /probe\/eye 是 uni-switch 自动选的识图模型，但它没能为 Codex 对 uni-switch 的 analyze_image MCP 工具的调用 描述图片：它出错了。/,
  },
  "zh-TW": {
    picked: /probe\/eye 是在 設定 › 模型 裡選的識圖模型，但它沒能為 Codex 的 zai\/glm-5\.2 描述圖片：502 upstream closed。所以 uni-switch 改讓下一個可用的識圖模型 other\/bigeye 來描述。不屬於對話輪次。$/,
    auto: /probe\/eye 是 uni-switch 自動選的識圖模型，但它沒能為 Codex 對 uni-switch 的 analyze_image MCP 工具的呼叫 描述圖片：它出錯了。/,
  },
  ja: {
    picked: /設定 › モデル で選んだ画像認識モデル probe\/eye は Codex の zai\/glm-5\.2 のための画像を説明できませんでした：502 upstream closed。そのため uni-switch は次に使える画像を見られるモデル other\/bigeye に代わりに説明させました。会話のターンではありません。$/,
    auto: /uni-switch が自動で選んだ画像認識モデル probe\/eye は Codex による uni-switch の analyze_image MCP ツールの呼び出し のための画像を説明できませんでした：エラーになりました。/,
  },
  de: {
    picked: /probe\/eye, das unter Einstellungen › Modelle gewählte Bilderkennungsmodell, konnte kein Bild für zai\/glm-5\.2 von Codex beschreiben: 502 upstream closed\. Daher ließ uni-switch other\/bigeye, das nächste nutzbare Modell, das Bilder sieht, es an seiner Stelle beschreiben\. Keine Runde des Gesprächs\.$/,
    auto: /probe\/eye, das von uni-switch gewählte Modell zum Beschreiben von Bildern, konnte kein Bild für den Aufruf von Codex an uni-switchs MCP-Werkzeug analyze_image beschreiben: es ist fehlgeschlagen\./,
  },
};

const now = new Date();
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const route = (id, f) => ({
  id, seq: id, time: new Date(now.getTime() - id * 60e3).toISOString(), agent: "uni-switch", model: "other/bigeye", provider: "other",
  kind: "vision", for: { agent: "codex", instead: "probe/eye", ...f },
  order: [{ id: "other", provider: "other", name: "Other", who: "a@b.c", kind: "account", model: "bigeye" }],
  tries: [{ id: "other", model: "bigeye", start: new Date(now.getTime() - id * 60e3).toISOString(), done: true, status: 200, ms: 900 }],
  done: true, status: 200, ms: 900, tokens: 1200,
});
// newest first: the picked describer that failed, and uni-switch's own pick
// that failed an agent's analyze_image call with no reason kept
const routes = [route(1, { model: "zai/glm-5.2", picked: true, why: "502 upstream closed" }), route(2, { mcp: true })];

function serve(lang) {
  const settings = { lang, theme: "light", vision: "probe/eye", gateway: "http://127.0.0.1:3999", port: 3999 };
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
    test(`${engine} ${lang}: an image described in place of the model asked first says which and why`, async (t) => {
      for (const width of [1100, 520]) {
        const { page, errors } = await open(t, engine, lang, width);
        await page.locator(".rt-days .rt-day").nth(1).click();
        // each row's story, read once its own is drawn
        const said = [];
        for (const i of [0, 1]) {
          const req = page.locator(".rt-req").nth(i);
          await req.waitFor();
          await req.click();
          const h = await page.waitForFunction((prev) => {
            const s = document.querySelector(".rt-steps li.kind")?.textContent.trim();
            return s && s !== prev && s;
          }, said.at(-1) || "");
          said.push(await h.jsonValue());
        }
        for (const want of [w.picked, w.auto]) {
          assert(said.some((s) => want.test(s)), `${width}px: ${want} in\n${said.join("\n")}`);
        }
        assert(await page.evaluate(() => document.scrollingElement.scrollWidth <= innerWidth + 1), `no sideways scroll at ${width}px`);
        assert.deepEqual(errors, []);
      }
    });
  }
}

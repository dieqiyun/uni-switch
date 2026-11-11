// Run with Node's test runner and Playwright on the module path; see README.md.
// #1832 (wfb835): Settings › Network's Proxy line says the global proxy, or
// going direct, holds for the requests to vendors that have no proxy of
// their own: a provider, account or key that names one (or Direct) keeps it
// whatever this row is set to (netproxy.With), as the provider editor's
// Direct option says. Off, Auto with no system proxy, and a proxy set here,
// in every language, at a window's width and at 420px, in both engines.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn, openPart } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");

const words = {
  en: {
    off: "Off: requests to vendors without a proxy of their own, and update checks, go direct",
    none: "No system proxy found; requests to vendors without a proxy of their own, and update checks, go direct",
    settings: "Requests to vendors without a proxy of their own, and update checks, go through http://127.0.0.1:7890",
  },
  zh: {
    off: "已关闭：未单独设置代理的模型厂商请求和检查更新都直连",
    none: "未发现系统代理，未单独设置代理的模型厂商请求和检查更新都直连",
    settings: "未单独设置代理的模型厂商请求和检查更新都经由 http://127.0.0.1:7890",
  },
  "zh-TW": {
    off: "已關閉：未單獨設定代理的模型廠商請求和檢查更新都直連",
    none: "未發現系統代理，未單獨設定代理的模型廠商請求和檢查更新都直連",
    settings: "未單獨設定代理的模型廠商請求和檢查更新都經由 http://127.0.0.1:7890",
  },
  ja: {
    off: "オフ：独自のプロキシを設定していないベンダーへのリクエストとアップデート確認は直接接続します",
    none: "システムプロキシが見つかりません。独自のプロキシを設定していないベンダーへのリクエストとアップデート確認は直接接続します",
    settings: "独自のプロキシを設定していないベンダーへのリクエストとアップデート確認は http://127.0.0.1:7890 を経由します",
  },
  de: {
    off: "Aus: Anfragen an Anbieter ohne eigenen Proxy und Update-Prüfungen laufen direkt",
    none: "Kein System-Proxy gefunden; Anfragen an Anbieter ohne eigenen Proxy und Update-Prüfungen laufen direkt",
    settings: "Anfragen an Anbieter ohne eigenen Proxy und Update-Prüfungen laufen über http://127.0.0.1:7890",
  },
};

// the backend's settings for each source: Off is proxy "direct", Auto
// finding none is "", a proxy set here is its URL
const proxyOf = { off: ["direct", "none"], none: ["", "none"], settings: ["http://127.0.0.1:7890", "http://127.0.0.1:7890"] };

function settingsPayload(lang, source) {
  const [proxy, proxyNow] = proxyOf[source];
  return {
    theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd",
    dock: false, dockWindow: false, proxy, redact: false, redactPersonal: false, redactWords: [],
    codexWarmup: "", claudeWarmup: "", codexWarmAt: "", claudeWarmAt: "", workbuddyCheckin: false, noStats: false,
    trayUsage: "", trayUsageEvery: 3, vision: "", imageGen: "",
    version: "0.1.400", dir: "~/.config/uni-switch", gateway: "http://127.0.0.1:3425",
    proxyNow, proxySource: source, login: false,
    visionModels: [], imageGenModels: [], workbuddyCheckins: [], lanURLs: [],
    fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
  };
}

function server(lang, source) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: settingsPayload(lang, source) });
    if (url.pathname === "/api/settings") return json(settingsPayload(lang, source));
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": the Proxy line holds for requests without a proxy of their own", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of Object.keys(words)) {
      for (const width of [900, 420]) {
        await t.test(`${lang} at ${width}px`, async () => {
          const ctx = await browser.newContext({ viewport: { width, height: 640 }, reducedMotion: "reduce" });
          try {
            for (const source of ["off", "none", "settings"]) {
              const page = await ctx.newPage();
              page.setDefaultTimeout(5000);
              page.setDefaultNavigationTimeout(60000);
              const errors = [];
              page.on("pageerror", (e) => errors.push(e.message));
              await page.route("**/*", server(lang, source));
              await watchSettingsReads(page);
              await page.goto("http://uni-switch.test/?view=settings");
              await settingsDrawn(page);
              await openPart(page, "network");
              const sub = page.locator("#proxySub");
              await sub.waitFor({ state: "visible" });
              assert.equal((await sub.textContent()).trim(), words[lang][source], `${lang} ${source}`);
              // the longer line wraps inside its row, never past the window
              const over = await sub.evaluate((e) => {
                const row = e.closest(".row");
                return row.scrollWidth - row.clientWidth;
              });
              assert.ok(over <= 1, `${lang} ${source} at ${width}: the row overflows by ${over}px`);
              assert.deepEqual(errors, []);
              await page.close();
            }
          } finally {
            await ctx.close();
          }
        });
      }
    }
  });
}

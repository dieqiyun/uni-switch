const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

// Auto affinity counts what the vendor wrote to its cache with what it
// read (vilicvane on Discord: a first turn reads nothing and writes the
// whole prompt). The Routing page says both: a conversation kept for a
// cache just written doesn't read as "read 0 tokens", and one moved says
// it wrote too little as well.
const words = {
  en: { kept: ["read 0 tokens", "wrote 3.0k"], moved: ["read only 300 tokens", "wrote 200"] },
  zh: { kept: ["读取了此会话的 0 tokens", "写入了 3.0k tokens"], moved: ["只从缓存读取了 300 tokens", "写入了 200 tokens"] },
  "zh-TW": { kept: ["讀取了此會話的 0 tokens", "寫入了 3.0k tokens"], moved: ["只從快取讀取了 300 tokens", "寫入了 200 tokens"] },
  ja: { kept: ["0 トークンをキャッシュから読み", "3.0k トークンを書き込みました"], moved: ["300 トークン", "書き込んだのは 200 トークン"] },
  de: { kept: ["0 Token dieser Unterhaltung", "3.0k hineingeschrieben"], moved: ["nur 300 Token", "200 hineingeschrieben"] },
};

function routes(now, why, read, wrote) {
  const time = new Date(now.getTime() - 5000).toISOString();
  const at = new Date(now.getTime() - 65000).toISOString();
  const k1 = { id: "cw#k1", provider: "cw", name: "CW", kind: "key", who: "k1", model: "m" };
  const k2 = { id: "cw#k2", provider: "cw", name: "CW", kind: "key", who: "k2", model: "m" };
  const order = why === "cache" ? [k1, k2] : [k2, k1];
  return [{ id: 1, seq: 1, time, agent: "claude", model: "cw/m", provider: "cw", order, done: true, status: 200, ms: 20,
    affinity: { mode: "", turn: 2, last: "cw#k1", lastTurn: 1, at, cacheRead: read, cacheWrite: wrote, kept: why === "cache", why },
    tries: [{ id: order[0].id, model: "m", start: time, done: true, status: 200, ms: 20 }] }];
}

function serve(lang, getRoutes, now) {
  const assets = path.resolve(__dirname, "../assets");
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs={lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window={};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.has("wait")) return;
      return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 1, rerouted: 0, errors: 0 }, routes: getRoutes() });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file).catch(() => ""), contentType });
  };
}

for (const engine of process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"]) {
  for (const width of [1100, 420]) {
    for (const lang of Object.keys(words)) {
      test(`${engine} ${width} ${lang}: affinity says what the vendor wrote to its cache`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await browser.newPage({ viewport: { width, height: 900 } });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const now = new Date();
        let rs = routes(now, "cache", 0, 3000);
        await page.route("**/*", serve(lang, () => rs, now));
        const story = async () => {
          await page.goto("http://uni-switch.test/?view=routing");
          await page.waitForFunction(() => /\d/.test(document.body.innerText) && document.querySelector(".rt-cap")?.textContent);
          return page.evaluate(() => document.body.innerText + "\n" + document.querySelector(".rt-cap").textContent);
        };
        let text = await story();
        for (const w of words[lang].kept) assert.ok(text.includes(w), `kept: ${w} not in\n${text}`);
        assert.doesNotMatch(text, /\{[rwn]\}/);

        rs = routes(now, "no-cache", 300, 200);
        text = await story();
        for (const w of words[lang].moved) assert.ok(text.includes(w), `moved: ${w} not in\n${text}`);
        assert.doesNotMatch(text, /\{[rwn]\}/);
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true, "no sideways scroll");
        assert.deepEqual(errors, []);
      });
    }
  }
}

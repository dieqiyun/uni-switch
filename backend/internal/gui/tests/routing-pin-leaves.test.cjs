// Run with Node's test runner and Playwright on the module path; see README.md.
// A request picked on the Routing page (a row in its list, or the tray
// panel's row that opened the window on it) holds the stage, and "Back to
// live" lets it go. The page keeps the newest 60 requests. Once the picked
// one had left them, nothing let it go: the stage kept a 13:42 qoder
// request at 15:58, while every request since had gone to WorkBuddy's
// deepseek (#1389, Creat55). Still in the list, it stays picked; gone from
// it, the stage follows live again. In every language, in both engines,
// narrow and wide. No backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = Date.now();
const at = (id) => new Date(now - (200 - id) * 20e3).toISOString();
const qoder = { id: "qoder", provider: "qoder", name: "Qoder", kind: "provider", model: "auto-qfmodel" };
const wb = { id: "workbuddy-ai", provider: "workbuddy-ai", name: "WorkBuddy AI", kind: "provider", model: "deepseek-v4.1" };
const route = (id, w, agent) => ({
  id, seq: id, time: at(id), agent, model: `${w.provider}/${w.model}`, provider: w.provider, done: true,
  order: [w], tries: [{ id: w.id, model: w.model, start: at(id), done: true, status: 200, ms: 900 }], status: 200, ms: 900,
});
const range = (a, b, w, agent) => Array.from({ length: b - a + 1 }, (_, i) => route(a + i, w, agent));
// the qoder session's requests, then the WorkBuddy one's: a few, then many
const batches = [range(1, 5, qoder, "claude"), range(6, 15, wb, "codex"), range(16, 75, wb, "codex")];

function serve(lang, feed) {
  const state = { agents: [{ id: "claude", name: "Claude Code", path: "/t/settings.json", fields: [] }, { id: "codex", name: "Codex", path: "/t/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/gateway/trace") {
      const after = Number(url.searchParams.get("after")) || 0;
      // a batch at a time, each when the test lets it go
      for (let i = 0; i < 400 && !feed.out.some((b) => b[b.length - 1].seq > after); i++) await new Promise((res) => setTimeout(res, 25));
      const b = feed.out.find((b) => b[b.length - 1].seq > after) || [];
      return json({ mine: true, now: new Date(now).toISOString(), seq: b.length ? b[b.length - 1].seq : after, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: structuredClone(b) }).catch(() => {});
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], excluded: [], presets: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await r.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { picked: "How the request at", last: "How the last request was routed", back: "Back to live" },
  zh: { picked: "的请求是怎么路由的", last: "最近一个请求是怎么路由的", back: "回到实时" },
  "zh-TW": { picked: "的請求是怎麼路由的", last: "最近一個請求是怎麼路由的", back: "回到即時" },
  ja: { picked: " のリクエストのルーティング", last: "直近のリクエストのルーティング", back: "ライブに戻る" },
  de: { picked: "Wie die Anfrage um", last: "Wie die letzte Anfrage weitergeleitet wurde", back: "Zurück zu live" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a picked request that has left the list lets the stage follow live again", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const lang of Object.keys(words)) {
      for (const width of [420, 1100]) {
        await t.test(`${lang} ${width}`, async () => {
          const w = words[lang], feed = { out: [batches[0]] };
          const ctx = await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" });
          const page = await ctx.newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", serve(lang, feed));
          await page.goto("http://uni-switch.test/?view=routing");
          const head = page.locator(".rt-log-head");
          const models = () => page.locator(".rt-accts code.mdl").allTextContents();
          await page.locator(".rt-req").nth(4).waitFor();
          await page.locator(".rt-req").nth(0).and(page.locator('[aria-pressed="true"]')).waitFor();

          // the qoder session's middle request, picked from the list
          const row = page.locator(".rt-req").nth(2);
          // wheeled down to it as the reader would, when it's past the window's foot
          await page.mouse.move(width / 2, 300);
          for (let i = 0; i < 60; i++) {
            const b = await row.boundingBox(), foot = await page.evaluate(() => document.querySelector("#view-routing").getBoundingClientRect().bottom);
            if (b && b.y + b.height < foot - 20) break;
            await page.mouse.wheel(0, 80);
            await page.waitForTimeout(50);
          }
          await page.waitForTimeout(300);
          await row.click();
          await page.waitForFunction((s) => document.querySelector(".rt-log-head")?.textContent.includes(s), w.picked);
          assert.equal(await head.locator("button", { hasText: w.back }).count(), 1);

          // newer ones to another provider: still in the list, it stays picked
          feed.out.push(batches[1]);
          await page.locator(".rt-req").nth(14).waitFor();
          await page.waitForTimeout(300);
          assert((await head.textContent()).includes(w.picked), await head.textContent());
          assert.deepEqual(await models(), ["auto-qfmodel"]);

          // past the 60 the page keeps, it has left the list: live again
          feed.out.push(batches[2]);
          await page.waitForFunction((s) => document.querySelector(".rt-log-head")?.textContent.includes(s), w.last, { timeout: 5000 })
            .catch(async () => assert.fail("the stage stayed on the picked request: " + await head.textContent()));
          assert.equal(await head.locator("button", { hasText: w.back }).count(), 0);
          await page.waitForFunction(() => document.querySelector(".rt-req")?.getAttribute("aria-pressed") === "true");
          await page.waitForFunction(() => [...document.querySelectorAll(".rt-accts code.mdl")].every((c) => c.textContent === "deepseek-v4.1"));
          assert(!(await models()).includes("auto-qfmodel"));
          // nothing runs off a narrow window
          assert.deepEqual(await page.evaluate(() => [document.documentElement.scrollWidth <= innerWidth, scrollX]), [true, 0]);
          await ctx.close();
        });
      }
    }
    assert.deepEqual(errors, []);
  });
}

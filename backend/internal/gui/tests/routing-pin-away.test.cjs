// Run with Node's test runner and Playwright on the module path; see README.md.
// A request picked on the Routing page holds the stage while the reader
// is with it. Once they had gone (to another app, another page, the window
// hidden) and requests came in meanwhile, it still held it when they came
// back: the stage and "How the request at 16:36 was routed" stayed on a
// Mozilla request the agent had canceled, while the list under it filled
// with newer deepseek requests (未寒 on Discord, 10-28; the picked one was
// still among the 60 the page keeps, so #1389's rule never let it go).
// Coming back to requests that came while away, the stage follows live
// again; coming back to none, the pick stays; a reader still there keeps
// it as requests come. Both engines, en and zh, narrow and wide. No
// backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = Date.now();
const at = (id) => new Date(now - (200 - id) * 20e3).toISOString();
const qw = { id: "qwenwork", provider: "qwenwork", name: "QwenWork", kind: "provider", model: "flash" };
const ds = { id: "deepseek", provider: "deepseek", name: "DeepSeek", kind: "provider", model: "deepseek-v4.1-flash" };
const route = (id, w, agent, failed) => ({
  id, seq: id, time: at(id), agent, model: `${w.provider}/${w.model}`, provider: w.provider, done: true,
  order: [w], tries: [{ id: w.id, model: w.model, start: at(id), done: true, status: failed ? 499 : 200, ms: 900, ...(failed ? { error: "the agent canceled the request" } : {}) }],
  status: failed ? 499 : 200, ms: 900, ...(failed ? { error: "the agent canceled the request" } : {}),
});
// a few to qwenwork, the middle one canceled; then deepseek's, few enough
// that the picked one stays among the 60 the page keeps
const first = [route(1, qw, "mozilla"), route(2, qw, "mozilla"), route(3, qw, "mozilla", true), route(4, qw, "mozilla"), route(5, qw, "mozilla")];
const more = (from) => Array.from({ length: 8 }, (_, i) => route(from + i, ds, "codex"));

function serve(lang, feed) {
  const state = { agents: [{ id: "codex", name: "Codex", path: "/t/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/gateway/trace") {
      const after = Number(url.searchParams.get("after")) || 0;
      // a long poll gives up after a second, as the gateway's does after its wait
      for (let i = 0; i < 40 && !feed.out.some((b) => b[b.length - 1].seq > after); i++) await new Promise((res) => setTimeout(res, 25));
      const b = feed.out.find((b) => b[b.length - 1].seq > after) || [];
      return json({ mine: true, now: new Date(now).toISOString(), seq: b.length ? b[b.length - 1].seq : after, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: structuredClone(b) }).catch(() => {});
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], excluded: [], presets: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/window/")) return r.fulfill({ status: 204 });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await r.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { picked: "How the request at", last: "How the last request was routed", back: "Back to live" },
  zh: { picked: "的请求是怎么路由的", last: "最近一个请求是怎么路由的", back: "回到实时" },
};

// the ways a reader goes from the page, and comes back to it
const ways = {
  "another app": {
    leave: (page) => page.evaluate(() => window.dispatchEvent(new Event("blur"))),
    back: (page) => page.evaluate(() => window.dispatchEvent(new Event("focus"))),
  },
  "another page": {
    leave: (page) => page.locator('#nav button[data-view="agents"]').click(),
    back: (page) => page.locator('#nav button[data-view="routing"]').click(),
  },
  "the window hidden": {
    leave: (page) => page.evaluate(() => window.__hide(true)),
    back: (page) => page.evaluate(() => window.__hide(false)),
  },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a picked request lets the stage go live when the reader comes back to newer requests", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const [lang, width] of [["en", 1100], ["zh", 420]]) {
      for (const [way, how] of Object.entries(ways)) {
        await t.test(`${lang} ${width} ${way}`, async () => {
          const w = words[lang], feed = { out: [first] };
          const ctx = await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" });
          await ctx.addInitScript(() => {
            window.__hid = false;
            Object.defineProperty(Document.prototype, "hidden", { configurable: true, get: () => window.__hid });
            Object.defineProperty(Document.prototype, "visibilityState", { configurable: true, get: () => (window.__hid ? "hidden" : "visible") });
            window.__hide = (h) => { window.__hid = h; document.dispatchEvent(new Event("visibilitychange")); };
          });
          const page = await ctx.newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", serve(lang, feed));
          await page.goto("http://uni-switch.test/?view=routing");
          const head = page.locator(".rt-log-head");
          const headSays = (s, why) => page.waitForFunction((s) => document.querySelector(".rt-log-head")?.textContent.includes(s), s)
            .catch(async () => assert.fail(why + ": " + await head.textContent()));
          const models = () => page.locator(".rt-accts code.mdl").allTextContents();
          await page.locator(".rt-req").nth(4).waitFor();

          // the canceled request, picked from the list
          await page.locator(".rt-req").nth(2).click();
          await headSays(w.picked, "the pick didn't hold the stage");

          // a reader still there keeps it as requests come
          feed.out.push(more(6));
          await page.locator(".rt-req").nth(12).waitFor();
          await page.waitForTimeout(300);
          assert((await head.textContent()).includes(w.picked), "a reader still there lost the pick: " + await head.textContent());

          // gone and back with nothing new: still picked
          await how.leave(page);
          await page.waitForTimeout(300);
          await how.back(page);
          await page.waitForTimeout(500);
          assert((await head.textContent()).includes(w.picked), "back to nothing new, the pick was lost: " + await head.textContent());
          assert.equal(await head.locator("button", { hasText: w.back }).count(), 1);

          // gone while more came, still among the 60 kept: back, it is live.
          // Out of sight, the poll has given up its wait and sleeps by the
          // time they come, so they are read only once the reader is back.
          await how.leave(page);
          await page.waitForTimeout(way === "another app" ? 200 : 1600);
          feed.out.push(more(14));
          if (way === "another app") await page.locator(".rt-req").nth(20).waitFor();
          else await page.waitForTimeout(400);
          await how.back(page);
          await headSays(w.last, "back to newer requests, the stage stayed on the picked one");
          assert.equal(await head.locator("button", { hasText: w.back }).count(), 0);
          await page.locator(".rt-req").nth(20).waitFor();
          await page.waitForFunction(() => document.querySelector(".rt-req")?.getAttribute("aria-pressed") === "true");
          await page.waitForFunction(() => [...document.querySelectorAll(".rt-accts code.mdl")].every((c) => c.textContent === "deepseek-v4.1-flash"));
          assert(!(await models()).includes("flash"));
          // nothing runs off a narrow window
          assert.deepEqual(await page.evaluate(() => [document.documentElement.scrollWidth <= innerWidth, scrollX]), [true, 0]);
          await ctx.close();
        });
      }
    }
    assert.deepEqual(errors, []);
  });
}

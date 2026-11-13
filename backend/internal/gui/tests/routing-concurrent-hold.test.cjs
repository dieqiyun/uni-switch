// Run with Node's test runner and Playwright on the module path; see README.md.
// qiang2040 on Discord ("狂闪", a screen recording): with several Codex
// conversations sending at once, one on group/glm-5.3-flash-free and one on
// group/deepseek-v4.1-flash, the Routing page's top card jumped between
// them on every trace update, and to "uni-switch hasn't routed this request
// yet" for each new request before it was routed. The card followed the
// newest request. It now stays on the request it tells of until that one
// is done, and a newer request routing hasn't picked for yet doesn't take
// it from one that was routed. Two conversations' requests interleaved
// here; every frame's head is watched. Every language, both engines.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const iso = (ms) => new Date(ms).toISOString();
const t0 = Date.now();
const GROUPS = {
  glm: { id: "glm-5.3-flash-free", name: "GLM Flash", routing: "", affinity: "", members: ["zhipu/glm-5.3-flash"] },
  ds: { id: "deepseek-v4.1-flash", name: "DeepSeek Flash", routing: "", affinity: "", members: ["deepseek/deepseek-v4.1-flash"] },
};
const SEAT = {
  glm: { id: "zhipu-key", provider: "zhipu", name: "Zhipu", kind: "key", model: "glm-5.3-flash", known: true },
  ds: { id: "deepseek-key", provider: "deepseek", name: "DeepSeek", kind: "key", model: "deepseek-v4.1-flash", known: true },
};
// a request of conversation g as the trace has it at a step: "held" (not
// routed yet), "going" (routed, under way), "done"
let seq = 10;
function req(id, g, step) {
  const at = t0 - (200 - id) * 1000, done = step === "done", routed = step !== "held";
  return {
    id, seq: ++seq, time: iso(at), agent: "codex", session: "conv-" + g, model: "group/" + GROUPS[g].id, provider: "uni-switch", group: GROUPS[g],
    order: routed ? [SEAT[g]] : [],
    tries: routed ? [{ id: SEAT[g].id, model: SEAT[g].model, start: iso(at), done, ...(done ? { status: 200, ms: 3000 } : {}) }] : [],
    done, ...(done ? { status: 200, ms: 3000, tokens: 900 } : {}),
  };
}
const first = [req(100, "glm", "done")];

function serve(lang, feed) {
  const state = { agents: [{ id: "codex", name: "Codex", path: "/test/codex", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/gateway/trace") {
      const trace = (routes) => json({ mine: true, now: new Date().toISOString(), seq: routes.length ? routes[routes.length - 1].seq : Number(url.searchParams.get("after")), totals: { requests: 0, rerouted: 0, errors: 0 }, routes });
      if (!url.searchParams.get("wait")) return trace(first);
      const rs = await new Promise((res) => { feed.next = res; });
      return trace(rs);
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ groups: Object.values(GROUPS), models: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file).catch(() => ""), contentType });
  };
}

const NOT_ROUTED = "uni-switch hasn't routed this request yet, so who takes it isn't picked yet.";

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: the routing card stays on one request while others come at once`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      await t.test(lang, async () => {
        const context = await browser.newContext({ viewport: { width: 1100, height: 900 } });
        t.after(() => context.close());
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const feed = { next: null };
        await page.route("**/*", serve(lang, feed));
        await page.goto("http://uni-switch.test/?view=routing");
        await page.locator(".rt-req").first().waitFor();
        await page.locator(".rt-what b").waitFor();
        const notRouted = await page.evaluate(([l, s]) => (l === "en" ? s : I18N[l]?.[s]), [lang, NOT_ROUTED]);
        assert.ok(notRouted, `the not-routed line has its ${lang}`);

        // every frame from here on: whose route the head names, and its line
        await page.evaluate(() => {
          window.__frames = [];
          window.__on = true;
          const tick = () => {
            window.__frames.push([document.querySelector(".rt-what b")?.textContent || "", document.querySelector(".rt-mode")?.textContent || ""]);
            if (window.__on) requestAnimationFrame(tick);
          };
          requestAnimationFrame(tick);
        });
        const send = async (...rs) => {
          for (let i = 0; i < 100 && !feed.next; i++) await page.waitForTimeout(50);
          const next = feed.next;
          assert(next, "the page asks for the next trace");
          feed.next = null;
          next(rs);
          await page.waitForTimeout(350);
        };
        const head = () => page.locator(".rt-what b").textContent();

        // conversation glm's request 101 is routed and under way
        await send(req(101, "glm", "going"));
        assert.equal(await head(), "GLM Flash");
        // conversation ds sends 102: not routed yet, then routed; glm's
        // 101 is still under way, and the card stays on it
        await send(req(102, "ds", "held"));
        await send(req(102, "ds", "going"));
        await send(req(101, "glm", "going"), req(102, "ds", "going"));
        assert.equal(await head(), "GLM Flash", "the card stays on 101 while it is under way");
        // 101 is done: the card goes on to the newest, ds's 102
        await send(req(101, "glm", "done"));
        assert.equal(await head(), "DeepSeek Flash");
        // glm's next, 103, comes in not routed yet: 102 still holds it
        await send(req(103, "glm", "held"));
        await send(req(102, "ds", "going"), req(103, "glm", "held"));
        assert.equal(await head(), "DeepSeek Flash", "the card stays on 102 while it is under way");
        // 102 is done while 103 is still held: a routed one stays rather
        // than the card dropping to "not routed yet"
        await send(req(102, "ds", "done"));
        assert.equal(await head(), "DeepSeek Flash", "a request not routed yet doesn't take the card from one routed");
        // 103 is routed: now it is told
        await send(req(103, "glm", "going"));
        assert.equal(await head(), "GLM Flash");

        const frames = await page.evaluate(() => { window.__on = false; return window.__frames; });
        assert(frames.length > 20, `frames were watched: ${frames.length}`);
        // the head named each route in one run: no going back and forth
        const runs = frames.map((f) => f[0]).filter((x, i, a) => i === 0 || x !== a[i - 1]);
        assert.deepEqual(runs, ["GLM Flash", "DeepSeek Flash", "GLM Flash"], `the head switched only when the request it told of was done: ${JSON.stringify(runs)}`);
        assert(!frames.some((f) => f[1] === notRouted), "the card never dropped to not routed yet");
        assert.deepEqual(errors, []);
      });
    }
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// The Routing stage holds every agent sending at once, and each one's
// route's accounts and keys. Several agents on several routes were drawn
// in one column under a header naming one route alone (hoobnn on Discord:
// "DeepSeek-V4.1-Flash · 路由组 · 启用 3 个 · 3 个 Agent 同时在发" over five
// rows, two of them Pi's zcode and OpenCode's Kimi Code, "两个没有使用的路
// 由"): nothing said those two were other agents' routes, not the group's.
// With more than one route on the stage, each route's rows hang under a
// heading naming it (the group, or the provider and the model asked for)
// and who sends to it; the group's heading has its three members and no
// other. One route alone has no heading, as before. Every row's tooltip is
// its own id. At 420px nothing spills. Every language, both engines; no
// backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const iso = (s = 0) => new Date(Date.now() - s * 1e3).toISOString();
// Pi on a Z.ai team account, OpenCode on Kimi Code's one key: neither in the group
const zai = { id: "zcode@lin@x.test", provider: "zcode", name: "ZCode", kind: "account", who: "lin@x.test", plan: "Team Pro", model: "GLM-5.3-Flash", routing: "" };
const kimi = { id: "kimi-code-cn", provider: "kimi-code-cn", name: "Kimi Code (China)", kind: "provider", model: "k3.1-pro", routing: "" };
// DeepSeek Harness on the group uni-switch found for deepseek-v4.1-flash, in order
const ark = { id: "volcengine", provider: "volcengine", name: "Volcengine Ark", kind: "provider", model: "deepseek-v4.1-flash", routing: "" };
const ent = { id: "dfm@hoob@gmail.com", provider: "dfm", name: "DF Model", kind: "account", who: "hoob@gmail.com", plan: "Enterprise", model: "dfmodel", routing: "", known: true, used: 0 };
const qwen = { id: "qwen-cn", provider: "qwen-cn", name: "Qwen (China)", kind: "provider", model: "vanchin/deepseek-v4.1-flash", routing: "" };
const group = { id: "deepseek-v4.1-flash", ref: "auto/deepseek-v4.1-flash", name: "DeepSeek-V4.1-Flash", routing: "order", auto: true,
  members: ["volcengine/deepseek-v4.1-flash", "dfm/dfmodel", "qwen-cn/vanchin/deepseek-v4.1-flash"] };
const live = (w) => [{ id: w.id, model: w.model, start: iso(1), done: false }];
const route = (id, agent, model, provider, order, extra = {}) => ({ id, seq: id, time: iso(2), agent, model, provider, order, tries: live(order[0]), done: false, ...extra });
const pi = (id) => route(id, "pi", "zcode/GLM-5.3-Flash", "zcode", [zai]);
const dsh = (id) => route(id, "dsh", "auto/deepseek-v4.1-flash", "volcengine", [ark, ent, qwen], { group });
const oc = (id) => route(id, "opencode", "kimi-code-cn/k3.1-pro", "kimi-code-cn", [kimi]);

function serve(lang, feed) {
  return async (request) => {
    const url = new URL(request.request().url());
    const json = (data) => request.fulfill({ json: data });
    if (url.pathname === "/boot.js") return request.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return request.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [["pi", "Pi"], ["dsh", "DeepSeek Harness"], ["opencode", "OpenCode"]].map(([id, name]) => ({ id, name, path: `/test/${id}.json`, fields: [] })), profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) {
        if (feed.queued.length) return json(feed.queued.shift());
        feed.pending.push(request);
        return;
      }
      return json({ mine: true, now: iso(), seq: 1, totals: { requests: 1, rerouted: 0, errors: 0 }, routes: [dsh(1)] });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await request.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// what the stage lists, top to bottom: a heading ("§" its name, its
// model or group, how many it routes to) or a row (its tooltip's first
// line, which is its id, and how far in it hangs)
const stage = (page) => page.locator(".rt-accts > li").evaluateAll((lis) => lis.map((li) => li.classList.contains("rt-sub")
  ? "§" + li.querySelector("b").textContent + " " + li.querySelector("code").textContent
  : (li.title || "").split("\n")[0] + "@" + (li.style.getPropertyValue("--depth") || "0")));

const spills = (page, sel) => page.locator(sel).evaluateAll((els) => els.filter((e) => e.scrollWidth > e.clientWidth + 1 || e.getBoundingClientRect().right > document.documentElement.clientWidth)
  .map((e) => e.className + ": " + e.textContent.trim().slice(0, 60)));

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [1100, 420]) {
      test(`${engine} ${lang} ${width}px: three agents on three routes, each route's rows under its own heading`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await browser.newPage({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
        page.setDefaultTimeout(6000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const feed = { pending: [], queued: [], seq: 1 };
        await page.route("**/*", serve(lang, feed));
        const send = (...routes) => {
          const d = { mine: true, now: iso(), seq: ++feed.seq, totals: { requests: feed.seq }, routes };
          const r = feed.pending.shift();
          if (r) r.fulfill({ json: d }); else feed.queued.push(d);
        };
        const say = (key, vars = {}) => page.evaluate(([lang, key, vars]) => {
          const s = (lang !== "en" && I18N[lang]?.[key]) || key;
          return s.replace(/\{(\w+)\}/g, (_, k) => vars[k] ?? `{${k}}`);
        }, [lang, key, vars]);
        await page.goto("http://uni-switch.test/?view=routing");
        await page.locator(".rt-accts > li[title]").nth(2).waitFor();

        // DeepSeek Harness alone: its group's three, no heading
        assert.deepEqual(await stage(page), ["volcengine@0", "dfm@hoob@gmail.com@0", "qwen-cn@0"]);

        // Pi and OpenCode start sending to routes of their own, and
        // DeepSeek Harness sends again: the stage of the report
        send(pi(2), oc(3));
        send(dsh(4));
        await page.waitForFunction(() => document.querySelectorAll(".rt-accts > li.rt-sub").length === 3);
        await page.waitForFunction(() => document.querySelectorAll(".rt-srcs .rt-node").length === 3);
        // the header still tells of the latest request, the group's
        await page.waitForFunction(() => document.querySelector(".rt-what b")?.textContent === "DeepSeek-V4.1-Flash");
        assert.deepEqual(await stage(page), [
          "§DeepSeek-V4.1-Flash auto/deepseek-v4.1-flash", "volcengine@1", "dfm@hoob@gmail.com@1", "qwen-cn@1",
          "§ZCode zcode/GLM-5.3-Flash", "zcode@lin@x.test@1",
          "§Kimi Code (China) kimi-code-cn/k3.1-pro", "kimi-code-cn@1",
        ]);
        // each heading says who sends to it, and how the group routes
        const heads = page.locator(".rt-accts > li.rt-sub");
        assert.equal((await heads.nth(0).getAttribute("title")).split("\n")[0], await say("Sent by {agents}", { agents: "DeepSeek Harness" }));
        assert.equal((await heads.nth(1).getAttribute("title")).split("\n")[0], await say("Sent by {agents}", { agents: "Pi" }));
        assert.equal((await heads.nth(2).getAttribute("title")).split("\n")[0], await say("Sent by {agents}", { agents: "OpenCode" }));
        assert.equal(await heads.nth(0).locator("i").textContent(), await say("In order"));
        // a provider with one key routes nothing: its heading says no way
        assert.equal(await heads.nth(2).locator("i").textContent(), "");
        // each heading has a wire from uni-switch, and lights while its own answers
        assert.equal(await page.locator(".rt-wires path[d]").count(), 3 + 3 + 5);
        for (let i = 0; i < 3; i++) assert.equal(await heads.nth(i).evaluate((li) => li.classList.contains("on")), true);

        // the pointer on Kimi Code's row is on Kimi Code's row
        const k = page.locator(".rt-accts > li", { has: page.locator(".who", { hasText: /^Kimi Code \(China\)$/ }) });
        // (tall enough that nothing scrolls: a scroll under the pointer is
        // the test's, not the page's)
        await page.setViewportSize({ width, height: 1600 });
        await page.waitForFunction(() => document.querySelector(".rt-accts > li:last-child").getBoundingClientRect().bottom < innerHeight);
        await k.hover();
        await page.waitForFunction(() => document.querySelector(".rt-accts > li:hover")?.title.split("\n")[0] === "kimi-code-cn");

        assert.deepEqual(await spills(page, ".rt-accts > li"), []);
        assert.deepEqual(errors, []);
      });
    }
  }
}

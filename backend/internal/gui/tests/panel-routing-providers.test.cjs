// Run with Node's test runner and Playwright on the module path; see README.md.
// The tray panel's Routing stage has three places on the right. Each was a
// provider's account, so one provider whose requests went from account to
// account filled all three, and the providers other sessions used weren't
// drawn (#1389, Creat55: qoder's accounts, while other sessions went to
// WorkBuddy). Now a place is a provider, naming the account its latest try
// went to. And a request still under way when the page dropped it from the
// 60 it keeps (a burst of others) went on waiting at its account for three
// minutes, holding its dot and its place; it ends now, as the window's
// stage ends one. In every language, in both engines. No backend: the API
// is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = Date.now();
const at = (id) => new Date(now - (200 - id) * 5e3).toISOString();
const claude = { id: "claude:a", provider: "claude", name: "Claude", kind: "account", who: "ann@example.com", model: "claude-opus-5" };
const wb = { id: "workbuddy-ai", provider: "workbuddy-ai", name: "WorkBuddy AI", kind: "provider", model: "deepseek-v4.1" };
const q = (n) => ({ id: "qoder:" + n, provider: "qoder", name: "Qoder", kind: "account", who: "q" + n, model: "auto-qfmodel" });
const tryOf = (w, id, extra = {}) => ({ id: w.id, model: w.model, start: at(id), done: true, status: 200, ms: 900, ...extra });
const route = (id, agent, order, tries, extra = {}) => ({
  id, seq: id, time: at(id), agent, model: `${order[0].provider}/${order[0].model}`, provider: order[0].provider, order, tries,
  done: true, status: tries[tries.length - 1].status, ms: 900, ...extra,
});
// one session on Claude, one on WorkBuddy, and the newest on qoder, going
// from account to account
const first = [
  route(1, "claude", [claude], [tryOf(claude, 1)]),
  route(2, "codex", [wb], [tryOf(wb, 2)]),
  route(3, "opencode", [q(1)], [tryOf(q(1), 3)]),
  route(4, "opencode", [q(2)], [tryOf(q(2), 4)]),
  route(5, "opencode", [q(1), q(3)], [tryOf(q(1), 5, { status: 429, fail: "rate" }), tryOf(q(3), 5)]),
  route(6, "opencode", [q(3), q(4)], [tryOf(q(3), 6, { status: 429, fail: "rate" }), tryOf(q(4), 6)]),
];
// a qoder request under way, then a burst of WorkBuddy's past the 60 kept
const underWay = [route(7, "opencode", [q(2)], [tryOf(q(2), 7, { done: false, status: 0 })], { done: false, status: 0 })];
const burst = Array.from({ length: 63 }, (_, i) => route(8 + i, "codex", [wb], [tryOf(wb, 8 + i)]));

function serve(lang, feed) {
  const state = {
    agents: ["claude", "codex", "opencode"].map((id) => ({ id, name: { claude: "Claude Code", codex: "Codex", opencode: "OpenCode" }[id], path: `/t/${id}`, fields: [] })),
    profiles: [], settings: { lang, theme: "light" },
  };
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data) => r.fulfill({ json: data }).catch(() => {});
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/gateway/trace") {
      const after = Number(url.searchParams.get("after")) || 0;
      // a batch at a time, each when the test lets it go
      for (let i = 0; i < 400 && !feed.out.some((b) => b[b.length - 1].seq > after); i++) await new Promise((res) => setTimeout(res, 25));
      const b = feed.out.find((b) => b[b.length - 1].seq > after) || [];
      return json({ mine: true, now: new Date().toISOString(), seq: b.length ? b[b.length - 1].seq : after, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: structuredClone(b) });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/usage") return json({ calls: 0, input: 0, output: 0, cache_read: 0, cache_write: 0, reasoning: 0, cost: 0, series: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/window/fit") return r.fulfill({ status: 204 });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await r.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const places = (page) => page.locator(".pr-stage").evaluate((s) => [...s.querySelectorAll(".ps-dst")].map((e) => [e.querySelector(".ps-name").textContent, e.className.split(" ").pop(), e.title]));

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": the panel's stage has a place a provider, and a request dropped under way ends", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      await t.test(lang, async () => {
        const feed = { out: [first] };
        const ctx = await browser.newContext({ viewport: { width: 440, height: 640 }, reducedMotion: "no-preference" });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, feed));
        await page.goto("http://uni-switch.test/?mode=panel");
        await page.locator('[data-ptab="routing"]').click();
        await page.locator(".pr-req").nth(first.length - 1).waitFor();
        await page.locator(".pr-stage .ps-dst").first().waitFor();

        // three providers, each naming the account its latest try went to
        assert.deepEqual(await places(page), [
          ["Qoderq4", "ok", "Qoder · q4"],
          ["WorkBuddy AI", "ok", "WorkBuddy AI"],
          ["Claudeann@example.com", "ok", "Claude · ann@example.com"],
        ]);
        assert.deepEqual(await page.locator(".pr-stage .ps-ag .ps-name").allTextContents(), ["OpenCode", "Codex", "Claude Code"]);
        // nothing runs off the panel
        assert(await page.locator(".pr-stage").evaluate((s) => [...s.querySelectorAll(".ps-node")].every((n) => { const a = n.getBoundingClientRect(), b = s.getBoundingClientRect(); return a.left >= b.left - 0.5 && a.right <= b.right + 0.5; })));

        // a qoder request under way: its dot waits at Qoder, which names its account
        feed.out.push(underWay);
        await page.locator(".ps-dot.wait").waitFor({ state: "attached" });
        assert.deepEqual((await places(page))[0], ["Qoderq2", "wait", "Qoder · q2"]);

        // a burst drops it from the 60 kept: its dot and its wait end
        feed.out.push(burst);
        await page.locator(".pr-req").first().filter({ hasText: "workbuddy-ai/deepseek-v4.1" }).waitFor();
        await page.waitForFunction(() => !document.querySelector(".ps-dot") && !document.querySelector(".ps-hub.busy") && !document.querySelector(".ps-wire.on"), null, { timeout: 8000 })
          .catch(async () => assert.fail("a dropped request still flies: " + JSON.stringify(await places(page))));
        assert(!(await places(page)).some(([, how]) => how === "wait"), JSON.stringify(await places(page)));
        await ctx.close();
      });
    }
    assert.deepEqual(errors, []);
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// A request whose vendor's reply names another model than the one asked for
// — sol served as luna — is marked in the Routing page's Requests list and
// in its story; one answered under its dated name is not (v, #feedback:
// 模型路由情况其实有的时候还是想知道的，比如之前 openai 会把 sol 降智到 luna 这样的).
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const at = (i) => new Date(now.getTime() - (i + 1) * 60e3).toISOString();
const key = { id: "relay", provider: "relay", name: "Relay", kind: "provider", model: "gpt-6-sol" };
// newest first: swapped, dated (the same model), none named, and a few
// more (with three rows alone the stage above jiggles in WebKit, on main)
const served = [["gpt-6-luna", true], ["gpt-6-sol-2026-09-01", false], ["", false], ["", false], ["", false], ["", false]];
const routes = served.map(([s, swapped], i) => ({
  id: 100 - i, seq: 100 - i, time: at(i), agent: "codex", model: "relay/gpt-6-sol", provider: "relay",
  order: [key], tries: [{ id: key.id, model: "gpt-6-sol", start: at(i), done: true, status: 200, ms: 900, ...(s ? { served: s } : {}), ...(swapped ? { swapped } : {}) }],
  done: true, status: 200, ms: 900, tokens: 1200, ...(s ? { served: s } : {}), ...(swapped ? { swapped } : {}),
}));

function serve(lang) {
  const state = { agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) await new Promise((r) => setTimeout(r, 20e3));
      return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname === "/api/gateway/history") {
      const d = url.searchParams.get("day");
      return json({ cut: false, days: [{ day, requests: routes.length }], routes: d ? routes : [] });
    }
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const want = {
  en: { row: "served gpt-6-luna", tag: "requested gpt-6-sol · served gpt-6-luna", story: /asked for gpt-6-sol, but its reply says gpt-6-luna answered: likely another model/ },
  zh: { row: "实际 gpt-6-luna", tag: "请求 gpt-6-sol · 实际 gpt-6-luna", story: /请求的是 gpt-6-sol，但回复说是 gpt-6-luna 作答，很可能换成了别的模型/ },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a request served by another model than asked is marked`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const context = await browser.newContext({ viewport: { width: 1100, height: 760 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-served.png`), fullPage: true });
        }
        await browser.close();
      });
      await page.goto("http://uni-switch.test/?view=routing");
      await page.locator(".rt-days .rt-day").nth(1).click();
      await page.locator(".rt-req").nth(served.length - 1).waitFor();

      // the list: the swapped one alone carries the mark, whole, beside the
      // model asked for
      const tags = await page.locator(".rt-req").evaluateAll((rows) => rows.map((r) => r.querySelector(".to .swap")?.textContent || ""));
      assert.deepEqual(tags, [want[lang].row, "", "", "", "", ""]);
      const look = await page.locator(".rt-req .to .swap").evaluate((e) => {
        const row = e.closest(".rt-req").getBoundingClientRect(), b = e.getBoundingClientRect(), cs = getComputedStyle(e);
        return { inRow: b.left >= row.left && b.right <= row.right + 0.5 && b.width > 0, full: e.clientWidth >= e.scrollWidth, border: cs.borderLeftWidth, title: e.title };
      });
      assert(look.inRow && look.full, JSON.stringify(look));
      assert.equal(look.border, "0px");
      assert.match(look.title, want[lang].story);

      // the story says so, and the click leaves the row where it is
      const row = page.locator(".rt-req").nth(0);
      // measure once the page holds still: the day's rows arriving grow the
      // page above the list, and the day button's click is kept in place by
      // scrolling the view, which can land after the last row is in
      const was = await row.evaluate(async (e) => {
        let last = NaN;
        for (let i = 0; i < 60; i++) {
          await new Promise((r) => requestAnimationFrame(r));
          const top = e.getBoundingClientRect().top;
          if (top === last) return top;
          last = top;
        }
        return last;
      });
      const scrolled = await page.evaluate(() => [scrollX, scrollY, document.scrollingElement.scrollTop]);
      await row.click();
      await page.waitForTimeout(600);
      assert(Math.abs((await row.evaluate((e) => e.getBoundingClientRect().top)) - was) <= 1, "picking the request moved the page");
      assert.deepEqual(await page.evaluate(() => [scrollX, scrollY, document.scrollingElement.scrollTop]), scrolled);
      const story = page.locator(".rt-steps li.swap");
      assert.equal(await story.count(), 1);
      assert.equal(await story.locator(".swap").textContent(), want[lang].tag);
      assert.match(await story.textContent(), want[lang].story);
      // the same model under its dated name, and a reply naming none: no mark
      for (const i of [1, 2]) {
        await page.locator(".rt-req").nth(i).click();
        await page.waitForTimeout(300);
        assert.equal(await page.locator(".rt-steps li.swap").count(), 0);
      }
      assert.deepEqual(errors, []);
    });
  }
}

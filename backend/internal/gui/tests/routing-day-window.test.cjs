// Run with Node's test runner and Playwright on the module path; see README.md.
// A day of history is up to 2,000 requests (#2524, UNscientific-9: a day of
// 3,410 froze the window for seconds each time it was picked). The list
// draws only the rows in and near its sight: picked, a day of 2,000 puts a
// few dozen rows in the page, not 2,000, and scrolled, the list draws the
// next ones, down to the day's first request. It looks as it did with all
// of them in: its columns are as wide as the widest row of the day, drawn
// or not, and stay so as it scrolls; a row scrolled to stays where it is
// as the rows around it are drawn; a row picked far down is marked.
// UNI_SWITCH_ROUTING_ASSETS points the page at an older asset tree, to watch
// this go red there.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(process.env.UNI_SWITCH_ROUTING_ASSETS || path.join(__dirname, "../assets"));
const iso = (ms) => new Date(ms).toISOString();
const acct = (n) => ({ id: `acct-${n}`, provider: `prov${n}`, name: `Provider ${n}`, who: `user${n}@example.com`, kind: "account", model: `model-${n}`, known: true, used: 10 * n, plan: "Pro" });
const agentsOf = ["codex", "claude", "opencode", "kimi"];
const N = 2000;
const WIDE_MODEL = "claude-wide-model-name-2026", LAST_MODEL = "first-of-the-day";

function req(id, t0) {
  const order = [acct(id % 5), acct((id + 1) % 5)];
  const tries = [{ id: order[0].id, model: order[0].model, start: iso(t0), done: true, ms: 300, status: 200, in: 1200, out: 300, cacheRead: 600 }];
  return { id, seq: id, time: iso(t0), agent: agentsOf[id % agentsOf.length], model: order[0].model, provider: order[0].provider, order, tries, done: true, status: 200, ms: 400, tokens: 900, session: "s" + (id % 9), prompt: { tokens: 5000, counted: true, window: 200000, parts: [{ kind: "chat", tokens: 5000, items: [{ name: "turn", tag: "turns", n: 1, tokens: 5000 }] }] } };
}
// a day's requests, newest last as the history serves them; far from the
// newest, one asks for a wide model and one has three tries and many
// tokens, so the columns are wider than any row near the top needs
function dayOf(d, base) {
  const start = Date.parse(`2026-10-${d}T00:00:00Z`);
  return Array.from({ length: N }, (_, i) => {
    const r = req(base + i + 1, start + i * 20000);
    r.model += `-d${d}`;
    if (i === 0) r.model = LAST_MODEL;
    if (i === 300) r.model = WIDE_MODEL;
    if (i === 500) {
      r.tries = [{ ...r.tries[0], status: 429, fail: "rate", again: true }, { ...r.tries[0], status: 429, fail: "rate", again: true }, r.tries[0]];
      r.tokens = 123456789;
    }
    return r;
  });
}

function serve(lang) {
  const state = { agents: agentsOf.map((id) => ({ id, name: id[0].toUpperCase() + id.slice(1), path: `/test/${id}`, fields: [] })), profiles: [], settings: { lang, theme: "light" } };
  const days = [{ day: "2026-10-16", requests: N }, { day: "2026-10-15", requests: N }];
  const hist = { "2026-10-16": dayOf(16, 160000), "2026-10-15": dayOf(15, 150000) };
  const live = [req(1, Date.now() - 500)];
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data }).catch(() => {});
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/gateway/trace") {
      const wait = url.searchParams.get("wait");
      // no request comes while a test runs: the live poll waits (each answer
      // draws the page again, and would be counted with what is measured)
      if (wait) await new Promise((r) => setTimeout(r, 60000).unref());
      return json({ mine: true, now: new Date().toISOString(), seq: wait ? +url.searchParams.get("after") || 1 : 1, totals: { requests: 1, rerouted: 0, errors: 0 }, routes: wait ? [] : live });
    }
    if (url.pathname === "/api/gateway/history") {
      const d = url.searchParams.get("day");
      return json({ cut: false, days, routes: d ? hist[d] || [] : [] });
    }
    if (url.pathname === "/api/groups") return json({ models: [], groups: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// what the reader sees of the list: the rows drawn (a probe is not one),
// the columns' widths, and the first row in sight with where it is
const look = (page) => page.evaluate(() => {
  const list = document.querySelector(".rt-reqs"), box = list.getBoundingClientRect();
  const rows = [...list.querySelectorAll(":scope > .rt-req:not(.probe)")];
  const seen = [...list.children].find((e) => (e.matches(".rt-req:not(.probe)") || e.matches(".rt-session")) && e.getBoundingClientRect().bottom > box.top + 1);
  return {
    rows: rows.length,
    // the rows' columns, which every drawn row has the same of
    cols: [...new Set(rows.map((r) => getComputedStyle(r).gridTemplateColumns))].join(" | "),
    models: rows.map((r) => r.querySelector(".m")?.textContent),
    first: seen && { text: seen.textContent, at: Math.round(seen.getBoundingClientRect().top - box.top) },
    top: list.scrollTop, height: list.scrollHeight, view: list.clientHeight,
  };
});
const frames = (page, n = 4) => page.evaluate((n) => new Promise((r) => { const f = (i) => (i ? requestAnimationFrame(() => f(i - 1)) : r()); f(n); }), n);
// scrolled as the reader scrolls, each step drawn before the next
async function scrollTo(page, top) {
  await page.evaluate((top) => { const l = document.querySelector(".rt-reqs"); l.scrollTop = top === "end" ? l.scrollHeight : top; }, top);
  await frames(page);
  await page.evaluate((top) => { const l = document.querySelector(".rt-reqs"); if (top === "end") l.scrollTop = l.scrollHeight; }, top);
  await frames(page);
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [1280, 420]) {
      test(`${engine} ${lang} ${width}px: a day of ${N} requests draws the rows in sight, looking as it did with all of them`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const page = await browser.newPage({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
        page.setDefaultTimeout(15000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        t.after(() => browser.close());
        await page.goto("http://uni-switch.test/?view=routing");
        await page.locator(".rt-req").first().waitFor();
        // a day picked from its pill, or at a narrow width, from the menu
        // the pills past the bar's width go under (#961); picked, the day's
        // requests are in the list (each day's models are its own)
        // (dispatched, as every click here: the bar sits below the fold,
        // and the page keeps its scroll on a click)
        const pickDay = async (i, d) => {
          const pill = page.locator(".rt-days .rt-day:not(.rt-day-more)").nth(i);
          if (await pill.isVisible()) await pill.dispatchEvent("click");
          else {
            const shown = await page.locator(".rt-days .rt-day:not(.rt-day-more)").evaluateAll((xs) => xs.filter((x) => !x.hidden).length);
            await page.locator(".rt-days .rt-day-more").dispatchEvent("click");
            await page.locator(".proto-menu .pm-item").nth(i - shown).dispatchEvent("click");
          }
          await page.waitForFunction((d) => [...document.querySelectorAll(".rt-reqs > .rt-req:not(.probe) .m")].some((m) => m.textContent.endsWith(`-d${d}`)), d);
          await frames(page);
        };
        await pickDay(1, 16);

        // a few dozen rows in the page, not the day's 2,000
        let at = await look(page);
        assert(at.rows > 5 && at.rows <= 150, `a day of ${N} put ${at.rows} rows in the list`);
        assert(at.height > N * 20, `the list is ${at.height}px tall: not all ${N} rows can be scrolled to`);
        const topCols = at.cols;

        // the columns are the widest row's, drawn or not: the same far down,
        // where the wide model and the three tries are, and the wide model
        // is not cut there
        // (the 301st request of the day is about 85% of the way down; from
        // there, a list's height at a time, each drawn before the next)
        await scrollTo(page, Math.round(at.height * 0.8));
        for (let i = 0; i < 60; i++) {
          at = await look(page);
          if (at.models.includes(WIDE_MODEL)) break;
          await scrollTo(page, at.top + at.view);
        }
        assert(at.models.includes(WIDE_MODEL), `the wide model's row was never drawn (at ${at.top} of ${at.height}, last ${at.models.slice(-2)})`);
        assert.equal(at.cols, topCols, "the columns changed width as the list scrolled");
        if (width > 760) {
          const cut = await page.evaluate((model) => {
            const m = [...document.querySelectorAll(".rt-reqs > .rt-req:not(.probe) .m")].find((m) => m.textContent === model);
            return m.scrollWidth > m.clientWidth + 1;
          }, WIDE_MODEL);
          assert.equal(cut, false, "the wide model is cut: its column is sized by the rows drawn only");
        }

        // a row scrolled to stays where it is while the rows around it are drawn
        await scrollTo(page, Math.round(at.height / 2));
        const before = (await look(page)).first;
        await frames(page, 8);
        const after = (await look(page)).first;
        assert.deepEqual(after, before, "the list moved under the reader");

        // scrolled to its foot: the day's first request, and picked there it is marked
        await scrollTo(page, "end");
        at = await look(page);
        assert(at.models.includes(LAST_MODEL), `the day's first request is not at the list's foot: ${at.models.slice(-3)}`);
        assert(at.top + at.view >= at.height - 2, "the list doesn't scroll to its foot");
        const last = page.locator(".rt-reqs > .rt-req:not(.probe)").filter({ has: page.locator(".m", { hasText: LAST_MODEL }) });
        await last.dispatchEvent("click");
        await page.waitForFunction((model) => [...document.querySelectorAll(".rt-reqs > .rt-req:not(.probe)")].some((r) => r.querySelector(".m")?.textContent === model && r.getAttribute("aria-pressed") === "true"), LAST_MODEL);
        assert.equal(await page.locator('.rt-reqs > .rt-req[aria-pressed="true"]').count(), 1);

        // another day: its rows, as few
        await pickDay(2, 15);
        at = await look(page);
        assert(at.rows > 5 && at.rows <= 150, `the next day put ${at.rows} rows in the list`);
        assert.equal(at.cols, topCols, "another day of the same shapes changed the columns");

        // by session: the heads among the rows, the last one reached at the foot
        await page.locator(".rt-group-by button").nth(1).dispatchEvent("click");
        await page.waitForFunction(() => document.querySelector(".rt-group-by button:nth-child(2)").getAttribute("aria-pressed") === "true");
        await scrollTo(page, 0);
        await page.waitForFunction(() => document.querySelector(".rt-reqs > .rt-session"));
        await frames(page);
        at = await look(page);
        assert(at.rows <= 150, `grouped, ${at.rows} rows are in the list`);
        await scrollTo(page, "end");
        at = await look(page);
        const foot = await page.evaluate(() => {
          const list = document.querySelector(".rt-reqs"), box = list.getBoundingClientRect();
          const last = [...list.children].filter((e) => e.matches(".rt-req:not(.probe), .rt-session")).pop();
          return last && Math.round(box.bottom - last.getBoundingClientRect().bottom);
        });
        assert(foot >= 0 && foot < 40, `grouped, the list's last row is ${foot}px above its foot: the rows below it are not drawn`);
        assert(at.top + at.view >= at.height - 2, "grouped, the list doesn't scroll to its foot");
        assert.deepEqual(errors, []);
      });
    }
  }
}

// Whether a request's numbers go under it is measured again when the list's
// column gets a new width only (#2524: measured on every change of its
// height too, a day of 3,410 was measured 8 times in 8s, ~200ms each, and
// the mark it set changed the height it was measured on)
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${engine} ${lang}: the list growing taller doesn't measure its rows' numbers again`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const page = await browser.newPage({ viewport: { width: 1800, height: 900 }, reducedMotion: "reduce" });
      page.setDefaultTimeout(15000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      t.after(() => browser.close());
      await page.goto("http://uni-switch.test/?view=routing");
      await page.locator(".rt-req").first().waitFor();
      await page.locator(".rt-days .rt-day:not(.rt-day-more)").nth(1).dispatchEvent("click");
      await page.waitForFunction(() => [...document.querySelectorAll(".rt-reqs > .rt-req:not(.probe) .m")].some((m) => m.textContent.endsWith("-d16")));
      await frames(page, 8);
      // the list's column, wide enough for its numbers to be measured, and
      // taller than the window already, so the page's own scrollbar is there
      // and the heights below leave its width alone
      await page.evaluate(() => { document.querySelector(".rt-reqs").parentElement.style.minHeight = "1100px"; });
      await frames(page, 6);
      const col = await page.evaluate(() => document.querySelector(".rt-reqs").parentElement.className);
      assert(!/max760/.test(col), `the column is too narrow to measure in (${col})`);
      // each measure reads where a row's numbers sit: count those reads
      await page.evaluate(() => {
        window.marks = 0;
        const rect = Element.prototype.getBoundingClientRect;
        Element.prototype.getBoundingClientRect = function () {
          if (this.parentElement?.matches(".rt-req .meta")) window.marks++;
          return rect.call(this);
        };
      });
      for (const h of ["1200px", "1500px", "1300px"]) {
        await page.evaluate((h) => { document.querySelector(".rt-reqs").parentElement.style.minHeight = h; }, h);
        await frames(page);
      }
      assert.equal(await page.evaluate(() => window.marks), 0, "the column grew taller and its rows were measured again");
      // a new width is measured (the column's own: the page's is capped)
      await page.evaluate(() => { const c = document.querySelector(".rt-reqs").parentElement; c.style.width = c.clientWidth - 40 + "px"; });
      await frames(page, 6);
      assert(await page.evaluate(() => window.marks) > 0, "a new width was not measured");
      assert.deepEqual(errors, []);
    });
  }
}

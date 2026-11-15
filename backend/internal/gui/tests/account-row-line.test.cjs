// Run with Node's test runner and Playwright on the module path; see README.md.
// A provider editor's accounts (the owner, 10-29: Make first sat alone on a
// line under the name, a band of hover pills took room they didn't show,
// and the sheet's left edge was twice its right). Each account's first
// line ends with what it is (First) or what it does (Make first) at the
// same right edge, on one line; nothing on it takes room it doesn't show,
// so its quota sits right under it. Its settings (models, cap, requests at
// once) and its Remove wait under ⋯, on a line of their own that ⋯ opens
// and closes; one that is set stays on the first line. The sheet's two
// edges match in the app, its scroll bar counted in. Every row of the accounts box is
// ruled off from the one above. In every language, at the window's
// narrowest and in a phone's browser. UNI_SWITCH_ACC_ASSETS runs it on older
// assets.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(process.env.UNI_SWITCH_ACC_ASSETS || path.resolve(__dirname, "../assets"));
const claude = () => ({
  id: "claude", name: "Claude Code", icon: "claude-color", chat: "", responses: "", anthropic: "", catalog: "", routing: "",
  models: [{ id: "claude-opus-5-5", name: "Claude Opus 5.5", on: true }], agents: [], fallback: [], headers: {}, keyList: [],
  account: {
    agent: "claude", agentName: "Claude Code", user: "first@example.com", plan: "team",
    logins: [
      { user: "first@example.com", org: "Example", plan: "team", active: true, on: true },
      { user: "spare@example.com", plan: "max", on: true },
      { user: "capped@example.com", plan: "pro", on: true },
    ],
  },
  accountCaps: { "capped@example.com": 70 },
});
const serve = (lang, web) => async (route) => {
  const url = new URL(route.request().url());
  const json = (data) => route.fulfill({ json: data });
  if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"dark",web:${web}};` });
  if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
  if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "dark" } });
  if (url.pathname === "/api/providers") return json({ providers: [claude()], presets: [], excluded: [], gateway: { running: true, window: true } });
  if (url.pathname === "/api/groups") return json({ groups: [] });
  if (url.pathname === "/api/plugins") return json({ plugins: [] });
  if (url.pathname === "/api/login/usage") return json({
    "first@example.com": { windows: [{ label: "5h", used: 10, limit: 100 }, { label: "Weekly", used: 20, limit: 100 }] },
    "spare@example.com": { error: "unavailable" },
    "capped@example.com": { windows: [{ label: "5h", used: 30, limit: 100 }] },
  });
  if (url.pathname.startsWith("/api/")) return json({});
  const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
  const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
  try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
};

// each first line: its height, how far its last child ends from its
// right, and what on it is there but drawn at no width or not at all
const lines = (page) => page.evaluate(() => [...document.querySelectorAll(".editor .accts .acc")].filter((r) => r.dataset.accountId).map((r) => {
  const line = r.querySelector(".acc-line") || r, b = line.getBoundingClientRect();
  const kids = [...line.children].filter((c) => !c.classList.contains("grow"));
  const last = kids[kids.length - 1].getBoundingClientRect();
  const hidden = kids.filter((c) => { const k = c.getBoundingClientRect(), s = getComputedStyle(c); return k.width > 1 && (+s.opacity === 0 || s.visibility === "hidden") && !c.classList.contains("acc-more"); }).map((c) => c.className);
  const q = r.querySelector(".aq")?.getBoundingClientRect();
  return { who: r.dataset.accountId, h: Math.round(b.height), right: Math.round(b.right - last.right) + 0, hidden, last: kids[kids.length - 1].textContent, gap: q ? Math.round(q.top - b.bottom) : 0 };
}));

const langs = ["en", "zh", "zh-TW", "ja", "de"];
for (const [engine, browserType] of [["chromium", chromium], ["webkit", webkit]]) {
  test(engine + ": an account's first line ends at one edge, its settings under ⋯", async (t) => {
    const browser = await browserType.launch();
    t.after(() => browser.close());
    for (const [web, width] of [[false, 1000], [false, 560], [true, 390]]) for (const lang of langs) {
      const at = `${lang} ${web ? "web " : ""}${width}`;
      const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      await page.route("**/*", serve(lang, web));
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator(".row.provider", { hasText: "Claude Code" }).first().click();
      const rows = page.locator(".editor .accts .acc[data-account-id]");
      await rows.nth(2).waitFor();
      await page.mouse.move(1, 1);
      const scrollY = await page.evaluate(() => document.querySelector(".modal .ebody")?.scrollTop ?? 0);

      for (const l of await lines(page)) {
        assert.equal(l.right, 0, `${at}: ${l.who}'s line ends with what ends it`);
        assert.deepEqual(l.hidden, [], `${at}: nothing on ${l.who}'s line takes room it doesn't show`);
        // one line: the tallest thing on it, a phone's touch-sized button
        assert.ok(l.h <= (web ? 48 : 24), `${at}: ${l.who}'s line is one line (${l.h}px)`);
        assert.ok(l.gap <= 8, `${at}: ${l.who}'s quota sits under its line (${l.gap}px)`);
      }
      const [first, spare, capped] = await lines(page);
      assert.ok(first.last.length && spare.last.length, `${at}: First and Make first end their lines`);
      // a cap that is set stays on the line, beside the name
      assert.equal(await rows.nth(2).locator(".acc-line > .acap.set").count(), 1, `${at}: a set cap stays on its line`);
      assert.equal(capped.right, 0);

      // ⋯ opens the settings line, and closes it
      const more = rows.nth(1).locator(".acc-more");
      const tools = rows.nth(1).locator(".acc-tools");
      assert.equal(await tools.isVisible(), false, `${at}: the settings wait under ⋯`);
      await rows.nth(1).hover();
      await more.click();
      assert.equal(await more.getAttribute("aria-expanded"), "true");
      for (const sel of [".amodels", ".acap", ".alane", ".text.quiet"]) assert.ok(await tools.locator(sel).isVisible(), `${at}: ⋯ shows ${sel}`);
      const tb = await tools.boundingBox(), lb = await rows.nth(1).locator(".acc-line").boundingBox();
      assert.ok(tb.y >= lb.y + lb.height, `${at}: the settings are on a line of their own`);
      assert.ok(tb.x + tb.width <= lb.x + lb.width + 1, `${at}: the settings line stays in the row`);
      await more.click();
      assert.equal(await tools.isVisible(), false, `${at}: ⋯ closes them`);
      assert.equal(await page.evaluate(() => document.querySelector(".modal .ebody")?.scrollTop ?? 0), scrollY, `${at}: a click moves nothing`);

      // every row of the box ruled off from the one above
      const unruled = await page.evaluate(() => [...document.querySelectorAll(".editor .accts > *")].slice(1).filter((x) => x.offsetHeight && parseFloat(getComputedStyle(x).borderTopWidth) < 1).map((x) => x.className));
      assert.deepEqual(unruled, [], `${at}: every row is ruled off`);

      // the sheet doesn't scroll sideways, and in the app, whose scroll bar
      // uni-switch draws, its edges match (a phone's bar takes no room)
      const m = await page.evaluate(() => {
        const body = document.querySelector(".modal .ebody"), b = body.getBoundingClientRect(), cs = getComputedStyle(body);
        return { wide: body.scrollWidth - body.clientWidth, left: parseFloat(cs.paddingLeft), right: b.right - body.getBoundingClientRect().left - body.clientWidth - parseFloat(cs.borderLeftWidth) + parseFloat(cs.paddingRight), bar: body.offsetWidth - body.clientWidth };
      });
      assert.ok(m.wide <= 0, `${at}: the sheet doesn't scroll sideways (${m.wide}px)`);
      if (!web && m.bar > 0) assert.ok(Math.abs(m.left - m.right) <= 1, `${at}: its left edge ${m.left}px, its right ${m.right}px`);
      await context.close();
    }
  });
}

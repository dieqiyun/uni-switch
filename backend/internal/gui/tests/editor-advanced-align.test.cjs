// Run with Node's test runner and Playwright on the module path; see README.md.
// The owner, on a provider editor's Advanced: 高级这个折叠按钮为什么在一个前不着村后不着店的位置？
// It was indented a fixed 86px, from when the label column was 72px; the
// column is now as wide as its labels, so it lined up with nothing. Its
// chevron starts where the fields do, and open, its fields start there
// too unless their own labels are longer (#875 keeps those on one line).
// With the labels above the fields (a phone's browser) it starts at the
// edge. In every language, wide, narrow and on a phone, both engines.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(process.env.UNI_SWITCH_GUI_ASSETS || path.resolve(__dirname, "../assets"));
const claude = () => ({
  id: "claude", name: "Claude Code", icon: "claude-color", chat: "", responses: "", anthropic: "", catalog: "", routing: "",
  models: [{ id: "claude-opus-5-5", name: "Claude Opus 5.5", on: true }], agents: [], fallback: [], headers: {}, keyList: [],
  account: { agent: "claude", agentName: "Claude Code", user: "first@example.com", plan: "team", logins: [{ user: "first@example.com", plan: "team", active: true, on: true }] },
});
const serve = (lang, web) => async (route) => {
  const url = new URL(route.request().url());
  const json = (data) => route.fulfill({ json: data });
  if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:${web}};` });
  if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
  if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
  if (url.pathname === "/api/providers") return json({ providers: [claude()], presets: [], excluded: [], gateway: { running: true, window: true } });
  if (url.pathname === "/api/groups") return json({ groups: [] });
  if (url.pathname === "/api/plugins") return json({ plugins: [] });
  if (url.pathname === "/api/login/usage") return json({});
  if (url.pathname.startsWith("/api/")) return json({});
  const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
  const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
  try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
};

// where the editor's fields start, and its Advanced's chevron, its open
// fields and how long their labels are
const measure = (page) => page.evaluate(() => {
  const adv = document.querySelector(".editor details.advanced");
  const g = adv.parentElement, cs = getComputedStyle(g);
  const label = [...g.children].find((c) => c.tagName === "LABEL" && c.offsetWidth);
  const field = label.nextElementSibling.getBoundingClientRect();
  const sum = adv.querySelector("summary").getBoundingClientRect();
  const out = { fields: field.left, edge: g.getBoundingClientRect().left + parseFloat(cs.paddingLeft), stacked: cs.gridTemplateColumns.split(" ").length === 1, summary: sum.left, labelCol: label.getBoundingClientRect().width };
  if (adv.open) {
    const ls = [...adv.querySelectorAll(":scope > .inner > label")].filter((l) => l.offsetWidth);
    out.innerFields = ls.length ? ls[0].nextElementSibling.getBoundingClientRect().left : null;
    out.innerLabels = Math.max(0, ...ls.map((l) => { const r = document.createRange(); r.selectNodeContents(l); return r.getBoundingClientRect().width; }));
  }
  return out;
});

for (const [engine, browserType] of [["chromium", chromium], ["webkit", webkit]]) {
  test(engine + ": a provider editor's Advanced starts where its fields do", async (t) => {
    const browser = await browserType.launch();
    t.after(() => browser.close());
    for (const [web, width] of [[false, 1000], [false, 560], [true, 390]]) for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      const at = `${lang} ${web ? "web " : ""}${width}`;
      const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, web));
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator(".row.provider", { hasText: "Claude Code" }).first().click();
      const adv = page.locator(".editor details.advanced");
      await adv.locator(":scope > summary").waitFor();
      // the editor drawn last has been laid out: a redraw between two
      // frames measured a details its observer hadn't seen yet (86px)
      await page.waitForFunction(() => document.querySelector(".editor details.advanced")?.style.getPropertyValue("--ed-at"));
      await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
      let m = await measure(page);
      const start = m.stacked ? m.edge : m.fields;
      assert.ok(Math.abs(m.summary - start) <= 1, `${at}: Advanced starts at ${m.summary}, its fields at ${start}`);

      await adv.locator(":scope > summary").click();
      await page.waitForFunction(() => document.querySelector(".editor details.advanced")?.open);
      await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
      m = await measure(page);
      if (m.innerFields != null && !m.stacked) {
        assert.ok(m.innerFields >= m.fields - 1, `${at}: Advanced's fields start left of the others (${m.innerFields} < ${m.fields})`);
        if (m.innerLabels <= m.labelCol) assert.ok(Math.abs(m.innerFields - m.fields) <= 1, `${at}: Advanced's fields at ${m.innerFields}, the others at ${m.fields}`);
      }
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1), true, `${at}: the page scrolls sideways`);
      await page.screenshot({ path: path.join(process.env.SHOTS || "/tmp", `editor-advanced-${engine}-${lang}-${width}.png`) }).catch(() => {});
      assert.deepEqual(errors, []);
      await context.close();
    }
  });
}

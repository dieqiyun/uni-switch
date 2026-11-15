// Run with Node's test runner and Playwright on the module path; see README.md.
// sylvenPR on X: the 高级 (Advanced) fold at the foot of Codex's editor
// didn't look like something to click: a 5px "▸" glyph in the muted grey,
// no pointer, nothing on hover. An editor's fold (Advanced, and a custom
// provider's More endpoints) is a macOS disclosure now: a chevron at least
// 10px, its label in the form labels' colour, a pointer, a brighter label
// under it, and the chevron where the fields start. Dark and light, every
// language, both engines.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(process.env.UNI_SWITCH_GUI_ASSETS || path.resolve(__dirname, "../assets"));
const codex = {
  id: "codex", name: "Codex", icon: "codex-color", chat: "", responses: "https://chatgpt.com/backend-api/codex", anthropic: "", catalog: "",
  models: [{ id: "gpt-6", name: "GPT-6", on: true }], agents: [], fallback: [], headers: {}, keyList: [],
  account: { agent: "codex", agentName: "Codex", user: "me@example.com", plan: "PRO", logins: [{ user: "me@example.com", plan: "PRO", active: true, on: true }] },
};
const relay = {
  id: "relay", name: "Relay", icon: "", chat: "https://relay.example.com/v1/chat/completions", responses: "", anthropic: "", catalog: "",
  models: [{ id: "m1", name: "m1", on: true }], agents: [], fallback: [], headers: {}, keyList: [], key: { set: true, masked: "sk-…1234" }, ready: true,
};
const serve = (lang, theme) => async (route) => {
  const url = new URL(route.request().url());
  const json = (data) => route.fulfill({ json: data });
  if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:${JSON.stringify(theme)},web:false};` });
  if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
  if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme } });
  if (url.pathname === "/api/providers") return json({ providers: [codex, relay], presets: [], excluded: [], gateway: { running: true, window: true } });
  if (url.pathname === "/api/groups") return json({ groups: [] });
  if (url.pathname === "/api/plugins") return json({ plugins: [] });
  if (url.pathname.startsWith("/api/")) return json({});
  const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
  const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
  try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
};

// how a fold's summary looks: cursor, label and chevron colours and size,
// where it starts against the fields
const look = (page, sel) => page.evaluate((sel) => {
  const s = document.querySelector(sel), cs = getComputedStyle(s), b = getComputedStyle(s, "::before");
  // the grid the fold sits in, and its first label beside a field
  const g = s.parentElement.parentElement, label = [...g.children].find((c) => c.tagName === "LABEL" && c.offsetWidth && !c.classList.contains("tick"));
  const lab = getComputedStyle(label);
  return {
    cursor: cs.cursor, color: cs.color, labelColor: lab.color,
    chevW: parseFloat(b.width) || 0, chevH: parseFloat(b.height) || 0, glyph: b.content,
    left: s.getBoundingClientRect().left, fields: label.nextElementSibling.getBoundingClientRect().left,
    stacked: getComputedStyle(g).gridTemplateColumns.split(" ").length === 1,
  };
}, sel);

for (const [engine, browserType] of [["chromium", chromium], ["webkit", webkit]]) {
  test(engine + ": an editor's fold reads as a control", async (t) => {
    const browser = await browserType.launch();
    t.after(() => browser.close());
    for (const theme of ["dark", "light"]) for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) for (const [name, sel] of [["Codex", ".editor details.advanced > summary"], ["Relay", ".editor details.more:not(.advanced) > summary"]]) {
      const at = `${name} ${theme} ${lang}`;
      const context = await browser.newContext({ viewport: { width: 900, height: 900 }, colorScheme: theme, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, theme));
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator(".row.provider", { hasText: name }).first().click();
      const sum = page.locator(sel);
      await sum.waitFor();
      await page.waitForFunction((sel) => document.querySelector(sel)?.parentElement.style.getPropertyValue("--ed-at"), sel);
      await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
      const m = await look(page, sel);
      assert.equal(m.cursor, "pointer", `${at}: the fold's cursor is ${m.cursor}`);
      assert.ok(m.chevW >= 9.5 && m.chevH >= 9.5, `${at}: the chevron is ${m.chevW}x${m.chevH}`);
      assert.ok(!/[▸▶►]/.test(m.glyph), `${at}: the chevron is a text glyph ${m.glyph}`);
      assert.equal(m.color, m.labelColor, `${at}: the fold's label is ${m.color}, the form's labels ${m.labelColor}`);
      if (!m.stacked) assert.ok(Math.abs(m.left - m.fields) <= 1, `${at}: the fold starts at ${m.left}, the fields at ${m.fields}`);
      await sum.hover();
      await page.waitForFunction(([sel, c]) => getComputedStyle(document.querySelector(sel)).color !== c, [sel, m.color]).catch(() => {});
      const hovered = await look(page, sel);
      assert.notEqual(hovered.color, m.color, `${at}: the fold's label doesn't change under the pointer`);
      // it still opens and turns its chevron down
      await sum.click();
      await page.waitForFunction((sel) => document.querySelector(sel).parentElement.open, sel);
      const turned = await page.evaluate((sel) => getComputedStyle(document.querySelector(sel), "::before").transform, sel);
      assert.notEqual(turned, "none", `${at}: the chevron doesn't turn when open`);
      assert.deepEqual(errors, []);
      await context.close();
    }
  });
}

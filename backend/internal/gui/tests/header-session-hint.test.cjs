// Run with Node's test runner and Playwright on the module path; see README.md.
// ${sessionId} and ${threadId} in a provider's Headers (Axel Burks on
// Discord: a gateway in front of several Azure accounts routes a
// conversation by a session id in a header). The editor's hint for
// Headers says what they stand for, as Extra body's does, and a value
// with one is saved as typed, for the gateway to fill in on each request.
// In English, Chinese, Traditional Chinese, Japanese and German, Chromium
// and WebKit, and at a narrow width.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const gw = {
  id: "azgw", name: "Azure Gateway", preset: "", icon: "generic", chat: "", responses: "https://gw.example.com/v1", anthropic: "", catalog: "", baseAPI: "responses",
  models: [{ id: "gpt-5.1", name: "gpt-5.1", on: true }], agents: [], fallback: [], headers: { "X-Session-Id": "${sessionId}" },
  key: { set: true, masked: "sk-…one" }, keyList: [{ id: "k1", name: "", masked: "sk-…one", on: true, active: true }],
  balanceToken: { takes: false, set: false }, proxy: "",
};

function serve(lang, calls) {
  const providers = { providers: [gw], presets: [], excluded: [], gateway: { running: true, window: true } };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/provider/")) {
      calls.push([url.pathname.slice(14), route.request().postDataJSON()]);
      if (url.pathname === "/api/provider/save") return json(providers);
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

// a word of each language's hint, so an untranslated one fails
const words = {
  en: { save: "Save", said: "conversation's id" },
  zh: { save: "保存", said: "会话的 ID" },
  "zh-TW": { save: "儲存", said: "對話的 ID" },
  ja: { save: "保存", said: "会話の ID" },
  de: { save: "Speichern", said: "ID der Unterhaltung" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(words)) {
    const w = words[lang];
    test(`${engine} ${lang}: Headers say what \${sessionId} and \${threadId} are, and keep them as typed`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 900, height: 900 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const calls = [];
      await page.route("**/*", serve(lang, calls));
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator(".row.provider", { hasText: "Azure Gateway" }).click();
      const ed = page.locator(".editor");
      await ed.locator(".headers").waitFor({ state: "attached" });

      const hintOf = (sel) => ed.locator(sel).locator("xpath=..").locator(":scope > .hint").textContent();
      for (const [what, sel] of [["Headers", ".headers"], ["Extra body", ".extra-body"]]) {
        const hint = await hintOf(sel);
        assert.ok(hint.includes("${sessionId}") && hint.includes("${threadId}"), `${what}'s hint: ${hint}`);
        assert.ok(hint.includes(w.said), `${what}'s hint in ${lang}: ${hint}`);
      }
      const value = ed.locator(".headers .pair input").nth(1);
      assert.equal(await value.inputValue(), "${sessionId}", "the saved value is shown as typed");
      await value.fill("route/${sessionId}/${threadId}");

      // the narrow window: the hint stays inside the editor
      await page.setViewportSize({ width: 420, height: 900 });
      const fits = await page.evaluate(() => {
        const ed = document.querySelector(".editor").getBoundingClientRect();
        const h = document.querySelector(".editor .headers").parentElement.querySelector(":scope > .hint");
        const r = h.getBoundingClientRect();
        return r.width === 0 || (r.left >= ed.left - 0.5 && r.right <= ed.right + 0.5);
      });
      assert.ok(fits, "the hint fits a narrow window");
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth), "no sideways scroll");
      await page.setViewportSize({ width: 900, height: 900 });

      await ed.locator(".bar").getByRole("button", { name: w.save, exact: true }).click();
      for (let i = 0; i < 60 && !calls.some((c) => c[0] === "save"); i++) await page.waitForTimeout(50);
      const saved = calls.find((c) => c[0] === "save");
      assert.ok(saved, "saved");
      assert.deepEqual(saved[1].headers, { "X-Session-Id": "route/${sessionId}/${threadId}" }, "saved as typed");
      assert.deepEqual(errors, []);
    });
  }
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// Copying from a browser tab with no clipboard API (悠悠哥 on Discord: on
// `uni-switch web` on a NAS, opened from another computer over http, the
// gateway's API key copy did nothing). uni-switch can't copy for such a tab
// (/api/copy refuses) and the page, not a secure context, has no
// navigator.clipboard; the copy command still copies there. English and
// Chinese, Chromium and WebKit; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const LINK = "https://factory.example/device?code=WDJB-MJHT&state=" + "x".repeat(120);

function server(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [{ id: "openai", name: "OpenAI", icon: "openai", preset: "openai", models: [], agents: [], key: { set: true, masked: "sk-…ab12" } }], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    const signing = { id: "s1", agent: "factory", state: "waiting", url: LINK, code: "WDJB-MJHT" };
    if (url.pathname === "/api/signin" || url.pathname.startsWith("/api/signin/")) return json(signing);
    if (url.pathname === "/api/copy") return route.fulfill({ status: 501, body: "" });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const W = {
  en: { anyway: "Sign in anyway", copied: "Sign-in link copied" },
  zh: { anyway: "仍然登录", copied: "已复制登录链接" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a tab with no clipboard API copies with the copy command`, async (t) => {
      const w = W[lang];
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const page = await (await browser.newContext({ viewport: { width: 900, height: 800 }, reducedMotion: "reduce" })).newPage();
      t.after(() => browser.close());
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      // a page over plain http from another computer: no clipboard API, and
      // the copy command is noted with what was selected when it ran
      await page.addInitScript(() => {
        Object.defineProperty(navigator, "clipboard", { value: undefined, configurable: true });
        window.__copied = [];
        document.execCommand = (cmd) => {
          if (cmd !== "copy") return false;
          const a = document.activeElement;
          window.__copied.push(a && a.tagName === "TEXTAREA" ? a.value.slice(a.selectionStart, a.selectionEnd) : String(getSelection()));
          return true;
        };
      });
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(lang));
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator("#addProvider").click();
      const sheet = page.locator("#addSheet");
      await sheet.locator('.tile[data-pick="Factory"]').click();
      const box = sheet.locator(".signing");
      await box.locator("button", { hasText: w.anyway }).click();
      const cp = box.locator(".signlink button.copy");
      await cp.waitFor();

      const scrolls = () => page.evaluate(() => [window.scrollY, ...[...document.querySelectorAll("#addSheet, #addSheet *")].filter((e) => e.scrollTop).map((e) => e.scrollTop)]);
      const before = await scrolls();
      await cp.click();
      await page.waitForFunction((s) => document.querySelector("#status").textContent === s, w.copied);
      assert.deepEqual(await page.evaluate(() => window.__copied), [LINK]);
      assert.equal(await page.locator("textarea[readonly]").count(), 0, "the textarea is gone");
      assert.notEqual(await page.evaluate(() => document.activeElement?.tagName), "TEXTAREA", "the focus went back");
      assert.deepEqual(await scrolls(), before, "nothing scrolled");
      assert.deepEqual(errors, []);
    });
  }
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// The sign-in link in sight (StringKe on Discord: the sign-in page only
// opened in the default browser, with no link to take to another one).
// While a sign-in waits, its link shows on one line under the question,
// selectable whole, with a copy button beside it that copies the link;
// "Open again" still opens it. Neither click moves the page. English and
// Chinese, Chromium and WebKit; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const LINK = "https://factory.example/device?code=WDJB-MJHT&state=" + "x".repeat(120);

function server(lang, copied, opened) {
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
    if (url.pathname === "/api/copy") { copied.push(route.request().postDataJSON().text); return json({}); }
    if (url.pathname === "/api/open") { opened.push(route.request().postDataJSON()); return json({}); }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const W = {
  en: { anyway: "Sign in anyway", copy: "Copy link", again: "Open again", copied: "Sign-in link copied" },
  zh: { anyway: "仍然登录", copy: "复制链接", again: "重新打开", copied: "已复制登录链接" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: the sign-in link can be copied`, async (t) => {
      const w = W[lang];
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const page = await (await browser.newContext({ viewport: { width: 900, height: 800 }, reducedMotion: "reduce" })).newPage();
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-signin-link.png`) });
        }
        await browser.close();
      });
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [], copied = [], opened = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(lang, copied, opened));
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator("#addProvider").click();
      const sheet = page.locator("#addSheet");
      await sheet.locator('.tile[data-pick="Factory"]').click();
      const box = sheet.locator(".signing");
      await box.locator("button", { hasText: w.anyway }).click();
      const link = box.locator(".signlink");
      await link.waitFor();

      // the whole link, on one line inside the box
      const code = link.locator("code");
      assert.equal(await code.textContent(), LINK);
      const fit = await link.evaluate((l) => {
        const c = l.querySelector("code"), b = l.closest(".signing").getBoundingClientRect(), r = c.getBoundingClientRect();
        return { oneLine: r.height < 30, inside: r.right <= b.right + 0.5, sel: getComputedStyle(c).userSelect || getComputedStyle(c).webkitUserSelect };
      });
      assert(fit.oneLine && fit.inside, JSON.stringify(fit));
      assert.equal(fit.sel, "all");
      // no coloured stripe down its side: a left border no other than the rest
      const stripes = await page.evaluate(() => [...document.querySelectorAll(".signlink, .signlink *")].filter((e) => {
        const s = getComputedStyle(e);
        return parseFloat(s.borderLeftWidth) > parseFloat(s.borderTopWidth) || (parseFloat(s.borderLeftWidth) && s.borderLeftColor !== s.borderTopColor);
      }).map((e) => e.className));
      assert.deepEqual(stripes, [], "no left-border accent");

      const scrolls = () => page.evaluate(() => [window.scrollY, ...[...document.querySelectorAll("#addSheet, #addSheet *")].filter((e) => e.scrollTop).map((e) => e.scrollTop)]);
      const before = await scrolls();
      const cp = link.locator("button.copy");
      assert.equal(await cp.getAttribute("title"), w.copy);
      await cp.click();
      for (let i = 0; i < 40 && !copied.length; i++) await page.waitForTimeout(50);
      assert.deepEqual(copied, [LINK]);
      await page.waitForFunction((s) => document.querySelector("#status").textContent === s, w.copied);
      assert.equal(await box.locator("button", { hasText: w.again }).count(), 1, "Open again is still there");
      assert.deepEqual(await scrolls(), before, "nothing scrolled");
      assert.deepEqual(errors, []);
    });
  }
}

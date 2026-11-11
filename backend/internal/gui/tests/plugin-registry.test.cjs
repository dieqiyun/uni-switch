// Run with Node's test runner and Playwright on the module path; see README.md.
// The Plugins page's npm registry row (timwangdev on X): Discover says the
// registry plugins are installed from and where it came from (here the
// user's ~/.npmrc); Change takes an address, an address the server refuses
// is said and not kept, a saved one says it is set here and offers Use
// default, which drops it. Every language, Chromium and WebKit, and a
// narrow window that doesn't scroll sideways. The API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

const listings = [
  { package: "the retired opencode-zed-auth", name: "Zed", icon: "zed", providers: ["zed"], community: true,
    summary: { en: "Your Zed plan.", zh: "Zed 订阅。" }, npm: { version: "0.1.0", weekly: 120 } },
];

const NPMRC = { url: "https://npm.corp.example/repo", from: "npmrc", path: "~/.npmrc" };

function server(lang, asked) {
  let registry = { ...NPMRC };
  const state = () => ({ bun: true, bunVersion: "1.3.0", plugins: [], mirror: true, registry });
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data, status) => route.fulfill({ json: data, status });
    const body = () => route.request().postDataJSON();
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/plugins") return json(state());
    if (url.pathname === "/api/plugins/listings") return json({ listings });
    if (url.pathname === "/api/plugins/market") return json({ listings, state: state() });
    if (url.pathname === "/api/plugins/registry") {
      const { url: u } = body();
      asked.push(u);
      if (u && !/^https?:\/\/[^/]/.test(u)) return json({ error: `"${u}" isn't a registry's address: give an http:// or https:// one, like https://registry.npmmirror.com` }, 400);
      registry = u ? { url: u.replace(/\/+$/, ""), from: "uni-switch" } : { ...NPMRC };
      return json({ registry });
    }
    if (url.pathname.startsWith("/api/")) return json({});
    if (!/^\/[\w./-]*$/.test(url.pathname) || url.host !== "uni-switch.test") return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const L = {
  en: { label: "npm registry", from: "from ~/.npmrc", here: "set here", change: "Change", save: "Save", def: "Use default" },
  zh: { label: "npm 源", from: "来自 ~/.npmrc", here: "在此设置", change: "修改", save: "保存", def: "使用默认" },
  "zh-TW": { label: "npm 來源", from: "來自 ~/.npmrc", here: "在此設定", change: "修改", save: "儲存", def: "使用預設" },
  ja: { label: "npm レジストリ", from: "~/.npmrc から", here: "ここで設定", change: "変更", save: "保存", def: "デフォルトを使用" },
  de: { label: "npm-Registry", from: "aus ~/.npmrc", here: "hier festgelegt", change: "Ändern", save: "Speichern", def: "Standard verwenden" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": the Plugins page's npm registry", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of Object.keys(L)) {
      await t.test(lang, async () => {
        const w = L[lang];
        for (const width of [980, 380]) {
          const page = await (await browser.newContext({ viewport: { width, height: 820 } })).newPage();
          page.setDefaultTimeout(8000);
          page.setDefaultNavigationTimeout(60000);
          const errors = [], asked = [];
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", server(lang, asked));
          await page.goto("http://uni-switch.test/?view=plugins");
          const row = page.locator("#view-plugins .pm-registry");
          await row.waitFor();
          assert.equal((await row.locator(".pm-mlabel").innerText()).trim(), w.label);
          assert.equal((await row.locator(".pm-reg-url").innerText()).trim(), NPMRC.url);
          assert.equal((await row.locator(".pm-reg-from").innerText()).trim(), w.from);
          // nothing of the page's runs out sideways, the long address included
          const sideways = await page.evaluate(() => {
            const v = document.querySelector("#view-plugins"), r = document.querySelector("#view-plugins .pm-registry");
            return { page: document.documentElement.scrollWidth - document.documentElement.clientWidth, row: r.scrollWidth - r.clientWidth, right: r.getBoundingClientRect().right - v.getBoundingClientRect().right };
          });
          assert.ok(sideways.page <= 0 && sideways.row <= 1 && sideways.right <= 1, JSON.stringify(sideways));
          if (width < 900 && lang !== "en") { assert.deepEqual(errors, []); await page.context().close(); continue; }

          await row.locator("button", { hasText: w.change }).click();
          const input = row.locator("input.pm-reg-input");
          await input.waitFor();
          assert.ok(await input.evaluate((i) => document.activeElement === i), "the field has the focus");
          await input.fill("ftp://npm.example");
          await input.press("Enter");
          await row.locator(".pm-reg-err").waitFor();
          assert.match(await row.locator(".pm-reg-err").innerText(), /isn't a registry's address/);
          assert.ok(await input.isVisible(), "a refused address stays in the field");
          assert.equal(await input.inputValue(), "ftp://npm.example");

          await input.fill("https://registry.npmmirror.com/");
          await row.locator("button", { hasText: w.save }).click();
          await row.locator(".pm-reg-from", { hasText: w.here }).waitFor();
          assert.equal((await row.locator(".pm-reg-url").innerText()).trim(), "https://registry.npmmirror.com");
          const def = row.locator("button", { hasText: w.def });
          await def.click();
          await row.locator(".pm-reg-from", { hasText: w.from }).waitFor();
          assert.equal(await def.count(), 0);
          assert.deepEqual(asked, ["ftp://npm.example", "https://registry.npmmirror.com/", ""]);
          assert.deepEqual(errors, []);
          await page.context().close();
        }
      });
    }
  });
}

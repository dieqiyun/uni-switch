// Run with Node's test runner and Playwright on the module path; see README.md.
// A provider with hundreds of keys (@NetRozee on X: a thousand keys on a
// custom endpoint froze uni-switch): its editor's per-key proxy lines fold as
// its Accounts list does. Folded, the first five and the keys with a proxy
// of their own, and All 300 keys; opened, a filter and a page of 50 lines
// and how many more. A key picked in the filter keeps its pick folded
// again, and Save posts every key's own proxy, drawn or not. In English
// and Chinese.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const N = 300;
const idOf = (i) => i.toString(16).padStart(10, "0");
const relay = () => ({
  id: "relay", name: "Relay", icon: "generic", chat: "https://relay.example.com/v1", responses: "", anthropic: "", catalog: "",
  models: [{ id: "big", name: "", on: true }], agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-…0000" }, balanceToken: { takes: false, set: false }, proxy: "",
  keyList: Array.from({ length: N }, (_, i) => ({ id: idOf(i), name: "", masked: "sk-…" + String(i).padStart(4, "0"), on: true, active: i === 0 })),
  accountProxies: { [idOf(120)]: "socks5://10.0.0.5:1080", [idOf(200)]: "direct" },
});

function serve(lang, posts) {
  const providers = { providers: [relay()], presets: [], excluded: [], gateway: { running: true, window: true } };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/provider/") && route.request().method() === "POST") {
      posts.push({ action: url.pathname.slice("/api/provider/".length), body: route.request().postDataJSON() });
      return json(providers);
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { all: "All 300 keys", fewer: "Show fewer", more: "250 more — type to narrow them down", none: "No key matches", direct: "Direct", save: "Save", filter: "Filter 300 keys — name, key, off, resting…" },
  zh: { all: "全部 300 个密钥", fewer: "收起", more: "还有 250 个，输入关键词缩小范围", none: "没有匹配的密钥", direct: "直连", save: "保存", filter: "筛选 300 个密钥：名称、密钥、已停用、休息中…" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    const w = words[lang];
    test(`${engine} ${lang}: 300 keys' proxy lines fold, open on a filter, and keep their picks`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      let page;
      t.after(async () => {
        if (process.env.ARTIFACT_DIR && page) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-many-keys-proxy.png`) });
        }
        await browser.close();
      });
      page = await (await browser.newContext({ viewport: { width: 900, height: 760 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const posts = [];
      await page.route("**/*", serve(lang, posts));
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator(".row.provider", { hasText: "Relay" }).first().click();
      // Proxy is under Advanced, open already when one is set
      { const adv = page.locator(".editor details.advanced"); await adv.waitFor({ state: "attached" }); if (!(await adv.evaluate((d) => d.open))) await adv.locator(":scope > summary").click(); }
      const more = page.locator(".editor .acct-proxies > .acct-proxy-more");
      await more.waitFor();
      const lines = () => page.locator(".editor .acct-proxy").evaluateAll((ls) => ls.map((l) => l.dataset.key));

      // folded: the first five and the two with a proxy of their own
      assert.deepEqual(await lines(), [0, 1, 2, 3, 4, 120, 200].map(idOf));
      assert.equal((await more.textContent()).trim(), w.all);
      assert(await page.locator(".editor .segs").count() < 20, "a line was drawn for every key");

      // opened: a filter, focused, over 50 lines and how many more
      await more.click();
      const filter = page.locator(".editor .acct-proxy-tools .acct-proxy-filter");
      await filter.waitFor();
      assert.equal(await filter.getAttribute("placeholder"), w.filter);
      assert(await filter.evaluate((e) => e === document.activeElement), "the filter is focused");
      assert.equal((await lines()).length, 50);
      assert.equal(await page.locator(".editor .acct-proxy-rest").textContent(), w.more);
      assert.equal(await more.count(), 0);

      await filter.fill("0123");
      assert.deepEqual(await lines(), [idOf(123)]);
      assert.equal(await page.locator(".editor .acct-proxy-rest").count(), 0);
      await page.locator(`.editor .acct-proxy[data-key="${idOf(123)}"] .proxy-mode .opt`, { hasText: w.direct }).click();
      await filter.fill("nothing-like-it");
      assert.equal(await page.locator(".editor .acct-proxy-none").textContent(), w.none);
      assert.equal((await lines()).length, 0);

      // folded again, the key just picked shows among the others
      await page.locator(".editor .acct-proxy-tools").getByRole("button", { name: w.fewer, exact: true }).click();
      await more.waitFor();
      assert.equal(await page.locator(".editor .acct-proxy-tools").count(), 0);
      assert.deepEqual(await lines(), [0, 1, 2, 3, 4, 120, 123, 200].map(idOf));

      const n = posts.length;
      await page.locator(".editor .bar").getByRole("button", { name: w.save, exact: true }).click();
      for (let i = 0; i < 50 && posts.length === n; i++) await page.waitForTimeout(50);
      assert.deepEqual(posts.at(-1).body.accountProxies, { [idOf(120)]: "socks5://10.0.0.5:1080", [idOf(123)]: "direct", [idOf(200)]: "direct" });
      assert.deepEqual(errors, []);
    });
  }
}

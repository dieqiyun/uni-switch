// Run with Node's test runner and Playwright on the module path; see README.md.
// Each of several keys copies its own (#1480, IamMiao): with more than one
// key the editor has no API key field to show one in, so each key's row
// has a Copy button. It asks provider/key for that row's key, copies it,
// says which was copied, and opens nothing else (the name beside it renames
// on a click). The page doesn't scroll and the rows fit a phone's width.
// In English and Chinese, Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(process.env.ASSET_DIR || path.resolve(__dirname, "../assets"));
const full = { aaaaaaaaaa: "sk-full-one", bbbbbbbbbb: "sk-full-two", cccccccccc: "sk-full-off" };
const relay = () => ({
  id: "relay", name: "Relay", icon: "generic", chat: "https://relay.example.com/v1", responses: "", anthropic: "", catalog: "",
  models: [{ id: "big", name: "", on: true }], agents: [], fallback: [], headers: {}, routing: "",
  key: { set: true, masked: "sk-…one" }, balanceToken: { takes: false, set: false }, proxy: "",
  keyList: [
    { id: "aaaaaaaaaa", name: "team-a", masked: "sk-…one", on: true, active: true },
    { id: "bbbbbbbbbb", name: "team-b", masked: "sk-…two", on: true },
    { id: "cccccccccc", name: "", masked: "sk-…off", on: false },
  ],
});

function serve(lang, asked, copied) {
  const providers = { providers: [relay()], presets: [], excluded: [], gateway: { running: true, window: true } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/provider/key") {
      const body = route.request().postDataJSON();
      asked.push(body);
      return json({ key: body.account ? full[body.account] : "sk-full-one" });
    }
    if (url.pathname === "/api/copy") { copied.push(route.request().postDataJSON().text); return json({}); }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file).catch(() => ""), contentType });
  };
}

const words = { en: { b: "team-b copied", off: "sk-…off copied" }, zh: { b: "已复制team-b", off: "已复制sk-…off" } };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    for (const width of [420, 900]) {
      test(`${engine} ${lang} ${width}px: each key's row copies that key`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await (await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const asked = [], copied = [];
        await page.route("**/*", serve(lang, asked, copied));
        await page.goto("http://uni-switch.test/?view=providers");
        await page.locator(".row.provider", { hasText: "Relay" }).first().click();
        await page.locator(".editor .accts .acc.add").waitFor();
        const row = (id) => page.locator(`.editor .accts.keys .acc[data-account-id="${id}"]`);
        for (const id of Object.keys(full)) assert.equal(await row(id).locator("button.copy").count(), 1, `${id} has no Copy`);
        const scrolled = () => page.evaluate(() => [window.scrollY, ...[...document.querySelectorAll("*")].filter((e) => e.scrollTop > 0).map((e) => `${e.className}:${e.scrollTop}`)].join(" "));

        for (const [id, say] of [["bbbbbbbbbb", words[lang].b], ["cccccccccc", words[lang].off]]) {
          const b = row(id).locator("button.copy");
          await b.scrollIntoViewIfNeeded();
          const box = await b.boundingBox();
          assert.ok(box && box.x >= 0 && box.x + box.width <= width, `${id}'s Copy off the row: ${JSON.stringify(box)}`);
          const sc = await scrolled();
          await b.click();
          await page.waitForFunction((n) => document.querySelector("#status")?.textContent.includes(n), say);
          assert.equal(await scrolled(), sc, "a click scrolled");
          assert.equal(await row(id).locator(".rename-in").count(), 0, "the click renamed the key");
        }
        assert.deepEqual(asked, [{ id: "relay", account: "bbbbbbbbbb" }, { id: "relay", account: "cccccccccc" }]);
        assert.deepEqual(copied, ["sk-full-two", "sk-full-off"]);
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, "the page scrolls sideways");
        assert.deepEqual(errors, []);
      });
    }
  }
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// An older uni-switch keeping the gateway's port sends every agent's request its
// own way, without this version's fixes (#506: a Factory 403 worded as
// v0.1.550 words it, to people on v0.1.630). The Gateway card says which
// version serves and what to do, in English and Chinese; another uni-switch of
// the same version is just "served by another uni-switch".
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function fixture(lang, gateway) {
  const providers = {
    providers: [{ id: "fixture", name: "Fixture", icon: "generic", models: [{ id: "model-a", name: "Model A", on: true }], agents: [] }],
    gateway: { running: true, window: true, mine: false, url: "http://127.0.0.1:3999", calls: [], groups: [], models: 1, ...gateway },
  };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (route.request().method() !== "GET" && url.pathname.startsWith("/api/")) return json({});
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) await new Promise((r) => setTimeout(r, 20e3));
      return json({ mine: false, now: new Date().toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const cases = [
  { lang: "en", state: "running · served by uni-switch 0.1.550, older than this one", sub: /through uni-switch 0\.1\.550.*Quit it here and this one serves the gateway at once/, same: "running · served by another uni-switch" },
  { lang: "zh", state: "运行中 · 由更旧的 uni-switch 0.1.550 提供", sub: /经 uni-switch 0\.1\.550.*在这里关闭它，当前版本会立即接管网关/, same: "运行中 · 由另一个 uni-switch 提供" },
];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const c of cases) {
    test(`${engine} ${c.lang}: an older uni-switch serving the gateway is named`, async (t) => {
      assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch());
      t.after(() => browser.close());
      const errors = [];
      for (const [gw, older] of [[{ version: "0.1.550", older: true }, true], [{ version: "0.1.630" }, false]]) {
        const context = await browser.newContext({ viewport: { width: 420, height: 640 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", fixture(c.lang, gw));
        await page.goto("http://uni-switch.test/?view=gateway");
        const state = page.locator("#gateway .state");
        await state.waitFor();
        if (older) {
          assert.equal((await state.textContent()).trim(), c.state);
          assert.match(await page.locator("#gateway .sub").textContent(), c.sub);
          assert(await page.locator("#gateway > .gw-mark").evaluate((d) => !d.classList.contains("on")), "the mark's light isn't green");
          // the whole text is there to read, nothing cut off, and the page doesn't scroll sideways
          const fits = await page.locator("#gateway .sub").evaluate((s) => s.scrollWidth <= s.clientWidth + 1);
          assert(fits, "the advice is cut off");
          assert(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), "the page scrolls sideways");
        } else {
          assert.equal((await state.textContent()).trim(), c.same);
          assert(await page.locator("#gateway > .gw-mark").evaluate((d) => d.classList.contains("on")));
        }
        await context.close();
      }
      assert.deepEqual(errors, []);
    });
  }
}

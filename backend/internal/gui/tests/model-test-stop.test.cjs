// Run with Node's test runner and Playwright on the module path; see README.md.
// A test under way can be stopped (RIck on Discord: 测试模型 took long to
// fail and couldn't be cancelled). While provider/test hasn't answered, the
// editor's Test models and the endpoints' Test read Stop; Stop aborts the
// request and posts provider/teststop with the run the test was sent with,
// so the server ends the vendor's request too, then the buttons read as
// before and the footer says the test was stopped. A chip's own test is
// stopped from the same button. No left-border accent, at a narrow width,
// in every language, Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const IDS = ["gpt-5.1", "model-b"];
const relay = {
  id: "relay", name: "Relay", preset: "relay", icon: "generic", chat: "https://relay.example.com/v1", responses: "", anthropic: "", catalog: "",
  models: IDS.map((id) => ({ id, name: id, on: true })), agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-…one" }, keyList: [], balanceToken: { takes: false, set: false }, proxy: "",
};

// provider/test never answers: the request waits until the page aborts it
function serve(lang, tests, stops) {
  // a preset's provider, so its editor shows the endpoints and their Test
  const providers = { providers: [relay], presets: [{ id: "relay", name: "Relay", icon: "generic", chat: relay.chat, added: true }], excluded: [], gateway: { running: true, window: true } };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/provider/test") { tests.push(route.request().postDataJSON()); return; }
    if (url.pathname === "/api/provider/teststop") { stops.push(route.request().postDataJSON()); return route.fulfill({ status: 204, body: "" }); }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { test: "Test", all: "Test models", item: "Test this model", stop: "Stop", stopped: "Test stopped" },
  zh: { test: "测试", all: "测试模型", item: "测试此模型", stop: "停止", stopped: "已停止测试" },
  "zh-TW": { test: "測試", all: "測試模型", item: "測試此模型", stop: "停止", stopped: "已停止測試" },
  ja: { test: "テスト", all: "モデルをテスト", item: "このモデルをテスト", stop: "停止", stopped: "テストを停止しました" },
  de: { test: "Testen", all: "Modelle testen", item: "Dieses Modell testen", stop: "Stoppen", stopped: "Test gestoppt" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(words)) {
    const w = words[lang];
    test(`${engine} ${lang}: a test under way is stopped from its button`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const page = await (await browser.newContext({ viewport: { width: 560, height: 760 }, reducedMotion: "reduce" })).newPage();
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-model-test-stop.png`) });
        }
        await browser.close();
      });
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const tests = [], stops = [];
      await page.route("**/*", serve(lang, tests, stops));
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator(".row.provider", { hasText: "Relay" }).click();
      await page.locator(".editor .mchips .mchip").first().waitFor();
      const all = page.locator(".editor .mfoot button[data-tests]");
      const ep = page.locator(".editor .eps button[data-tests]");
      const until = async (cond, what) => { for (let i = 0; i < 100; i++) { if (await cond()) return; await page.waitForTimeout(50); } assert.fail(what); };
      const reads = async (b, text) => until(async () => (await b.textContent()) === text, `the button never read ${text}`);
      const footer = () => page.waitForFunction((want) => document.querySelector("#status")?.textContent === want, w.stopped);

      // Test models, then Stop
      assert.equal(await all.textContent(), w.all);
      await all.click();
      await until(async () => tests.length === 1, "Test models sent nothing");
      assert.deepEqual(tests[0].test, IDS);
      assert(tests[0].run, "the test is sent with a run to stop it by");
      await reads(all, w.stop);
      await reads(ep, w.stop);
      const border = await page.evaluate(() => [...document.querySelectorAll(".editor button[data-tests]")].map((e) => getComputedStyle(e).borderLeftWidth).filter((b) => parseFloat(b) > 1));
      assert.deepEqual(border, [], "no left-border accent");
      const over = await page.evaluate(() => document.documentElement.scrollWidth > innerWidth);
      assert.equal(over, false, "nothing runs off the page");
      await all.click();
      await until(async () => stops.length === 1, "Stop posted no teststop");
      assert.deepEqual(stops[0], { run: tests[0].run }, "teststop names the test's run");
      await reads(all, w.all);
      await reads(ep, w.test);
      await footer();

      // the endpoints' Test, stopped from itself
      await page.evaluate(() => { document.querySelector("#status").textContent = ""; });
      await ep.click();
      await until(async () => tests.length === 2, "Test sent nothing");
      assert.equal(tests[1].test, undefined, "the endpoint test names no models");
      await reads(ep, w.stop);
      await ep.click();
      await until(async () => stops.length === 2, "Stop posted no teststop");
      assert.equal(stops[1].run, tests[1].run);
      assert.notEqual(tests[1].run, tests[0].run, "each test has its own run");
      await reads(ep, w.test);
      await footer();

      // a chip's own test, stopped from Test models' place
      await page.evaluate(() => { document.querySelector("#status").textContent = ""; });
      await page.locator(".editor .mchips .mchip", { hasText: "model-b" }).click({ button: "right" });
      await page.locator(".pop.row-menu").getByRole("menuitem", { name: w.item }).click();
      await until(async () => tests.length === 3, "the chip's test sent nothing");
      assert.deepEqual(tests[2].test, ["model-b"]);
      await reads(all, w.stop);
      await all.click();
      await until(async () => stops.length === 3, "Stop posted no teststop");
      assert.equal(stops[2].run, tests[2].run);
      await reads(all, w.all);
      await footer();

      if (lang !== "en") {
        const missing = await page.evaluate((l) => ["Stop", "Test stopped"].filter((k) => !I18N[l][k]), lang);
        assert.deepEqual(missing, [], "every string has its translation");
      }
      assert.deepEqual(errors, []);
    });
  }
}

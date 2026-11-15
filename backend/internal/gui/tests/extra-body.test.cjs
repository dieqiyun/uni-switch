// Run with Node's test runner and Playwright on the module path; see README.md.
// A provider's Extra body (#1604, Luc-xz: a relay that turns Responses
// requests away without a prompt_cache_key): the editor shows the saved
// JSON, says live why what is typed can't be saved — not JSON, not an
// object, a field uni-switch sets itself (model, stream, the conversation) —
// and a Save is held until it can; a good one is saved as an object, sent
// with Test before a Save, and an emptied box clears it ({}). It fits a
// narrow window. In English, Chinese, Japanese and German, Chromium and
// WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const relay = {
  id: "relay", name: "Relay", preset: "", icon: "generic", chat: "", responses: "https://relay.example.com/v1", anthropic: "", catalog: "", baseAPI: "responses",
  models: [{ id: "gpt-5.1", name: "gpt-5.1", on: true }], agents: [], fallback: [], headers: {},
  extraBody: { prompt_cache_key: "${sessionId}" },
  key: { set: true, masked: "sk-…one" }, keyList: [{ id: "k1", name: "", masked: "sk-…one", on: true, active: true }],
  balanceToken: { takes: false, set: false }, proxy: "",
};

function serve(lang, calls) {
  const providers = { providers: [relay], presets: [], excluded: [], gateway: { running: true, window: true } };
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
      const body = route.request().postDataJSON();
      calls.push([url.pathname.slice(14), body]);
      if (url.pathname === "/api/provider/test") return json({ results: (body.test || ["gpt-5.1"]).map((model) => ({ protocol: "responses", ok: true, ms: 9, model })) });
      if (url.pathname === "/api/provider/save") return json(providers);
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const words = {
  en: { label: "Extra body", save: "Save", test: "Test models", model: "uni-switch sets model itself", object: "write a JSON object", json: "not valid JSON" },
  zh: { label: "额外请求体", save: "保存", test: "测试模型", model: "model 由 uni-switch 自己设置", object: "请写一个 JSON 对象", json: "不是有效的 JSON" },
  ja: { label: "追加ボディ", save: "保存", test: "モデルをテスト", model: "model は uni-switch が自分で設定", object: "JSON オブジェクトを書いてください", json: "有効な JSON ではありません" },
  de: { label: "Zusätzlicher Body", save: "Speichern", test: "Modelle testen", model: "model setzt uni-switch selbst", object: "schreib ein JSON-Objekt", json: "kein gültiges JSON" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(words)) {
    const w = words[lang];
    test(`${engine} ${lang}: Extra body is checked as it is typed, saved, tested and cleared`, async (t) => {
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
      const open = async () => {
        await page.locator(".row.provider", { hasText: "Relay" }).click();
        const ed = page.locator(".editor");
        await ed.locator(".extra-body textarea").waitFor();
        return ed;
      };
      let ed = await open();
      const box = ed.locator(".extra-body textarea");
      const why = ed.locator(".extra-body .why");
      assert.equal(await page.locator(`label[for="${await box.getAttribute("id")}"]`).textContent(), w.label, "labelled");
      assert.deepEqual(JSON.parse(await box.inputValue()), { prompt_cache_key: "${sessionId}" }, "the saved JSON is shown");
      assert.ok(!(await box.evaluate((e) => e.classList.contains("bad"))));
      assert.equal(await why.textContent(), "");

      const said = async (text, want) => {
        await box.fill(text);
        assert.ok(await box.evaluate((e) => e.classList.contains("bad")), `${text} is marked`);
        assert.equal(await box.getAttribute("aria-invalid"), "true");
        assert.ok((await why.textContent()).includes(want), `${text}: ${await why.textContent()}`);
        assert.ok(await why.isVisible());
      };
      await said(`{"model": "gpt-4o"}`, w.model);
      await said(`{"!stream": false}`, "stream");
      await said(`{"messages": []}`, "messages");
      await said(`[1, 2]`, w.object);
      await said(`{"a":`, w.json);

      // a Save is held while it can't be saved, and says why
      await ed.locator(".bar").getByRole("button", { name: w.save, exact: true }).click();
      await page.waitForTimeout(200);
      assert.deepEqual(calls.map((c) => c[0]), [], "nothing saved");
      assert.ok(await ed.isVisible());
      assert.ok(await box.evaluate((e) => e === document.activeElement), "the box is focused");

      // a good one: no mark, Test asks with it before a Save
      const good = { prompt_cache_key: "${sessionId}", "!service_tier": "flex", metadata: { app: "uni-switch" } };
      await box.fill(JSON.stringify(good, null, 2));
      assert.ok(!(await box.evaluate((e) => e.classList.contains("bad"))));
      assert.equal(await box.getAttribute("aria-invalid"), "false");
      assert.ok(!(await why.isVisible()));
      await ed.locator(".mfoot").getByRole("button", { name: w.test, exact: true }).click();
      for (let i = 0; i < 60 && !calls.length; i++) await page.waitForTimeout(50);
      assert.equal(calls[0][0], "test");
      assert.deepEqual(calls[0][1].extraBody, good, "Test asks with the extra body typed");

      // the narrow window: the box and its reason stay inside the editor
      await page.setViewportSize({ width: 420, height: 900 });
      await box.fill(`{"model": "a-model-name-long-enough-to-need-wrapping-in-a-narrow-window-x"}`);
      const fits = await page.evaluate(() => {
        const ed = document.querySelector(".editor").getBoundingClientRect();
        return [...document.querySelectorAll(".extra-body textarea, .extra-body .why")].every((e) => { const r = e.getBoundingClientRect(); return r.left >= ed.left - 0.5 && r.right <= ed.right + 0.5; });
      });
      assert.ok(fits, "fits a narrow window");
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth), "no sideways scroll");
      await page.setViewportSize({ width: 900, height: 900 });

      await box.fill(JSON.stringify(good));
      await ed.locator(".bar").getByRole("button", { name: w.save, exact: true }).click();
      for (let i = 0; i < 60 && !calls.some((c) => c[0] === "save"); i++) await page.waitForTimeout(50);
      const saved = calls.find((c) => c[0] === "save");
      assert.ok(saved, "saved");
      assert.deepEqual(saved[1].extraBody, good, "saved as an object");

      // emptied: a Save clears it
      calls.length = 0;
      await page.locator(".editor").waitFor({ state: "hidden" });
      ed = await open();
      await ed.locator(".extra-body textarea").fill("");
      await ed.locator(".bar").getByRole("button", { name: w.save, exact: true }).click();
      for (let i = 0; i < 60 && !calls.some((c) => c[0] === "save"); i++) await page.waitForTimeout(50);
      assert.deepEqual(calls.find((c) => c[0] === "save")?.[1].extraBody, {}, "an empty box clears it");
      assert.deepEqual(errors, []);
    });
  }
}

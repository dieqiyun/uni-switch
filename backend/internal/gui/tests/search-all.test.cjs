// Run with Node's test runner and Playwright on the module path; see README.md.
// Settings › Models › Web search (cybercaius on Discord: "能否强制支持联网搜索的
// 供应商模型走代搜 API"): "Models that search by themselves" picks their
// provider's own search, as before, or uni-switch's, as every other model has.
// A click sets searchAll on its own (POST /api/settings/search-all), a save
// of the rest of the page doesn't send it (the server keeps it), and the
// page isn't moved. With nothing uni-switch
// can search with the row is there, dimmed, and says why. With provider search off and
// searchAll on, the searcher's note no longer says a provider's own search
// is unchanged. In every language, wide and narrow.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn, reach } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const words = {
  en: { name: "Models that search by themselves", own: "Their own", uniSwitch: "uni-switch's", apis: "Search APIs", every: "for every model", unchanged: "unchanged", none: "add a Search API" },
  zh: { name: "自带联网搜索的模型", own: "自带搜索", uniSwitch: "uni-switch 代搜", apis: "搜索 API", every: "所有模型", unchanged: "不受影响", none: "添加搜索 API" },
  "zh-TW": { name: "自帶聯網搜尋的模型", own: "內建搜尋", uniSwitch: "uni-switch 代搜", apis: "搜尋 API", every: "所有模型", unchanged: "不受影響", none: "新增搜尋 API" },
  ja: { name: "自分でウェブ検索するモデル", own: "ネイティブ検索", uniSwitch: "uni-switch の検索", apis: "検索 API", every: "どのモデルにも", unchanged: "影響しません", none: "検索 API を追加" },
  de: { name: "Modelle, die selbst suchen", own: "Eigene Suche", uniSwitch: "Von uni-switch", apis: "Such-APIs", every: "für jedes Modell", unchanged: "unverändert", none: "Such-API hinzu" },
};

function serve(lang, saved, all, { provider = true, apis = true, searcher = "ds", searchAll = false } = {}) {
  const settings = { lang, theme: "light", redact: true, searcher, searchAll,
    searchProvider: provider ? "DeepSeek · deepseek-flash" : "",
    searchVendors: [{ id: "tavily", name: "Tavily", keysURL: "https://app.tavily.com/home" }],
    searchAPIs: apis ? [{ vendor: "tavily", name: "Tavily", key: "tvly…abcd", ready: true, ...(apis === "off" ? { off: true } : {}) }] : [],
    searchChoices: [{ id: "ds", name: "DeepSeek", icon: "deepseek", small: "deepseek-flash", models: [] }] };
  const state = { agents: [], profiles: [], settings };
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/settings/search-all") {
      all.push(JSON.parse(r.request().postData()));
      settings.searchAll = all.at(-1).on;
      return json(settings);
    }
    if (url.pathname === "/api/settings") {
      if (r.request().method() === "POST") {
        saved.push(JSON.parse(r.request().postData()));
        Object.assign(settings, saved.at(-1));
      }
      return json(settings);
    }
    if (url.pathname === "/api/groups") return json({ models: [], groups: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await r.fulfill({ body: await fs.readFile(file), contentType }); } catch { await r.fulfill({ status: 404 }); }
  };
}

async function open(engine, t, lang, saved, opts, width = 1100, all = []) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  t.after(() => browser.close());
  const page = await (await browser.newContext({ viewport: { width, height: 900 } })).newPage();
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/*", serve(lang, saved, all, opts));
  await watchSettingsReads(page);
  await page.goto(`http://uni-switch.test/?view=settings&tab=models&lang=${lang}`);
  await settingsDrawn(page); // rows found are the ones that stay (#1659)
  return { page, errors };
}

async function press(page, locator) {
  await reach(page, locator);
  const b = await locator.boundingBox();
  await page.mouse.click(b.x + b.width / 2, b.y + b.height / 2);
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const [lang, w] of Object.entries(words)) {
    for (const width of [1100, 560]) {
      test(`${engine} ${lang} ${width}px: a model that searches by itself can be given uni-switch's search`, async (t) => {
        const saved = [], all = [];
        const { page, errors } = await open(engine, t, lang, saved, {}, width, all);
        const row = page.locator("#searchList #searchAllRow");
        await reach(page, row);
        assert.equal(await row.locator(".name").textContent(), w.name);
        const own = row.locator(".segs button", { hasText: w.own });
        const mine = row.locator(".segs button", { hasText: w.uniSwitch });
        assert(await own.evaluate((b) => b.classList.contains("on")), "their own search at first");
        assert(!await mine.evaluate((b) => b.classList.contains("on")));
        // the row fits the window: nothing wider than the page
        assert(await page.evaluate(() => document.scrollingElement.scrollWidth <= innerWidth), "the page scrolls sideways");
        const rb = await row.boundingBox();
        for (const b of [own, mine]) {
          const bb = await b.boundingBox();
          assert(bb.x >= rb.x - 1 && bb.x + bb.width <= rb.x + rb.width + 1, `${await b.textContent()} sticks out of its row`);
        }

        await reach(page, mine);
        const before = await page.evaluate(() => [scrollX, scrollY, document.scrollingElement.scrollTop]);
        await press(page, mine);
        for (let i = 0; i < 50 && !all.length; i++) await page.waitForTimeout(50);
        assert.deepEqual(all, [{ on: true }], "set once, on its own");
        assert.equal(saved.length, 0, "the rest of the settings not written");
        await page.waitForFunction(() => prefsBusy === 0);
        await page.locator("#searchAllRow .segs button.on", { hasText: w.uniSwitch }).waitFor();
        assert.deepEqual(await page.evaluate(() => [scrollX, scrollY, document.scrollingElement.scrollTop]), before, "the click moved nothing");

        // another choice saved on the page keeps it
        await press(page, page.locator("#searchFirstRow .segs button", { hasText: w.apis }));
        for (let i = 0; i < 50 && saved.length < 1; i++) await page.waitForTimeout(50);
        assert.equal(saved.at(-1).searchFirst, "api");
        await page.waitForFunction(() => prefsBusy === 0);
        await page.locator("#searchAllRow .segs button.on", { hasText: w.uniSwitch }).waitFor();

        // and back
        await press(page, page.locator("#searchAllRow .segs button", { hasText: w.own }));
        for (let i = 0; i < 50 && all.length < 2; i++) await page.waitForTimeout(50);
        assert.deepEqual(all.at(-1), { on: false });
        assert.deepEqual(errors, []);
      });
    }

    test(`${engine} ${lang}: with nothing uni-switch can search with the row says why and can't be chosen`, async (t) => {
      // cybercaius turned the search APIs off and couldn't find the row
      // ("开关在哪个位置"): it stays, dimmed, and says where to add a search
      const all = [];
      const { page } = await open(engine, t, lang, [], { provider: false, apis: "off", searcher: "off" }, 560, all);
      const row = page.locator("#searchList #searchAllRow");
      await reach(page, row);
      assert.equal(await row.locator(".name").textContent(), w.name);
      assert((await row.locator(".sub").innerText()).includes(w.none), await row.locator(".sub").innerText());
      assert.equal(await row.locator(".segs button:disabled").count(), 2, "both choices can't be picked");
      assert(await row.evaluate((r) => r.classList.contains("off")));
      assert(await page.evaluate(() => document.scrollingElement.scrollWidth <= innerWidth), "the page scrolls sideways");
      await row.locator(".segs button", { hasText: w.uniSwitch }).click({ force: true });
      await page.waitForTimeout(200);
      assert.deepEqual(all, [], "a dimmed choice saved nothing");
    });

    test(`${engine} ${lang}: provider search off, every model is searched with the APIs`, async (t) => {
      const { page } = await open(engine, t, lang, [], { provider: false, searcher: "off", searchAll: true });
      await page.locator("#searchAllRow").waitFor();
      const note = await page.locator("#searchList .searcher-row .sub").first().innerText();
      assert(note.includes(w.every) && !note.includes(w.unchanged), note);
    });
  }
}

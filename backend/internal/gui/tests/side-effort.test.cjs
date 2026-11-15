// Run with Node's test runner and Playwright on the module path; see README.md.
// How hard the models uni-switch asks on its own think (MOMO on Discord,
// "外置模型能不能提供思考深度选项"): Settings › Models has, beside Image
// recognition's model and the searcher's, a chip that opens the app's menu
// (no native select) with Default and only the levels that model has. A
// level picked is saved as visionEffort / searcherEffort and shown on the
// chip; Default saves "" (nothing sent beyond uni-switch's own choice). Another
// setting saved keeps both (prefsKeep). A model whose levels aren't known,
// or a row turned off, has no chip. No click moves the page, and both chips
// fit their row at 1100 and 420. Every language, Chromium and WebKit, with
// the API faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn, reach } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const choices = [
  { id: "antigravity", name: "Antigravity", icon: "antigravity", small: "gemini-3-flash", own: true, models: [
    { id: "antigravity/gemini-3-flash", name: "Gemini 3 Flash", provider: "antigravity", providerName: "Antigravity" }] },
];
// the menu's head and Default, said in each language
const words = {
  en: { head: "Thinking", dflt: "Default", visionNote: "the least it has", searchNote: "none sent, the model's own" },
  zh: { head: "思考", dflt: "默认", visionNote: "它最低的一档", searchNote: "不发送，由模型自己决定" },
  "zh-TW": { head: "思考", dflt: "預設", visionNote: "它最低的一檔", searchNote: "不傳送，由模型自己決定" },
  ja: { head: "思考", dflt: "デフォルト", visionNote: "最も低いレベル", searchNote: "送らず、モデル自身に任せる" },
  de: { head: "Denken", dflt: "Standard", visionNote: "seine niedrigste Stufe", searchNote: "nichts gesendet, das Modell entscheidet" },
};

function serve(lang, posted, st) {
  const settings = () => ({ lang, theme: "light", gateway: "http://127.0.0.1:3999", port: 3999,
    vision: st.vision, visionAuto: "codex/gpt-6.1-sol",
    visionModels: [{ id: "codex/gpt-6.1-sol", name: "GPT-6.1 Sol", provider: "codex", providerName: "Codex", icon: "openai" }],
    visionEfforts: st.vision === "off" ? undefined : st.visionLevels, visionEffort: st.visionEffort, visionEffortNow: st.visionEffort || undefined,
    searchVendors: [], searchAPIs: [], searchChoices: choices, searchAuto: "Antigravity · gemini-3-flash",
    searcher: st.searcher, searchProvider: "Gemini 3 Flash · Antigravity",
    searchEfforts: st.searcher === "off" ? undefined : st.searchLevels, searcherEffort: st.searcherEffort, searchEffortNow: st.searcherEffort || undefined });
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = ${JSON.stringify({ lang, theme: "light", web: true })};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: settings() });
    if (url.pathname === "/api/settings") {
      if (r.request().method() === "POST") {
        const b = JSON.parse(r.request().postData());
        posted.push(b);
        Object.assign(st, { vision: b.vision, searcher: b.searcher, visionEffort: b.visionEffort, searcherEffort: b.searcherEffort });
      }
      return json(settings());
    }
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/groups") return json({ models: [], groups: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    const body = await fs.readFile(file).catch(() => null);
    await (body ? r.fulfill({ body, contentType }) : r.fulfill({ status: 404, body: "" }));
  };
}

const rows = [
  { name: "Image recognition", chip: "#imagesList button.vision-effort", key: "visionEffort", levels: "visionLevels", note: "visionNote" },
  { name: "the searcher", chip: "#searchList button.searcher-effort", key: "searcherEffort", levels: "searchLevels", note: "searchNote" },
];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(words)) {
    const w = words[lang];
    test(`${engine} ${lang}: Image recognition and the searcher pick how hard their model thinks`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const posted = [];
      const st = { vision: "codex/gpt-6.1-sol", searcher: "antigravity/gemini-3-flash", visionEffort: "", searcherEffort: "",
        visionLevels: ["low", "medium", "high", "xhigh"], searchLevels: ["minimal", "low", "medium", "high"] };
      await page.route("**/*", serve(lang, posted, st));
      await watchSettingsReads(page);
      const where = () => page.evaluate(() => [scrollX, scrollY, document.scrollingElement.scrollTop, document.querySelector(".view:not([hidden]) ")?.scrollTop ?? 0, document.querySelector("main")?.scrollTop ?? 0]);
      const press = async (loc) => {
        const b = await loc.boundingBox();
        await page.mouse.click(b.x + b.width / 2, b.y + b.height / 2);
      };
      for (const width of [1100, 420]) {
        await page.mouse.move(0, 0);
        await page.setViewportSize({ width, height: 900 });
        await page.goto("http://uni-switch.test/?view=settings&tab=models");
        await settingsDrawn(page);
        const tr = (k) => page.evaluate((k) => t(k), k);
        for (const row of rows) {
          const chip = page.locator(row.chip);
          await reach(page, chip);
          // the chip fits its row, beside the model's, and the page doesn't scroll sideways
          const fit = await chip.evaluate((c) => {
            const r = c.closest(".row").getBoundingClientRect(), b = c.getBoundingClientRect(), m = c.previousElementSibling.getBoundingClientRect();
            return { inside: b.left >= r.left - 1 && b.right <= r.right + 1, beside: m.right <= b.left + 1 || m.bottom <= b.top + 1,
              wide: document.documentElement.scrollWidth - document.documentElement.clientWidth, tag: c.tagName, native: !!c.closest(".row").querySelector("select") };
          });
          assert(fit.inside && fit.beside && fit.wide <= 0 && !fit.native, `${row.name} ${width}: ${JSON.stringify(fit)}`);
          assert.equal(await chip.innerText(), w.dflt, `${row.name}: nothing picked reads Default`);
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await chip.locator("xpath=ancestor::div[contains(@class,'row')][1]").screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-${row.key}.png`) });
          }
          const before = await where();
          // the menu: Default, then only the model's own levels
          await press(chip);
          const menu = page.locator(".proto-menu.side-effort-menu");
          await menu.waitFor({ state: "visible" });
          assert.equal(await menu.locator(".pm-head").innerText(), w.head);
          const names = await menu.locator(".pm-name").allInnerTexts();
          const want = [w.dflt, ...await Promise.all(st[row.levels].map(tr))];
          assert.deepEqual(names, want, `${row.name}: only the model's levels`);
          // German's minimal is the same word
          if (lang !== "en") for (const l of st[row.levels]) if (!(lang === "de" && l === "minimal")) assert.notEqual(await tr(l), l, `${l} is said in ${lang}`);
          assert.equal(await menu.locator(".pm-item").first().locator(".pm-note").innerText(), w[row.note]);
          assert.equal(await menu.locator(".pm-item.on .pm-name").innerText(), w.dflt);
          const mb = await menu.boundingBox();
          assert(mb.x >= 0 && mb.x + mb.width <= width + 1, `menu in the window at ${width}: ${JSON.stringify(mb)}`);
          // a level picked is saved and shown
          await press(menu.locator(".pm-item").nth(3));
          await page.waitForFunction(([sel, k]) => document.querySelector(sel)?.innerText === k, [row.chip, await tr(st[row.levels][2])]);
          assert.equal(posted.at(-1)[row.key], st[row.levels][2]);
          assert.equal(await page.locator(row.chip).getAttribute("aria-expanded"), "false");
          // ticked when opened again, and Default clears it
          await press(page.locator(row.chip));
          await menu.waitFor({ state: "visible" });
          assert.equal(await menu.locator(".pm-item.on .pm-name").innerText(), await tr(st[row.levels][2]));
          await press(menu.locator(".pm-item").first());
          await page.waitForFunction(([sel, d]) => document.querySelector(sel)?.innerText === d, [row.chip, w.dflt]);
          assert.equal(posted.at(-1)[row.key], "");
          // the chip again closes its menu, Escape too, and neither saves
          const n = posted.length;
          await press(page.locator(row.chip));
          await menu.waitFor({ state: "visible" });
          await press(page.locator(row.chip));
          await menu.waitFor({ state: "detached" });
          await press(page.locator(row.chip));
          await menu.waitFor({ state: "visible" });
          await page.keyboard.press("Escape");
          await menu.waitFor({ state: "detached" });
          assert.equal(posted.length, n);
          assert.deepEqual(await where(), before, `${row.name} ${width}: the clicks moved nothing`);
        }
        // another setting saved keeps both picks
        st.visionEffort = "high"; st.searcherEffort = "minimal";
        await page.evaluate(() => fetch("/api/settings").then((r) => r.json()).then((s) => { prefs = s; state.settings = s; renderSettings(); }));
        await page.waitForFunction(() => document.querySelector("#imagesList button.vision-effort")?.innerText !== "" && document.querySelector("#searchList button.searcher-effort"));
        await page.evaluate(() => savePrefs({ ...prefsKeep(prefs), noStats: true }));
        assert.equal(posted.at(-1).visionEffort, "high");
        assert.equal(posted.at(-1).searcherEffort, "minimal");
        st.visionEffort = ""; st.searcherEffort = "";
      }
      // levels not known, or the row off: no chip
      st.visionLevels = undefined;
      st.searcher = "off";
      await page.goto("http://uni-switch.test/?view=settings&tab=models");
      await settingsDrawn(page);
      await page.locator("#searchList button.searcher-pick").waitFor();
      assert.equal(await page.locator("#imagesList button.vision-effort, #searchList button.searcher-effort").count(), 0);
      assert.deepEqual(errors, []);
    });
  }
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// Settings' Web search: the providers that search, in their order, when the
// one named fails (axelburks on Discord: "代搜供应商能不能支持设置多个，这样
// 可以一个失败了换另一个供应商"). A row under the one named lists them as
// numbered chips; Add offers every searcher but the one named, Automatic,
// Off and those listed, and puts the pick at the end; a chip's picker
// changes it or removes it. Saved as searcherThen. With none named (or Off)
// there is no such row. The failure note says who searched in its place.
// Every language, Chromium and WebKit, at a narrow and a wide window, with
// the API faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn, reach, pickItem } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const choices = [
  { id: "claude", name: "Claude", icon: "claude", small: "claude-haiku-4-5", models: [
    { id: "claude/claude-haiku-4-5", name: "Claude Haiku 4.5", provider: "claude", providerName: "Claude" },
    { id: "claude/claude-opus-4-5", name: "Claude Opus 4.5", provider: "claude", providerName: "Claude" }] },
  { id: "openai", name: "OpenAI", icon: "openai", small: "gpt-5-mini", models: [
    { id: "openai/gpt-5-mini", name: "GPT-5 mini", provider: "openai", providerName: "OpenAI" },
    { id: "openai/gpt-5.5", name: "GPT Five Five", provider: "openai", providerName: "OpenAI" }] },
  { id: "relay", name: "MyRelay", icon: "generic", small: "gpt-5.5", models: [
    { id: "relay/gpt-5.5", name: "gpt-5.5", provider: "relay", providerName: "MyRelay" }] },
];
const words = {
  en: { name: "If it fails", sub: "These providers search in its place, in this order, when the one above fails; no other provider does.", add: "Add", remove: "Remove",
    instead: /^GPT Five Five · OpenAI didn't search .+: 400 search quota used up\. Claude Opus 4\.5 · Claude searched in its place\.$/,
    none: /^GPT Five Five · OpenAI didn't search .+: 400 search quota used up\. The ones after it didn't search either; add another provider after it, or a Search API\.$/ },
  zh: { name: "失败时依次换用", sub: "上面的供应商搜索失败时，按此顺序由这些供应商代为搜索；不会换用其他供应商。", add: "添加", remove: "移除",
    instead: /^GPT Five Five · OpenAI .+没能搜索：400 search quota used up。已由 Claude Opus 4\.5 · Claude 代为搜索。$/,
    none: /^GPT Five Five · OpenAI .+没能搜索：400 search quota used up。排在它后面的供应商也都没能搜索；/ },
  "zh-TW": { name: "失敗時依序換用", sub: "上面的供應商搜尋失敗時，依此順序由這些供應商代為搜尋；不會換用其他供應商。", add: "新增", remove: "移除",
    instead: /^GPT Five Five · OpenAI .+沒能搜尋：400 search quota used up。已由 Claude Opus 4\.5 · Claude 代為搜尋。$/,
    none: /^GPT Five Five · OpenAI .+沒能搜尋：400 search quota used up。排在它後面的供應商也都沒能搜尋；/ },
  ja: { name: "失敗したとき", sub: "上のプロバイダが検索に失敗すると、これらのプロバイダがこの順番で代わりに検索します。他のプロバイダは使われません。", add: "追加", remove: "削除",
    instead: /^GPT Five Five · OpenAI は.+、検索できませんでした：400 search quota used up。代わりに Claude Opus 4\.5 · Claude が検索しました。$/,
    none: /^GPT Five Five · OpenAI は.+、検索できませんでした：400 search quota used up。後ろのプロバイダも検索できませんでした。/ },
  de: { name: "Wenn er fehlschlägt", sub: "Diese Anbieter suchen an seiner Stelle, in dieser Reihenfolge, wenn der obige fehlschlägt; kein anderer Anbieter.", add: "Hinzufügen", remove: "Entfernen",
    instead: /^GPT Five Five · OpenAI hat .+ nicht gesucht: 400 search quota used up\. Claude Opus 4\.5 · Claude hat an seiner Stelle gesucht\.$/,
    none: /^GPT Five Five · OpenAI hat .+ nicht gesucht: 400 search quota used up\. Die Anbieter danach haben auch nicht gesucht;/ },
};

function serve(lang, posted, st) {
  const settings = () => ({ lang, theme: "light", searchVendors: [], searchAPIs: [], searchChoices: choices,
    searchAuto: "Claude · claude-haiku-4-5", searchProvider: st.searcher, searchRelays: ["MyRelay"], searchLeftOut: [],
    searcher: st.searcher, searcherThen: st.then, searchUnused: "", searchFailed: st.failed });
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data, status = 200) => r.fulfill({ status, json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = ${JSON.stringify({ lang, theme: "light", web: true })};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: settings() });
    if (url.pathname === "/api/settings") {
      if (r.request().method() === "POST") {
        const b = JSON.parse(r.request().postData());
        posted.push(b);
        st.searcher = b.searcher ?? st.searcher;
        if (b.searcherThen) st.then = b.searcherThen;
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

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const [lang, w] of Object.entries(words)) {
    for (const width of [560, 1000]) {
      test(`${engine} ${lang} ${width}: the searchers after the one named, in their order`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await (await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const posted = [];
        const st = { searcher: "", then: [], failed: null };
        await page.route("**/*", serve(lang, posted, st));
        const load = async () => {
          await watchSettingsReads(page);
          await page.goto("http://uni-switch.test/?view=settings&tab=models");
          await settingsDrawn(page);
          await page.locator("#searchList .searcher-row").waitFor();
        };
        const redraw = () => page.evaluate(() => fetch("/api/settings").then((r) => r.json()).then((s) => { prefs = s; state.settings = s; renderSettings(); }));
        const chips = () => page.locator("#searchList .searcher-then button.searcher-then-pick").allInnerTexts().then((x) => x.map((s) => s.replace(/\s+/g, " ").trim()));
        const offered = () => page.locator("#list li:not(.group)").allInnerTexts();

        // none named, or Off: nothing to follow it
        await load();
        assert.equal(await page.locator("#searchList .searcher-then").count(), 0, "automatic");
        st.searcher = "off";
        await redraw();
        await page.waitForFunction(() => document.querySelector("#searchList button.searcher-pick")?.innerText.length > 0);
        assert.equal(await page.locator("#searchList .searcher-then").count(), 0, "off");

        // one named: the row under it, said in the language, empty
        st.searcher = "openai/gpt-5.5";
        await load();
        const row = page.locator("#searchList .row.searcher-then");
        await row.waitFor();
        assert.equal(await row.locator(".name").innerText(), w.name);
        assert.equal(await row.locator(".sub").innerText(), w.sub);
        assert.deepEqual(await chips(), []);
        assert.equal(await row.evaluate((r) => r.previousElementSibling?.classList.contains("searcher-row")), true, "right under the one named");
        await reach(page, row);
        const before = await page.evaluate(() => [scrollY, document.querySelector("main")?.scrollTop ?? 0]);

        // Add: every searcher but the one named, Automatic and Off
        await pickItem(page, row.locator("button.searcher-then-add"));
        await page.locator("#pop").waitFor({ state: "visible" });
        let items = await offered();
        assert(!items.some((x) => x.includes("GPT Five Five")), "not the one named: " + items.join(" | "));
        assert(!items.some((x) => x.startsWith("Automatic") || x.startsWith(w.remove)), items.join(" | "));
        assert(items.some((x) => x.includes("Claude Opus 4.5")) && items.some((x) => x.includes("MyRelay")), items.join(" | "));
        await pickItem(page, page.locator("#list li:not(.group)", { hasText: "Claude Opus 4.5" }));
        await page.waitForFunction(() => document.querySelectorAll("#searchList .searcher-then-pick").length === 1);
        assert.deepEqual(posted.at(-1).searcherThen, ["claude/claude-opus-4-5"]);
        assert.equal(posted.at(-1).searcher, "openai/gpt-5.5", "the one named is kept");
        assert.deepEqual(await chips(), ["1. Claude Opus 4.5 · Claude"]);

        // a second goes after it; the first is not offered again
        await pickItem(page, row.locator("button.searcher-then-add"));
        await page.locator("#pop").waitFor({ state: "visible" });
        items = await offered();
        assert(!items.some((x) => x.includes("Claude Opus 4.5")), "listed already: " + items.join(" | "));
        await pickItem(page, page.locator("#list li:not(.group)", { hasText: "MyRelay" }).last());
        await page.waitForFunction(() => document.querySelectorAll("#searchList .searcher-then-pick").length === 2);
        assert.deepEqual(posted.at(-1).searcherThen, ["claude/claude-opus-4-5", "relay"]);
        assert.deepEqual(await chips(), ["1. Claude Opus 4.5 · Claude", "2. MyRelay · gpt-5.5"]);

        // the row fits the window, its chips wrapped in it
        const fits = await row.evaluate((r) => {
          const a = r.getBoundingClientRect();
          return [...r.querySelectorAll("button")].every((b) => { const c = b.getBoundingClientRect(); return c.left >= a.left - 1 && c.right <= a.right + 1; })
            && document.scrollingElement.scrollWidth <= innerWidth;
        });
        assert(fits, "chips inside the row");

        // a chip's picker: change it, or remove it
        await pickItem(page, row.locator("button.searcher-then-pick").first());
        await page.locator("#pop").waitFor({ state: "visible" });
        items = await offered();
        assert(items.some((x) => x.trim() === w.remove) && items.some((x) => x.includes("MyRelay")) === false, "Remove, and none listed after it: " + items.join(" | "));
        await pickItem(page, page.locator("#list li:not(.group)").filter({ hasText: new RegExp(`^\\s*${w.remove}\\s*$`) }));
        await page.waitForFunction(() => document.querySelectorAll("#searchList .searcher-then-pick").length === 1);
        assert.deepEqual(posted.at(-1).searcherThen, ["relay"]);
        assert.deepEqual(await chips(), ["1. MyRelay · gpt-5.5"]);
        assert.deepEqual(await page.evaluate(() => [scrollY, document.querySelector("main")?.scrollTop ?? 0]), before, "no click moved the page");

        // the failure note: who searched in its place, or that none did
        const at = new Date(Date.now() - 5 * 60e3).toISOString();
        st.then = ["claude/claude-opus-4-5"];
        st.failed = { searcher: "openai/gpt-5.5", why: "400 search quota used up", instead: "claude/claude-opus-4-5", at };
        await load();
        assert.match(await page.locator("#searchList .searcher-failed").innerText(), w.instead);
        st.failed = { searcher: "openai/gpt-5.5", why: "400 search quota used up", at };
        await load();
        assert.match(await page.locator("#searchList .searcher-failed").innerText(), w.none);
        assert.deepEqual(errors, []);
      });
    }
  }
}

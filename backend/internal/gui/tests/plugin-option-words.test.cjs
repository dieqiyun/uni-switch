// Run with Node's test runner and Playwright on the module path; see README.md.
// A plugin option in the reader's language, and a short model list said to
// be filtered (plugins#590, anewuser6937 on Discord): WorkBuddy's "Free
// models only" showed in English inside the Chinese window, and with it on
// the editor listed one model with nothing saying why, so the plugin read as
// broken. The option's labels, descriptions and confirms give its words by
// the window's language (zh-TW falls back to zh, any other to the plain
// one), and while a switch that says filtersModels is on, as saved, the
// Models field says so under the chips. Off, nothing is said. Both engines,
// 1100 and 420 wide, every language.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

const free = (on) => ({
  key: "free_only", label: "Free models only", description: "Lists only the models WorkBuddy serves at no cost.",
  labels: { zh: "仅免费模型", "zh-TW": "僅免費模型", ja: "無料モデルのみ", de: "Nur kostenlose Modelle" },
  descriptions: { zh: "只列出 WorkBuddy 免费提供的模型。", "zh-TW": "只列出 WorkBuddy 免費提供的模型。", ja: "WorkBuddy が無料で提供するモデルだけを表示します。", de: "Listet nur die Modelle, die WorkBuddy kostenlos anbietet." },
  filtersModels: true, on,
});
// only Simplified Chinese words: zh-TW reads them, the rest the plain ones
const beta = { key: "beta", label: "Beta models", confirm: "Beta models may be withdrawn.", labels: { zh: "测试版模型" }, confirms: { zh: "测试版模型可能会下线。" }, on: false };

function serve(lang, on) {
  const plugin = { id: "workbuddy", pid: "workbuddy", name: "WorkBuddy", icon: "generic", spec: "the retired opencode-workbuddy-auth", signedIn: true, models: 1,
    methods: [{ type: "api", label: "API key" }], options: [free(on), beta] };
  const payload = { providers: [{ id: "workbuddy", name: "WorkBuddy", icon: "generic", chat: "plugin://workbuddy/v1", responses: "", anthropic: "", catalog: "",
    models: [{ id: "hy3", name: "Hy3", on: true }], agents: [], fallback: [], headers: {}, keyList: [], key: {}, proxy: "", maxConcurrency: null,
    account: { agent: "workbuddy", agentName: "WorkBuddy", agentIcon: "generic", user: "me@fake", logins: [{ user: "me@fake", active: true, on: true }] } }],
  // moved onto its plugin, as every WorkBuddy user is since 10-17
  presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [plugin], onPlugins: ["workbuddy"] };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json(payload);
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const W = {
  en: { models: "Models", on: "On", label: "Free models only", about: "Lists only the models WorkBuddy serves at no cost.", beta: "Beta models", ask: "Beta models may be withdrawn.",
    note: "Only some models are listed: the plugin option “Free models only” is on." },
  zh: { models: "模型", on: "开启", label: "仅免费模型", about: "只列出 WorkBuddy 免费提供的模型。", beta: "测试版模型", ask: "测试版模型可能会下线。",
    note: "只列出部分模型：插件选项「仅免费模型」已开启。" },
  "zh-TW": { models: "模型", on: "開啟", label: "僅免費模型", about: "只列出 WorkBuddy 免費提供的模型。", beta: "测试版模型", ask: "测试版模型可能会下线。",
    note: "只列出部分模型：外掛選項「僅免費模型」已開啟。" },
  ja: { models: "モデル", on: "オン", label: "無料モデルのみ", about: "WorkBuddy が無料で提供するモデルだけを表示します。", beta: "Beta models", ask: "Beta models may be withdrawn.",
    note: "一部のモデルのみ表示しています。プラグインのオプション「無料モデルのみ」がオンです。" },
  de: { models: "Modelle", on: "An", label: "Nur kostenlose Modelle", about: "Listet nur die Modelle, die WorkBuddy kostenlos anbietet.", beta: "Beta models", ask: "Beta models may be withdrawn.",
    note: "Nur einige Modelle werden angezeigt: Die Plugin-Option „Nur kostenlose Modelle“ ist an." },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a plugin option's own words and its filtered list", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const width of [1100, 420]) for (const lang of Object.keys(W)) for (const on of [true, false]) {
      await t.test(`${width} ${lang} ${on ? "on" : "off"}`, async () => {
        const w = W[lang];
        const ctx = await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, on));
        await page.goto("http://uni-switch.test/?view=providers");
        await page.locator(".row.provider", { hasText: "WorkBuddy" }).click();
        const ed = page.locator("#modal .editor");
        const box = ed.locator(".plugin-options");
        await box.waitFor();
        await page.waitForTimeout(300); // the editor drawn again as what it asked for comes in
        const row = (k) => box.locator(`.plugin-option[data-key="${k}"]`);
        assert.equal(await row("free_only").locator(".n").textContent(), w.label);
        assert.equal(await row("free_only").locator(".hint").textContent(), w.about);
        assert.equal(await row("beta").locator(".n").textContent(), w.beta);

        // the Models field: its label, then the chips, the note under them
        const models = ed.locator("label", { hasText: new RegExp("^" + w.models + "$") }).first();
        await models.waitFor();
        const field = models.locator("xpath=following-sibling::div[1]");
        const notes = await field.locator(":scope > .hint").allTextContents();
        if (on) {
          assert.deepEqual(notes, [w.note]);
          const where = await field.evaluate((f) => {
            const hint = f.querySelector(":scope > .hint"), chips = f.firstElementChild, r = f.closest(".editor").getBoundingClientRect(), h = hint.getBoundingClientRect();
            return { below: h.top >= chips.getBoundingClientRect().bottom - 1, spill: h.right - r.right, page: document.documentElement.scrollWidth - document.documentElement.clientWidth };
          });
          assert.ok(where.below, "the note is under the chips");
          assert.ok(where.spill <= 0, "the note runs past the editor by " + where.spill);
          assert.ok(where.page <= 0, "the page scrolls sideways by " + where.page);
          if (process.env.ARTIFACT_DIR) await field.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `plugin-option-words-${engine}-${width}-${lang}.png`) });
        } else {
          assert.deepEqual(notes, [], "nothing said while the switch is off");
        }

        // its confirm in the reader's language too
        await row("beta").locator(".opt", { hasText: w.on }).click();
        const dialog = page.locator("dialog.action-confirm");
        await dialog.waitFor();
        assert.equal(await dialog.locator("p").textContent(), w.ask);
        assert.ok((await dialog.locator("h2").textContent()).includes(w.beta));
        assert.deepEqual(errors, []);
        await ctx.close();
      });
    }
  });
}

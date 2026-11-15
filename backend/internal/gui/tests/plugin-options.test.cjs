// Run with Node's test runner and Playwright on the module path; see README.md.
// A plugin's own switches (#2038, Vocllum): a plugin declares options for
// its provider (auth.options) and its provider's editor shows each as an
// Off/On beside the plugin's words, put in as text. One the plugin asks to
// confirm asks before it is turned on, and Cancel leaves it off. Changes
// wait for Save, which posts each changed one by the plugin's own provider
// id before the provider; one switched back is not posted. Both engines,
// 1100 and 420 wide, every language.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

const LONG = "Claims the daily reward whenever usage is read; <b>not</b> on a schedule, and only for accounts that are signed in through this plugin";
const plugin = { id: "fakeco", pid: "fakeco", name: "FakeCo", icon: "generic", spec: "opencode-fakeco-auth", signedIn: true, models: 1,
  methods: [{ type: "api", label: "API key" }],
  options: [
    { key: "auto", label: "Claim <i>rewards</i>", description: LONG, default: true, on: true },
    { key: "beta", label: "Beta models", confirm: "Beta models may be withdrawn at any time.", on: false },
    { key: "quiet", label: "Quiet", on: false },
  ] };

function serve(lang, posts) {
  const payload = { providers: [{ id: "fakeco", name: "FakeCo", icon: "generic", chat: "plugin://fakeco/v1", responses: "", anthropic: "", catalog: "",
    models: [{ id: "m1", name: "M1", on: true }], agents: [], fallback: [], headers: {}, keyList: [], key: {}, proxy: "", maxConcurrency: null,
    account: { agent: "fakeco", agentName: "FakeCo", agentIcon: "generic", user: "me@fake", logins: [{ user: "me@fake", active: true, on: true }] } }],
  presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [plugin] };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json(payload);
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/settings/plugin-option") {
      posts.push(["option", route.request().postDataJSON()]);
      return json({ lang, theme: "light" });
    }
    if (url.pathname.startsWith("/api/provider/") && route.request().method() === "POST") {
      posts.push([url.pathname.slice("/api/provider/".length), route.request().postDataJSON()]);
      return json(payload);
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const W = {
  en: { field: "Plugin options", off: "Off", on: "On", turnOn: "Turn on", cancel: "Cancel", save: "Save" },
  zh: { field: "插件选项", off: "关闭", on: "开启", turnOn: "开启", cancel: "取消", save: "保存" },
  "zh-TW": { field: "外掛選項", off: "關閉", on: "開啟", turnOn: "開啟", cancel: "取消", save: "儲存" },
  ja: { field: "プラグインのオプション", off: "オフ", on: "オン", turnOn: "オンにする", cancel: "キャンセル", save: "保存" },
  de: { field: "Plugin-Optionen", off: "Aus", on: "An", turnOn: "Einschalten", cancel: "Abbrechen", save: "Speichern" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a plugin's options", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const width of [1100, 420]) for (const lang of Object.keys(W)) {
      await t.test(`${width} ${lang}`, async () => {
        const w = W[lang];
        const ctx = await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], posts = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, posts));
        await page.goto("http://uni-switch.test/?view=providers");
        await page.locator(".row.provider", { hasText: "FakeCo" }).click();
        const ed = page.locator("#modal .editor");
        const box = ed.locator(".plugin-options");
        await box.waitFor();
        await page.waitForTimeout(300); // the editor drawn again as what it asked for comes in
        assert.equal(await ed.locator("label", { hasText: w.field }).count(), 1);
        const row = (k) => box.locator(`.plugin-option[data-key="${k}"]`);
        const state = async (k) => (await row(k).locator(".opt.on").innerText()).trim();
        // the plugin's words are text: its markup shows as typed
        assert.equal(await row("auto").locator(".n").textContent(), "Claim <i>rewards</i>");
        assert.equal(await row("auto").locator(".hint").textContent(), LONG);
        assert.equal(await box.locator("i, b").count(), 0);
        assert.equal(await state("auto"), w.on, "its default");
        assert.equal(await state("beta"), w.off);
        // the long words wrap inside the editor; nothing runs past it
        const spill = await page.evaluate(() => {
          const e = document.querySelector("#modal .editor"), r = e.getBoundingClientRect();
          return { page: document.documentElement.scrollWidth - document.documentElement.clientWidth,
            rows: [...e.querySelectorAll(".plugin-option, .plugin-option .segs")].map((x) => x.getBoundingClientRect().right - r.right) };
        });
        assert.ok(spill.page <= 0, "the page scrolls sideways by " + spill.page);
        for (const s of spill.rows) assert.ok(s <= 0, "an option runs past the editor by " + s);
        if (process.env.ARTIFACT_DIR) await box.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `plugin-options-${engine}-${width}-${lang}.png`) });

        // turning on one the plugin asks to confirm: Cancel leaves it off
        await row("beta").locator(".opt", { hasText: w.on }).click();
        const dialog = page.locator("dialog.action-confirm");
        await dialog.waitFor();
        assert.equal(await dialog.locator("p").textContent(), "Beta models may be withdrawn at any time.");
        assert.match(await dialog.locator("h2").textContent(), /Beta models/);
        await dialog.locator("button", { hasText: w.cancel }).click();
        await dialog.waitFor({ state: "detached" });
        assert.equal(await state("beta"), w.off);
        await row("beta").locator(".opt", { hasText: w.on }).click();
        await dialog.locator("button", { hasText: w.turnOn }).click();
        await dialog.waitFor({ state: "detached" });
        assert.equal(await state("beta"), w.on);
        // no confirm asked of the others; one switched back isn't posted
        await row("auto").locator(".opt", { hasText: w.off }).click();
        await row("quiet").locator(".opt", { hasText: w.on }).click();
        await row("quiet").locator(".opt", { hasText: w.off }).click();
        assert.equal(await dialog.count(), 0);
        assert.deepEqual(posts, [], "nothing saved before Save");

        await ed.locator(".bar").getByRole("button", { name: w.save, exact: true }).click();
        for (let i = 0; i < 50 && !posts.some(([k]) => k === "save"); i++) await page.waitForTimeout(50);
        assert.deepEqual(posts.map(([k, b]) => k === "option" ? b : k), [
          { provider: "fakeco", key: "beta", on: true },
          { provider: "fakeco", key: "auto", on: false },
          "save",
        ]);
        assert.deepEqual(errors, []);
        await ctx.close();
      });
    }
  });
}

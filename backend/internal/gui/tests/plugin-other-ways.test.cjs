// Run with Node's test runner and Playwright on the module path; see README.md.
// A moved subscription still signs in with one click (#777), and its
// plugin's other ways are offered beside it: ZCode's site step also offers
// the ZCode app's own sign-in and the API key (Jinyu: add the account ZCode
// is signed in to, not a sign-in page), and the waiting box of one with
// ways besides the one under way and its key has "Other ways to sign in…",
// which cancels that sign-in and shows the ways. Factory, with its real two
// ways, still goes straight to its device code, its key the one link away:
// its key import, which sends the keys to its plugin, not a second link.
// English and Chinese; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

const plugins = [
  { id: "zcode", pid: "zcode", name: "ZCode", icon: "generic", spec: "the retired opencode-zcode-auth", signedIn: false, models: 3,
    methods: [{ type: "oauth", label: "ZCode: Z.ai GLM Coding Plan" }, { type: "oauth", label: "ZCode: BigModel (智谱) GLM Coding Plan" }, { type: "oauth", label: "ZCode app's sign-in" }, { type: "api", label: "GLM Coding Plan API key" }] },
  { id: "factory", pid: "factory", name: "Factory", icon: "generic", spec: "the retired opencode-factory-auth", signedIn: false, models: 3,
    methods: [{ type: "oauth", label: "Sign in with Factory (device code)" }, { type: "api", label: "Factory API key (fk-…)", placeholder: "fk-…" }] },
];

function server(lang, asked) {
  const payload = () => ({ providers: [{ id: "deepseek", name: "DeepSeek", icon: "deepseek", chat: "https://api.deepseek.com/v1", models: [], agents: [], key: {} }], presets: [], excluded: [], gateway: { running: true, window: true }, plugins, onPlugins: ["zcode", "factory"] });
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    const body = () => route.request().postDataJSON();
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json(payload());
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname === "/api/plugin-signin/prompt") { asked.push(["prompt", body()]); return json({ prompt: null, inputs: {} }); }
    if (url.pathname === "/api/plugin-signin") {
      const b = body();
      asked.push(["signin", b]);
      return json({ id: "s" + asked.length, agent: b.provider, state: "waiting", url: "https://fake.test/device", code: b.provider === "factory" ? "ABCD-EFGH" : "" });
    }
    if (/^\/api\/signin\/s\d+\/cancel$/.test(url.pathname)) { asked.push(["cancel", url.pathname.split("/")[3]]); return json({}); }
    if (/^\/api\/signin\/s\d+$/.test(url.pathname)) return new Promise(() => {}); // stays waiting
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const L = {
  en: { anyway: "Sign in anyway", app: "ZCode app's sign-in", key: "GLM Coding Plan API key", other: "Other ways to sign in…", how: "How do you sign in to", apiKey: "Use an API key instead" },
  zh: { anyway: "仍然登录", app: "使用 ZCode 应用已登录的账号", key: "GLM Coding Plan API Key", other: "其他登录方式…", how: "用哪种方式登录", apiKey: "改用 API" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a moved subscription's other ways to sign in", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh"]) {
      await t.test(lang, async () => {
        const w = L[lang];
        const page = await (await browser.newContext({ viewport: { width: 900, height: 800 } })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], asked = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, asked));
        await page.goto("http://uni-switch.test/?view=providers");
        await page.locator("#addProvider").click();
        const sheet = page.locator("#addSheet");
        const box = sheet.locator(".signing");
        const signins = () => asked.filter(([k]) => k === "signin").map(([, b]) => b);

        // ZCode's site step: the two sites, then the app's sign-in and the key
        await sheet.locator('.tile[data-pick="ZCode"]').click();
        await box.locator('button[data-site="bigmodel"]').waitFor();
        const ways = await box.locator("button[data-site], button[data-method]").evaluateAll((bs) => bs.map((b) => b.dataset.site || "m" + b.dataset.method + ":" + b.textContent));
        assert.deepEqual(ways, ["zai", "bigmodel", "m2:" + w.app, "m3:" + w.key]);
        if (process.env.ARTIFACT_DIR) await box.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `plugin-other-ways-zcode-${engine}-${lang}.png`) });

        // the app's sign-in signs in with the plugin's way 2, no page to open
        await box.locator('button[data-method="2"]').click();
        await box.locator("button.other-ways").waitFor();
        assert.deepEqual(signins(), [{ provider: "zcode", method: 2, inputs: {} }]);
        if (process.env.ARTIFACT_DIR) await box.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `plugin-other-ways-waiting-${engine}-${lang}.png`) });

        // "Other ways to sign in…" cancels it and shows every way
        await box.locator("button.other-ways", { hasText: w.other }).click();
        await box.locator("span.n", { hasText: w.how }).waitFor();
        assert.deepEqual(asked.filter(([k]) => k === "cancel").map(([, id]) => id), ["s2"]);
        assert.equal(await box.locator(".choices button[data-method]").count(), 4);
        await box.locator("button.text:not(.primary)").first().click(); // Cancel

        // Factory, two ways: still one click to its device code, no picker;
        // its other way is the key link it had, with no "Other ways" beside it
        asked.length = 0;
        await sheet.locator('.tile[data-pick="Factory"]').click();
        await box.locator("button", { hasText: w.anyway }).click();
        await box.locator(".devcode code", { hasText: "ABCD-EFGH" }).waitFor();
        assert.deepEqual(signins(), [{ provider: "factory", method: 0, inputs: {} }]);
        assert.equal(await box.locator(".choices").count(), 0);
        assert.equal(await box.locator("button.other-ways").count(), 0);
        if (process.env.ARTIFACT_DIR) await box.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `plugin-other-ways-factory-${engine}-${lang}.png`) });
        // one key link, not the plugin's beside the import's: the import
        // sends its keys to the plugin (factory_key.go)
        const keys = box.locator(".acts button.link", { hasText: w.apiKey });
        assert.equal(await keys.count(), 1, "two API key links");
        await keys.click();
        await page.locator("#addSheet textarea").waitFor();
        assert.deepEqual(errors, []);
      });
    }
  });
}

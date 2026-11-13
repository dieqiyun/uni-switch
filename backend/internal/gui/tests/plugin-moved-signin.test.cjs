// Run with Node's test runner and Playwright on the module path; see README.md.
// Built-ins moved onto their plugins sign in as they did: they stay under
// Subscriptions (not "From plugins"); ZCode still asks the site, then
// takes the plugin's way for that site with no picker, and Try again runs
// that way again; Factory shows the code its page asks about to copy;
// Cursor, which kept one account built in, keeps as
// many as its plugin does. English and Chinese; the API is faked here.
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
    methods: [{ type: "oauth", label: "Factory account" }] },
  { id: "cursor", pid: "cursor", name: "Cursor", icon: "generic", spec: "the retired opencode-cursor-auth", signedIn: true, models: 3,
    methods: [{ type: "oauth", label: "Cursor (browser)" }, { type: "oauth", label: "cursor-agent's sign-in" }, { type: "api", label: "API key" }] },
];

function server(lang, asked) {
  const payload = () => ({
    providers: [{ id: "cursor", name: "Cursor", icon: "cursor", chat: "plugin://cursor/v1", models: [], agents: [], key: {},
      account: { agent: "cursor", agentName: "Cursor", agentIcon: "cursor", user: "a@c", logins: [{ user: "a@c", active: true, on: true }, { user: "b@c", on: true }] } }],
    presets: [], excluded: [], gateway: { running: true, window: true }, plugins, onPlugins: ["zcode", "factory", "cursor"],
  });
  // the provider being signed in to: kept apart from asked, which the test
  // empties while the last sign-in's poll may still be on its way (#1440)
  let polls = 0, signing = "";
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
      polls = 0;
      signing = b.provider;
      return json({ id: "s1", agent: b.provider, state: "waiting", url: "https://fake.test/device", code: b.provider === "factory" ? "ABCD-EFGH" : "", instructions: "Confirm the code ABCD-EFGH on Factory's page" });
    }
    if (url.pathname === "/api/signin/s1") {
      // ZCode's fails on the second look, to be tried again
      const p = signing;
      if (++polls < 2 || p !== "zcode") return json({ id: "s1", agent: p, state: "waiting", url: "https://fake.test/device", code: p === "factory" ? "ABCD-EFGH" : "" });
      return json({ id: "s1", agent: "zcode", state: "failed", error: "the sign-in page expired" });
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const L = {
  en: { subs: "Subscriptions", plugins: "From plugins", anyway: "Sign in anyway", key: "Use an API key instead…", again: "Try again", single: "keeps one account", signedIn: "Signed in ·" },
  zh: { subs: "订阅", plugins: "来自插件", anyway: "仍然登录", key: "改用 API Key…", again: "重试", single: "只保存一个账号", signedIn: "已登录 ·" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": moved built-ins sign in as they did", async (t) => {
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
        await sheet.locator(".kind b", { hasText: w.subs }).waitFor();
        // under Subscriptions, where they were, and nowhere else
        const sections = await sheet.evaluate((s) => {
          const out = {};
          let at = "";
          for (const x of s.querySelectorAll(".kind b, .tile")) {
            if (x.matches(".kind b")) at = x.textContent;
            else out[x.dataset.pick] = (out[x.dataset.pick] || []).concat(at);
          }
          return out;
        });
        for (const name of ["ZCode", "Factory", "Cursor"]) assert.deepEqual(sections[name], [w.subs], name + " is under " + sections[name]);
        assert.equal(await sheet.locator(".kind b", { hasText: w.plugins }).count(), 0);

        // Cursor keeps both its accounts, not "signed in" to one
        const cursor = sheet.locator('.tile[data-pick="Cursor"]');
        assert.equal(await cursor.locator(".cnt").innerText(), "2");
        assert.ok(!(await cursor.getAttribute("title")).includes(w.signedIn), "Cursor reads as keeping one account");

        // ZCode: the site, then that site's way, no picker; tried again,
        // the same way
        const box = sheet.locator(".signing");
        await sheet.locator('.tile[data-pick="ZCode"]').click();
        await box.locator('button[data-site="bigmodel"]').click();
        const again = box.locator("button", { hasText: w.again });
        await again.waitFor();
        await again.click();
        await box.locator(".signlink").waitFor();
        assert.deepEqual(asked.filter(([k]) => k === "signin").map(([, b]) => b), [{ provider: "zcode", method: 1, inputs: {} }, { provider: "zcode", method: 1, inputs: {} }]);
        await box.locator("button.text:not(.primary)").last().click();

        // Factory: warned, its one way, the code to copy
        asked.length = 0;
        await sheet.locator('.tile[data-pick="Factory"]').click();
        // and its API keys, which its plugin keeps (factory_key.go)
        await box.locator("button", { hasText: w.key }).waitFor();
        await box.locator("button", { hasText: w.anyway }).click();
        await box.locator(".devcode code", { hasText: "ABCD-EFGH" }).waitFor();
        if (process.env.ARTIFACT_DIR) await box.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `plugin-moved-code-${engine}-${lang}.png`) });
        assert.deepEqual(asked.filter(([k]) => k === "signin").map(([, b]) => b), [{ provider: "factory", method: 0, inputs: {} }]);
        await page.keyboard.press("Escape");

        // Cursor's editor: as many accounts as the plugin keeps
        await page.locator(".row.provider", { hasText: "Cursor" }).click();
        const ed = page.locator("#modal .editor");
        await ed.locator(".accts").waitFor();
        assert.ok(!(await ed.innerText()).includes(w.single), "the editor says Cursor keeps one account");
        assert.deepEqual(errors, []);
      });
    }
  });
}

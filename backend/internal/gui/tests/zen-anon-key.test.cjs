// Run with Node's test runner and Playwright on the module path; see README.md.
// #1795 (cuddly-guacamole): Add another key on an OpenCode Zen provider
// that has a key of the user's own says what an empty field adds, public,
// the key Zen's free models answer with none of the user's, as adding a
// Zen provider with no key says its free models need none. Add with
// nothing pasted posts keys/add, and the new row is marked. A provider
// with no such key (anonKey) keeps "paste an API key". In every language,
// at a wide and a narrow window, with no page error.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const PUBLIC_ID = "efa1f375d7"; // sha256("public")[:5], provider.keyID

const zen = () => ({
  id: "opencode-zen", name: "OpenCode Zen", icon: "opencode", preset: "opencode-zen", chat: "https://opencode.ai/zen/v1", responses: "", anthropic: "", catalog: "opencode",
  models: [{ id: "big-pickle", name: "", on: true }], agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-z…zzzz" }, anonKey: "public", balanceToken: { takes: false, set: false }, proxy: "",
  keyList: [{ id: "aaaaaaaaaa", name: "", masked: "sk-z…zzzz", on: true, active: true }],
});
const relay = () => ({
  id: "relay", name: "Relay", icon: "generic", chat: "https://relay.example.com/v1", responses: "", anthropic: "", catalog: "",
  models: [{ id: "big", name: "", on: true }], agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-…one" }, balanceToken: { takes: false, set: false }, proxy: "",
  keyList: [{ id: "bbbbbbbbbb", name: "", masked: "sk-…one", on: true, active: true }],
});

function serve(lang, posts) {
  const z = zen();
  const providers = { providers: [z, relay()], presets: [], excluded: [], gateway: { running: true, window: true } };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/keys/add" && route.request().method() === "POST") {
      posts.push(route.request().postDataJSON());
      z.keyList = [...z.keyList, { id: PUBLIC_ID, name: "", masked: "••••••", on: true }];
      delete z.anonKey;
      return json(providers);
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { another: "Add another key", add: "Add", hint: "paste an API key, or leave empty for public: free models only", plain: "paste an API key" },
  zh: { another: "添加另一个密钥", add: "添加", hint: "粘贴 API 密钥，或留空使用 public：仅限免费模型", plain: "粘贴 API 密钥" },
  "zh-TW": { another: "新增另一個金鑰", add: "新增", hint: "貼上 API 金鑰，或留空使用 public：僅限免費模型", plain: "貼上 API 金鑰" },
  ja: { another: "別のキーを追加", add: "追加", hint: "APIキーを貼り付け、空欄なら public：無料モデルのみ", plain: "APIキーを貼り付け" },
  de: { another: "Weiteren Schlüssel hinzufügen", add: "Hinzufügen", hint: "API-Schlüssel einfügen, oder leer lassen für public: nur kostenlose Modelle", plain: "API-Schlüssel einfügen" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(words)) {
    for (const width of [900, 560]) {
      const w = words[lang];
      const open = async (t, name, row) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        let page;
        t.after(async () => {
          if (process.env.ARTIFACT_DIR && page) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-${name}-zen-anon-key.png`) });
          }
          await browser.close();
        });
        page = await (await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const posts = [];
        await page.route("**/*", serve(lang, posts));
        // localhost is a secure context, where the page can hash the new key to mark its row
        await page.goto("http://localhost/?view=providers");
        await page.locator(".row.provider", { hasText: row }).first().click();
        await page.locator(".editor .accts .acc.add").waitFor();
        await page.getByRole("button", { name: w.another }).click();
        await page.locator(".acc.adding input[type=password]").waitFor();
        return { page, errors, posts };
      };

      test(`${engine} ${lang} ${width}px: Zen's Add another key says an empty field adds public, and adds it`, async (t) => {
        const { page, errors, posts } = await open(t, "zen", "OpenCode Zen");
        const field = page.locator(".acc.adding input[type=password]");
        assert.equal(await field.getAttribute("placeholder"), w.hint);
        assert.equal(await field.getAttribute("title"), w.hint, "the whole hint, where the field cuts it short");
        await page.locator(".acc.adding").getByRole("button", { name: w.add, exact: true }).click();
        for (let i = 0; i < 60 && !posts.length; i++) await new Promise((r) => setTimeout(r, 50));
        assert.deepEqual(posts.map((b) => [b.id, b.key]), [["opencode-zen", ""]]);
        await page.locator(`.acc.new[data-account-id="${PUBLIC_ID}"]`).waitFor();
        assert.equal(await page.locator(".acc.adding").count(), 0);
        // the box fits the window: nothing runs off its right edge
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true);
        assert.deepEqual(errors, []);
      });

      test(`${engine} ${lang} ${width}px: a provider with no key of its own to add keeps "paste an API key"`, async (t) => {
        const { page, errors } = await open(t, "relay", "Relay");
        const field = page.locator(".acc.adding input[type=password]");
        assert.equal(await field.getAttribute("placeholder"), w.plain);
        assert.equal(await field.getAttribute("title"), null);
        assert.deepEqual(errors, []);
      });
    }
  }
}

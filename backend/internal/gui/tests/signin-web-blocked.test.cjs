// Run with Node's test runner and Playwright on the module path; see README.md.
// uni-switch web on a server or in Docker (treeMan on Discord): a sign-in's page
// is opened in the viewer's browser as a new tab once the sign-in has
// started, after the click, and a browser may block it. The window said
// the page was open while nothing opened. Now a blocked tab is said to be
// blocked, beside the link, and Open again (a click) opens it. Right
// away and once the CLI a plugin needed is installed; in every language,
// Chromium and WebKit; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

// a plugin of its own name: the built-in Grok tile sits beside one named Grok
const plugin = { id: "fakeco", pid: "fakeco", name: "FakeCo", icon: "generic", spec: "opencode-fakeco-auth", signedIn: false, models: 3,
  methods: [{ type: "oauth", label: "Sign in with Grok Build" }, { type: "api", label: "API key" }] };

const URL_ = "https://accounts.x.ai/oauth2/device?user_code=AB12-CD34";

// first is what /api/plugin-signin answers, then is what /api/signin/p1 does
function server(lang, first, then, opens) {
  const payload = () => ({ providers: [{ id: "openai", name: "OpenAI", icon: "openai", preset: "openai", models: [], agents: [], key: { set: true, masked: "sk-…ab12" } }], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [plugin] });
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json(payload());
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname === "/api/plugin-signin/prompt") return json({ prompt: null, inputs: {} });
    if (url.pathname === "/api/plugin-signin") return json(first);
    if (url.pathname === "/api/signin/p1") { then.asked = (then.asked || 0) + 1; return json(then); }
    if (url.pathname === "/api/open") { opens.push(JSON.parse(route.request().postData() || "{}").url); return route.fulfill({ status: 204, body: "" }); }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const waiting = { id: "p1", agent: "fakeco", state: "waiting", url: URL_, instructions: "Approve the sign-in on the page; Grok Build finishes it." };
const installing = { id: "p1", agent: "fakeco", state: "installing", installing: "Grok Build" };
const cases = [
  { name: "at the start", first: waiting },
  { name: "after the install", first: installing },
];
const blocked = {
  en: "Your browser blocked the sign-in page",
  zh: "浏览器拦下了登录页面",
  "zh-TW": "瀏覽器擋下了登入頁面",
  ja: "ブラウザがサインインページを開くのをブロックしました",
  de: "Dein Browser hat das Öffnen der Anmeldeseite blockiert",
};
const again = { en: "Open again", zh: "重新打开", "zh-TW": "重新開啟", ja: "もう一度開く", de: "Erneut öffnen" };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a sign-in page the browser blocked says so, and Open again opens it", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      for (const c of cases) {
        await t.test(`${lang} ${c.name}`, async () => {
          const page = await (await browser.newContext({ viewport: { width: 1100, height: 800 }, reducedMotion: "reduce" })).newPage();
          page.setDefaultTimeout(15000); // a busy machine takes seconds over a click
          page.setDefaultNavigationTimeout(60000);
          const errors = [];
          page.on("pageerror", (e) => errors.push(e.message));
          const opens = [];
          await page.route("**/*", server(lang, c.first, { ...waiting }, opens));
          // the browser blocks the tab asked for after the sign-in started,
          // and lets the one a click asks for open
          await page.exposeFunction("__opened", (u) => opens.push(u));
          await page.addInitScript(() => {
            window.__tabs = [];
            window.open = (u) => {
              window.__opened(String(u));
              if (!window.__clicked) return null;
              const w = { opener: window };
              window.__tabs.push(w);
              return w;
            };
          });
          await page.goto("http://uni-switch.test/?view=providers");
          await page.locator("#addProvider").click();
          const sheet = page.locator("#addSheet");
          await sheet.locator('.tile[data-pick="FakeCo"]').click();
          const box = sheet.locator(".signing");
          await box.locator("button", { hasText: "Sign in with Grok Build" }).press("Enter");
          await box.locator(".s", { hasText: blocked[lang] }).waitFor();
          assert.deepEqual(opens, [URL_], "the page was asked for once");
          assert.equal(await box.locator(".signlink code").textContent(), URL_, "the link is shown");
          await page.evaluate(() => { window.__clicked = true; });
          await box.locator("button", { hasText: again[lang] }).click();
          await box.locator(".s", { hasText: "Grok Build finishes it" }).waitFor();
          assert.equal(await box.locator(".s", { hasText: blocked[lang] }).count(), 0, "the blocked line is gone");
          assert.deepEqual(opens, [URL_, URL_]);
          assert.deepEqual(errors, []);
          await page.context().close();
        });
      }
    }
  });
}

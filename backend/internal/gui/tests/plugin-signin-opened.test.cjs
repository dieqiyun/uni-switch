// Run with Node's test runner and Playwright on the module path; see README.md.
// A plugin's sign-in page opens once. Grok Build's `grok login` opens its
// page in the browser itself on macOS and Windows, and the window opened it
// again: two pages every time (andyfmyth on X). A sign-in that says its page
// is open already (opened) has the window ask for no /api/open, right away
// or once the CLI it needed is installed; one that doesn't still gets one.
// In every language, Chromium and WebKit; the API is faked here.
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

const waiting = (opened) => ({ id: "p1", agent: "fakeco", state: "waiting", url: URL_, instructions: "Approve the sign-in on the page; Grok Build finishes it.", ...(opened ? { opened: true } : {}) });
const installing = { id: "p1", agent: "fakeco", state: "installing", installing: "Grok Build" };
const cases = [
  { name: "opened by its CLI", first: waiting(true), then: waiting(true), want: 0 },
  { name: "not opened", first: waiting(false), then: waiting(false), want: 1 },
  { name: "opened by its CLI after the install", first: installing, then: waiting(true), want: 0 },
  { name: "not opened, after the install", first: installing, then: waiting(false), want: 1 },
];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a plugin sign-in's page opens once", async (t) => {
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
          const then = { ...c.then };
          await page.route("**/*", server(lang, c.first, then, opens));
          // in a browser tab the window opens a page itself
          await page.exposeFunction("__opened", (u) => opens.push(u));
          await page.addInitScript(() => { window.open = (u) => { window.__opened(String(u)); return {}; }; });
          await page.goto("http://uni-switch.test/?view=providers");
          await page.locator("#addProvider").click();
          const sheet = page.locator("#addSheet");
          await sheet.locator('.tile[data-pick="FakeCo"]').click();
          const box = sheet.locator(".signing");
          await box.locator("button", { hasText: "Sign in with Grok Build" }).press("Enter");
          // the window shows the page's words, then follows the sign-in two
          // more times: past where it would have opened the page
          await box.locator(".s", { hasText: "Grok Build finishes it" }).waitFor();
          const asked = then.asked || 0;
          await new Promise((r) => { const tick = () => (then.asked || 0) >= asked + 2 ? r() : setTimeout(tick, 50); tick(); });
          assert.deepEqual(opens, c.want ? [URL_] : [], "pages opened");
          assert.deepEqual(errors, []);
          await page.context().close();
        });
      }
    }
  });
}

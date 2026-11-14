// Run with Node's test runner and Playwright on the module path; see README.md.
// A plugin's sign-in that has no page to open (its authorize gave url "",
// a desktop app it drives itself) waits with the link to the proxy it goes
// through, a button that opens the proxy menu, not the text
// "[object HTMLButtonElement]" (#2181, guanwenpeng2001-bot: el() put the
// button into textContent). In every language, Chromium and WebKit, at a
// window's and a phone's width; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

const plugin = { id: "fakeco", pid: "fakeco", name: "FakeCo", icon: "generic", spec: "opencode-fakeco-auth", signedIn: false, models: 3,
  methods: [{ type: "oauth", label: "Sign in with the FakeCo app" }, { type: "api", label: "API key" }] };

function server(lang) {
  const flow = { id: "p1", agent: "fakeco", state: "waiting", url: "", instructions: "Finish in the FakeCo app." };
  const payload = () => ({ providers: [{ id: "openai", name: "OpenAI", icon: "openai", preset: "openai", models: [], agents: [], key: { set: true, masked: "sk-…ab12" } }], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [plugin] });
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json(payload());
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname === "/api/plugin-signin/prompt") return json({ prompt: null, inputs: {} });
    if (url.pathname === "/api/plugin-signin") return json(flow);
    if (url.pathname === "/api/signin/p1") return json(flow);
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

// "Proxy: {proxy}" in each language, up to the proxy's name
const VIA = { en: "Proxy: ", zh: "代理：", "zh-TW": "代理：", ja: "プロキシ：", de: "Proxy: " };
const sizes = [{ width: 1100, height: 800 }, { width: 420, height: 800 }];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a plugin sign-in with no page waits with its proxy link", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of Object.keys(VIA)) {
      for (const size of sizes) {
        await t.test(`${lang} ${size.width}`, async () => {
          const page = await (await browser.newContext({ viewport: size, reducedMotion: "reduce" })).newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          const errors = [];
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", server(lang));
          await page.goto("http://uni-switch.test/?view=providers");
          await page.locator("#addProvider").click();
          const sheet = page.locator("#addSheet");
          await sheet.locator('.tile[data-pick="FakeCo"]').click();
          const box = sheet.locator(".signing");
          // pressed, not clicked: at a phone's width the sheet scrolls the
          // way under its footer, which a click in WebKit lands on
          await box.locator("button", { hasText: "Sign in with the FakeCo app" }).press("Enter");
          await box.locator(".s", { hasText: "Finish in the FakeCo app." }).waitFor();
          if (process.env.ARTIFACT_DIR) await box.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `plugin-no-url-${engine}-${lang}-${size.width}.png`) });

          assert.doesNotMatch(await box.innerText(), /\[object /, "a node written out as text");
          const via = box.locator(".acts button.via");
          assert.equal(await via.count(), 1, "the proxy link is a button");
          assert.ok((await via.innerText()).startsWith(VIA[lang]), await via.innerText());
          const fit = await via.evaluate((v) => { const r = v.getBoundingClientRect(), b = v.closest(".signing").getBoundingClientRect(); return r.left >= b.left - 0.5 && r.right <= b.right + 0.5; });
          assert.ok(fit, "the link stays inside the sheet");
          await via.press("Enter");
          await page.waitForFunction(() => document.querySelector(".signing .acts button.via")?.getAttribute("aria-expanded") === "true");
          assert.deepEqual(errors, []);
          await page.context().close();
        });
      }
    }
  });
}

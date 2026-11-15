// Run with Node's test runner and Playwright on the module path; see README.md.
// Claude Desktop names a Claude model it knows after its own catalog and
// tells two providers' rows of it apart only by the ten digits in the id
// uni-switch lists it by (#2578). The Providers page says that number on the
// model's own chip, so a row of that menu can be matched to its provider.
// A model the gateway gives no number (gui.desktopNumbers) has no badge.
// In English and Chinese, Chromium and WebKit, wide and narrow.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const fetched = new Date(Date.now() - 3600e3).toISOString();
const relay = (models) => ({
  id: "relay", name: "Relay", icon: "generic", host: "relay.example.com", chat: "https://relay.example.com/v1", responses: "", anthropic: "", catalog: "",
  models, chosen: [], fetched, agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-…one" }, keyList: [], balanceToken: { takes: false, set: false }, proxy: "",
});
// the number is the one the gateway puts in the id Claude Desktop is shown:
// two providers' same Claude model, one carrying it, one its own id
const models = [
  { id: "claude-opus-5-5", name: "Opus 5.5", on: true, desktop: "3233491987" },
  { id: "claude-sonnet-5", name: "Sonnet 5", on: true },
];

function serve(lang) {
  const providers = { providers: [relay(models)], presets: [], excluded: [], gateway: { running: true, window: true } };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: "The number Claude Desktop's menu shows beside this model",
  zh: "Claude Desktop 菜单在该模型旁显示的编号",
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const [lang, width] of [["en", 900], ["zh", 900], ["en", 420]]) {
    test(`${engine} ${lang} ${width}px: the Desktop number is said on the model's chip`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const page = await (await browser.newContext({ viewport: { width, height: 600 }, reducedMotion: "reduce" })).newPage();
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-model-desktop-number.png`) });
        }
        await browser.close();
      });
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      await page.goto("http://uni-switch.test/?view=providers");

      const editor = page.locator("#modal:not([hidden]) .editor");
      await page.locator(".row.provider", { hasText: "Relay" }).click();
      await editor.locator(".ehead b", { hasText: "Relay" }).waitFor();
      await editor.locator(".mchips .mchip").first().waitFor();

      // the number on its model's chip, said where a hover reads it
      const chip = editor.locator(".mchips .mchip", { hasText: "Opus 5.5" });
      const badge = chip.locator(".badge", { hasText: "3233491987" });
      assert.equal(await badge.count(), 1, "the number is on the chip");
      assert.equal(await badge.getAttribute("title"), words[lang]);

      // the badge stays inside its chip, and the chip inside the editor,
      // narrow too
      const [b, c, ed] = await Promise.all([badge.boundingBox(), chip.boundingBox(), editor.boundingBox()]);
      assert.ok(b.x >= c.x && b.x + b.width <= c.x + c.width + 0.5, `badge ${JSON.stringify(b)} inside chip ${JSON.stringify(c)}`);
      assert.ok(c.x + c.width <= ed.x + ed.width + 0.5, `chip ${JSON.stringify(c)} inside editor ${JSON.stringify(ed)}`);

      // a model Desktop shows by its own id carries no number
      assert.equal(await editor.locator(".mchips .mchip", { hasText: "Sonnet 5" }).locator(".badge").count(), 0, "no number on its own id");

      const missing = await page.evaluate(() => ["The number Claude Desktop's menu shows beside this model"].filter((k) => !I18N.zh[k]));
      assert.deepEqual(missing, [], "every string has its Chinese");
      assert.deepEqual(errors, []);
    });
  }
}

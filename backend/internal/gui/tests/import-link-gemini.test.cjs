// A uni-switch://import link with only gemini= (#2251, ggbdpq): the dialog's
// warning names the host the user's prompts and key would go to, the
// Gemini endpoint shows, and Add saves it. Before, the warning read "will
// go to ;" with no host. All APIs and keys are fixtures.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const assets = process.env.ASSET_DIR || path.resolve(__dirname, "../assets");
const engines = process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"];
const langs = ["en", "zh", "zh-TW", "ja", "de"];

const linked = { id: "gem-relay", name: "Gem Relay", gemini: "https://gem-2251.example/v1beta", key: "", models: ["gemini-3-pro"] };

async function fixture(t, engine, lang, width) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  const page = await browser.newPage({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
  page.setDefaultTimeout(7000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [], saves = [];
  page.on("pageerror", (e) => errors.push(e.stack || e.message));
  await page.route("**/*", async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    const state = { providers: [], presets: [], excluded: [], gateway: { running: true, window: true } };
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs={lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json(state);
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/import/gem2251") return json({ provider: linked });
    if (url.pathname === "/api/provider/save") { saves.push(req.postDataJSON()); return json(state); }
    if (url.pathname === "/api/gateway/trace") return new Promise(() => {});
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  });
  t.after(async () => {
    if (process.env.ARTIFACT_DIR) {
      await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
      await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-import-link-gemini.png`) });
    }
    await browser.close();
    assert.deepEqual(errors, []);
  });
  await page.goto("http://uni-switch.test/?view=providers&import=gem2251");
  const ed = page.locator(".editor.import");
  await ed.waitFor();
  return { page, ed, saves };
}

for (const engine of engines) {
  for (const lang of langs) {
    for (const width of [1100, 420]) {
      test(`${engine} ${lang} ${width}px: a link with only gemini= names its host and adds it`, async (t) => {
        const { page, ed, saves } = await fixture(t, engine, lang, width);
        const want = await page.evaluate(() => t("Added from a link. Your prompts and this key will go to {hosts}; add it only if you trust the site that sent you here.", { hosts: "gem-2251.example" }));
        assert.equal((await ed.locator(".warnbox").first().textContent()).trim(), want);
        assert.deepEqual(await ed.locator(".eps .ep").evaluateAll((es) => es.map((e) => [e.querySelector(".pl").textContent, e.querySelector("code").textContent])), [["Gemini", "https://gem-2251.example/v1beta"]], "the Gemini endpoint shows");
        const fits = await ed.evaluate((e) => e.getBoundingClientRect().right <= innerWidth && e.scrollWidth <= e.clientWidth + 1);
        assert(fits, "the dialog fits the window");
        await ed.locator('input[type="password"]').fill("sk-fixture-2251");
        await ed.locator(".bar button.primary").click();
        await page.waitForFunction(() => !importing);
        assert.equal(saves.length, 1);
        assert.equal(saves[0].gemini, "https://gem-2251.example/v1beta");
        assert.equal(saves[0].key, "sk-fixture-2251");
      });
    }
  }
}

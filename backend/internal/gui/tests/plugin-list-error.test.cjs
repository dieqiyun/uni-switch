// Run with Node's test runner and Playwright on the module path; see README.md.
// A plugin's account whose model list failed has only the plugin's
// defaults (Cursor's Auto alone, gnayiab on X running uni-switch in WSL): its
// editor says why under the models, in the user's language, with no left
// border; an account whose list came in says nothing.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const said = "Cursor isn't signed in; sign in from uni-switch's Providers page or run `cursor-agent login`";
const cursor = (listError) => ({
  id: "cursor", name: "Cursor", icon: "cursor", chat: "http://127.0.0.1/plugin/cursor/v1", responses: "", anthropic: "", catalog: "",
  models: [{ id: "auto", name: "Auto", on: true }], agents: [], fallback: [], headers: {}, chosen: [],
  key: { set: false, masked: "" }, keyList: [], ready: true, exposed: 1, fetched: new Date().toISOString(),
  account: { agent: "cursor", agentName: "Cursor", agentIcon: "cursor", user: "me@example.com", builtin: "cursor" },
  move: { package: "the retired opencode-cursor-auth", state: "plugin" },
  ...(listError ? { listError } : {}),
});

function server(lang, listError) {
  const providers = { providers: [cursor(listError)], presets: [], excluded: [], gateway: { running: true, window: true } };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const [lang, want] of [["en", "Cursor couldn't list its models: " + said], ["zh", "Cursor 没能获取模型列表：" + said]]) {
    test(`${engine} ${lang}: a plugin's failed list says why`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const page = await (await browser.newContext({ viewport: { width: 900, height: 760 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-plugin-list-error.png`) });
        }
        await browser.close();
      });

      await page.route("**/*", server(lang, said));
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator(".row.provider", { hasText: "Cursor" }).click();
      const hint = page.locator(".editor .list-error");
      await hint.waitFor();
      assert.equal(await hint.textContent(), want);
      const look = await hint.evaluate((e) => { const s = getComputedStyle(e); return { left: s.borderLeftWidth, color: s.color, ground: getComputedStyle(document.body).color }; });
      assert.equal(look.left, "0px", "no left border");
      assert.notEqual(look.color, look.ground, "said as a warning");
      assert.equal(await page.locator(".editor").getByText(/vendor list ·|供应商列表 ·/).count(), 0, "its defaults aren't called the vendor's list");
      assert.deepEqual(errors, []);
    });
  }

  test(`${engine}: a plugin whose list came in says nothing`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const page = await (await browser.newContext({ viewport: { width: 900, height: 760 }, reducedMotion: "reduce" })).newPage();
    page.setDefaultTimeout(5000);
    page.setDefaultNavigationTimeout(60000);
    t.after(() => browser.close());
    await page.route("**/*", server("en", ""));
    await page.goto("http://uni-switch.test/?view=providers");
    await page.locator(".row.provider", { hasText: "Cursor" }).click();
    await page.locator(".editor").waitFor();
    await page.waitForTimeout(200);
    assert.equal(await page.locator(".editor .list-error").count(), 0);
  });
}

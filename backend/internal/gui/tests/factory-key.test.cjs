// Run with Node's test runner and Playwright on the module path; see README.md.
// A Factory account comes in by its API key (fk-…), as droid takes
// FACTORY_API_KEY (aohun on #506): the accounts list offers it, the box says where keys are made and that each is
// checked with Factory, what is pasted is posted as it is, and each key's
// outcome is listed; in English and Chinese, nothing new left untranslated.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const pasted = "FACTORY_API_KEY=fk-abc_0123456789\nfk-bad_0123456789";
const account = (logins) => ({
  id: "factory", name: "Factory", icon: "factory", chat: "", responses: "", anthropic: "", catalog: "",
  models: [{ id: "glm-5.3-flash", name: "GLM-5.3 Flash", on: true }], agents: [], fallback: [], headers: {}, keyList: [],
  account: { agent: "factory", agentName: "Droid", user: "me@example.com", logins },
});
const before = [{ user: "me@example.com", active: true, on: true }];
const after = [...before, { user: "key@example.com", on: true }];

function serve(lang, posted) {
  let providers = { providers: [account(before)], presets: [], excluded: [], gateway: { running: true, window: true } };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/signin") throw new Error("a sign-in was started");
    if (url.pathname === "/api/signin/import") {
      posted.push(route.request().postDataJSON());
      await new Promise((r) => setTimeout(r, 300));
      providers = { ...providers, providers: [account(after)] };
      return json({ providers, results: [
        { user: "key@example.com", status: "added" },
        { user: "fk-bad_…6789", status: "failed", error: "Factory didn't take this key: Factory: Invalid API key" },
      ] });
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a Factory account comes in by its API key`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch());
      const context = await browser.newContext({ viewport: { width: 900, height: 760 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const posted = [];
      await page.route("**/*", serve(lang, posted));
      t.after(() => browser.close());
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator(".row.provider", { hasText: "Factory" }).click();
      const adds = page.locator(".editor .accts .acc.add");
      await adds.nth(1).waitFor();
      const T = async (s, v) => page.evaluate(([s, v]) => t(s, v), [s, v || {}]);
      if (lang === "zh") {
        for (const s of ["Add accounts by their Factory API keys (fk-…), as Droid takes FACTORY_API_KEY", "Add Factory accounts by API key",
          "Paste one or more Factory API keys (fk-…, from app.factory.ai/settings/api-keys), one a line, or choose a file with them. Each key is checked with Factory before it is added.",
          "Checking the keys with Factory…", "Each key is asked whose account it is, as Droid does with FACTORY_API_KEY.",
          "Use an API key instead…", "Add an account by API key…"]) {
          assert.notEqual(await T(s), s, "no Chinese for: " + s);
        }
      }
      const say = {
        row: await T("Add an account by API key…"),
        title: await T("Add Factory accounts by API key"),
        checking: await T("Checking the keys with Factory…"),
      };
      if (lang === "en") assert.equal(say.row, "Add an account by API key…");

      // the accounts list offers it
      assert.equal((await adds.nth(1).locator(".n").textContent()).trim(), say.row);
      await adds.nth(1).click();
      await page.locator(".signing.import textarea").fill(pasted);
      await page.locator(".signing.import button.primary").click();
      await page.locator(".signing.import .spinner").waitFor();
      assert.equal(await page.locator(".signing.import .tt .n").textContent(), say.checking);
      await page.locator(".signing.import .results").waitFor();
      assert.deepEqual(posted, [{ agent: "factory", files: [pasted] }]);
      assert.match(await page.locator(".signing.import .res.failed .why").textContent(), /didn't take this key/);
      await page.locator(".editor .accts .acc .n", { hasText: "key@example.com" }).waitFor();
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), "the page scrolls sideways");
      assert.deepEqual(errors, []);
    });
  }
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// A plugin account is kept by its accountId, which for Huawei Cloud's
// CodeArts is an IAM id ("iam::<domain>:user:<name>"), while the plugin's
// usage names it by a masked phone (#4416). The editor's account row names
// it, in order, by the user's own name, the plugin's name for it, its id;
// the id stays in the tooltip, and an account whose usage was never read
// is its id, never blank. The proxy picker's lines name it the same way.
// It fits at 360px. In every language.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const IAM = "iam::0a1b2c:user:dev", PHONE = "180****2361", OWN = "iam::0a1b2c:user:ops", NEW = "iam::0a1b2c:user:new";
const fresh = () => ({
  id: "codearts", name: "CodeArts", icon: "", chat: "", responses: "", anthropic: "", catalog: "",
  models: [{ id: "glm-5", name: "", on: true }],
  agents: [], fallback: [], headers: {}, keyList: [], proxy: "",
  account: {
    agent: "codearts", agentName: "CodeArts", user: IAM, plan: "",
    logins: [
      { user: IAM, active: true, on: true },
      { user: OWN, on: true },
      { user: NEW, on: true },
    ],
  },
  accountNames: { [OWN]: "Ops" },
  accountUsers: { [IAM]: PHONE, [OWN]: "139****0000" },
});

function serve(lang) {
  const settings = { theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd" };
  const providers = { providers: [fresh()], presets: [], excluded: [], gateway: { running: true, window: true },
    plugins: [{ id: "codearts", pid: "codearts", name: "CodeArts", icon: "", spec: "@example/opencode-codearts-auth" }] };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/login/usage") return json({});
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const LANGS = ["en", "zh", "zh-TW", "ja", "de"];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of LANGS) {
    test(`${engine} ${lang}: a plugin account is named by its usage's user, its own name first, its id last`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      for (const width of [900, 360]) {
        const page = await (await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        await page.goto("http://uni-switch.test/?view=providers");
        await page.locator(".row.provider", { hasText: "CodeArts" }).first().click();
        const who = (id) => page.locator(`.editor .accts .acc[data-account-id="${id}"] .acct-who`);
        await who(IAM).waitFor();
        const tr = (k, v) => page.evaluate(([k, v]) => t(k, v), [k, v]);

        // the plugin's name for it, the id in its tooltip
        assert.equal(await who(IAM).locator(".n.rename").textContent(), PHONE);
        assert.equal(await who(IAM).locator(".n.rename").getAttribute("title"), await tr("{user} · click to rename", { user: IAM }));
        // the user's own name wins, the plugin's beside it
        assert.equal(await who(OWN).locator(".n.rename").textContent(), "Ops");
        assert.equal(await who(OWN).locator(".acct-user").textContent(), "139****0000");
        // nothing read yet: its id, never blank
        assert.equal(await who(NEW).locator(".n.rename").textContent(), NEW);
        assert.equal(await who(NEW).locator(".n.rename").getAttribute("title"), await tr("Name this account"));
        // the page's search finds it by the plugin's name too
        assert.ok(await page.evaluate((ph) => providerText(providers.providers[0], document.createElement("div")).includes(ph), PHONE));
        // the proxy picker's lines name it the same way
        const names = await page.evaluate(() => [...accountProxyPicker(providers.providers[0].account, providers.providers[0]).querySelectorAll("*")].map((e) => e.textContent));
        assert.ok(names.some((s) => s.includes(PHONE)), names.join(" | "));
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth), 0, `no sideways scroll at ${width}px`);
        assert.deepEqual(errors, []);
        await page.context().close();
      }
    });
  }
}

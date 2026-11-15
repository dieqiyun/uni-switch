// Run with Node's test runner and Playwright on the module path; see README.md.
// A custom provider's Balance field left empty shows, as its placeholder,
// the field uni-switch reads from a Balance URL whose reply it knows (#881):
// new-api's /api/usage/token and /api/user/self, a sub2api panel's
// /api/v1/user/profile and its /v1/usage for a key, OpenAI's old
// credit_grants; any other URL keeps
// the example. The placeholder follows the URL as it is typed.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const relay = {
  id: "relay", name: "Relay", icon: "generic", chat: "https://relay.example.com/v1", responses: "", anthropic: "", catalog: "",
  models: [{ id: "model-a", name: "Model A", on: true }], agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-…one" }, keyList: [],
  balanceURL: "https://relay.example.com/api/usage/token", balancePath: "",
  balanceToken: { takes: true, set: false },
};
const providers = { providers: [relay], presets: [], excluded: [], gateway: { running: true, window: true } };
const state = { agents: [], profiles: [], settings: { lang: "en", theme: "light" } };

function server() {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: 'window.bootPrefs = {lang:"en",theme:"light",web:true};' });
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
  test(engine + ": an empty Balance field shows the one a known Balance URL is read with", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const context = await browser.newContext({ viewport: { width: 900, height: 760 }, reducedMotion: "reduce" });
    const page = await context.newPage();
    page.setDefaultTimeout(5000);
    page.setDefaultNavigationTimeout(60000);
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.route("**/*", server());
    t.after(() => browser.close());

    await page.goto("http://uni-switch.test/?view=providers");
    await page.locator(".row.provider", { hasText: "Relay" }).click();
    const field = page.locator(".editor .bal-path");
    await field.waitFor({ state: "attached" });
    assert.equal(await field.inputValue(), "");
    assert.equal(await field.getAttribute("placeholder"), "$data.total_available / 500000");

    const url = page.locator(".editor .bal-url");
    for (const [typed, want] of [
      ["https://relay.example.com/api/user/self/", "$data.quota / 500000"],
      ["https://panel.example.com/api/v1/user/profile", "$data.balance"],
      ["https://panel.example.com/v1/usage", "$remaining"],
      ["https://relay.example.com/v1/dashboard/billing/credit_grants", "$total_available"],
      ["https://relay.example.com/whatever", "data.balance"],
      ["not a url", "data.balance"],
    ]) {
      await url.evaluate((e, v) => { e.value = v; e.dispatchEvent(new Event("input", { bubbles: true })); }, typed);
      assert.equal(await field.getAttribute("placeholder"), want, typed);
    }
    assert.deepEqual(errors, []);
  });
}

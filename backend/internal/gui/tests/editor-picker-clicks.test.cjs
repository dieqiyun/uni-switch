// Run with Node's test runner and Playwright on the module path; see README.md.
// #2188 (Deja-vuuu): in a provider's editor, the Agents row's chip opens
// the agent's model picker over the dialog, and nothing in it could be
// clicked: the rail's providers and the models under it let the click
// through to the editor's fields beneath. The open editor makes the page
// behind it inert, and the picker, a sibling of the dialog under <body>,
// was made inert with it. It now takes clicks: a rail item filters the
// list, and a model picked from it is set. In five languages, chromium and
// webkit, at 1100 and 420 wide. No backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const options = [
  { value: "glm-5-2", label: "glm-5-2", note: "routing group", group: "Routing groups" },
  { value: "seed-evolving", label: "Seed Evolving", note: "routing group", group: "Routing groups" },
  { value: "volc/doubao-seed", label: "doubao-seed", note: "Volcengine · via uni-switch", icon: "volcengine-color", group: "Volcengine", ref: "volc/doubao-seed" },
  { value: "zhipu/glm-5-2", label: "GLM-5.2", note: "Zhipu · via uni-switch", icon: "zhipu-color", group: "Zhipu", ref: "zhipu/glm-5-2" },
];
const state = (value) => ({
  agents: [{ id: "claude", name: "Claude Code", icon: "claudecode-color", path: "/test/settings.json", wired: true,
    fields: [{ key: "model", label: "model", value, options }] }],
  profiles: [],
});
// the provider in the report: Claude Code is on a routing group with one of
// its models among the members
const volc = {
  id: "volc", name: "doubao-seed", icon: "volcengine-color", chat: "https://ark.example.com/api/v3", responses: "", anthropic: "", catalog: "", host: "ark.example.com",
  models: [{ id: "doubao-seed", name: "doubao-seed", on: true }], fallback: [], headers: {},
  agents: [{ id: "claude", name: "Claude Code", icon: "claudecode-color", current: true, group: "glm-5-2", model: "doubao-seed" }],
  key: { set: true, masked: "sk-…one" }, keyList: [], balanceToken: { takes: false, set: false }, proxy: "", ready: true,
};

function serve(lang, sets) {
  let cur = state("glm-5-2");
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ ...cur, settings: { lang, theme: "light" } });
    if (url.pathname === "/api/set") {
      const body = req.postDataJSON();
      sets.push(body);
      cur = state(body.value);
      return json({ ...cur, settings: { lang, theme: "light" } });
    }
    if (url.pathname === "/api/providers") return json({ providers: [volc], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": the model picker opened from a provider's editor takes clicks", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const width of [1100, 420]) {
      for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
        await t.test(`${lang} at ${width}`, async () => {
          const sets = [];
          const context = await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" });
          const page = await context.newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", serve(lang, sets));
          await page.goto("http://uni-switch.test/?view=providers");
          await page.locator('.row.provider[data-id="volc"]').click();
          const chip = page.locator(".modal .achip");
          await chip.waitFor();
          await chip.click();
          const pop = page.locator("#pop");
          await pop.waitFor();
          assert.equal(await pop.evaluate((e) => !!e.closest("[inert]")), false, "the picker over the editor is not inert");

          // a provider in the rail narrows the list to its models
          await page.locator('#pickerRail .rail-item[data-group="Zhipu"]').click();
          await page.waitForFunction(() => document.querySelector('#pickerRail .rail-item[data-group="Zhipu"]')?.classList.contains("on"));
          const row = page.locator("#list li").filter({ hasText: "GLM-5.2" });
          await row.waitFor();
          assert.equal(await page.locator("#list li").filter({ hasText: "Seed Evolving" }).count(), 0, "the rail's pick filtered the list");

          // and a model in it is picked, not the field under it clicked
          await row.click();
          await page.waitForFunction(() => document.getElementById("pop").hidden);
          assert.deepEqual(sets, [{ agent: "claude", field: "model", value: "zhipu/glm-5-2" }]);
          assert.equal(await page.locator(".modal:not([hidden])").count(), 1, "the editor stays open");
          await context.close();
        });
      }
    }
    assert.deepEqual(errors, []);
  });
}

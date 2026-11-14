// Run with Node's test runner and Playwright on the module path; see README.md.
// Claude Code wired to uni-switch while a provider switch uni-switch can't take out
// sends its requests elsewhere (jw1dev on X: CLAUDE_CODE_USE_BEDROCK=1 kept
// in the env, Bedrock refused uni-switch's model, uni-switch said nothing). Check
// says which variable and where, and the row says it in the window's
// language: the line under the name and the Reconnect pill carry the advice,
// with its file, variable and cloud as given. Both notices (a file, the
// shell), English, Chinese, Traditional Chinese, Japanese and German, a wide
// window and a narrow one, Chromium and WebKit. No backend: the API is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const FILE = "{file} sets {var}, so Claude Code sends its requests to {cloud}, not to uni-switch: take it out there to go through uni-switch.";
const SHELL = "{var} is set in your shell's environment, so Claude Code started there sends its requests to {cloud}, not to uni-switch: take it out of your shell's profile to go through uni-switch.";
const cases = [
  { name: "managed settings", en: FILE, vars: { file: "/Library/Application Support/ClaudeCode/managed-settings.json", var: "CLAUDE_CODE_USE_BEDROCK", cloud: "Amazon Bedrock" } },
  { name: "the shell", en: SHELL, vars: { var: "CLAUDE_CODE_USE_VERTEX", cloud: "Google Vertex AI" } },
];
const fill = (en, vars) => en.replace(/\{(\w+)\}/g, (_, k) => vars[k]);
const claude = (detail) => ({
  id: "claude", name: "Claude Code", icon: "claude", path: "/c", wired: true,
  fields: [{ key: "model", label: "model", value: "fake/m1", options: [{ value: "fake/m1", label: "m1", ref: "fake/m1" }] }],
  drift: { kind: "unwired", field: "model", now: "fake/m1", want: "fake/m1", detail },
});

function serve(lang, detail) {
  return async (route) => {
    const url = new URL(route.request().url());
    const st = { agents: [claude(detail)], profiles: [], settings: { lang, theme: "light" } };
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: st });
    if (url.pathname === "/api/drift") return route.fulfill({ json: { claude: st.agents[0].drift } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const row = `.row.agent[data-id="claude"]`;

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a Claude provider switch that bypasses uni-switch is said in the window's language", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      for (const width of [1100, 420]) {
        for (const c of cases) {
          await t.test(`${lang} at ${width}px: ${c.name}`, async () => {
            const page = await (await browser.newContext({ viewport: { width, height: 900 } })).newPage();
            page.setDefaultTimeout(5000);
            page.setDefaultNavigationTimeout(60000);
            page.on("pageerror", (e) => errors.push(e.message));
            const given = fill(c.en, c.vars);
            await page.route("**/*", serve(lang, given));
            await page.goto("http://uni-switch.test/");
            await page.locator(`${row} .ag-fix`).waitFor();
            const want = await page.evaluate(([en, vars]) => t(en, vars), [c.en, c.vars]);
            if (lang !== "en") assert.notEqual(want, given, "untranslated");
            // the variable, the file and the cloud stay as given, in every language
            for (const v of Object.values(c.vars)) assert.ok(want.includes(v), want);

            const title = await page.locator(`${row} .ag-st-t`).getAttribute("title");
            assert.equal(title, want);
            const pill = await page.locator(`${row} .ag-fix`).getAttribute("title");
            assert.equal(pill.split("\n").slice(1).join("\n"), want);
            if (lang !== "en") {
              assert.ok(!title.includes("sends its requests"), title);
              assert.ok(!pill.includes("sends its requests"), pill);
            }
            assert.ok(await page.evaluate(() => document.scrollingElement.scrollWidth <= innerWidth), "scrolls sideways");
            await page.context().close();
          });
        }
      }
    }
    assert.deepEqual(errors, []);
  });
}

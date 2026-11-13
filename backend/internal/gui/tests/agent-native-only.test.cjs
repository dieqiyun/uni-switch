// Run with Node's test runner and Playwright on the module path; see README.md.
// @G_Z_Y on X installed cursor-agent and didn't find it on the Agents page:
// its row was "Cursor", with no version, and "Only Cursor's own models"
// beside "Can't connect". The row is Cursor CLI, with what cursor-agent
// --version says, and its line says why uni-switch can't connect it, whole,
// never cut. So for every agent with no way onto uni-switch. In the window,
// wide and narrow, in every language. No backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const field = (value) => [{ key: "model", label: "model", value, options: [{ value }] }];
const AGENTS = [
  { id: "cursor", name: "Cursor CLI", icon: "cursor", path: "~/.cursor/cli-config.json", fields: field("auto") },
  { id: "copilot", name: "GitHub Copilot CLI", path: "~/.copilot/config.json", fields: field("gpt-5") },
  { id: "goose", name: "Goose", path: "~/.config/goose/config.yaml", fields: field("gpt-5") },
  { id: "devin", name: "Devin", path: "~/.devin/config.json", fields: field("swe-1") },
];
const CLIS = { cursor: { version: "2026.10.01-e373342", path: "/Users/me/.local/bin/cursor-agent" } };
const WHY = {
  cursor: "Uses only Cursor's own models, so uni-switch can't connect it",
  copilot: "Uses only GitHub Copilot's own models, so uni-switch can't connect it",
  goose: "Uses only Goose's own providers, so uni-switch can't connect it",
  devin: "Uses only Devin's own models, so uni-switch can't connect it",
};

function server(lang) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: AGENTS, profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/agents/cli" && req.method() === "GET") return route.fulfill({ json: { agents: CLIS, pending: false } });
    if (url.pathname === "/api/agents/install") return route.fulfill({ json: [] });
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [{ id: "relay", name: "Relay", models: ["glm-4.6"] }], gateway: { running: true } } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const row = (id) => `.row.agent[data-id="${id}"]`;
const said = (page, lang, key) => page.evaluate(([lang, key]) => (lang === "en" ? key : I18N[lang]?.[key]) || null, [lang, key]);
const noSideScroll = (page) => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": an agent uni-switch can't connect says why, whole, and Cursor CLI is called so", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      for (const width of [1100, 420]) {
        await t.test(`${lang} ${width}`, async () => {
          const page = await (await browser.newContext({ viewport: { width, height: 760 } })).newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", server(lang));
          await page.goto("http://uni-switch.test/");
          await page.locator(`${row("cursor")} .ag-ver`).waitFor();
          assert.equal(await page.locator(`${row("cursor")} .name`).first().textContent(), "Cursor CLI");
          assert.equal(await page.locator(`${row("cursor")} .ag-ver`).textContent(), "2026.10.01-e373342");
          for (const id of Object.keys(WHY)) {
            const want = await said(page, lang, WHY[id]);
            assert(want, `${id}'s reason has no ${lang}`);
            // it says that uni-switch is what can't connect it
            assert(want.includes("uni-switch"), `${lang} ${id}: ${want}`);
            const words = page.locator(`${row(id)} .ag-st-t`);
            assert(await words.isVisible(), `${id}'s reason isn't shown`);
            assert.equal(await words.textContent(), want, id);
            assert(await words.evaluate((b) => b.scrollWidth <= b.clientWidth + 1 && b.scrollHeight <= b.clientHeight + 1), `${lang} ${width}: ${id}'s reason is cut`);
            // inside its row
            const inside = await page.locator(row(id)).evaluate((r, sel) => {
              const a = r.getBoundingClientRect(), b = r.querySelector(sel).getBoundingClientRect();
              return b.left >= a.left - 0.5 && b.right <= a.right + 0.5 && b.bottom <= a.bottom + 0.5;
            }, ".ag-st-t");
            assert(inside, `${id}'s reason is off its row`);
            assert.equal(await page.locator(`${row(id)} .ag-conn`).count(), 0, id + " has a switch");
          }
          assert(await noSideScroll(page), "the page scrolls sideways");
          await page.screenshot({ path: path.join(process.env.SHOTS || "/tmp", `native-only-${engine}-${lang}-${width}.png`), fullPage: true }).catch(() => {});
          await page.context().close();
        });
      }
    }
    assert.deepEqual(errors, []);
  });
}

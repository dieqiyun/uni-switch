// Run with Node's test runner and Playwright on the module path; see README.md.
// Claude Code's auto mode classifier can run on a model of its own
// (CLAUDE_CODE_AUTO_MODE_MODEL, Claude Code 2.1.302; Linka on Discord).
// Connected, the opened row's Tiers has an Auto mode line after Subagents:
// unset it says default, a click opens the app's model picker with Default
// first, noted as Claude Code's own pick, and a model picked is posted;
// Default posts it empty. It is no square of the row's own. Before Claude
// Code goes through uni-switch it has no options, so neither line nor square
// shows. No click scrolls the page. In English and Chinese, at a normal
// and a narrow width. No backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const models = [{ value: "uni-switch/v/glm", label: "GLM", ref: "v/glm" }, { value: "uni-switch/v/flash", label: "Flash", ref: "v/flash" }];
const tiers = ["opus", "sonnet", "haiku", "fable"];
const claude = (id, routed) => ({
  id, name: "Claude Code", path: "/test/settings.json", icon: "claudecode-color", wired: routed,
  fields: [
    { key: "model", label: "model", value: routed ? "uni-switch/v/glm" : "opus", options: routed ? models : [{ value: "opus" }] },
    ...tiers.map((tier) => ({ key: tier, label: tier, value: "", options: models })),
    { key: "subagent", label: "subagents", value: "", options: routed ? models : [] },
    { key: "auto_mode", label: "auto mode", value: "", options: routed ? models : [] },
  ],
});
const fresh = () => ({ agents: [claude("claude", true), claude("cc-own", false)], profiles: [] });

function server(lang, sets) {
  let cur = fresh();
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { ...cur, settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/set") {
      const body = req.postDataJSON();
      sets.push(body);
      cur = JSON.parse(JSON.stringify(cur));
      cur.agents.find((a) => a.id === body.agent).fields.find((f) => f.key === body.field).value = body.value;
      return route.fulfill({ json: { ...cur, settings: { lang, theme: "light" } } });
    }
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/agents/cli") return route.fulfill({ json: { agents: {}, pending: false } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// the words as i18n.js has them
const W = {
  en: { line: "Auto mode", hint: "the model that checks each action in auto mode", def: "Default", note: "the model Claude Code picks for its auto mode classifier", unset: "auto mode: agent default", set: "auto mode: uni-switch/v/flash" },
  zh: { line: "自动模式", hint: "自动模式下检查每个操作的模型", def: "默认", note: "Claude Code 为自动模式分类器挑选的模型", unset: "自动模式：Agent 默认值", set: "自动模式：uni-switch/v/flash" },
};

const cc = '.row.agent[data-id="claude"]';

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    for (const width of [1100, 560]) {
      test(`${engine} ${lang} ${width}px: Claude Code's auto mode model is picked in its Tiers`, async (t) => {
        const w = W[lang];
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const page = await browser.newPage({ viewport: { width, height: 800 } });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], sets = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, sets));
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-claude-auto-mode.png`) });
          }
          await browser.close();
        });
        await page.goto("http://uni-switch.test/");
        await page.locator(cc).waitFor();
        // not through uni-switch: nothing to pick, so nothing shows
        assert.equal(await page.locator('.row.agent[data-id="cc-own"] [data-key="auto_mode"]').count(), 0);
        await page.locator(`${cc} .ag-link`).click();
        await page.locator(`${cc} .ag-exp`).waitFor();

        // a line of the opened row's Tiers, after Subagents; no square
        assert.equal(await page.locator(`${cc} .field.extra[data-key="auto_mode"]`).count(), 0);
        const pick = page.locator(`${cc} .ag-exp .field.ag-pick[data-key="auto_mode"]`);
        assert.equal(await pick.count(), 1);
        const keys = await page.locator(`${cc} .ag-exp .field.ag-pick`).evaluateAll((es) => es.map((e) => e.dataset.key));
        assert.deepEqual(keys.slice(-2), ["subagent", "auto_mode"]);
        const line = pick.locator("xpath=..");
        assert.equal(await line.locator(".ag-tier").textContent(), w.line);
        assert.equal(await line.locator(".ag-hint").textContent(), w.hint);
        assert.equal(await pick.getAttribute("title"), w.unset);
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "the Agents view overflows");

        // the app's model picker, Default first, noted as Claude Code's pick
        const y = await page.evaluate(() => scrollY);
        await pick.click();
        await page.locator("#pop:not([hidden]) #list li").first().waitFor();
        assert.equal(await page.evaluate(() => scrollY), y, "the click scrolled the page");
        assert.equal(await page.locator("#pop").evaluate((e) => e.classList.contains("model-picker")), true);
        const first = await page.locator("#pop #list li").first().innerText();
        assert.ok(first.includes(w.def) && first.includes(w.note), first);
        await page.locator("#pop #list li", { hasText: "Flash" }).first().click();
        await page.waitForFunction(() => document.querySelector('.row.agent[data-id="claude"] .ag-exp .field[data-key="auto_mode"] .v:not(.empty)'));
        assert.deepEqual(sets, [{ agent: "claude", field: "auto_mode", value: "uni-switch/v/flash" }]);
        assert.equal(await page.locator(`${cc} .ag-exp .field.ag-pick[data-key="auto_mode"]`).getAttribute("title"), w.set);

        // Default takes it out again
        await page.locator(`${cc} .ag-exp .field.ag-pick[data-key="auto_mode"]`).click();
        await page.locator("#pop:not([hidden]) #list li").first().waitFor();
        await page.locator("#pop #list li").first().click();
        await page.waitForFunction(() => document.querySelector('.row.agent[data-id="claude"] .ag-exp .field[data-key="auto_mode"] .v.empty'));
        assert.deepEqual(sets.at(-1), { agent: "claude", field: "auto_mode", value: "" });
        assert.equal(await page.evaluate(() => scrollY), y, "a pick scrolled the page");
        assert.deepEqual(errors, []);
      });
    }
  }
}

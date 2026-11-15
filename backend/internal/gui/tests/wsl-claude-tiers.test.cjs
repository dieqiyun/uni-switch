// Run with Node's test runner and Playwright on the module path; see README.md.
// Claude Code in a WSL distro (claude@wsl:Ubuntu) is the same agent as on
// Windows, with the same fields from the backend, yet its card had no
// Tiers, Subagents or Auto mode rows and no "Claude models" line: the page
// keyed them on the id "claude" alone (cybercaius on Discord). Opened, the
// distro's card now has the same rows as Windows', its tier squares aren't
// shown loose in the row, a pick there goes to the distro's agent, and no
// click scrolls the page. In every language, wide and narrow. No backend:
// the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const models = [{ value: "uni-switch/v/glm", label: "GLM", ref: "v/glm" }, { value: "uni-switch/v/flash", label: "Flash", ref: "v/flash" }];
const levels = ["low", "medium", "high"].map((value) => ({ value }));
const tiers = ["opus", "sonnet", "haiku", "fable"];
const claude = (id, wsl) => ({
  id, name: "Claude Code", path: wsl ? "\\\\wsl.localhost\\Ubuntu\\home\\me\\.claude\\settings.json" : "/test/settings.json", icon: "claudecode-color", wired: true,
  ...(wsl ? { wsl } : {}),
  fields: [
    { key: "model", label: "model", value: "uni-switch/v/glm", options: models },
    { key: "effort", label: "effort", value: "high", options: levels },
    ...tiers.map((tier) => ({ key: tier, label: tier, value: "", options: models })),
    ...tiers.map((tier) => ({ key: tier + "_effort", label: tier + " effort", value: "", options: levels })),
    { key: "subagent", label: "subagents", value: "", options: models },
    { key: "subagent_effort", label: "subagent effort", value: "", options: levels },
    { key: "auto_mode", label: "auto mode", value: "", options: models },
  ],
});
const WSL = "claude@wsl:Ubuntu";

function server(lang, sets) {
  let cur = { agents: [claude("claude"), claude(WSL, "Ubuntu")], profiles: [] };
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

const row = (id) => `.row.agent[data-id="${id}"]`;
// an opened card: its rows' names, the fields in them, and the squares left
// loose in the row
async function card(page, id) {
  await page.locator(`${row(id)} .ag-link`).click();
  await page.locator(`${row(id)} .ag-exp`).waitFor();
  return page.locator(row(id)).evaluate((r) => ({
    rows: [...r.querySelectorAll(".ag-exp .ag-k")].map((k) => k.textContent),
    fields: [...r.querySelectorAll(".ag-exp .field[data-key]")].map((f) => f.dataset.key),
    loose: [...r.querySelectorAll(".field.extra[data-key]")].filter((f) => !f.closest(".ag-exp")).map((f) => f.dataset.key),
  }));
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [1100, 560]) {
      test(`${engine} ${lang} ${width}: Claude Code in WSL has the tier rows Windows' has`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const page = await browser.newPage({ viewport: { width, height: 900 } });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], sets = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, sets));
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-wsl-claude-tiers.png`), fullPage: true });
          }
          await browser.close();
        });
        await page.goto("http://uni-switch.test/");
        await page.locator(row(WSL)).waitFor();

        const win = await card(page, "claude");
        await page.locator(`${row("claude")} .ag-link`).click(); // closed again
        const y = await page.evaluate(() => scrollY);
        const wsl = await card(page, WSL);
        assert.ok(win.fields.includes("haiku_effort") && win.fields.includes("auto_mode"), `Windows' card: ${JSON.stringify(win)}`);
        assert.deepEqual(wsl.rows, win.rows, "the same rows, in this language");
        assert.deepEqual(wsl.fields, win.fields, "the same pickers");
        assert.deepEqual(wsl.loose, win.loose, "no tier square loose in the row");

        // a tier's effort picked on the distro's card goes to the distro's agent
        const haiku = page.locator(`${row(WSL)} .ag-exp .field.ag-eff[data-key="haiku_effort"]`);
        await haiku.click();
        await page.locator("#effortControl:not([hidden])").waitFor();
        await page.locator("#effortRange").evaluate((r) => {
          r.value = "1";
          r.dispatchEvent(new Event("input", { bubbles: true }));
          r.dispatchEvent(new Event("change", { bubbles: true }));
        });
        await page.waitForFunction(() => document.querySelector("#status")?.textContent);
        assert.deepEqual(sets, [{ agent: WSL, field: "haiku_effort", value: "low" }]);
        assert.ok(Math.abs((await page.evaluate(() => scrollY)) - y) < 1, "a click scrolled the page");
        // nothing wider than the window
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "the page scrolls sideways");
        assert.deepEqual(errors, []);
      });
    }
  }
}

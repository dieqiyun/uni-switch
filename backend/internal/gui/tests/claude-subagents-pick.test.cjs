// Run with Node's test runner and Playwright on the module path; see README.md.
// Claude Code's subagents left unset are Claude Code's pick, not "same as
// model" (KorenKrita on Discord): uni-switch then writes no
// CLAUDE_CODE_SUBAGENT_MODEL, and Claude Code takes the Agent call's model,
// then the subagent's own model:, then the session's (2.1.306). The
// Subagents line of the opened row's Tiers says so, its title says what a
// model set there is for, and the picker opens on "Claude Code's own pick"
// with the main model in its note; picked, then taken back, it posts the
// model and then "". Every language, Chromium and WebKit, wide and narrow.
// No backend: the API is faked here.
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
  en: { pick: "Claude Code's own pick", note: "GLM unless the lead or the subagent names another", unset: "subagents: Claude Code's own pick",
    says: "Claude Code's lead, or a subagent's own definition, may name a model or tier for it; a model set here is for the subagents that name none" },
  zh: { pick: "由 Claude Code 自己挑选", note: "GLM，主代理或子代理指定其他模型时除外", unset: "子代理：由 Claude Code 自己挑选",
    says: "Claude Code 的主代理或子代理自己的定义可以给它指定模型或档位；这里设置的模型只用于没有指定的子代理" },
  "zh-TW": { pick: "由 Claude Code 自己挑選", note: "GLM，主代理或子代理指定其他模型時除外", unset: "子代理：由 Claude Code 自己挑選",
    says: "Claude Code 的主代理或子代理自己的定義可以給它指定模型或檔位；這裡設定的模型只用於沒有指定的子代理" },
  ja: { pick: "Claude Code が自分で選ぶ", note: "GLM（リードかサブエージェントが別のモデルを指定した場合を除く）", unset: "サブエージェント：Claude Code が自分で選ぶ",
    says: "Claude Code のリードやサブエージェント自身の定義が、モデルや階層を指定できます。ここで設定したモデルは、指定のないサブエージェントに使われます" },
  de: { pick: "wählt Claude Code selbst", note: "GLM, außer der Haupt-Agent oder der Subagent nennt ein anderes", unset: "Subagenten: wählt Claude Code selbst",
    says: "Der Haupt-Agent von Claude Code oder die eigene Definition eines Subagenten kann ihm ein Modell oder eine Stufe geben; ein hier gesetztes Modell gilt für Subagenten, die keines nennen" },
};

const cc = '.row.agent[data-id="claude"]';
const sub = `${cc} .ag-exp .field.ag-pick[data-key="subagent"]`;

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(W)) {
    for (const width of [1100, 560]) {
      test(`${engine} ${lang} ${width}px: Claude Code's subagents unset are its own pick`, async (t) => {
        const w = W[lang];
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const page = await browser.newPage({ viewport: { width, height: 800 } });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], sets = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, sets));
        t.after(() => browser.close());
        await page.goto("http://uni-switch.test/");
        await page.locator(cc).waitFor();
        await page.locator(`${cc} .ag-link`).click();
        await page.locator(sub).waitFor();

        const pick = page.locator(sub);
        assert.equal(await pick.locator(".v").textContent(), w.pick);
        assert.equal(await pick.getAttribute("title"), w.unset + "\n" + w.says);
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "the Agents view overflows");

        const y = await page.evaluate(() => scrollY);
        await pick.click();
        await page.locator("#pop:not([hidden]) #list li").first().waitFor();
        assert.equal(await page.evaluate(() => scrollY), y, "the click scrolled the page");
        const first = await page.locator("#pop #list li").first().innerText();
        assert.ok(first.includes(w.pick) && first.includes(w.note), first);
        await page.locator("#pop #list li", { hasText: "Flash" }).first().click();
        await page.waitForFunction((s) => document.querySelector(s + " .v:not(.empty)"), sub);
        assert.deepEqual(sets, [{ agent: "claude", field: "subagent", value: "uni-switch/v/flash" }]);

        // its own pick takes the model out again
        await page.locator(sub).click();
        await page.locator("#pop:not([hidden]) #list li").first().waitFor();
        await page.locator("#pop #list li").first().click();
        await page.waitForFunction((s) => document.querySelector(s + " .v.empty"), sub);
        assert.deepEqual(sets.at(-1), { agent: "claude", field: "subagent", value: "" });
        assert.equal(await page.locator(sub).locator(".v").textContent(), w.pick);
        assert.deepEqual(errors, []);
      });
    }
  }
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// Every model taken out of an agent's list, then the agent switched off
// (塔哥 on Discord, Grok Build): connecting it again is refused until one
// is picked, and the Pick that picks one is in a connected agent's row
// only. The refused switch opens the agent's list itself, a model picked
// there lets it connect; so does the Get started card's chip.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function fixture(lang, guide) {
  const list = ["grok-5", "grok-5-mini", "grok-code"].map((m) =>
    ({ id: "xai/" + m, name: m, group: "xAI", icon: "xai", context: 1e6, hidden: true }));
  const posts = [];
  let wired = false, connects = 0;
  const count = () => ({ shown: list.filter((m) => !m.hidden).length, listed: list.length });
  const state = () => {
    const options = list.filter((m) => !m.hidden).map((m) => ({ value: m.id, label: m.name, ref: m.id }));
    return {
      agents: [{
        id: "grok", name: "Grok Build", path: "/test/.grok/user-settings.json", icon: "xai", wired,
        fields: [{ key: "model", label: "model", value: wired ? options[0]?.value || "" : "grok-4", options }],
        models: count(),
      }],
      profiles: [], settings: { lang, theme: "light", guideHidden: !guide },
      // models served, so the Get started card offers to connect an agent
      served: guide ? 3 : 0,
    };
  };
  async function serve(route) {
    const req = route.request();
    const url = new URL(req.url());
    const json = (data, status = 200) => route.fulfill({ status, json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state());
    if (url.pathname === "/api/agents/connect/grok") {
      connects++;
      // the gateway's answer to Connect's AllHiddenError (api.go)
      if (!list.some((m) => !m.hidden)) return json({ error: "None of uni-switch's models is left in Grok Build's model list", code: "all_hidden", agent: "Grok Build" }, 400);
      wired = true;
      return json({ ...state(), connected: { how: "first", field: "model", value: list.find((m) => !m.hidden).id } });
    }
    if (url.pathname === "/api/agent-models/grok") {
      if (req.method() === "POST") {
        const { hidden } = req.postDataJSON();
        posts.push(hidden);
        for (const m of list) m.hidden = hidden.includes(m.id);
        return json({ models: list, count: count() });
      }
      return json({ models: list });
    }
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  }
  return { serve, posts, connects: () => connects };
}

const W = {
  en: { pick: "None of uni-switch's models is left in Grok Build's model list: pick one, then connect Grok Build" },
  zh: { pick: "uni-switch 的模型都不在 Grok Build 的模型列表里：先选一个，再接入 Grok Build" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    for (const [width, guide] of [[1000, false], [560, false], [1000, true]]) {
      test(`${engine} ${lang} ${width}${guide ? " Get started" : ""}: a switched-off agent with every model hidden opens its list to pick one`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const page = await browser.newPage({ viewport: { width, height: 700 } });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const fx = fixture(lang, guide);
        await page.route("**/*", fx.serve);
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}${guide ? "-guide" : ""}-connect-all-hidden.png`) });
          }
          await browser.close();
        });
        await page.goto("http://uni-switch.test/");
        const row = page.locator('.row.agent[data-id="grok"]');
        const sw = row.locator(".ag-conn");
        const chip = page.locator('.guide-agent[data-id="grok"]');
        const connect = guide ? chip : sw;
        await connect.waitFor();
        assert.equal(await row.locator(".ag-exp").count(), 0, "a switched-off row has no Pick");

        await connect.click();
        const pop = page.locator(".am-pop:not(.leaving)");
        await pop.waitFor();
        assert.equal(fx.connects(), 1);
        assert.equal(await page.locator("#status").innerText(), W[lang].pick);
        assert.equal(await sw.getAttribute("aria-checked"), "false", "not connected");

        // one picked, the list closed: the switch connects
        await pop.locator(".am-mr").first().click();
        await page.waitForTimeout(150);
        assert.deepEqual(fx.posts.at(-1), ["xai/grok-5-mini", "xai/grok-code"]);
        await page.keyboard.press("Escape");
        await pop.waitFor({ state: "detached" });
        await page.waitForTimeout(300);
        await (guide ? chip : row.locator(".ag-conn")).click();
        await page.waitForFunction(() => document.querySelector('.row.agent[data-id="grok"] .ag-conn')?.getAttribute("aria-checked") === "true");
        assert.equal(fx.connects(), 2);
        assert.deepEqual(errors, []);
      });
    }
  }
}

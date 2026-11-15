// Run with Node's test runner and Playwright on the module path; see README.md.
// Show on a hidden agent nothing is set on (Fate on Discord: the Agents
// panel's hide and show "isn't intuitive"): it moves from Hidden to Not set
// up, still folded, and said "Idle shown" while it stayed out of sight. It
// now says it stays under Not set up until a model is picked for it; one
// with a model set comes up the list and still says shown. Chromium and
// WebKit, English and Chinese.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const models = ["model-a", "model-b"].map((m) => ({ value: m, label: m }));
const agent = (id, used) => ({
  id, name: id[0].toUpperCase() + id.slice(1), path: "/test/" + id,
  fields: [{ key: "model", label: "model", value: used ? "model-a" : "", options: models }],
});

function serve(lang) {
  const state = {
    agents: [agent("main", true), agent("idle", false), agent("busy", true)],
    profiles: [], settings: { lang, theme: "light", agentsHidden: ["idle", "busy"] },
  };
  return async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: state });
    if (url.pathname === "/api/agents/arrange") {
      const b = JSON.parse(route.request().postData() || "{}");
      return route.fulfill({ json: { agentOrder: b.order, agentsHidden: b.hidden, agentsShown: b.shown } });
    }
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { idle: "Idle is no longer hidden · it stays under Not set up until a model is picked for it", busy: "Busy shown" },
  zh: { idle: "Idle 已取消隐藏 · 选好模型前留在「未设置」", busy: "已显示 Busy" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": Show on a hidden agent says where it went", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(() => browser.close());
    for (const lang of ["en", "zh"]) {
      await t.test(lang, async () => {
        const page = await (await browser.newContext({ viewport: { width: 1240, height: 800 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        await page.goto("http://uni-switch.test/");
        await page.locator(".agent-more").click();
        const showOf = async (name) => {
          const btns = page.locator(".agent-fold .ag-show");
          const n = await btns.count();
          for (let i = 0; i < n; i++) {
            const txt = await btns.nth(i).evaluate((b) => b.closest("[class*='agent-row'], .agent, li, .row")?.textContent || b.parentElement.textContent);
            if (txt.includes(name)) return btns.nth(i);
          }
          throw new Error("no Show for " + name);
        };
        await (await showOf("Idle")).click();
        await page.waitForTimeout(150);
        assert.equal((await page.locator("#status").textContent()).trim(), words[lang].idle);
        // it is still folded, under Not set up
        const folded = await page.locator(".agent-fold").textContent();
        assert(folded.includes("Idle"), "Idle stays folded");
        await (await showOf("Busy")).click();
        await page.waitForTimeout(150);
        assert.equal((await page.locator("#status").textContent()).trim(), words[lang].busy);
        assert(!(await page.locator(".agent-fold").textContent()).includes("Busy"), "Busy comes up the list");
        await page.close();
      });
    }
    assert.deepEqual(errors, []);
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// An agent whose own model menu is the list picked here (ZCode, Claude
// Desktop, Cursor Private Inference…) says on its closed button what that
// list holds, as the list opened ticks it: every model on is "All n
// models", one or two picked are named, more are counted "Showing x / n
// models", none is "No models". It named the first two models and +N
// (routing groups come first), which read as a pick of one group and one
// model while every model was ticked (NG_shiyu on X: 「路由组，模型+数字」
// on one row, one 「路由」 on the row above it, and every model in the
// list). The window's button and the tray panel's row words alike, in
// every language, in Chromium and WebKit. No backend: faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function fixture(lang) {
  const list = [
    { id: "group/steady", name: "Never stuck", group: "Routing groups", icons: ["deepseek"] },
    { id: "deepseek/pro", name: "DeepSeek Pro", group: "DeepSeek", icon: "deepseek" },
    { id: "deepseek/flash", name: "DeepSeek Flash", group: "DeepSeek", icon: "deepseek" },
    { id: "kimi/k3", name: "Kimi K3", group: "Kimi", icon: "generic" },
  ];
  const count = () => {
    const on = list.filter((m) => !m.hidden);
    const by = [];
    for (const m of on) {
      let g = by.find((x) => x.name === m.group);
      if (!g) by.push(g = { name: m.group, icon: m.icon, n: 0 });
      g.n++;
    }
    return { shown: on.length, listed: list.length, by, names: on.slice(0, 3).map((m) => m.name) };
  };
  const state = () => ({
    agents: [{
      id: "zcode", name: "ZCode", path: "~/.zcode/v2/config.json", icon: "zcode", wired: true, menu: true,
      fields: [{ key: "provider", label: "provider", value: "uni-switch", options: [{ value: "uni-switch", label: "uni-switch", icon: "uni-switch", note: "every uni-switch model in ZCode's picker" }] }],
      models: count(),
    }],
    profiles: [], settings: { lang, theme: "light" },
  });
  async function serve(route) {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state());
    if (url.pathname === "/api/agent-models/zcode") {
      if (req.method() === "POST") {
        const { hidden } = req.postDataJSON();
        for (const m of list) m.hidden = hidden.includes(m.id);
        return json({ models: list, count: count() });
      }
      return json({ models: list });
    }
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    const body = await fs.readFile(file).catch(() => null);
    await route.fulfill(body ? { body, contentType } : { status: 404, body: "" });
  }
  return { serve };
}

const row = '.row.agent[data-id="zcode"]';

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const where of ["window", "panel"]) {
      test(`${engine} ${lang} ${where}: the closed models button says what its list ticks`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await browser.newPage({ viewport: where === "panel" ? { width: 360, height: 640 } : { width: 1100, height: 700 } });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", fixture(lang).serve);
        await page.goto("http://uni-switch.test/" + (where === "panel" ? "?mode=panel" : ""));
        await page.locator(row).waitFor();
        const tr = (s, v) => page.evaluate(([s, v]) => t(s, v), [s, v]);
        // the button, and in the panel the row's words beside it
        const said = async () => {
          const b = await page.locator(".ag-menu .v").textContent();
          if (where === "panel") assert.equal(await page.locator(`${row} .ag-sum .vt`).textContent(), b, "the row's words and its button differ");
          return b;
        };
        const settle = (want) => page.waitForFunction((w) => document.querySelector(".ag-menu .v")?.textContent === w, want).catch(async () => assert.equal(await said(), want));

        let menu;
        if (where === "window") menu = page.locator(`${row} > .ag-menu`);
        else {
          await page.locator(`${row} .ag-sum`).click();
          menu = page.locator(`${row} .ag-open .ag-menu`);
          await page.waitForFunction((sel) => document.querySelector(sel)?.getBoundingClientRect().height > 0, `${row} .ag-open .ag-menu`);
        }
        // every model ticked, the group first: all of them, not the group
        // and a model +2
        const all = await tr("All {n} models", { n: 4 });
        assert.equal(await said(), all);
        assert.ok(!(await said()).includes("Never stuck"), "named the routing group as if picked");

        await menu.click();
        const box = page.locator(".am-pop:not(.leaving)");
        await box.waitFor();
        const ticked = () => box.locator(".am-mr").evaluateAll((es) => es.filter((e) => e.getAttribute("aria-checked") === "true").length);
        assert.equal(await ticked(), 4);
        const flip = (name) => box.locator(".am-mr", { hasText: name }).click();

        // three of four: counted as the list counts them
        await flip("Kimi K3");
        await settle(await tr("Showing {shown} / {listed} models", { shown: 3, listed: 4 }));
        assert.equal(await ticked(), 3);
        // two: named, both of them and nothing more
        await flip("Never stuck");
        await settle("DeepSeek Pro, DeepSeek Flash");
        // one
        await flip("DeepSeek Flash");
        await settle("DeepSeek Pro");
        // none
        await flip("DeepSeek Pro");
        await settle(await tr("No models"));
        // all back
        for (const n of ["DeepSeek Pro", "DeepSeek Flash", "Never stuck", "Kimi K3"]) await flip(n);
        await settle(all);
        assert.ok(!/\+\d/.test(await said()), "a +N left on the button");
        assert.equal(await page.locator("select").count(), 0, "a native select");
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "the row runs past the window");
        assert.deepEqual(errors, []);
      });
    }
  }
}

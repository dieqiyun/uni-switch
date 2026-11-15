// Run with Node's test runner and Playwright on the module path; see README.md.
// Cursor Private Inference's own model picker lists uni-switch's /v1/models as
// its key is shown them (mamba on Discord: connected through its
// environment variables, its models couldn't be picked in uni-switch): its
// connected row picks which models that picker lists, several at once, as
// Claude Desktop's does, beside the square copying its launch command. In
// the window the button beside its switch names them and opens the model
// list; in the tray panel the opened row has it. Unticking one is posted at
// once and the row says so. No native <select>, and no click scrolls the
// page. In English and Chinese. No backend: faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const LAUNCH = "CURSOR_LOCAL_AGENT_BASE_URL=http://127.0.0.1:3425/v1 CURSOR_LOCAL_AGENT_API_KEY=uni-switch-cursor-local '/Applications/Cursor.app/Contents/MacOS/Cursor'";

function fixture(lang) {
  const list = [
    { id: "opencode-go/deepseek-v4.1-flash", name: "DeepSeek V4.1 Flash", group: "OpenCode Go", icon: "opencode" },
    { id: "grok-2/grok-4.6", name: "grok-4.6", group: "Grok", icon: "generic" },
    { id: "relay/glm-5", name: "GLM-5", group: "Relay", icon: "generic" },
    { id: "relay/kimi-k3", name: "Kimi K3", group: "Relay", icon: "generic", hidden: true },
  ];
  const posts = [];
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
      id: "cursor-local", name: "Cursor Private Inference", path: "", icon: "cursor", launch: LAUNCH, wired: true,
      fields: [
        { key: "provider", label: "provider", value: "uni-switch", options: [{ value: "uni-switch", label: "uni-switch", icon: "uni-switch" }] },
      ],
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
    if (url.pathname === "/api/agent-models/cursor-local") {
      if (req.method() === "POST") {
        const { hidden } = req.postDataJSON();
        posts.push(hidden);
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
    await route.fulfill({ body: await fs.readFile(file), contentType });
  }
  return { serve, posts };
}

const W = {
  en: { said: "Connected · 3 models in Cursor Private Inference's model menu", after: "Connected · 2 models in Cursor Private Inference's model menu", title: "Cursor Private Inference's model list", restart: "quit and reopen it", details: "Details" },
  zh: { said: "已接入 · Cursor Private Inference 的模型菜单里有 3 个模型", after: "已接入 · Cursor Private Inference 的模型菜单里有 2 个模型", title: "Cursor Private Inference 的模型列表", restart: "退出重开", details: "详情" },
};
const cd = '.row.agent[data-id="cursor-local"]';

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    for (const where of ["window", "panel"]) {
      test(`${engine} ${lang} ${where}: Cursor Private Inference's row picks the models its picker lists`, async (t) => {
        const w = W[lang];
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const page = await browser.newPage({ viewport: where === "panel" ? { width: 420, height: 640 } : { width: 1100, height: 700 } });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const fx = fixture(lang);
        await page.route("**/*", fx.serve);
        t.after(() => browser.close());
        await page.goto("http://uni-switch.test/" + (where === "panel" ? "?mode=panel" : ""));
        await page.locator(cd).waitFor();

        let menu;
        if (where === "window") {
          assert.equal(await page.locator(`${cd} .ag-st-t`).textContent(), w.said);
          assert.equal(await page.locator(`${cd} .ag-link`).textContent(), w.details);
          assert.equal(await page.locator(`${cd} > .field.launch`).count(), 1, "the launch command's square is gone");
          menu = page.locator(`${cd} > .ag-menu`);
        } else {
          assert.equal(await page.locator(`${cd} .ag-sum .vt`).textContent(), await page.evaluate(() => t("Showing {shown} / {listed} models", { shown: 3, listed: 4 })));
          await page.locator(`${cd} .ag-sum`).click();
          menu = page.locator(`${cd} .ag-open .ag-menu`);
          await page.waitForFunction((sel) => document.querySelector(sel)?.getBoundingClientRect().height > 0, `${cd} .ag-open .ag-menu`);
          assert.equal(await page.locator(`${cd} .ag-conn-said`).textContent(), w.said);
        }
        assert.equal(await menu.count(), 1, "no model button");
        assert.equal(await menu.locator(".v").textContent(), await page.evaluate(() => t("Showing {shown} / {listed} models", { shown: 3, listed: 4 })));
        assert.ok((await menu.getAttribute("title")).includes(w.restart), await menu.getAttribute("title"));

        const y = await page.evaluate(() => scrollY);
        await menu.click();
        const box = page.locator(".am-pop:not(.leaving)");
        await box.waitFor();
        assert.equal(await box.locator(".am-t").textContent(), w.title);
        const rows = await box.locator(".am-mr").evaluateAll((es) => es.map((e) => [e.querySelector(".n").textContent, e.getAttribute("aria-checked")]));
        assert.deepEqual(rows, [["DeepSeek V4.1 Flash", "true"], ["grok-4.6", "true"], ["GLM-5", "true"], ["Kimi K3", "false"]]);

        // several at once: GLM-5 out, the list stays open, the row says so
        await box.locator(".am-mr", { hasText: "GLM-5" }).click();
        await page.waitForFunction(() => document.querySelector(".ag-menu .v")?.textContent === "DeepSeek V4.1 Flash, grok-4.6");
        await page.waitForTimeout(150);
        assert.equal(fx.posts.length, 1);
        assert.deepEqual(fx.posts.at(-1).sort(), ["relay/glm-5", "relay/kimi-k3"]);
        assert.equal(await box.count(), 1, "the list closed after one pick");
        assert.equal(await menu.locator(".v").textContent(), "DeepSeek V4.1 Flash, grok-4.6");
        const said = where === "window" ? `${cd} .ag-st-t` : `${cd} .ag-conn-said`;
        assert.equal(await page.locator(said).textContent(), w.after);
        if (where === "panel") assert.equal(await page.locator(`${cd} .ag-sum .vt`).textContent(), "DeepSeek V4.1 Flash, grok-4.6");
        // and Kimi K3 back in
        await box.locator(".am-mr", { hasText: "Kimi K3" }).click();
        await page.waitForFunction(() => document.querySelector(".ag-menu .v")?.textContent === t("Showing {shown} / {listed} models", { shown: 3, listed: 4 }));
        await page.waitForTimeout(150);
        assert.equal(fx.posts.length, 2);
        assert.deepEqual(fx.posts.at(-1), ["relay/glm-5"]);
        assert.equal(await page.locator(said).textContent(), w.said);

        assert.equal(await page.evaluate(() => scrollY), y, "a click scrolled the page");
        assert.equal(await page.locator("select").count(), 0, "a native select");
        assert.deepEqual(errors, []);
      });
    }
  }
}

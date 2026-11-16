const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const assets = path.resolve(__dirname, "../assets");

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${engine} ${lang}: provider Use persists active state and applies Claude CLI and Desktop`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await browser.newPage({ viewport: { width: 1200, height: 850 }, reducedMotion: "reduce" });
      const errors = [], posts = [];
      page.on("pageerror", err => errors.push(err.message));
      t.after(() => { if (errors.length) console.error(errors); });
      let current = "a", claude = false;
      const field = id => ({ key: "model", label: "model", value: `${id}/m1`, options: [{ value: `${id}/m1`, ref: `${id}/m1`, label: "Model 1" }] });
      const settings = () => ({ lang, theme: "light", guideHidden: true, agentProviders: { codex: current, ...(claude ? { claude: "b", "claude-desktop": "b" } : {}) } });
      const state = () => ({ agents: [
        { id: "codex", name: "Codex", wired: true, fields: [field(current)] },
        { id: "claude", name: "Claude Code", wired: claude, fields: [field("b")] },
        ...(claude ? [{ id: "claude-desktop", name: "Claude Desktop", wired: true, fields: [{ ...field("b"), key: "sonnet" }] }] : []),
      ], profiles: [], settings: settings() });
      const providers = () => ({ providers: ["a", "b"].map(id => ({ id, name: `Provider ${id}`, icon: "generic", host: "example.invalid", chat: "http://127.0.0.1:1/v1", key: { set: true, masked: "synthetic" }, ready: true, models: [{ id: "m1", name: "Model 1", on: true }], agents: [], fallback: [], headers: {}, keyList: [], proxy: "" })), presets: [], excluded: [], gateway: { running: true, window: true } });
      await page.route("**/*", async route => {
        const req = route.request(), url = new URL(req.url());
        const json = data => route.fulfill({ json: data });
        if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs=${JSON.stringify({ lang, theme: "light", web: true, uniSwitch: true })};` });
        if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window={};" });
        if (url.pathname === "/api/state") return json(state());
        if (url.pathname === "/api/settings") return json(settings());
        if (url.pathname === "/api/providers") return json(providers());
        if (url.pathname.startsWith("/api/agents/provider-models/")) return json([{ value: "b/m1", ref: "b/m1", label: "Model 1" }]);
        if (url.pathname.startsWith("/api/agents/use-provider/")) {
          posts.push({ path: url.pathname, ...req.postDataJSON() });
          if (url.pathname.endsWith("/claude")) claude = true; else current = "b";
          return json(state());
        }
        if (url.pathname === "/api/usage/quotas") return json([]);
        if (url.pathname === "/api/plugins") return json({ plugins: [] });
        if (url.pathname.startsWith("/api/")) return json({});
        const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
        const type = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
        try {
          let body = await fs.readFile(file);
          if (path.extname(file) === ".html") body = Buffer.from(body.toString().replace('<html lang="en">', '<html class="uni-switch" lang="en">').replace('</head>', '<link rel="stylesheet" href="uni-switch.css"></head>'));
          return await route.fulfill({ body, contentType: type });
        }
        catch { return route.fulfill({ status: 404, body: "" }); }
      });
      await page.goto("http://fixture.invalid/?view=providers");
      await page.waitForFunction(() => providers?.providers?.length === 2);
      assert.deepEqual(errors, []);
      const card = id => page.locator(`.row.provider[data-id="${id}"]`);
      await card("a").waitFor();
      assert.equal(await card("a").locator(".uni-use").isDisabled(), true);
      await card("b").locator(".uni-use").click();
      await page.locator(".uni-provider-pick .primary").click();
      await page.waitForFunction(() => document.querySelector('.row.provider[data-id="b"]')?.classList.contains("uni-provider-active"));
      assert.equal(await card("a").locator(".uni-use").isEnabled(), true);
      assert.equal(await card("b").locator(".uni-use").isDisabled(), true);
      // State survives a full reload, rather than only a click-local flag.
      await page.evaluate(() => loadUpstream());
      await page.reload();
      await page.evaluate(() => show("providers"));
      await card("b").waitFor();
      assert.equal(await card("b").locator(".uni-use").isDisabled(), true);
      await page.locator('[data-uni-client="claude"]').click();
      assert.match(await page.locator("#uniClientControls").innerText(), /Claude Code CLI/);
      assert.match(await page.locator("#uniClientControls").innerText(), /Claude Desktop/);
      await card("b").locator(".uni-use").click();
      await page.locator(".uni-provider-pick .primary").click();
      await page.waitForFunction(() => document.querySelector('.row.provider[data-id="b"] .uni-use')?.disabled);
      assert.deepEqual(posts, [
        { path: "/api/agents/use-provider/codex", provider: "b", model: "b/m1" },
        { path: "/api/agents/use-provider/claude", provider: "b", model: "b/m1" },
      ]);
      assert.deepEqual(errors, []);
    });
  }
}

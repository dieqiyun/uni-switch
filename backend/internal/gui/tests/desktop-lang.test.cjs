// Run with Node's test runner and Playwright on the module path; see README.md.
// Claude Desktop on uni-switch runs in its 3p mode, whose window is Anthropic's
// bundled one for a gateway: 11 languages, no Chinese (up to 4.4212.0), so a
// Chinese reader saw it turn English and took it for uni-switch's (GZXYMH on X).
// Claude Desktop's opened row says so in Chinese and Traditional Chinese, and
// to no one else, whose language Desktop has. Every language, wide and
// narrow. No backend: faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function fixture(lang) {
  const settings = { lang, theme: "light" };
  const posts = [];
  const state = () => ({
    agents: [{
      id: "claude-desktop", name: "Claude Desktop", path: "/test/claude_desktop_config.json", icon: "claude-color", wired: true,
      fields: [{ key: "provider", label: "provider", value: "uni-switch", options: [{ value: "uni-switch", label: "uni-switch", icon: "uni-switch" }] }],
      models: { shown: 2, listed: 2, by: [{ name: "OpenCode Go", icon: "opencode", n: 2 }], names: ["DeepSeek V4 Pro", "Exo Free"] },
    }],
    profiles: [], settings,
  });
  async function serve(route) {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state());
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

// "Details" in each language, and what the line says where it is shown
const W = {
  en: { details: "Details" },
  zh: { details: "详情", says: "没有中文" },
  "zh-TW": { details: "詳情", says: "沒有中文" },
  ja: { details: "詳細" },
  de: { details: "Details" },
};
const cd = '.row.agent[data-id="claude-desktop"]';

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(W)) {
    for (const width of [1100, 640]) {
      test(`${engine} ${lang} ${width}px: Claude Desktop's row says its gateway window has no Chinese, to a Chinese reader alone`, async (t) => {
        const w = W[lang];
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const page = await browser.newPage({ viewport: { width, height: 700 } });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const fx = fixture(lang);
        await page.route("**/*", fx.serve);
        t.after(() => browser.close());
        await page.goto("http://uni-switch.test/");
        await page.locator(cd).waitFor();
        await page.locator(`${cd} .ag-link`, { hasText: w.details }).click();
        await page.locator(`${cd} .ag-exp .ag-longest`).waitFor();
        const line = page.locator(`${cd} .ag-exp .ag-desktop-lang`);
        if (!w.says) {
          assert.equal(await line.count(), 0, "said to a reader whose language Desktop's gateway window has");
        } else {
          assert.ok((await line.textContent()).includes(w.says), await line.textContent());
          const box = await line.boundingBox();
          assert.ok(box.width <= width && box.height > 0, JSON.stringify(box));
          // the page's width holds at a narrow window
          assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "the page scrolls sideways");
        }
        assert.deepEqual(errors, []);
      });
    }
  }
}

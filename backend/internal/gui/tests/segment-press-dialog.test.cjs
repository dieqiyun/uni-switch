// Run with Node's test runner and Playwright on the module path; see README.md.
// A segment pressed in a dialog leaves the dialog where it was (the owner,
// 10-23: each click on Kimi Code's Plan moved the dialog a few pixels
// down). A pressed segment scales (.opt:active) and is still scaling when
// its click redraws; the dialog's hold measured it by the segments' strip
// then, 2px above where the press had marked it, and went down by that at
// every click. Pressed as a hand does, down and up a beat apart, eight
// times round the four plans. English and Chinese, Chromium and WebKit; no
// backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const kimi = {
  id: "kimi", name: "Kimi Code", icon: "generic", kind: "partner", sponsored: true, regionLabel: "Plan",
  notes: { en: "One subscription, one K3 quota shared by every agent", zh: "一份订阅，所有 agent 共享 K3 额度" },
  website: "https://github.com/dieqiyun/uni-switch/go/kimi/site", keysUrl: "https://github.com/dieqiyun/uni-switch/go/kimi/keys",
  chat: "https://api.kimi.example/coding/v1", anthropic: "https://api.kimi.example/coding",
  regions: [
    { id: "cn", name: "China", chat: "https://api.kimi.example/coding/v1", anthropic: "https://api.kimi.example/coding" },
    { id: "global", name: "Global", chat: "https://api.kimi.example/global/v1", anthropic: "https://api.kimi.example/global" },
    { id: "api-cn", name: "Pay as you go · China", chat: "https://api.moonshot.example/cn/v1", anthropic: "https://api.moonshot.example/cn/anthropic" },
    { id: "api", name: "Pay as you go · Global", chat: "https://api.moonshot.example/v1", anthropic: "https://api.moonshot.example/anthropic" },
  ],
};

function serve(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"dark",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "dark" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [kimi], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a plan pressed leaves its dialog where it was`, async (t) => {
      const browser = await (engine === "webkit" ? webkit : chromium).launch();
      t.after(() => browser.close());
      const page = await browser.newPage({ viewport: { width: 1100, height: 800 } });
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      await page.goto("http://uni-switch.test/?view=providers");
      // with no provider the page opens its add sheet itself once the list
      // is in, and its add button goes away: a press before that opens it
      // instead (on a slow machine the list came first, and the press
      // waited for a button that was gone)
      const tile = page.locator("#addSheet .tile", { hasText: "Kimi Code" });
      await tile.waitFor();
      await tile.click();
      const d = page.locator("#modal .editor");
      const opts = d.locator(".segs.regions .opt");
      await opts.nth(3).waitFor();
      // the dialog come in and still
      await page.waitForFunction(() => !document.querySelector("#modal").getAnimations({ subtree: true }).length);
      const top = () => d.evaluate((e) => e.getBoundingClientRect().top);
      const at = await top();
      for (let i = 0; i < 8; i++) {
        const b = await opts.nth((i + 1) % 4).boundingBox();
        await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2);
        await page.mouse.down();
        await page.waitForTimeout(120);
        await page.mouse.up();
        await page.waitForFunction((n) => document.querySelectorAll("#modal .segs.regions .opt")[n].classList.contains("on"), (i + 1) % 4);
        await page.waitForTimeout(300);
        assert(Math.abs(await top() - at) < 0.5, `press ${i + 1}: the dialog moved from ${at} to ${await top()}`);
      }
      assert.deepEqual(errors, []);
    });
  }
}

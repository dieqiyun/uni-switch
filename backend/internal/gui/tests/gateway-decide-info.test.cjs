// Run with Node's test runner and Playwright on the module path; see README.md.
// A decision model on the Gateway list (ARNO on Discord: decision模型也是需要
// 拉取正确的上下文长度以及是否支持视觉功能的): its row says its context and
// whether it takes images, as any model's does, and its tooltip says it is a
// decision model routing groups ask, rather than "Reasoning levels: none known".
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const providers = (lang) => ({
  providers: [
    { id: "workers", name: "Workers AI", icon: "generic", agents: [], decide: "https://api.cloudflare.com/client/v4/accounts/a/ai/run", models: [
      { id: "@cf/typesafe/jev", name: "Jev", on: true, images: false, context: 32000 },
      { id: "@cf/cloudflare/clef", name: "Clef", on: true, images: true, context: 65536 },
    ] },
    { id: "acme", name: "Acme", icon: "generic", agents: [], chat: "https://acme.test/v1", models: [
      { id: "text-only", name: "Text Only", on: true, images: false, context: 128000 },
    ] },
  ],
  gateway: { running: true, window: true, mine: true, url: "http://127.0.0.1:3999", models: 3, calls: [], groups: [] },
});

function server(lang) {
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers(lang));
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) await new Promise((r) => setTimeout(r, 20e3));
      return json({ mine: true, now: new Date().toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const rowOf = (page, id) => page.locator("#gwModels .row.model").filter({ has: page.locator(".name", { hasText: new RegExp("^" + id.replace(/[./@]/g, "\\$&") + "$") }) });

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a decision model's row says its context and images`, async (t) => {
      assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 900, height: 700 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(lang));
      await page.goto("http://uni-switch.test/?view=gateway");
      await rowOf(page, "workers/@cf/cloudflare/clef").waitFor();

      const info = async (id) => {
        const box = rowOf(page, id).locator(".minfo");
        return { chips: (await box.locator(".badge").allTextContents()).map((s) => s.trim()), img: await box.locator(".mi-img").count(), tip: await box.getAttribute("title") };
      };
      const said = lang === "zh" ? "决策模型：路由组会问它，agent 看不到它" : "Decision model: routing groups ask it, agents never see it";
      let i = await info("workers/@cf/cloudflare/clef");
      assert.equal(i.img, 1, "Clef takes images");
      assert.deepEqual(i.chips, ["", "66K"]);
      assert.equal(i.tip.split("\n")[0], said);
      assert.match(i.tip, /65,536/);
      i = await info("workers/@cf/typesafe/jev");
      assert.equal(i.img, 0, "Jev is text only");
      assert.deepEqual(i.chips, ["32K"]);
      assert.equal(i.tip.split("\n")[0], said);
      assert.match(i.tip, /32,000/);
      // a chat model's tooltip is as it was
      i = await info("acme/text-only");
      assert.notEqual(i.tip.split("\n")[0], said);
      assert.deepEqual(errors, []);
    });
  }
}

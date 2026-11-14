// Run with Node's test runner and Playwright on the module path; see README.md.
// nb5p on Discord: on the Gateway page's model list, the image chip at a
// row's end stood shorter than its reasoning chip ("低–ultra") and its
// context chip ("272K"): it holds a glyph, not a line of text, so it took
// the glyph's 11px where the text chips take their 16px line. Every chip
// of a row is as tall as the others and centred on the same line, in light
// and dark, in English and Chinese, at the window's width and a narrow one.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const providers = {
  providers: [
    { id: "acme", name: "Acme", icon: "generic", agents: [], models: [
      { id: "gpt-5.5", name: "GPT-5.5", on: true, efforts: ["low", "medium", "high", "xhigh"], images: true, context: 272000 },
      { id: "flash", name: "Flash", on: true, efforts: ["low", "high"], images: true, context: 500000 },
      { id: "text-only", name: "Text Only", on: true, images: false, context: 128000 },
    ] },
  ],
  gateway: { running: true, window: true, mine: true, url: "http://127.0.0.1:3999", models: 3, calls: [], groups: [] },
};

function serveAs(lang, theme) {
  const state = { agents: [], profiles: [], settings: { lang, theme } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:${JSON.stringify(theme)},web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
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

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a model row's chips are all one height, the image chip too", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh"]) for (const theme of ["light", "dark"]) for (const width of [900, 420]) {
      const context = await browser.newContext({ viewport: { width, height: 700 }, reducedMotion: "reduce", colorScheme: theme });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serveAs(lang, theme));
      await page.goto("http://uni-switch.test/?view=gateway");
      await page.locator("#gwModels .row.model .minfo .mi-img").first().waitFor();
      const rows = await page.locator("#gwModels .row.model .minfo").evaluateAll((boxes) => boxes.map((box) =>
        [...box.querySelectorAll(".badge")].filter((b) => b.offsetParent).map((b) => {
          const r = b.getBoundingClientRect();
          return { cls: b.className, h: r.height, mid: r.top + r.height / 2 };
        })));
      const where = `${lang} ${theme} ${width}px`;
      let imgs = 0;
      for (const chips of rows) {
        const text = chips.find((c) => !c.cls.includes("mi-img"));
        for (const c of chips) {
          if (c.cls.includes("mi-img")) imgs++;
          assert(Math.abs(c.h - text.h) < 0.5, `${where}: ${c.cls} is ${c.h}px tall, ${text.cls} ${text.h}px`);
          assert(Math.abs(c.mid - text.mid) < 0.5, `${where}: ${c.cls} centred at ${c.mid}, ${text.cls} at ${text.mid}`);
        }
      }
      assert(imgs >= 2, `${where}: the image chips were measured (${imgs})`);
      assert.deepEqual(errors, [], where);
      await context.close();
    }
  });
}

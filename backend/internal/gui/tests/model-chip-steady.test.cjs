// Run with Node's test runner and Playwright on the module path; see README.md.
// frankdeng on Discord: "点击某个模型，还会因为选中状态导致排版有变化，位置
// 发生偏移". A pick drew a tick before the name and set it bolder, so the
// chip grew, and every chip after it on the row, and the rows under it,
// moved under the pointer. A chip that a click picks and unpicks now keeps
// its place for its tick, and a pick no longer changes the weight: no chip
// in the provider editor's list moves or changes size when one is picked,
// unpicked, or picked first out of none picked (#681's path, which flips
// every served chip at once). Chromium and WebKit, wide and narrow, in
// every language; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const fetched = new Date(Date.now() - 3600e3).toISOString();
const names = ["DeepSeek V4", "DeepSeek V4 Flash", "Kimi K3", "GLM 5.3", "Qwen 4 Max", "MiniMax M3", "Doubao Seed 2.5 Pro", "Claude Sonnet 5", "GPT-5.5", "Gemini 3.5 Pro"];
const models = Array.from({ length: 30 }, (_, i) => {
  const name = names[i % names.length] + (i >= names.length ? ` ${Math.floor(i / names.length) + 1}` : "");
  return { id: name.toLowerCase().replaceAll(" ", "-"), name, rate: i % 3 ? 0.1 : undefined };
});

function serve(lang, chosen) {
  const provider = () => {
    const on = new Set(chosen.length ? chosen : models.map((m) => m.id));
    return {
      id: "relay", name: "Relay", icon: "generic", host: "", chat: "https://relay.example.com/v1", responses: "", anthropic: "", catalog: "",
      models: models.map((m) => ({ ...m, on: on.has(m.id) })), chosen, fetched, agents: [], fallback: [], headers: {},
      key: { set: true, masked: "sk-…one" }, keyList: [], balanceToken: { takes: false, set: false }, proxy: "", ready: true,
    };
  };
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/providers") return json({ providers: [provider()], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const LANGS = ["en", "zh", "zh-TW", "ja", "de"];
const cases = [...LANGS.map((lang) => [lang, 900]), ...LANGS.map((lang) => [lang, 420])];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const [lang, width] of cases) {
    test(`${engine} ${lang} ${width}px: a model chip picked or unpicked moves no chip`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      for (const [label, chosen] of [["some picked", models.filter((_, i) => i % 2 === 0).map((m) => m.id)], ["none picked", []]]) {
        await t.test(label, async () => {
          const page = await (await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" })).newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          const errors = [];
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", serve(lang, chosen));
          await page.goto("http://uni-switch.test/?view=providers");
          const editor = page.locator("#modal:not([hidden]) .editor");
          await page.locator(".row.provider", { hasText: "Relay" }).first().click();
          const chips = editor.locator(".mchips .mchip");
          await chips.first().waitFor();
          // each chip's box within the list's own content, so that a scroll
          // of the page or the list (the click brings a chip into view at
          // 420px) is not counted as a chip moving
          const rects = () => chips.evaluateAll((cs) => {
            const list = cs[0].parentElement, l = list.getBoundingClientRect();
            const r1 = (v) => Math.round(v * 10) / 10;
            return cs.map((c) => { const r = c.getBoundingClientRect(); return [c.textContent, r1(r.x - l.x + list.scrollLeft), r1(r.y - l.y + list.scrollTop), r1(r.width), r1(r.height)]; });
          });
          const on = () => chips.evaluateAll((cs) => cs.filter((c) => c.classList.contains("on")).length);

          const before = await rects();
          assert.equal(before.length, models.length);
          // a chip early in the list, so a change in its width would move
          // the rest of its row and the rows under it
          const target = chips.nth(1);
          const was = await on();
          await target.click();
          assert.notEqual(await on(), was, "the click changed no pick");
          assert.deepEqual(await rects(), before, "a chip moved or changed size as one was picked");
          await target.click();
          assert.deepEqual(await rects(), before, "a chip moved or changed size as one was unpicked");
          await chips.nth(2).click();
          assert.deepEqual(await rects(), before, "a chip moved or changed size on a second pick");
          assert.deepEqual(errors, []);
          await page.context().close();
        });
      }
    });
  }
}

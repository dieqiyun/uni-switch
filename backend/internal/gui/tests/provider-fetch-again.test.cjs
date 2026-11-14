// Run with Node's test runner and Playwright on the module path; see README.md.
// A saved provider's editor fetches its models again under the name the add
// form gave the button (耍赖天都爱 on Discord: 模型拉取一次就没有这个按钮了 —
// the add form's Fetch models was "Refresh" once saved, so after one fetch
// the button seemed gone, while a key moved to another group on a relay
// serves another list). A provider never fetched says Fetch models, one
// fetched says Fetch models again, and its hint names the same button. The
// fetch asks with the key just typed (the other group's), the editor shows
// the new list, the pick the vendor dropped goes, an id added by hand
// stays, and the button is still there, named the same, to fetch again.
// Nothing scrolls the page. Every GUI language, Chromium and WebKit, wide
// and narrow.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const fetched = new Date(Date.now() - 3600e3).toISOString();
const relay = (id, name, models, on, when) => ({
  id, name, icon: "generic", host: id + ".example.com", chat: `https://${id}.example.com/v1`, responses: "", anthropic: "", catalog: "",
  models: models.map((m) => ({ id: m, name: m, on: on.includes(m) })), chosen: on, fetched: when, agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-…aaa" }, keyList: [], balanceToken: { takes: false, set: false }, proxy: "",
});

function serve(lang, calls) {
  // group A's list until the fetch asked with group B's key answers
  let grouped = relay("grouped", "Grouped Relay", ["a-chat", "a-code", "shared-1"], ["a-chat", "shared-1"], fetched);
  const fresh = relay("fresh", "Fresh Relay", ["md-1", "md-2"], ["md-1"], "");
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/providers") return json({ providers: [grouped, fresh], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/provider/models") {
      const body = req.postDataJSON();
      calls.push(body);
      // as Refetch: the pick group B doesn't list is dropped from the saved picks
      if (body.key === "sk-group-b") {
        grouped = { ...relay("grouped", "Grouped Relay", ["b-chat", "b-vision", "shared-1"], ["shared-1"], new Date().toISOString()) };
        return json({ count: 3, dropped: ["a-chat"], provider: grouped });
      }
      return json({ count: 3, provider: grouped });
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const sizes = { en: [900, 420], zh: [900, 420], "zh-TW": [900], ja: [900], de: [420] };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const [lang, widths] of Object.entries(sizes)) {
    for (const width of widths) {
      test(`${engine} ${lang} ${width}px: a fetched provider's editor fetches its models again`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const page = await (await browser.newContext({ viewport: { width, height: 820 }, reducedMotion: "reduce" })).newPage();
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-provider-fetch-again.png`) });
          }
          await browser.close();
        });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const calls = [];
        await page.route("**/*", serve(lang, calls));
        await page.goto("http://uni-switch.test/?view=providers");
        const tr = (s) => page.evaluate(([l, s]) => (l === "en" ? s : I18N[l][s]), [lang, s]);
        const w = {
          fetch: await tr("Fetch models"), again: await tr("Fetch models again"),
          hint: await tr("from models.dev · Fetch models asks the vendor"),
        };
        for (const [k, v] of Object.entries(w)) assert.ok(v, `${k} has its ${lang}`);

        const editor = page.locator("#modal:not([hidden]) .editor");
        const open = async (name) => {
          await page.locator(".row.provider", { hasText: name }).click();
          await editor.locator(".ehead b", { hasText: name }).waitFor();
          await editor.locator(".mchips .mchip").first().waitFor();
        };
        const close = async () => {
          await page.keyboard.press("Escape");
          const sure = page.locator("dialog.action-confirm[open] button").last();
          if (await sure.isVisible().catch(() => false)) await sure.click();
          await page.locator("#modal").waitFor({ state: "hidden" });
        };
        const foot = () => editor.locator(".mfoot");
        const chips = () => editor.locator(".mchips .mchip").evaluateAll((cs) => cs.map((c) => c.dataset.model || c.textContent).sort());
        const picks = () => editor.locator(".mchips .mchip.on").evaluateAll((cs) => cs.map((c) => c.dataset.model || c.textContent).sort());

        // never fetched: the add form's name, and the hint names it too
        await open("Fresh Relay");
        await foot().getByRole("button", { name: w.fetch, exact: true }).waitFor();
        assert.ok((await foot().innerText()).includes(w.hint), "the hint names the button");
        await close();

        // fetched once: the same button, "again"; a key of group B typed
        await open("Grouped Relay");
        const again = foot().getByRole("button", { name: w.again, exact: true });
        await again.waitFor();
        assert.ok(await again.isVisible(), "the button is shown after a fetch");
        await editor.locator('input[type="password"]').first().fill("sk-group-b");
        const add = foot().locator("input");
        await add.fill("my-own-model");
        await add.press("Enter");
        await editor.locator(".mchips .mchip.on.own", { hasText: "my-own-model" }).waitFor();
        const view = page.locator("#view-providers");
        const top = await view.evaluate((v) => v.scrollTop);
        const edTop = await editor.evaluate((e) => e.scrollTop);
        await again.click();
        for (let i = 0; i < 60 && !(await chips()).includes("b-chat"); i++) await page.waitForTimeout(50);
        assert.equal(calls.length, 1);
        assert.equal(calls[0].key, "sk-group-b", "asked with the key just typed");
        assert.equal(calls[0].typed, true);
        assert.deepEqual(await chips(), ["b-chat", "b-vision", "my-own-model", "shared-1"], "group B's list, the id added by hand kept");
        assert.deepEqual(await picks(), ["my-own-model", "shared-1"], "the pick group B lacks is dropped, the others stay");
        // still there, named the same, and it asks again
        await again.waitFor();
        await again.click();
        for (let i = 0; i < 60 && calls.length < 2; i++) await page.waitForTimeout(50);
        assert.equal(calls.length, 2, "a second fetch is asked");
        await page.waitForTimeout(200);
        assert.equal(await view.evaluate((v) => v.scrollTop), top, "the page moved");
        assert.equal(await editor.evaluate((e) => e.scrollTop), edTop, "the editor moved");
        // the button fits the row at this width, on screen in the footer
        const box = await again.boundingBox(), row = await foot().boundingBox();
        assert.ok(box && box.x >= row.x - 1 && box.x + box.width <= row.x + row.width + 1, "the button fits its row");
        assert.deepEqual(errors, []);
      });
    }
  }
}

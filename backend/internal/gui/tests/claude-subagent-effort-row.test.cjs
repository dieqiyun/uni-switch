// Run with Node's test runner and Playwright on the module path; see README.md.
// Claude Code's subagent effort sits at the right of the Subagents row in the
// opened row's Tiers, laid out as the Opus, Sonnet, Haiku and Fable rows'
// efforts are, and no longer as a square under New sessions. While the
// subagents are on Claude Code's pick (no model of their own) it reads
// Default and can't be used: uni-switch writes no effort then, as the main model
// at an effort pinned every subagent to it (KorenKrita on Discord). Given a
// model, it opens the effort slider and posts the level picked. In Chromium
// and WebKit, wide and narrow, in English and Chinese. No backend: the API is
// faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const models = [{ value: "uni-switch/v/glm", label: "GLM", ref: "v/glm" }, { value: "uni-switch/v/flash", label: "Flash", ref: "v/flash" }];
const levels = ["", "low", "medium", "high"].map((value) => ({ value }));
const tiers = ["opus", "sonnet", "haiku", "fable"];
const claude = (sub) => ({
  id: "claude", name: "Claude Code", path: "/test/settings.json", icon: "claudecode-color", wired: true,
  fields: [
    { key: "model", label: "model", value: "uni-switch/v/glm", options: models },
    { key: "effort", label: "effort", value: "high", options: levels },
    ...tiers.map((tier) => ({ key: tier, label: tier, value: "", options: models })),
    ...tiers.map((tier) => ({ key: tier + "_effort", label: tier + " effort", value: "", options: levels })),
    { key: "subagent", label: "subagents", value: sub, options: models },
    // a level the backend still holds from before reads Default with no model
    { key: "subagent_effort", label: "subagent effort", value: sub ? "" : "high", options: levels },
  ],
});

function server(lang, sub, sets) {
  let cur = { agents: [claude(sub)], profiles: [] };
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { ...cur, settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/set") {
      const body = req.postDataJSON();
      sets.push(body);
      cur = JSON.parse(JSON.stringify(cur));
      cur.agents[0].fields.find((f) => f.key === body.field).value = body.value;
      return route.fulfill({ json: { ...cur, settings: { lang, theme: "light" } } });
    }
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/agents/cli") return route.fulfill({ json: { agents: {}, pending: false } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// the words as i18n.js has them
const W = {
  en: { subs: "Subagents", def: "default", high: "high", label: "subagent effort", asks: "the effort Claude Code asks for", first: "Set a subagent model first", sep: (l, v) => `${l}: ${v}` },
  zh: { subs: "子代理", def: "默认", high: "高", label: "子代理推理强度", asks: "跟随 Claude Code 请求的推理强度", first: "先设置子代理模型", sep: (l, v) => `${l}：${v}` },
};
const cc = '.row.agent[data-id="claude"]';

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    for (const width of [1100, 560]) {
      for (const sub of ["", "uni-switch/v/flash"]) {
        test(`${engine} ${lang} ${width}px: the subagent effort is on the Subagents row, ${sub ? "with a model" : "disabled with none"}`, async (t) => {
          const w = W[lang];
          const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
          const page = await browser.newPage({ viewport: { width, height: 800 } });
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          const errors = [], sets = [];
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", server(lang, sub, sets));
          t.after(async () => {
            if (process.env.ARTIFACT_DIR) {
              await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
              await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-${sub ? "model" : "none"}-subagent-effort.png`), fullPage: true });
            }
            await browser.close();
          });
          await page.goto("http://uni-switch.test/");
          await page.locator(cc).waitFor();
          await page.locator(`${cc} .ag-link`).click();
          await page.locator(`${cc} .ag-exp`).waitFor();

          // the old square under New sessions is gone, from the whole row
          assert.equal(await page.locator(`${cc} .field.extra[data-key="subagent_effort"]`).count(), 0, "the old square is still there");
          assert.equal(await page.locator(`${cc} .field[data-key="subagent_effort"]`).count(), 1);

          // the Subagents row: its name, its model, then its effort at the right,
          // as each tier's row has them
          const row = page.locator(`${cc} .ag-exp .ag-vl`, { has: page.locator('.field[data-key="subagent"]') });
          assert.equal(await row.count(), 1);
          assert.deepEqual(await row.evaluate((r) => [...r.children].map((c) => c.dataset.key || c.className)), ["ag-tier", "subagent", "subagent_effort"]);
          assert.equal(await row.locator(".ag-tier").textContent(), w.subs);
          const eff = row.locator('.field.ag-eff[data-key="subagent_effort"]');
          const opus = page.locator(`${cc} .ag-exp .field.ag-eff[data-key="opus_effort"]`);
          // laid out as a tier's: the same class, bars and height, in the
          // same column, right of a picker as wide as the tiers'
          assert.equal(await eff.getAttribute("class"), await opus.getAttribute("class"));
          assert.equal(await eff.locator(".effort-ic").count(), 1);
          const [eb, ob] = [await eff.boundingBox(), await opus.boundingBox()];
          assert.ok(Math.abs(eb.x - ob.x) < 0.5 && Math.abs(eb.height - ob.height) < 0.5, `effort at ${eb.x},${eb.height}; opus's at ${ob.x},${ob.height}`);
          const pick = await row.locator('.field.ag-pick[data-key="subagent"]').boundingBox();
          assert.ok(eb.x >= pick.x + pick.width, "the effort sits right of the model");
          assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "the Agents view overflows");

          if (!sub) {
            // no model: Default, and it can't be used
            assert.equal(await eff.isDisabled(), true);
            assert.equal(await eff.locator(".v").textContent(), w.def);
            const title = await eff.getAttribute("title");
            assert.ok(title.startsWith(w.sep(w.label, w.def)) && title.includes(w.first), title);
            await eff.click({ force: true });
            assert.equal(await page.locator("#effortControl:not([hidden])").count(), 0, "a disabled effort opened the slider");
            assert.deepEqual(sets, []);
          } else {
            // a model of their own: the slider, and the level picked posted
            assert.equal(await eff.isDisabled(), false);
            assert.equal(await eff.getAttribute("title"), w.sep(w.label, w.asks));
            const y = await page.evaluate(() => scrollY);
            await eff.click();
            await page.locator("#effortControl:not([hidden])").waitFor();
            assert.equal(await page.evaluate(() => scrollY), y, "a click scrolled the page");
            await page.locator("#effortRange").evaluate((r) => {
              r.value = "3";
              r.dispatchEvent(new Event("input", { bubbles: true }));
              r.dispatchEvent(new Event("change", { bubbles: true }));
            });
            await page.waitForFunction(() => document.querySelector("#status")?.textContent);
            assert.deepEqual(sets, [{ agent: "claude", field: "subagent_effort", value: "high" }]);
            assert.equal(await eff.getAttribute("title"), w.sep(w.label, w.high));
          }
          assert.deepEqual(errors, []);
        });
      }
    }
  }
}

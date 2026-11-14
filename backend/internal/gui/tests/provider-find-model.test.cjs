// Run with Node's test runner and Playwright on the module path; see README.md.
// frankdeng (Discord): type a model on the Providers page and see which
// providers serve it; Neo had two providers serving Opus 5.5 and couldn't
// tell which. The search box keeps the rows with a model on that has every
// word the row doesn't say itself, written with or without its separators
// ("opus 5.5", "opus5.5" find claude-opus-5-5 and opus-5.5), and names the
// models under the row. Words spread over two models don't count, nor a
// model turned off. A search on the provider's own name names no model.
// Every language, Chromium and WebKit, in a narrow window; no backend.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const prov = (id, name, models, extra = {}) => ({
  id, name, icon: "generic", host: id + ".example.com", chat: "https://" + id + ".example.com/v1", responses: "", anthropic: "", catalog: "",
  models, agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-…" + id }, keyList: [], balanceToken: { takes: false, set: false }, proxy: "", ...extra,
});
const m = (id, name, on = true) => ({ id, name, on });

function fixture(lang) {
  const providers = {
    providers: [
      prov("anthropic", "Anthropic", [m("claude-opus-5-5", "Claude Opus 5.5"), m("claude-sonnet-5", "Claude Sonnet 5")]),
      prov("relay-a", "Relay A", [m("opus-5.5", "opus-5.5"), m("opus-5.5-thinking", "Opus 5.5 Thinking"), m("gpt-5.5", "GPT-5.5")]),
      // opus in one model, 5.5 in another: it serves no Opus 5.5
      prov("relay-b", "Relay B", [m("claude-opus-4-1", "Claude Opus 4.1"), m("gpt-5.5", "GPT-5.5")]),
      // Opus 5.5 listed but turned off: not served
      prov("relay-c", "Relay C", [m("claude-opus-5-5", "Claude Opus 5.5", false), m("glm-5", "GLM-5")]),
      prov("relay-off", "Relay Off", [m("claude-opus-5-5", "Claude Opus 5.5")], { off: true }),
    ],
    presets: [], excluded: [], gateway: { running: true, window: true },
  };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  const route = async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
  return { route };
}

// each row shown, on and off, with the models named under it
const shown = (page) => page.evaluate(() => [...document.querySelectorAll("#providers > .row.provider, #offProviders > .row.provider")]
  .filter((r) => r.getClientRects().length)
  .map((r) => [r.dataset.id, [...r.querySelectorAll(".pv-hits")].map((h) => h.textContent).join("|")]));
// the line names a model inside the row, clipped with "…" when long, and nothing runs off to the side
const overflow = (page) => page.evaluate(() => {
  const out = [], de = document.documentElement, v = document.querySelector("#view-providers");
  if (de.scrollWidth > de.clientWidth) out.push("page " + de.scrollWidth + ">" + de.clientWidth);
  if (v.scrollWidth > v.clientWidth) out.push("view " + v.scrollWidth + ">" + v.clientWidth);
  for (const h of document.querySelectorAll(".pv-hits")) {
    if (!h.getClientRects().length) continue;
    const r = h.getBoundingClientRect(), row = h.closest(".row").getBoundingClientRect();
    if (r.left < row.left - 0.5 || r.right > row.right + 0.5) out.push("line " + h.textContent);
    if (getComputedStyle(h).textOverflow !== "ellipsis") out.push("not clipped " + h.textContent);
  }
  return out;
});

const LANGS = ["en", "zh", "zh-TW", "ja", "de"];
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": the Providers page finds the providers serving a model, and names it", { timeout: 180000 }, async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      await browser.close();
      assert.deepEqual(errors, []);
    });
    for (const lang of LANGS) {
      await t.test(lang, async () => {
        const ctx = await browser.newContext({ viewport: { width: 380, height: 560 }, reducedMotion: "reduce" });
        const page = await ctx.newPage(), f = fixture(lang);
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", f.route);
        await page.goto("http://uni-switch.test/?view=providers");
        await page.locator("#providers > .row.provider").nth(3).waitFor();
        const tr = (s, vars) => page.evaluate(([s, vars]) => t(s, vars), [s, vars]);
        const serves = (models) => tr("Serves {models}", { models });
        if (lang !== "en") assert.notEqual(await serves("x"), "Serves x", "no translation");
        const q = page.locator("#pvQ");
        const type = async (s) => { await q.fill(s); await page.waitForTimeout(60); };
        const y0 = await page.evaluate(() => [scrollX, scrollY, document.querySelector("#view-providers").scrollTop]);

        // "opus 5.5", as frankdeng typed it, and "opus5.5", "Opus-5.5", as
        // Neo did: the ones serving it, each saying which model
        const opus = [
          ["anthropic", await serves("claude-opus-5-5")],
          ["relay-a", await serves("opus-5.5, opus-5.5-thinking")],
          ["relay-off", await serves("claude-opus-5-5")],
        ];
        for (const typed of ["opus 5.5", "opus5.5", "Opus-5.5", "claude opus 5 5"]) {
          await type(typed);
          assert.deepEqual(await shown(page), typed === "claude opus 5 5" ? [opus[0], opus[2]] : opus, typed);
        }
        assert.deepEqual(await overflow(page), []);

        // a model's own name; its provider's name and a model's together
        await type("Claude Sonnet");
        assert.deepEqual(await shown(page), [["anthropic", await serves("claude-sonnet-5")]]);
        await type("relay a thinking");
        assert.deepEqual(await shown(page), [["relay-a", await serves("opus-5.5-thinking")]]);

        // the provider's own name names no model
        await type("relay");
        assert.deepEqual(await shown(page), [["relay-a", ""], ["relay-b", ""], ["relay-c", ""], ["relay-off", ""]]);

        // a word no model or provider has: nothing, and no line left behind
        await type("opus 9");
        assert.deepEqual(await shown(page), []);
        assert.equal(await page.locator(".pv-hits").count(), 0);

        // Escape: every row back, none naming a model
        await type("gpt 5.5");
        assert.deepEqual(await shown(page), [["relay-a", await serves("gpt-5.5")], ["relay-b", await serves("gpt-5.5")]]);
        await q.focus();
        await page.keyboard.press("Escape");
        assert.deepEqual(await shown(page), [["anthropic", ""], ["relay-a", ""], ["relay-b", ""], ["relay-c", ""]]);
        assert.equal(await page.locator(".pv-hits").count(), 0);
        assert.deepEqual(await page.evaluate(() => [scrollX, scrollY, document.querySelector("#view-providers").scrollTop]), y0);
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await type("opus 5.5");
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-provider-find-model-${lang}.png`) });
        }
        await ctx.close();
      });
    }
  });
}

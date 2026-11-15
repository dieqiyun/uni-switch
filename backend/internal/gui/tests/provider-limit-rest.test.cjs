// Run with Node's test runner and Playwright on the module path; see README.md.
// zoecats (Discord #feedback, 10-25): a local aggregator answers "quota
// exhausted" for the moment it switches upstreams, and uni-switch set its key
// aside for half an hour. A provider the user added has, under Advanced,
// "Set it aside when out of quota or rate limited", ticked by default and
// with a note. Unticked and saved, it posts limitRestOff: true; one saved
// so opens unticked, with Advanced open, and ticked again posts false. A
// preset's and a signed-in account have no such row, and a preset's Save
// sends none. A checkbox, never a native select. Every language, wide and
// narrow.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const base = {
  icon: "generic", chat: "", responses: "", anthropic: "", catalog: "",
  models: [{ id: "glm-5", name: "glm-5", on: true }], agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-…agg" }, keyList: [], balanceToken: { takes: false, set: false }, proxy: "", searches: false,
};
const agg = { ...base, id: "agg", name: "Local Aggregator", chat: "http://127.0.0.1:8317/v1", limitRestOff: false };
const kept = { ...base, id: "kept", name: "Kept Relay", chat: "http://127.0.0.1:8318/v1", limitRestOff: true };
const openai = { ...base, id: "openai", name: "OpenAI", preset: "openai", chat: "https://api.openai.example/v1" };
const codex = {
  id: "codex", name: "Codex", icon: "codex-color", chat: "", responses: "", anthropic: "", catalog: "",
  models: [{ id: "gpt-6", name: "GPT-6", on: true }], agents: [], fallback: [], headers: {}, keyList: [],
  account: { agent: "codex", agentName: "Codex", user: "me@example.com", plan: "PLUS", logins: [{ user: "me@example.com", plan: "PLUS", active: true, on: true }] },
  proxy: "",
};

function serve(lang, posts) {
  const providers = {
    providers: [agg, kept, openai, codex],
    presets: [{ id: "openai", name: "OpenAI", kind: "vendor", icon: "openai", chat: "https://api.openai.example/v1", added: true }],
    excluded: [], gateway: { running: true, window: true },
  };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/provider/") && route.request().method() === "POST") {
      posts.push({ action: url.pathname.slice("/api/provider/".length), body: route.request().postDataJSON() });
      return json(providers);
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const strings = [
  "Rests", "Set it aside when out of quota or rate limited",
  "Off: a 429 or an out-of-quota refusal doesn't rest its keys, and the next request is sent to it as usual. For a relay whose limits pass in a moment.",
];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [1100, 420]) {
      const open = async (t, name) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        let page;
        t.after(async () => {
          if (process.env.ARTIFACT_DIR && page) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-${name}-limit-rest.png`), fullPage: true });
          }
          await browser.close();
        });
        page = await (await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const posts = [];
        await page.route("**/*", serve(lang, posts));
        await page.goto("http://uni-switch.test/?view=providers");
        const w = await page.evaluate(([lang, ks]) => Object.fromEntries(ks.map((k) => [k, (I18N[lang] || {})[k] || k])), [lang, [...strings, "Save"]]);
        await page.locator(".row.provider", { hasText: name }).click();
        await page.locator(".editor .bar").waitFor();
        return { page, errors, posts, w };
      };
      const adv = (page) => page.locator(".editor details.advanced");
      const unfold = async (page) => {
        await adv(page).waitFor({ state: "attached" });
        if (!(await adv(page).evaluate((d) => d.open))) await adv(page).locator(":scope > summary").click();
      };
      const row = (page, w) => page.locator(".editor label.tick", { hasText: w["Set it aside when out of quota or rate limited"] });
      const box = (page, w) => row(page, w).locator("input[type=checkbox]");
      const save = async (page, posts, w) => {
        const n = posts.length;
        await page.locator(".editor .bar").getByRole("button", { name: w.Save, exact: true }).click();
        for (let i = 0; i < 50 && posts.length === n; i++) await page.waitForTimeout(50);
        return posts.at(-1);
      };

      test(`${engine} ${lang} ${width}: a provider of the user's rests for a limit by default, and is saved not to`, async (t) => {
        const { page, errors, posts, w } = await open(t, "Local Aggregator");
        if (lang !== "en") {
          const missing = await page.evaluate(([lang, ks]) => ks.filter((k) => !(I18N[lang] || {})[k]), [lang, strings]);
          assert.deepEqual(missing, [], `every string has its ${lang}`);
        }
        assert.equal(await adv(page).evaluate((d) => d.open), false, "Advanced stays folded while it is as by default");
        await unfold(page);
        assert.equal(await row(page, w).isVisible(), true);
        assert.equal(await box(page, w).isChecked(), true, "ticked by default");
        assert.equal(await page.locator(".editor select").count(), 0, "no native select");
        await box(page, w).scrollIntoViewIfNeeded();
        const r = await row(page, w).boundingBox();
        assert.ok(r.width > 60 && r.x >= 0 && r.x + r.width <= width, `the row fits: ${JSON.stringify(r)}`);
        assert.ok(await page.locator(".editor", { hasText: w[strings[2]] }).count() > 0, "the note is shown");
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true, "no sideways scroll");
        await box(page, w).uncheck();
        const saved = await save(page, posts, w);
        assert.equal(saved.action, "save");
        assert.equal(saved.body.id, "agg");
        assert.equal(saved.body.limitRestOff, true);
        assert.deepEqual(errors, []);
      });

      test(`${engine} ${lang} ${width}: one saved not to rest opens unticked with Advanced open, and ticked is saved to rest`, async (t) => {
        const { page, errors, posts, w } = await open(t, "Kept Relay");
        assert.equal(await adv(page).evaluate((d) => d.open), true, "Advanced opens with it set");
        assert.equal(await box(page, w).isChecked(), false);
        await box(page, w).scrollIntoViewIfNeeded();
        await box(page, w).check();
        const saved = await save(page, posts, w);
        assert.equal(saved.body.id, "kept");
        assert.equal(saved.body.limitRestOff, false);
        assert.deepEqual(errors, []);
      });

      test(`${engine} ${lang} ${width}: a preset's and a sign-in's have no row, and a preset's Save sends none`, async (t) => {
        {
          const { page, errors, posts, w } = await open(t, "OpenAI");
          await unfold(page);
          assert.equal(await row(page, w).count(), 0);
          const saved = await save(page, posts, w);
          assert.equal(saved.action, "save");
          assert.equal("limitRestOff" in saved.body, false, `a preset's Save sends none: ${JSON.stringify(saved.body)}`);
          assert.deepEqual(errors, []);
        }
        const { page, errors, w } = await open(t, "Codex");
        await unfold(page);
        assert.equal(await row(page, w).count(), 0);
        assert.deepEqual(errors, []);
      });
    }
  }
}

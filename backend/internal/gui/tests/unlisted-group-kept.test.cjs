// Run with Node's test runner and Playwright on the module path; see README.md.
// hayden4608 on Discord: with a provider's "Only through routing groups"
// switched off, its models were still missing from the Agents page, while
// the editor said agents see them. A routing group whose Model lists is
// "The group alone" keeps its members out of every agent's list, this
// switch off or on, so the editor names that group under the models and on
// each chip, and says where to change it.
// Chromium and WebKit, wide and narrow, in every language.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const relay = {
  id: "relay", name: "Relay", icon: "generic", chat: "https://relay.example.com/v1", responses: "", anthropic: "", catalog: "",
  models: ["alpha", "beta", "gamma"].map((id) => ({ id, name: id, on: true })), chosen: ["alpha", "beta", "gamma"],
  groups: {}, unlisted: false, fallback: [], headers: {},
  agents: [{ id: "codex", name: "Codex", icon: "generic" }], keptBy: { beta: ["Alone"], gamma: ["Alone", "Solo"] },
  key: { set: true, masked: "sk-…one" }, keyList: [], balanceToken: { takes: false, set: false }, proxy: "",
};

const LINE = "The routing group {group} is listed alone, so agents don't see {models}: change its Model lists on the Routing page.";
const CHIP = "Kept out of agents' lists by {groups}, a routing group listed alone";
const fill = (s, v) => s.replace(/\{(\w+)\}/g, (_, k) => v[k]);

function serve(lang, p) {
  return async (r) => {
    const req = r.request(), url = new URL(req.url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state" || url.pathname === "/api/set") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) return new Promise(() => {});
      return json({ mine: true, now: new Date().toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups" || url.pathname.startsWith("/api/groups/")) return json({ groups: [], pools: [], deciders: [], models: [] });
    if (url.pathname === "/api/providers") return json({ providers: [p], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999", groups: [] } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await r.fulfill({ body: await fs.readFile(file), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [1000, 420]) {
      test(`${engine} ${lang} ${width}px: the editor names the group listed alone that keeps a model from agents`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
        t.after(() => browser.close());
        const errors = [];
        const open = async (p) => {
          const page = await (await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" })).newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", serve(lang, p));
          await page.goto("http://uni-switch.test/?view=providers");
          await page.locator(".row.provider", { hasText: "Relay" }).click();
          await page.locator(".editor .mchip").first().waitFor();
          return page;
        };
        let page = await open(relay);
        const w = await page.evaluate(([keys, lang]) => Object.fromEntries(keys.map((k) => [k, lang === "en" ? k : I18N[lang][k]])), [[LINE, CHIP, "Only through routing groups"], lang]);
        assert.deepEqual(Object.entries(w).filter(([, v]) => !v).map(([k]) => k), [], "every string has its words");

        const hid = page.locator(".editor .agent-hides");
        assert.ok(await hid.isVisible(), "said under the models");
        assert.deepEqual(await hid.locator("div").allTextContents(), [
          fill(w[LINE], { group: "Alone", models: "beta, gamma" }),
          fill(w[LINE], { group: "Solo", models: "gamma" }),
        ]);
        const chip = (id) => page.locator(".editor .mchip", { hasText: new RegExp(`^${id}`) });
        assert.ok((await chip("gamma").getAttribute("title")).includes(fill(w[CHIP], { groups: "Alone, Solo" })), "the chip says it too");
        assert.ok(!(await chip("alpha").getAttribute("title") || "").includes(fill(w[CHIP], { groups: "Alone" })), "only on the ones kept out");
        const box = await hid.boundingBox();
        assert.ok(box.x >= 0 && box.x + box.width <= width, `fits the width: ${JSON.stringify(box)}`);
        assert.equal(await page.evaluate(() => document.scrollingElement.scrollWidth <= innerWidth), true, "no sideways scroll");
        assert.equal(parseFloat(await hid.evaluate((e) => getComputedStyle(e).borderLeftWidth)), 0, "no left-border accent");

        // kept for routing groups: no agent lists them anyway
        const tick = page.locator(".editor label", { hasText: w["Only through routing groups"] }).locator("input");
        await tick.check();
        assert.ok(await hid.isHidden(), "not said while no agent lists them");
        assert.ok(!(await chip("beta").getAttribute("title")).includes(fill(w[CHIP], { groups: "Alone" })));
        await tick.uncheck();
        assert.ok(await hid.isVisible(), "said again when unticked");

        // no group keeps them out: nothing said
        page = await open({ ...relay, keptBy: undefined });
        assert.ok(await page.locator(".editor .agent-hides").isHidden());
        assert.deepEqual(errors, []);
      });
    }
  }
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// Kylin_Cloud on X: models left out of an agent's own list on the Agents
// page still looked ticked in the provider's editor, as if one setting had
// failed to sync. They are two settings (the picks are every agent's), so
// the editor says, under the models and on each chip, which agent's list
// leaves which ticked model out. While the provider is kept for routing
// groups no agent lists its models, and a picked chip says so instead.
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
  agents: [{ id: "codex", name: "Codex", icon: "generic", hides: ["beta"] }, { id: "claude", name: "Claude Code", icon: "generic" }],
  key: { set: true, masked: "sk-…one" }, keyList: [], balanceToken: { takes: false, set: false }, proxy: "",
};

const LINE = "{agent}'s model list leaves out {models}: shown again from there, on the Agents page.";
const CHIP = "Left out of {agents}'s model list, on the Agents page";
const PICKED = "Picked: the routing groups it is in use it. Agents don't see it.";
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
      test(`${engine} ${lang} ${width}px: the editor says which agent's list leaves a ticked model out`, async (t) => {
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
        const w = await page.evaluate(([keys, lang]) => Object.fromEntries(keys.map((k) => [k, lang === "en" ? k : I18N[lang][k]])), [[LINE, CHIP, PICKED, "Only through routing groups"], lang]);
        assert.deepEqual(Object.entries(w).filter(([, v]) => !v).map(([k]) => k), [], "every string has its words");

        const hid = page.locator(".editor .agent-hides");
        assert.ok(await hid.isVisible(), "said under the models");
        assert.equal(await hid.textContent(), fill(w[LINE], { agent: "Codex", models: "beta" }));
        const chip = (id) => page.locator(".editor .mchip", { hasText: new RegExp(`^${id}`) });
        assert.ok((await chip("beta").getAttribute("title")).includes(fill(w[CHIP], { agents: "Codex" })), "the chip says it too");
        assert.ok(!(await chip("alpha").getAttribute("title") || "").includes(fill(w[CHIP], { agents: "Codex" })), "only on the one left out");
        const box = await hid.boundingBox();
        assert.ok(box.x >= 0 && box.x + box.width <= width, `fits the width: ${JSON.stringify(box)}`);
        assert.equal(await page.evaluate(() => document.scrollingElement.scrollWidth <= innerWidth), true, "no sideways scroll");
        assert.equal(parseFloat(await hid.evaluate((e) => getComputedStyle(e).borderLeftWidth)), 0, "no left-border accent");

        // kept for routing groups: no agent lists them, so no agent's list
        // is named, and a picked chip says the groups use it
        const tick = page.locator(".editor label", { hasText: w["Only through routing groups"] }).locator("input");
        const y = await page.evaluate(() => document.scrollingElement.scrollTop);
        await tick.check();
        assert.equal(await page.evaluate(() => document.scrollingElement.scrollTop), y, "a click never scrolls the page");
        assert.ok(await hid.isHidden(), "not said while no agent lists them");
        assert.ok((await chip("beta").getAttribute("title")).includes(w[PICKED]));
        assert.ok(!(await chip("beta").getAttribute("title")).includes(fill(w[CHIP], { agents: "Codex" })));
        await tick.uncheck();
        assert.ok(await hid.isVisible(), "said again when unticked");

        // saved as kept for routing groups, it opens that way
        page = await open({ ...relay, unlisted: true });
        assert.ok(await page.locator(".editor .agent-hides").isHidden());
        assert.ok((await page.locator(".editor .mchip", { hasText: /^alpha/ }).getAttribute("title")).includes(w[PICKED]));

        // nothing left out: nothing said
        page = await open({ ...relay, agents: [{ id: "codex", name: "Codex", icon: "generic" }] });
        assert.ok(await page.locator(".editor .agent-hides").isHidden());
        assert.deepEqual(errors, []);
      });
    }
  }
}

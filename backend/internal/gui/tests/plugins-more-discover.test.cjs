// Run with Node's test runner and Playwright on the module path; see README.md.
// The add sheet's More in Plugins always opens Plugins on Discover, where
// the subscriptions' plugins are, whichever tab of Plugins was open last:
// after Built-in it opened Built-in (outloudvi on Discord). Its search's
// "look for a plugin" too. From the tray panel, both ask the window to open
// on Discover, with what was looked for, and the window opened so does,
// once (#3579). English and Chinese; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

const listings = [
  { package: "the retired opencode-zed-auth", name: "Zed", icon: "zed", providers: ["zed"], community: true,
    summary: { en: "Your Zed plan.", zh: "你的 Zed 订阅。" }, npm: { version: "0.1.0", weekly: 120 } },
];

// opened, given for the tray panel, collects what it asks the window to open;
// updates.waiting, once set, is a plugin update waiting, known on each load
// before the window's first page is shown (start.js waits for its answer;
// a routed page has no HTTP cache, so every load asks for start.js again)
function server(lang, fetched, opened = null, updates = {}) {
  let told, known;
  return async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/" && updates.waiting) known = new Promise((r) => { told = r; });
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") {
      // the panel draws its add sheet on a first use, with no provider yet
      return json({ providers: opened ? [] : [{ id: "openai", name: "OpenAI", icon: "openai", preset: "openai", models: [], agents: [], key: { set: true, masked: "sk-…ab12" } }],
        presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    }
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname === "/api/plugins") return json({ bun: true, bunVersion: "1.3.0", plugins: [] });
    if (url.pathname === "/api/plugins/listings") return json({ listings });
    if (url.pathname === "/api/plugins/market") return json({ listings, state: { bun: true, plugins: [] } });
    if (url.pathname === "/api/plugins/search") return json({ hits: [] });
    if (url.pathname === "/api/plugins/builtin") return json([{ id: "usage", name: "Usage", sub: "Your allowances", on: true, chosen: true, source: "preset", state: "active", inUse: 0 }]);
    if (url.pathname === "/api/window/main" && opened) { opened.push(url.search); return route.fulfill({ status: 204 }); }
    if (url.pathname === "/api/plugins/updates" && updates.waiting) { await json({ waiting: updates.waiting, updated: [] }); return told(); }
    if (url.pathname.startsWith("/api/")) return json({});
    if (!/^\/[\w./-]*$/.test(url.pathname) || url.host !== "uni-switch.test") { fetched.push(url.href); return route.fulfill({ status: 404, body: "" }); }
    if (url.pathname === "/start.js" && updates.waiting) await known;
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const L = {
  en: { builtin: "Built-in", discover: "Discover", installed: "Installed", lookFor: "look for a plugin" },
  zh: { builtin: "内置", discover: "发现", installed: "已安装", lookFor: "找找插件" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": More in Plugins opens Discover", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh"]) {
      await t.test(lang, async () => {
        const w = L[lang];
        const page = await (await browser.newContext({ viewport: { width: 980, height: 820 } })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], fetched = [], updates = {};
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, fetched, null, updates));
        const view = page.locator("#view-plugins");
        const tabOn = () => view.locator(".lib-tabs .opt[aria-pressed=true]").innerText();
        const pick = (name) => page.locator(":is(.lib-tabs .opt, .nav-s):visible", { hasText: name }).first().click();
        const toProviders = async () => {
          await page.locator('#nav button[data-view="providers"]').click();
          await page.locator("#view-providers").waitFor({ state: "visible" });
          // the add sheet, open still from the last search or opened now,
          // with nothing looked for
          if (!await page.locator("#addSheet input.find").isVisible()) await page.locator("#addProvider").click();
          await page.locator("#addSheet input.find").fill("");
        };
        const expectDiscover = async (why, q = "") => {
          await view.waitFor({ state: "visible" });
          await page.waitForFunction(() => document.querySelector("#view-plugins .lib-tabs .opt[aria-pressed=true]"));
          assert.equal((await tabOn()).trim(), w.discover, why);
          assert.equal(await view.locator(".pm-find input").inputValue(), q, why);
          if (!q) await view.locator('.pm-card[data-pkg="the retired opencode-zed-auth"]').waitFor();
          assert.equal(await view.locator("#builtinList").count(), 0, why);
        };

        await page.goto("http://uni-switch.test/?view=plugins");
        for (const [last, name] of [["builtin", w.builtin], ["installed", w.installed]]) {
          // a tab of Plugins other than Discover, then back to Providers
          await view.locator(".lib-tabs").waitFor();
          await pick(name);
          await page.waitForFunction((n) => document.querySelector("#view-plugins .lib-tabs .opt[aria-pressed=true]")?.textContent.startsWith(n), name);
          if (last === "builtin") await view.locator("#builtinList .row, #builtinList > *").first().waitFor();
          await toProviders();
          await page.locator("#addSheet .tile.more-plugins").click();
          await expectDiscover("More in Plugins after " + last);

          // the search's way there too
          await pick(name);
          await toProviders();
          await page.locator("#addSheet input.find").fill("fakeco");
          await page.locator("#addSheet .none button", { hasText: w.lookFor }).click();
          await expectDiscover("look for a plugin after " + last, "fakeco");
          await view.locator(".pm-find input").fill("");
        }

        // the tray panel's More in Plugins, and its search's way: the
        // window is asked to open Plugins on Discover, with what was
        // looked for, a space and an & in it too
        const panel = await (await browser.newContext({ viewport: { width: 560, height: 720 } })).newPage();
        panel.setDefaultTimeout(5000);
        panel.setDefaultNavigationTimeout(60000);
        panel.on("pageerror", (e) => errors.push(e.message));
        const opened = [];
        await panel.route("**/*", server(lang, fetched, opened));
        await panel.goto("http://uni-switch.test/?mode=panel");
        await panel.evaluate(() => show("providers"));
        await panel.locator("#addSheet .tile.more-plugins").click();
        for (let i = 0; i < 250 && opened.length < 1; i++) await panel.waitForTimeout(20);
        await panel.locator("#addSheet input.find").fill("fake co&x");
        await panel.locator("#addSheet .none button", { hasText: w.lookFor }).click();
        for (let i = 0; i < 250 && opened.length < 2; i++) await panel.waitForTimeout(20);
        assert.deepEqual(opened, ["?view=plugins&q=", "?view=plugins&q=fake%20co%26x"]);
        await panel.context().close();

        // the window at the address mainView makes of those (TestMainView),
        // with Installed the tab visited last: Discover, with what was looked
        // for, and the address gives it up, so a reload opens Installed again
        for (const [q, find] of [["fake+co%26x", "fake co&x"], ["", ""]]) {
          await page.goto("http://uni-switch.test/?view=plugins&q=" + q);
          await expectDiscover("the window the panel opened, q " + JSON.stringify(q), find);
          assert.equal(await page.evaluate(() => location.search), "?view=plugins");
        }
        await page.reload();
        await page.waitForFunction((n) => document.querySelector("#view-plugins .lib-tabs .opt[aria-pressed=true]")?.textContent.startsWith(n), w.installed);

        // with a plugin's update waiting, Plugins opens on Installed, where
        // it is (show); the window the panel opened shows Discover still
        updates.waiting = [{ package: "the retired opencode-zed-auth", latest: "0.2.0" }];
        await page.goto("http://uni-switch.test/?view=plugins");
        await page.waitForFunction((n) => document.querySelector("#view-plugins .lib-tabs .opt[aria-pressed=true]")?.textContent.startsWith(n), w.installed);
        await page.goto("http://uni-switch.test/?view=plugins&q=");
        await expectDiscover("the window the panel opened, an update waiting");

        assert.deepEqual(fetched, [], "nothing fetched from elsewhere");
        assert.deepEqual(errors, []);
      });
    }
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// Built-in subscriptions that a community plugin can run are deprecated: their
// rows and Add tiles carry a Deprecated badge whose tooltip says why (a
// subscription can break its vendor's terms, so it is decoupled from uni-switch to
// keep uni-switch itself from being banned), and a notice over the list names the
// signed-in ones with the same reason. One already on its plugin, or a
// subscription with no plugin, carries no badge; Not now hides the notice
// until another deprecated subscription signs in. One with no account isn't
// in the Add sheet at all (yetone: 对于新用户来说，这里应该只显示内置的
// provider): a search naming it offers More in Plugins, opened on that
// search, where it is installed. In English and Chinese, Chromium and
// WebKit; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const pkg = (id) => `the retired opencode-${id}-auth`;
const sub = (id, name, state) => ({
  id, name, icon: id, chat: "", responses: "", anthropic: "", catalog: "", models: [{ id: "m", name: "M", on: true }],
  agents: [], fallback: [], headers: {}, keyList: [], key: {},
  account: { agent: id, agentName: name, user: "ada", logins: [{ user: "ada", active: true, on: true, own: true }] },
  move: { package: pkg(id), state },
});

function serve(lang, calls) {
  const onPlugins = ["grok"];
  const plugins = [];
  const providers = [
    sub("cursor", "Cursor", ""), sub("kiro", "Kiro", ""), sub("grok", "Grok", "plugin"),
    { id: "deepseek", name: "DeepSeek", icon: "deepseek", chat: "https://api.deepseek.com", responses: "", anthropic: "", catalog: "", models: [{ id: "m", name: "M", on: true }], agents: [], fallback: [], headers: {}, keyList: [], key: { set: true } },
  ];
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    const state = () => ({ providers, presets: [], excluded: [], gateway: { running: true, window: true }, plugins, onPlugins, movable: ["cursor", "grok", "kiro", "zed"],
      movesTo: Object.fromEntries(["cursor", "grok", "kiro", "zed"].map((id) => [id, pkg(id)])) });
    if (url.pathname === "/api/providers") return json(state());
    if (url.pathname === "/api/plugin-signin/prompt") calls.push("plugin sign-in " + route.request().postDataJSON().provider);
    if (url.pathname === "/api/signin") calls.push("built-in sign-in " + route.request().postDataJSON().agent);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins/listings") return json({ listings: [{ name: "Zed", package: pkg("zed"), providers: ["zed"], community: true, summary: "Zed's hosted models" }] });
    if (url.pathname === "/api/plugins/search") return json({ hits: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const L = {
  en: { badge: "Deprecated", why: "So that uni-switch itself isn't banned over them", head: "These built-in subscriptions are deprecated: Cursor, Kiro", later: "Not now",
    more: "More in Plugins" },
  zh: { badge: "已弃用", why: "为免 uni-switch 本体因此被封禁", head: "以下内置订阅已弃用：Cursor、Kiro", later: "暂不",
    more: "插件中还有更多" },
};

// The notice after a load is the page's own boot, not an action on a
// loaded page (#3484): goto returns at the load event, and only then are
// /api/state and /api/providers read and the rows drawn. Under the whole
// suite's load that boot ran past the 5s default meant for actions; it is
// timed as the navigation it finishes (60s, #4470), while the waits inside
// a loaded page keep the quick 5s.
const boot = 60000;

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: deprecated built-in subscriptions are badged and explained`, async (t) => {
      const w = L[lang];
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 900, height: 1000 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      // A reload kills the page's own background reads (the web update
      // poll, What's new), and WebKit reports each killed fetch as a page
      // error on the page it leaves (#1350, #3484): not this test's.
      page.on("pageerror", (e) => {
        if (engine === "webkit" && /^Fetch API cannot load .* due to access control checks/.test(e.message)) return;
        errors.push(e.message);
      });
      const calls = [];
      await page.route("**/*", serve(lang, calls));
      await page.goto("http://uni-switch.test/?view=providers");

      // the notice: the signed-in deprecated ones, and why
      const notice = page.locator("#movable .deprecation");
      await notice.waitFor({ timeout: boot });
      const said = await notice.innerText();
      assert.ok(said.includes(w.head), `the notice doesn't name Cursor and Kiro: ${said}`);
      assert.ok(said.includes(w.why), "the notice doesn't say why");
      assert.ok(!said.includes("Grok"), "names Grok, already on its plugin");

      // the rows: badged with the reason in the tooltip, but not Grok or DeepSeek
      for (const id of ["cursor", "kiro"]) {
        const b = page.locator(`.row.provider[data-id="${id}"] .badge.deprecated`);
        assert.equal((await b.textContent()), w.badge, `${id}'s badge`);
        assert.ok((await b.getAttribute("title")).includes(w.why), `${id}'s badge doesn't say why`);
      }
      for (const id of ["grok", "deepseek"]) assert.equal(await page.locator(`.row.provider[data-id="${id}"] .badge.deprecated`).count(), 0, `${id} is badged`);

      // the Add sheet: Cursor and Kiro (signed in) badged; Grok (on its
      // plugin) and Claude not; Zed (no account) not there at all
      const sheet = page.locator("#addSheet");
      await page.locator(".after-list button").first().click();
      const cursor = page.locator('.tile[data-pick="Cursor"]');
      await cursor.waitFor();
      assert.equal(await cursor.locator(".badge.deprecated").count(), 1, "Cursor's tile isn't badged");
      for (const name of ["Claude", "Grok"]) assert.equal(await page.locator(`.tile[data-pick="${name}"] .badge.deprecated`).count(), 0, `${name}'s tile is badged`);
      assert.equal(await page.locator('.tile[data-pick="Grok"]').count(), 1, "Grok, on its plugin, isn't there");
      assert.equal(await page.locator('.tile[data-pick="Zed"]').count(), 0, "Zed, with no account, is offered");
      assert.equal(await sheet.locator(".deprecated").count(), 2, "more than Cursor's and Kiro's badges in the sheet");
      // a search for Zed: More in Plugins, which opens Plugins on it
      await sheet.locator("input.find").fill("zed");
      const more = sheet.locator('.tile[data-pick="plugins"]');
      await more.waitFor();
      assert.equal(await page.locator('.tile[data-pick="Zed"]').count(), 0, "the search offers Zed's built-in");
      assert.equal((await more.innerText()).trim(), w.more);
      await more.click();
      await page.locator("#view-plugins").waitFor();
      assert.equal(await page.locator(".pm-find input").inputValue(), "zed", "Plugins isn't opened on the search");
      await page.locator(`#view-plugins .pm-card[data-pkg="${pkg("zed")}"]`).waitFor();
      assert.deepEqual(calls, [], "a sign-in or an install started by itself");

      // Not now hides the notice, and it stays hidden on the next load
      await page.goto("http://uni-switch.test/?view=providers");
      await notice.waitFor({ timeout: boot });
      await notice.locator("button", { hasText: w.later }).click();
      await page.locator("#movable").waitFor({ state: "hidden" });
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator('.row.provider[data-id="cursor"] .badge.deprecated').waitFor({ timeout: boot });
      assert.equal(await page.locator("#movable").isHidden(), true, "the notice came back");
      assert.deepEqual(errors, []);
    });
  }
}

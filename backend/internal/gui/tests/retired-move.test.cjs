// Run with Node's test runner and Playwright on the module path; see README.md.
// A retired built-in subscription (Zed here) whose accounts haven't moved
// onto its community plugin yet is served by nothing, so it has no row: a
// quiet line over the list says so, with its move. A move that fails says
// why in the line, in the reader's language, and the button becomes "Try
// again"; one that goes through takes the line away and lists Zed as its
// plugin's, whose editor says the plugin runs it with no way back and shows
// no plugin:// endpoints to test. The add sheet offers Zed once, as the
// plugin's, and before the move, with no plugin installed, not at all: it
// is the Plugins page's. In English and Chinese; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const pkg = "the retired opencode-zed-auth";

function serve(lang, posts, hold) {
  let move = { package: pkg, state: "" };
  let tries = 0;
  const payload = () => {
    const onPlugin = move.state === "plugin";
    const zed = {
      id: "zed", name: "Zed", icon: "zed", chat: "plugin://zed/v1", responses: "", anthropic: "plugin://zed", catalog: "",
      models: Array.from({ length: 30 }, (_, i) => ({ id: "m" + i, name: "Model " + i, on: i === 0 })),
      agents: [], fallback: [], headers: {}, keyList: [], key: {},
      account: { agent: "zed", agentName: "Zed", user: "ada", logins: [{ user: "ada", active: true, on: true, own: true }, { user: "bob", on: true }] },
      move: { package: pkg, state: "plugin" },
    };
    return {
      providers: onPlugin ? [zed] : [], presets: [], excluded: [], gateway: { running: true, window: true },
      plugins: onPlugin ? [{ id: "zed", pid: "zed", name: "Zed", icon: "zed", spec: pkg, signedIn: true, models: 1, methods: [] }] : [],
      onPlugins: onPlugin ? ["zed"] : [],
      movable: ["zed"], movesTo: { zed: pkg },
      unmoved: onPlugin ? [] : [{ id: "zed", name: "Zed", package: pkg, accounts: 2, ...(move.why ? { why: move.why, error: move.error } : {}) }],
    };
  };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json(payload());
    if (url.pathname === "/api/groups") return json({ groups: [] });
    // the page asks for the market in parts (#488)
    if (url.pathname === "/api/plugins/market" || url.pathname === "/api/plugins" || url.pathname === "/api/plugins/listings") {
      const m = ({ listings: [{ package: pkg, name: "Zed", icon: "zed", providers: ["zed"], community: true, replaces: "zed", summary: { en: "Zed", zh: "Zed" }, npm: { version: "0.1.0" } }],
        state: { bun: true, bunVersion: "1.3.0", plugins: [], movable: move.state === "plugin" ? [] : [{ id: "zed", name: "Zed", package: pkg, accounts: 2 }] } });
      return json(url.pathname === "/api/plugins" ? m.state : url.pathname === "/api/plugins/listings" ? { listings: m.listings } : m);
    }
    if (url.pathname === "/api/provider/move") {
      posts.push({ path: url.pathname, body: route.request().postDataJSON() });
      // the first move is held until the test has read the busy button,
      // so a slow machine can't end it before the button is looked at
      if (!tries) await new Promise((r) => (hold.release = r));
      // the first move can't reach npm; the second goes through
      if (!tries++) {
        move = { package: pkg, state: "failed", error: "uni-switch couldn't reach npm to install the plugin.", why: { code: "offline" } };
        return route.fulfill({ status: 400, json: { error: move.error, why: move.why } });
      }
      move = { package: pkg, state: "plugin" };
      return json(payload());
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const L = {
  en: { line: "Zed's built-in subscription is retired, and its accounts haven't moved to its plugin yet",
    move: "Move to the plugin", busy: "Installing the plugin and checking each account…", again: "Try again", retired: "uni-switch's built-in is retired, so this subscription stays on its plugin.",
    failed: "Nothing serves them until they move: uni-switch couldn't reach npm to install the plugin. Check the network or proxy, then try again.",
    onPlugin: "The community Zed plugin", done: "Zed now runs on its plugin — 2 accounts, 1 model.",
    subs: "Subscriptions", card: "Move" },
  zh: { line: "Zed 的内置订阅已下线，它的账号还没迁移到插件",
    move: "迁移到插件", busy: "正在安装插件并逐个检查账号…", again: "重试", retired: "uni-switch 的内置已下线，此订阅一直由插件运行。",
    failed: "迁移之前，它们无法使用：无法连接 npm 安装插件，请检查网络或代理后重试。",
    onPlugin: "社区 Zed 插件", done: "Zed 已改由插件运行：2 个账号，1 个模型。",
    subs: "订阅", card: "迁移" },
};

const launch = (engine) => engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" });

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a retired subscription's accounts move to its plugin from the line over the list`, async (t) => {
      const w = L[lang];
      const browser = await launch(engine);
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 900, height: 700 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [], posts = [], hold = {};
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, posts, hold));
      await page.goto("http://uni-switch.test/?view=providers");

      // no row: nothing serves it; the line over the list says so
      const line = page.locator("#movable");
      const btn = line.locator("button");
      await btn.waitFor();
      assert.ok((await line.innerText()).includes(w.line), "no line saying Zed's accounts haven't moved");
      assert.equal(await page.locator('#providers .row.provider[data-id="zed"]').count(), 0, "a row for the retired built-in");
      assert.equal(await btn.innerText(), w.move);
      assert.equal(await btn.getAttribute("title"), pkg, "the package isn't in the tooltip");
      assert.ok(!(await line.innerText()).includes(pkg), "names the npm package in the sentence");
      // nothing serves it and it has no row, so the add sheet (open from
      // the start: the list is empty) leaves it to the Plugins page
      const sheet = page.locator("#addSheet");
      if (await sheet.isHidden()) await page.locator("#addProvider").click();
      await sheet.locator(".kind b", { hasText: w.subs }).waitFor();
      assert.equal(await sheet.locator('.tile[data-pick="Zed"]').count(), 0, "the add sheet offers a retired built-in nothing serves");

      await btn.click();
      await page.waitForFunction((busy) => document.querySelector("#movable button")?.textContent === busy, w.busy);
      assert.equal(await btn.isDisabled(), true, "the button can be pressed again mid-move");
      // let the held move go, once its request is in
      for (let i = 0; !hold.release && i < 250; i++) await page.waitForTimeout(20);
      assert.ok(hold.release, "the move was never asked");
      hold.release();
      // it fails: said in the line, and Try again
      await page.waitForFunction((want) => document.querySelector("#movable")?.innerText.includes(want), w.failed);
      assert.equal(await btn.innerText(), w.again);
      assert.equal(await btn.isDisabled(), false);
      await page.evaluate(() => renderProviders());
      assert.ok((await line.innerText()).includes(w.failed), "a redraw forgot why the move failed");

      // tried again, it goes through: the line goes, Zed is its plugin's
      await btn.click();
      await page.waitForFunction((s) => document.querySelector("#status").textContent.startsWith(s), w.done);
      assert.equal(await line.isVisible(), false, "the line still offers the move");
      assert.deepEqual(posts.map((p) => [p.path, p.body.id]), [["/api/provider/move", "zed"], ["/api/provider/move", "zed"]]);
      await page.locator("#providers .row.provider", { hasText: "Zed" }).first().click();
      const ed = page.locator("#modal .editor");
      await ed.locator(".hint", { hasText: w.retired }).waitFor();
      assert.equal(await ed.locator(".move button").count(), 0, "a moved subscription offers a way back");
      assert.ok((await ed.innerText()).includes(w.onPlugin), "doesn't say the plugin runs it");
      // its plugin:// URLs are uni-switch's own: no Endpoints, no Test
      assert.equal(await ed.locator(".eps").count(), 0, "shows the plugin:// endpoints");
      if (process.env.ARTIFACT_DIR) await ed.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `plugin-moved-${engine}-${lang}.png`) });
      await page.keyboard.press("Escape");

      // the add sheet offers Zed once, as the plugin's
      await page.locator("#modal").waitFor({ state: "hidden" });
      if (await sheet.isHidden()) await page.locator("#addProvider").click();
      await sheet.locator(".kind b", { hasText: w.subs }).waitFor();
      assert.equal(await sheet.locator('.tile[data-pick="Zed"]').count(), 1);
      await page.keyboard.press("Escape");

      assert.deepEqual(errors, []);
    });
  }
}

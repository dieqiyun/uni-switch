// Run with Node's test runner and Playwright on the module path; see README.md.
// A built-in subscription a community plugin can run (Zed here). The
// providers list says so in a quiet line whose link opens its editor; the
// editor says which runs it, and "Move to the plugin" says what it is doing
// on the button. A move that fails says why right under it, in the
// reader's language, and the button becomes "Try again"; one that goes
// through turns the field over in the editor, still open where it was, with
// no way back: uni-switch's built-in is retired. The add sheet no longer
// offers the built-in Zed once it runs on its plugin, and its editor shows
// no plugin:// endpoints to test. The Plugins tab, opened as the page
// (?view=plugins), shows its market and offers Zed's accounts on the
// plugin's card. In English and Chinese; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const pkg = "the retired opencode-zed-auth";

function serve(lang, posts) {
  let move = { package: pkg, state: "" };
  let tries = 0;
  const payload = () => {
    const onPlugin = move.state === "plugin";
    const zed = {
      id: "zed", name: "Zed", icon: "zed", chat: onPlugin ? "plugin://zed/v1" : "", responses: "", anthropic: onPlugin ? "plugin://zed" : "", catalog: "",
      // enough models that the editor scrolls to reach Runs on
      models: Array.from({ length: 30 }, (_, i) => ({ id: "m" + i, name: "Model " + i, on: i === 0 })),
      agents: [], fallback: [], headers: {}, keyList: [], key: {},
      account: { agent: "zed", agentName: "Zed", user: "ada", logins: [{ user: "ada", active: true, on: true, own: true }, { user: "bob", on: true }] },
      move,
    };
    return {
      providers: [zed], presets: [], excluded: [], gateway: { running: true, window: true },
      plugins: onPlugin ? [{ id: "zed", pid: "zed", name: "Zed", icon: "zed", spec: pkg, signedIn: true, models: 1, methods: [] }] : [],
      onPlugins: onPlugin ? ["zed"] : [],
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
    if (url.pathname === "/api/provider/move" || url.pathname === "/api/provider/moveback") {
      posts.push({ path: url.pathname, body: route.request().postDataJSON() });
      await new Promise((r) => setTimeout(r, 400)); // long enough to read the button
      // the first move can't reach npm; the second goes through
      if (url.pathname.endsWith("/move") && !tries++) {
        move = { package: pkg, state: "failed", error: "uni-switch couldn't reach npm to install the plugin.", why: { code: "offline" } };
        return route.fulfill({ status: 400, json: { error: move.error, why: move.why } });
      }
      move = { package: pkg, state: url.pathname.endsWith("moveback") ? "back" : "plugin" };
      return json(payload());
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const L = {
  en: { runs: "Runs on", line: "Zed's built-in subscription is deprecated", look: "Review the move",
    move: "Move to the plugin", busy: "Installing the plugin and checking each account…", again: "Try again", retired: "uni-switch's built-in is retired, so this subscription stays on its plugin.",
    failed: "It stays built-in: uni-switch couldn't reach npm to install the plugin. Check the network or proxy, then try again.",
    onPlugin: "The community Zed plugin", builtin: "uni-switch's built-in · or the community Zed plugin", done: "Zed now runs on its plugin — 2 accounts, 1 model.",
    subs: "Subscriptions", card: "Move", own: "Zed itself stays signed in as it is." },
  zh: { runs: "运行方式", line: "Zed 的内置订阅已弃用", look: "查看迁移",
    move: "迁移到插件", busy: "正在安装插件并逐个检查账号…", again: "重试", retired: "uni-switch 的内置已下线，此订阅一直由插件运行。",
    failed: "仍使用内置：无法连接 npm 安装插件，请检查网络或代理后重试。",
    onPlugin: "社区 Zed 插件", builtin: "uni-switch 内置 · 也可改用社区 Zed 插件", done: "Zed 已改由插件运行：2 个账号，1 个模型。",
    subs: "订阅", card: "迁移", own: "Zed 本身的登录不变。" },
};

const launch = (engine) => engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" });

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a built-in subscription moves to its plugin for good`, async (t) => {
      const w = L[lang];
      const browser = await launch(engine);
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 900, height: 700 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [], posts = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, posts));
      await page.goto("http://uni-switch.test/?view=providers");

      // the deprecation notice over the list opens Zed's editor
      const line = page.locator("#movable");
      await line.locator("button", { hasText: w.look }).waitFor();
      assert.ok((await line.innerText()).includes(w.line), "no notice saying Zed is deprecated");
      await line.locator("button", { hasText: w.look }).click();

      const ed = page.locator("#modal .editor");
      const btn = ed.locator(".move button");
      await btn.waitFor();
      assert.equal(await btn.innerText(), w.move);
      const text = await ed.innerText();
      assert.ok(text.includes(w.runs), "no Runs on field");
      assert.ok(text.includes(w.builtin), "doesn't say it's built-in");
      assert.equal(await ed.locator(".move").getAttribute("title"), null);
      assert.equal(await ed.locator(".move > div").first().getAttribute("title"), pkg, "the package isn't in the tooltip");
      assert.ok(!text.includes(pkg), "names the npm package in the sentence");
      assert.ok(text.includes(w.own), "doesn't say Zed stays signed in");

      // the reader opened Advanced (so the editor runs on past the field)
      // and scrolled to the field: nothing the move does moves it, and the
      // fold stays as they left it though the move starts a new draft
      await ed.locator("details.advanced > summary").click();
      assert.equal(await ed.locator("details.advanced").evaluate((d) => d.open), true);
      // the reader scrolled to the field: nothing the move does moves it.
      // A wheel, as a reader's scroll (a script's scrollTop is put back), and
      // only so far that the button stays in sight: a click on a button out
      // of sight is Playwright's own scroll first, not the reader's
      const body = ed.locator(".ebody"), box = await body.boundingBox();
      const at = () => body.evaluate((e) => e.scrollTop);
      await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
      for (let i = 0; i < 20 && (await btn.boundingBox()).y > box.y + 40; i++) {
        const was = await at();
        await page.mouse.wheel(0, 30);
        await page.waitForTimeout(80);
        if (await at() === was) break;
      }
      // WebKit can still be applying the last wheel when the loop ends
      // (367, then 397 a moment later): the place the reader left is
      // where the scroll settles, two reads 150ms apart agreeing
      let top = await at();
      for (let i = 0; i < 40; i++) {
        await page.waitForTimeout(150);
        const now = await at();
        if (now === top) break;
        top = now;
      }
      assert.ok(top > 0, "the editor doesn't scroll: the test proves nothing about keeping its place");
      // a press where the button is, as the reader's mouse does
      const press = async () => {
        const b = await btn.boundingBox();
        assert.ok(b.y >= box.y && b.y + b.height <= box.y + box.height, "the button is out of sight");
        await page.mouse.click(b.x + b.width / 2, b.y + b.height / 2);
      };

      await press();
      await page.waitForFunction((busy) => [...document.querySelectorAll("#modal .move button")].some((b) => b.textContent === busy), w.busy, { timeout: 300 });
      assert.equal(await btn.isDisabled(), true, "the button can be pressed again mid-move");
      // an account's allowance landing mid-move redraws the editor
      // (loginUsageOf): the move is still said to run, and its end is said
      // in the field drawn now
      await page.evaluate(() => renderProviders());
      assert.equal(await btn.innerText(), w.busy, "a redraw mid-move forgot the move");
      assert.equal(await btn.isDisabled(), true, "a redraw mid-move lets the button be pressed again");

      // it fails: said under the button, the editor open where it was
      const why = ed.locator(".move-why");
      await why.waitFor({ state: "visible" });
      assert.equal(await why.innerText(), w.failed);
      assert.equal(await btn.innerText(), w.again);
      assert.equal(await btn.isDisabled(), false);
      assert.equal(await body.evaluate((e) => e.scrollTop), top, "the failure moved the editor");
      await page.evaluate(() => renderProviders());
      assert.equal(await why.innerText(), w.failed, "a redraw forgot why the move failed");
      assert.equal(await btn.innerText(), w.again);
      if (process.env.ARTIFACT_DIR) await ed.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `plugin-move-failed-${engine}-${lang}.png`) });

      // tried again, it goes through: the same editor, turned over
      await press();
      await ed.locator(".hint", { hasText: w.retired }).waitFor();
      assert.equal(await ed.locator(".move button").count(), 0, "a moved subscription still offers a way back");
      assert.equal(await page.locator("#modal .editor").count(), 1, "the editor closed");
      assert.ok((await ed.innerText()).includes(w.onPlugin), "doesn't say the plugin runs it");
      // as the built-in said: the agent's own sign-in is where it was
      assert.ok((await ed.innerText()).includes(w.own), "the moved Zed no longer says Zed stays signed in");
      assert.ok(!(await why.count()) || !(await why.isVisible()), "the failure is still said");
      assert.equal(await body.evaluate((e) => e.scrollTop), top, "the move moved the editor");
      assert.equal(await ed.locator("details.advanced").evaluate((d) => d.open), true, "the move shut Advanced");
      assert.ok((await page.locator("#status").innerText()).startsWith(w.done), "no toast saying what moved");
      assert.deepEqual(posts.map((p) => [p.path, p.body.id]), [["/api/provider/move", "zed"], ["/api/provider/move", "zed"]]);
      // its plugin:// URLs are uni-switch's own: no Endpoints, no Test
      assert.equal(await ed.locator(".eps").count(), 0, "shows the plugin:// endpoints");
      assert.equal(await line.isVisible(), false, "the line still offers the move");
      if (process.env.ARTIFACT_DIR) await ed.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `plugin-moved-${engine}-${lang}.png`) });
      await page.keyboard.press("Escape");

      // the add sheet offers Zed once, as the plugin's
      await page.locator("#addProvider").click();
      const sheet = page.locator("#addSheet");
      await sheet.locator(".kind b", { hasText: w.subs }).waitFor();
      assert.equal(await sheet.locator('.tile[data-pick="Zed"]').count(), 1);
      await page.keyboard.press("Escape");

      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: the Plugins tab opened as the page offers Zed's accounts`, async (t) => {
      const w = L[lang];
      const browser = await launch(engine);
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 980, height: 820 } })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [], posts = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, posts));
      await page.goto("http://uni-switch.test/?view=plugins");
      const card = page.locator("#view-plugins button", { hasText: w.card });
      await card.waitFor();
      await card.click();
      // the first move fails (npm unreachable): said, and the card offers it again
      await page.waitForFunction((s) => document.querySelector("#status").textContent.startsWith(s), w.failed.split(":")[0] || w.failed.split("：")[0]);
      await card.waitFor();
      await card.click();
      await page.waitForFunction((s) => document.querySelector("#status").textContent.startsWith(s), w.done.split(" — ")[0].split("——")[0]);
      assert.deepEqual(posts.map((p) => [p.path, p.body.id]), [["/api/provider/move", "zed"], ["/api/provider/move", "zed"]]);
      assert.deepEqual(errors, []);
    });
  }
}

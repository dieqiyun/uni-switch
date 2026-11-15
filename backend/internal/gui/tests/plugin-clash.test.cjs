// Run with Node's test runner and Playwright on the module path; see README.md.
// Two installed plugins that sign in to one provider (neiko on Discord: a
// third-party plugin and their own folder plugin both name it). Both rows
// stay, each says which one serves the provider, and the other one offers
// "Use for …", which tells uni-switch to run the provider on it. Neither row
// reads "Signs in to nothing uni-switch can use". Chromium and WebKit, every
// language, wide and narrow; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

const MINE = "/Users/me/dev/my-acme";
const THEIRS = "opencode-acme-auth@latest";

function server(lang, asked) {
  let by = THEIRS;
  const clash = (spec) => [{ id: "acme", by, with: [spec === MINE ? THEIRS : MINE], name: "Acme" }];
  const installed = () => [
    { spec: MINE, providers: by === MINE ? ["Acme"] : [], version: "", clashes: clash(MINE) },
    { spec: THEIRS, providers: by === THEIRS ? ["Acme"] : [], version: "1.0.0", clashes: clash(THEIRS) },
  ];
  const state = () => ({ bun: true, bunVersion: "1.3.0", plugins: installed() });
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    const body = () => route.request().postDataJSON();
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [{ id: "acme", pid: "acme", name: "Acme", spec: by, signedIn: false, models: [] }] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname === "/api/plugins/market" || url.pathname === "/api/plugins" || url.pathname === "/api/plugins/listings") return json(url.pathname === "/api/plugins" ? state() : url.pathname === "/api/plugins/listings" ? { listings: [] } : { listings: [], state: state() });
    if (url.pathname === "/api/plugins/search") return json({ hits: [] });
    if (url.pathname === "/api/plugins/npm") return json({});
    if (url.pathname === "/api/plugins/prefer") {
      const b = body();
      asked.push(["prefer", b]);
      by = b.spec;
      return json({});
    }
    if (url.pathname.startsWith("/api/plugins/")) { asked.push([url.pathname, body()]); return json({}); }
    if (url.pathname.startsWith("/api/")) return json({});
    if (!/^\/[\w./-]*$/.test(url.pathname) || url.host !== "uni-switch.test") { asked.push(["fetched", url.href]); return route.fulfill({ status: 404, body: "" }); }
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

// what each row says, in each language
const L = {
  en: { installed: "Installed", nothing: "Signs in to nothing uni-switch can use", served: "Acme is served by {other}, which signs in to it too", serves: "{other} signs in to Acme too; this one serves it", use: "Use for Acme", done: "Acme now runs on my-acme" },
  zh: { installed: "已安装", nothing: "没有 uni-switch 能用的登录", served: "Acme 由同样登录它的 {other} 提供", serves: "{other} 也能登录 Acme，现由这个插件提供", use: "用于 Acme", done: "Acme 现在由 my-acme 提供" },
  "zh-TW": { installed: "已安裝", nothing: "沒有 uni-switch 能用的登入", served: "Acme 由同樣登入它的 {other} 提供", serves: "{other} 也能登入 Acme，現由這個外掛提供", use: "用於 Acme", done: "Acme 現在由 my-acme 提供" },
  ja: { installed: "インストール済み", nothing: "uni-switch で使えるサインインはありません", served: "Acme は、同じくサインインする {other} が提供しています", serves: "{other} も Acme にサインインします。このプラグインが提供中です", use: "Acme に使う", done: "Acme は my-acme で動くようになりました" },
  de: { installed: "Installiert", nothing: "Meldet bei nichts an, das uni-switch nutzen kann", served: "Acme wird von {other} bereitgestellt, die sich ebenfalls dort anmeldet", serves: "{other} meldet sich auch bei Acme an; diese Erweiterung stellt es bereit", use: "Für Acme nutzen", done: "Acme läuft jetzt über my-acme" },
};
const say = (s, other) => s.replace("{other}", other);

const shot = async (loc, name) => {
  if (!process.env.ARTIFACT_DIR) return;
  await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
  await loc.screenshot({ path: path.join(process.env.ARTIFACT_DIR, name + ".png") });
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": two plugins that sign in to one provider", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    t.after(() => browser.close());
    for (const lang of Object.keys(L)) {
      for (const width of [1100, 560]) {
        await t.test(`${lang} ${width}px`, async () => {
          const w = L[lang];
          const asked = [];
          const ctx = await browser.newContext({ viewport: { width, height: 820 } });
          const page = await ctx.newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          const errors = [];
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", server(lang, asked));
          await page.goto("http://uni-switch.test/?view=plugins");
          const view = page.locator("#view-plugins");
          await view.waitFor({ state: "visible" });
          await view.page().locator(":is(.lib-tabs .opt, .nav-s):visible", { hasText: w.installed }).click();
          const row = (n) => view.locator(".pm-row", { has: page.locator(".name span", { hasText: new RegExp("^" + n + "$") }) });
          const mine = row("my-acme"), theirs = row("opencode-acme-auth");
          await mine.waitFor();
          await theirs.waitFor();

          // the third-party one serves it: both rows are there, and say so
          assert.equal((await mine.locator(".pm-clash").innerText()).trim(), say(w.served, "opencode-acme-auth"));
          assert.match(await mine.locator(".pm-clash").getAttribute("class"), /\bwarn\b/);
          assert.equal((await theirs.locator(".pm-clash").innerText()).trim(), say(w.serves, "my-acme"));
          assert.doesNotMatch(await mine.innerText(), new RegExp(w.nothing), "the row doesn't say it signs in to nothing");
          assert.equal(await theirs.locator("button", { hasText: w.use }).count(), 0, "the one serving it has nothing to take over");
          // the row fits: nothing wider than the page
          const over = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
          assert.ok(over <= 0, `the page scrolls sideways by ${over}px`);
          await shot(view, `plugin-clash-${engine}-${lang}-${width}`);

          // Use for Acme on the user's own one: uni-switch is told to run it there
          const use = mine.locator("button", { hasText: w.use });
          assert.equal(await use.count(), 1);
          assert.ok((await use.getAttribute("title")).includes("opencode-acme-auth"), "its title names the one it takes over from");
          await use.click();
          await mine.locator(".pm-clash:not(.warn)").waitFor();
          assert.deepEqual(asked.filter(([k]) => k === "prefer"), [["prefer", { spec: MINE, provider: "acme" }]]);
          assert.equal((await mine.locator(".pm-clash").innerText()).trim(), say(w.serves, "opencode-acme-auth"));
          assert.equal((await theirs.locator(".pm-clash").innerText()).trim(), say(w.served, "my-acme"));
          assert.equal(await theirs.locator("button", { hasText: w.use }).count(), 1, "and the other one can take it back");
          assert.equal(await mine.locator("button", { hasText: w.use }).count(), 0);
          assert.equal(await page.locator("#status").textContent(), w.done);
          await shot(view, `plugin-clash-used-${engine}-${lang}-${width}`);
          assert.deepEqual(errors, []);
          await ctx.close();
        });
      }
    }
  });
}

// #565 (ferocknew): the same plugin installed twice, from two places, and
// signing in to two providers. Its row read "CodeArts / OfficeAce 也登录
// CodeArts；由这个插件提供" and again for OfficeAce: the plugin naming
// itself, once a provider. Now each row has one line, naming the other copy
// by where it came from.
const NPM = "the retired opencode-codearts-auth@latest";
const DEV = "/Users/me/dev/codearts";
const NAME = "CodeArts / OfficeAce";

function twice(lang) {
  const clash = (spec) => ["codearts", "officeace"].map((id, i) => ({ id, by: NPM, with: [spec === NPM ? DEV : NPM], name: ["CodeArts", "OfficeAce"][i] }));
  const installed = [
    { spec: DEV, ownName: NAME, providers: [], version: "0.3.0", clashes: clash(DEV) },
    { spec: NPM, providers: ["CodeArts", "OfficeAce"], serves: ["CodeArts", "OfficeAce"], version: "0.3.0", clashes: clash(NPM) },
  ];
  const state = { bun: true, bunVersion: "1.3.0", plugins: installed };
  const listings = [{ package: "the retired opencode-codearts-auth", name: NAME, providers: ["codearts", "officeace"] }];
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname === "/api/plugins") return json(state);
    if (url.pathname === "/api/plugins/listings") return json({ listings });
    if (url.pathname === "/api/plugins/market") return json({ listings, state });
    if (url.pathname === "/api/plugins/search") return json({ hits: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    if (!/^\/[\w./-]*$/.test(url.pathname) || url.host !== "uni-switch.test") return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const T = {
  en: { serves: `Another copy of this plugin (${DEV}) signs in to CodeArts, OfficeAce too; this copy serves them`, served: `Another copy of this plugin (${NPM}) serves CodeArts, OfficeAce` },
  zh: { serves: `这个插件还装了另一份（${DEV}），也登录 CodeArts、OfficeAce；现在用的是这一份`, served: `CodeArts、OfficeAce 现由这个插件的另一份（${NPM}）提供` },
  "zh-TW": { serves: `這個外掛還裝了另一份（${DEV}），也登入 CodeArts、OfficeAce；現在用的是這一份`, served: `CodeArts、OfficeAce 現由這個外掛的另一份（${NPM}）提供` },
  ja: { serves: `このプラグインはもう一つ（${DEV}）インストールされていて、CodeArts、OfficeAce にもサインインします。使われているのはこちらです`, served: `このプラグインのもう一つのインストール（${NPM}）が CodeArts、OfficeAce を提供しています` },
  de: { serves: `Eine weitere Kopie dieser Erweiterung (${DEV}) meldet sich auch bei CodeArts, OfficeAce an; diese Kopie stellt sie bereit`, served: `Eine weitere Kopie dieser Erweiterung (${NPM}) stellt CodeArts, OfficeAce bereit` },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": one plugin installed twice says so once, not by its own name", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    t.after(() => browser.close());
    for (const lang of Object.keys(T)) {
      for (const width of [1100, 560]) {
        await t.test(`${lang} ${width}px`, async () => {
          const ctx = await browser.newContext({ viewport: { width, height: 820 } });
          const page = await ctx.newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          const errors = [];
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", twice(lang));
          await page.goto("http://uni-switch.test/?view=plugins");
          const view = page.locator("#view-plugins");
          await view.waitFor({ state: "visible" });
          await view.page().locator(":is(.lib-tabs .opt, .nav-s):visible", { hasText: L[lang].installed }).click();
          const rows = view.locator(".pm-row", { has: page.locator(".name span", { hasText: NAME }) });
          await rows.nth(1).waitFor();
          assert.equal(await rows.count(), 2);
          const lines = [];
          for (let i = 0; i < 2; i++) {
            const clash = rows.nth(i).locator(".pm-clash");
            assert.equal(await clash.count(), 1, "one line for both providers");
            lines.push((await clash.innerText()).trim());
          }
          assert.deepEqual(lines.sort(), [T[lang].serves, T[lang].served].sort());
          for (const l of lines) assert.ok(!l.includes(NAME), `the line doesn't name the plugin itself: ${l}`);
          const over = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
          assert.ok(over <= 0, `the page scrolls sideways by ${over}px`);
          await shot(view, `plugin-clash-twice-${engine}-${lang}-${width}`);
          assert.deepEqual(errors, []);
          await ctx.close();
        });
      }
    }
  });
}

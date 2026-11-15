// Run with Node's test runner and Playwright on the module path; see README.md.
// A community plugin's panels in uni-switch's own pages (pluginpages.js, P5):
// the example plugin (testdata/panel-plugin) adds a note to each
// provider's editor, told which provider it is in, the top models at the
// end of Usage, and its settings on Settings' Plugins tab, a tab there
// only while a plugin's panel is. A panel on the Agents page draws as
// tall as it says. Each is a frame sandboxed to scripts. One that throws
// says so in its own place, with Try again, and the page around it goes
// on. A page that replaces a built-in page takes its tab's place, and a
// way to the built-in page opens it. The Plugins page says where a
// plugin's panels go and what they may read, and which page a page
// replaces. Every language, both engines, a narrow window. The API is
// faked here; the server's side is pluginpanel_test.go.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const example = path.resolve(__dirname, "../testdata/panel-plugin/ui");
const bridge = path.resolve(__dirname, "../pagebridge.js");

const NOTES = "1".repeat(32), SPEND = "2".repeat(32), SETS = "3".repeat(32), TALL = "4".repeat(32), BROKEN = "5".repeat(32), LIB = "6".repeat(32);
const panel = (tok, i, slot, title, file, name = "uni-switch-provider-notes", reads = ["usage:summary"]) => ({
  spec: "/x/" + name, name, key: `plugin-${name.replace("uni-switch-", "")}-${i}`, slot, title, reads, url: `/_uni-switch/plugin/${tok}/${file}`,
});
const allPanels = [
  panel(NOTES, 0, "provider", "Notes", "notes.html"),
  panel(SPEND, 1, "usage", "Top models today", "spend.html"),
  panel(SETS, 2, "settings", "Provider notes", "settings.html"),
  panel(TALL, 0, "agents", "Tall", "tall.html", "tall", []),
  panel(BROKEN, 0, "agents", "Broken", "broken.html", "broken", []),
];
const pages = [{ spec: "/x/my-library", name: "my-library", key: "plugin-my-library", title: "My library", replaces: "library", reads: [], url: `/_uni-switch/plugin/${LIB}/index.html` }];

const TALL_HTML = `<!doctype html><meta charset="utf-8"><style>body{margin:0;padding:0}</style><script src="_uni-switch/bridge.js"></script><div id="box" style="height:180px">tall</div>`;
const BROKEN_HTML = `<!doctype html><meta charset="utf-8"><script src="_uni-switch/bridge.js"></script><script src="app.js"></script><p id="ok">drawn</p>`;
const LIB_HTML = `<!doctype html><meta charset="utf-8"><script src="_uni-switch/bridge.js"></script><p id="mine">my library</p>`;

const providerList = [{ id: "openai", name: "OpenAI", icon: "generic", host: "api.openai.com", chat: "https://api.openai.com/v1", models: [] }]
  .map((p) => ({ responses: "", anthropic: "", agents: [], fallback: [], headers: {}, key: { set: false }, keyList: [], ready: true, ...p }));

function server(lang, seen, opts) {
  return async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: providerList, presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/plugin-pages") return json(opts.pages);
    if (url.pathname === "/api/plugin-panels") return json(opts.panels);
    if (url.pathname === "/api/plugin-pages/read") {
      seen.reads.push(req.postDataJSON());
      return json({ period: "today", calls: 12, models: [{ model: "gpt-5", calls: 9 }, { model: "glm-5", calls: 3 }] });
    }
    if (url.pathname === "/api/plugin-pages/options") {
      const b = req.postDataJSON();
      if (b.options) { seen.options.push(b); seen.saved = b.options; }
      return json(seen.saved || {});
    }
    if (url.pathname === "/api/plugins") return json(opts.plugins || { plugins: [] });
    if (url.pathname === "/api/plugins/market") return json({ listings: [], state: opts.plugins || { plugins: [] } });
    if (url.pathname === "/api/plugins/listings") return json({ listings: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const m = url.pathname.match(/^\/_uni-switch\/plugin\/([0-9a-f]{32})\/(.+)$/);
    if (m) {
      const [, tok, rel] = m;
      const type = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css" }[path.extname(rel)];
      let body;
      if (rel === "_uni-switch/bridge.js") body = await fs.readFile(bridge);
      else if ([NOTES, SPEND, SETS].includes(tok)) body = await fs.readFile(path.join(example, rel)).catch(() => null);
      else if (tok === TALL && rel === "tall.html") body = TALL_HTML;
      else if (tok === BROKEN && rel === "broken.html") body = BROKEN_HTML;
      else if (tok === BROKEN && rel === "app.js") body = seen.broken ? "throw new Error('fixture: the panel broke')" : "1";
      else if (tok === LIB && rel === "index.html") body = LIB_HTML;
      if (body == null) return route.fulfill({ status: 404, body: "" });
      return route.fulfill({ status: 200, headers: { "Content-Security-Policy": "sandbox allow-scripts; connect-src 'none'", "Content-Type": type }, body });
    }
    if (url.host !== "uni-switch.test") return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const frameOf = (page, tok) => page.frames().find((f) => f.url().includes(tok));
async function frameReady(page, tok) {
  await page.waitForFunction((tok) => [...document.querySelectorAll("iframe.plugin-panel-frame, iframe.plugin-frame")].some((f) => f.src.includes(tok)), tok);
  for (let i = 0; i < 100 && !frameOf(page, tok); i++) await page.waitForTimeout(50);
  return frameOf(page, tok);
}
const fits = (page, sel) => page.evaluate((sel) => { const v = document.querySelector(sel); return v.scrollWidth <= v.clientWidth + 1; }, sel);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: a plugin's panels draw in uni-switch's own pages, shut in`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      for (const width of [1100, 420]) {
        const where = `${engine} ${lang} ${width}`;
        const context = await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(8000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const seen = { reads: [], options: [], broken: lang === "en" && width === 1100 };
        await page.route("**/*", server(lang, seen, { panels: allPanels, pages }));
        await page.goto("http://uni-switch.test/");
        const tr = (k, v) => page.evaluate(([k, v]) => t(k, v), [k, v]);

        // the Agents page: a panel as tall as it draws, in a frame sandboxed to scripts
        const tall = page.locator('#view-agents .plugin-slot[data-slot="agents"] .plugin-panel[data-panel="plugin-tall-0"]');
        await tall.waitFor();
        const tf = tall.locator("iframe");
        assert.equal(await tf.getAttribute("sandbox"), "allow-scripts", where);
        await page.waitForFunction(() => Math.round(document.querySelector('.plugin-panel[data-panel="plugin-tall-0"] iframe').getBoundingClientRect().height) === 180);
        assert.equal((await tall.locator(".row-head .label").textContent()), "Tall", where);
        assert.ok(await fits(page, "#view-agents"), `${where}: Agents fits`);

        // the size watch's own "ResizeObserver loop" notice is no error of
        // the panel's, named or (WebKit, sandboxed) a bare "Script error."
        // while the watch has just run: the panel stays
        if (lang === "en" && width === 1100) {
          const tallFrame = await frameReady(page, TALL);
          await tallFrame.evaluate(() => new Promise((done) => {
            dispatchEvent(new ErrorEvent("error", { message: "ResizeObserver loop completed with undelivered notifications.", cancelable: true }));
            // a real loop: the watch is told of a new size, and an observer
            // told in the same turn changes it again; the browser's notice
            // comes after every observer of the turn has run
            const box = document.getElementById("box");
            box.style.height = "181px";
            let first = true;
            new ResizeObserver(() => {
              if (first) { first = false; box.style.height = "182px"; setTimeout(done, 100); }
            }).observe(document.documentElement);
          }));
          await page.waitForTimeout(100);
          assert.equal(await tall.locator(".plugin-panel-broke").count(), 0, `${where}: a size notice broke the panel`);
          assert.equal(await tall.locator("iframe").count(), 1, where);
        }

        if (seen.broken) {
          // a panel that throws says so in its own place; the one beside it goes on
          const broke = page.locator('.plugin-panel[data-panel="plugin-broken-0"] .plugin-panel-broke');
          await broke.waitFor();
          assert.equal(await broke.locator("b").textContent(), await tr("{name} stopped working", { name: "Broken" }), where);
          assert.equal(await broke.locator(".note").textContent(), await tr("The page hit an error"), where);
          assert.equal(await tall.locator("iframe").count(), 1, `${where}: the other panel stays`);
          seen.broken = false;
          // by the keyboard: WebKit's Playwright calls a button low in a
          // scrolled view outside the viewport after scrolling it in
          await broke.locator("button").focus();
          await page.keyboard.press("Enter");
          const fixed = await frameReady(page, BROKEN);
          await fixed.locator("#ok").waitFor();
          assert.equal(await broke.count(), 0, `${where}: the card gone`);
        }

        // the end of Usage: what it declared it reads
        await page.evaluate(() => show("usage"));
        const spend = await frameReady(page, SPEND);
        await spend.waitForFunction(() => document.querySelectorAll("#top li").length === 2);
        assert.equal(await spend.locator("#top li").first().textContent(), "gpt-5: 9 calls", where);
        assert.ok(seen.reads.some((r) => r.spec === "/x/uni-switch-provider-notes" && r.read === "usage:summary"), where);
        assert.equal(await spend.evaluate(() => document.documentElement.lang), lang, `${where}: the window's language`);
        assert.ok(await fits(page, "#view-usage"), `${where}: Usage fits`);
        // Usage hides its own headings while a period loads, not a panel's
        const spendHead = page.locator('#view-usage .plugin-panel:has(iframe[src*="' + SPEND + '"]) > .row-head');
        await page.waitForFunction(() => !document.querySelector("#view-usage .loading, #view-usage[aria-busy=true]"));
        assert.ok(await spendHead.isVisible(), `${where}: the panel's heading shows`);

        // a provider's editor: the note panel knows which provider it is in
        await page.evaluate(() => show("providers"));
        await page.waitForFunction(() => providers?.providers?.length === 1);
        await page.evaluate(() => openModal(renderEditor(providers.providers[0])));
        const notes = await frameReady(page, NOTES);
        assert.equal(await page.locator('#modal .plugin-slot[data-slot="provider"] iframe').getAttribute("sandbox"), "allow-scripts", where);
        await notes.waitForFunction(() => window.uniSwitch.context?.provider?.id === "openai");
        assert.equal(await notes.evaluate(() => uniSwitch.context.provider.name), "OpenAI", where);
        await notes.locator("#note").fill("work account");
        await notes.locator("#saved").filter({ hasText: "Saved" }).waitFor();
        assert.deepEqual(seen.options.at(-1), { spec: "/x/uni-switch-provider-notes", options: { notes: { openai: "work account" } } }, `${where}: its own options`);
        assert.ok(await page.evaluate(() => { const m = document.querySelector("#modal .plugin-slot"); return m.getBoundingClientRect().right <= innerWidth + 1; }), `${where}: the editor fits`);
        // a new provider's editor has no provider to tell: no panel
        await page.evaluate(() => openModal(renderEditor(null)));
        assert.equal(await page.locator('#modal .plugin-slot[data-slot="provider"]').count(), 0, `${where}: none for a new one`);
        await page.evaluate(() => closeModal());

        // Settings: the Plugins tab, with the plugin's settings
        await page.evaluate(() => show("settings"));
        // its tab, in the tab strip or (a wide window) the sidebar
        const plug = await tr("Plugins");
        const tab = page.locator(":is(#setTabs button, .nav-sub .nav-s):visible", { hasText: plug });
        await tab.first().waitFor();
        assert.equal((await tab.first().textContent()).trim(), plug, where);
        await tab.first().click();
        const sets = await frameReady(page, SETS);
        // drawn when the window opened, before the note was written
        await sets.locator("#count").filter({ hasText: /^\d+ notes?$/ }).waitFor();
        await page.waitForTimeout(1100); // options are set once a second at most
        await sets.locator("#clear").click();
        await sets.locator("#count").filter({ hasText: "0 notes" }).waitFor();
        assert.deepEqual(seen.options.at(-1).options.notes, {}, `${where}: cleared`);
        assert.ok(await page.locator("#setPage-plugins").isVisible(), where);
        assert.ok(await fits(page, "#view-settings"), `${where}: Settings fits`);

        // a page that replaces Library: its tab in Library's place, and
        // Library opens it
        const lib = page.locator('#nav button[data-view="library"]:not([data-plugin-page])');
        assert.equal(await lib.getAttribute("data-replaced"), "plugin-my-library", where);
        assert.equal(await lib.isVisible(), false, `${where}: the built-in steps aside`);
        assert.equal(await page.evaluate(() => document.querySelector('#nav button[data-view="library"]').nextElementSibling?.dataset.view), "plugin-my-library", `${where}: in its place`);
        assert.equal(await page.locator("#nav .nav-gl[data-plugin-pages]").count(), 0, `${where}: no Plugins group for a page in a built-in's place`);
        await page.evaluate(() => show("library"));
        await page.waitForFunction(() => view === "plugin-my-library");
        const mine = await frameReady(page, LIB);
        await mine.locator("#mine").waitFor();

        // the broken panel's own throw, in its frame, is the only error
        assert.deepEqual(errors.filter((e) => e !== "fixture: the panel broke"), [], where);
        await context.close();
      }
    }
  });

  test(`${engine}: with no plugin's settings, Settings has no Plugins tab`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const page = await (await browser.newContext({ viewport: { width: 980, height: 700 } })).newPage();
    page.setDefaultTimeout(5000);
    page.setDefaultNavigationTimeout(60000);
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.route("**/*", server("en", { reads: [], options: [] }, { panels: allPanels.filter((p) => p.slot !== "settings"), pages: [] }));
    await page.goto("http://uni-switch.test/?view=settings");
    await page.locator('.plugin-panel[data-panel="plugin-tall-0"]').waitFor({ state: "attached" });
    assert.equal(await page.locator("#setTab-plugins").isVisible(), false);
    assert.equal(await page.locator(".nav-sub .nav-s", { hasText: "Plugins" }).count(), 0, "not in the sidebar");
    // the address of the tab opens the first there
    await page.evaluate(() => setSetTab("plugins", false));
    assert.equal(await page.evaluate(() => setShown), "general");
    // Library is Library
    assert.equal(await page.locator('#nav button[data-view="library"]').getAttribute("data-replaced"), null);
    assert.deepEqual(errors, []);
    await page.context().close();
  });

  test(`${engine}: Plugins says where panels go and what a page replaces`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const plugins = { bun: true, bunVersion: "1.3.0", plugins: [
      { spec: "/x/uni-switch-provider-notes", providers: [], moved: [], inUniSwitchOnly: true, panels: [{ slot: "provider", title: "Notes" }, { slot: "usage", title: "Top models today" }, { slot: "settings", title: "Provider notes" }], panelReads: ["usage:summary"] },
      { spec: "/x/my-library", providers: [], moved: [], inUniSwitchOnly: true, page: { title: "My library", replaces: "library", reads: [] } },
    ] };
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      for (const width of [980, 420]) {
        const where = `${engine} ${lang} ${width}`;
        const page = await (await browser.newContext({ viewport: { width, height: 820 } })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, { reads: [], options: [] }, { panels: [], pages: [], plugins }));
        await page.goto("http://uni-switch.test/?view=plugins");
        const view = page.locator("#view-plugins");
        const installed = await page.evaluate(() => t("Installed"));
        await page.locator(":is(.lib-tabs .opt, .nav-s):visible", { hasText: installed }).first().click();
        const row = (name) => view.locator(".pm-row").filter({ has: page.locator(".name", { hasText: name }) });
        const tr = (k, v) => page.evaluate(([k, v]) => t(k, v), [k, v]);
        const notes = row("uni-switch-provider-notes");
        await notes.waitFor();
        assert.deepEqual((await notes.locator(".pm-chip.kind").allInnerTexts()).map((x) => x.trim()), [await tr("Panels")], where);
        const where2 = [await tr("each provider's editor"), await tr("the Usage page"), await tr("Settings")].join(await tr(", "));
        assert.equal((await notes.locator(".pm-page").innerText()).trim(), await tr("Adds panels to {where}, which can read {reads}", { where: where2, reads: await tr("your usage totals, by model") }), where);
        const lib = row("my-library");
        assert.equal((await lib.locator(".pm-page").innerText()).trim(), await tr("Takes the place of the {page} page, and reads nothing of uni-switch's", { page: await tr("Library") }), where);
        if (lang !== "en") assert.ok(!(await notes.locator(".pm-page").innerText()).includes("Adds panels"), `${where}: translated`);
        assert.ok(await view.evaluate((v) => v.scrollWidth <= v.clientWidth + 1), `${where}: fits`);
        assert.deepEqual(errors, [], where);
        await page.context().close();
      }
    }
  });
}

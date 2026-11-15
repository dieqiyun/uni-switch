// Run with Node's test runner and Playwright on the module path; see README.md.
// The owner, on Agents: opening a row's details made the page run past the
// window, the scrollbar appeared, and the whole page went 8px narrower in
// one frame, shaking back when it closed. Every page keeps its bar's room
// whether it runs past the window or not, so nothing in it moves sideways
// when something opens: each page is grown past the window here and its
// width held, and the owner's own case (an agent's details opened) is
// played out. Chromium keeps its scrollbars on (Playwright hides them by
// default), and both engines draw the app's own thin bar, as the app does:
// the Mac's 8px one and, with the platform said to be Windows, the 10px
// one Windows and Linux get. A dialog's fields keep their bar's room too.
// The bar's room comes out of the right padding (the owner: does the
// clear bar make the sides uneven?), so the content sits as far from the
// window's right edge, bar and all, as from its left.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function serve(lang) {
  const state = {
    agents: [{
      id: "pi", name: "Pi", icon: "generic", wired: true, path: "~/.pi/agent/settings.json",
      fields: [{ key: "model", label: "model", value: "uni-switch/deepseek/deepseek-v4-pro", options: [{ value: "uni-switch/deepseek/deepseek-v4-pro", label: "DeepSeek V4 Pro", ref: "deepseek/deepseek-v4-pro" }] }],
    }],
    profiles: [], settings: { lang, theme: "dark" },
  };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = ${JSON.stringify({ lang, theme: "dark", web: false })};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/settings") return json(state.settings);
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [], pools: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file).catch(() => ""), contentType });
  };
}

// the shown page: its id, how wide its content is, whether it runs past the window
const shown = (page) => page.evaluate(() => {
  const v = [...document.querySelectorAll(".view")].find((x) => !x.hidden && x.offsetParent);
  return v && { id: v.id, width: v.clientWidth, over: v.scrollHeight > v.clientHeight + 1 };
});

// a page told it runs on this platform (the app reads navigator.platform)
const onPlatform = async (browser, platform, viewport) => {
  const context = await browser.newContext({ viewport, reducedMotion: "reduce" });
  await context.addInitScript((p) => Object.defineProperty(Navigator.prototype, "platform", { get: () => p }), platform);
  return context.newPage();
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) for (const platform of ["MacIntel", "Win32"]) {
  const launch = () => (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium", ignoreDefaultArgs: ["--hide-scrollbars"] }));

  // 1600 wide, the pages that centre their content have more than 16px a side
  for (const width of [1100, 1600]) test(`${engine} ${platform} ${width}: no page narrows when it grows past the window`, async (t) => {
    const browser = await launch();
    t.after(() => browser.close());
    const page = await onPlatform(browser, platform, { width, height: 700 });
    page.setDefaultTimeout(6000);
    page.setDefaultNavigationTimeout(60000);
    await page.route("**/*", serve("en"));
    await page.goto("http://uni-switch.test/?view=agents");
    const ids = await page.evaluate(() => [...document.querySelectorAll(".view[id^='view-']")].map((v) => v.id.slice(5)));
    assert(ids.length >= 8, `pages found: ${ids}`);
    let tried = 0;
    for (const id of ids) {
      await page.goto(`http://uni-switch.test/?view=${id}`);
      const before = await shown(page).catch(() => null);
      if (!before || before.id !== `view-${id}`) continue; // a page this fixture doesn't open
      tried++;
      const sides = await page.evaluate((vid) => {
        const v = document.getElementById(vid), cs = getComputedStyle(v), bar = v.offsetWidth - v.clientWidth;
        return { left: parseFloat(cs.paddingLeft), right: parseFloat(cs.paddingRight) + bar, bar, wide: v.scrollWidth > v.clientWidth + 1 };
      }, before.id);
      assert(sides.bar > 0, `${id}: the page keeps its bar's room`);
      assert(Math.abs(sides.left - sides.right) <= 0.5, `${id}: ${sides.left}px on the left, ${sides.right}px on the right (bar ${sides.bar}px)`);
      assert(!sides.wide, `${id}: the page scrolls sideways`);
      // whatever opens in it: a block taller than the window, then gone
      const grown = await page.evaluate((vid) => {
        const v = document.getElementById(vid), b = document.createElement("div");
        b.id = "grow-test"; b.style.cssText = "flex:none;height:3000px";
        v.append(b);
        const r = { width: v.clientWidth, over: v.scrollHeight > v.clientHeight + 1 };
        b.remove();
        return r;
      }, before.id);
      const after = await shown(page);
      assert(grown.over, `${id}: the block ran past the window`);
      assert.equal(grown.width, before.width, `${id}: the page narrowed by ${before.width - grown.width}px when it ran past the window`);
      assert.equal(after.width, before.width, `${id}: the page changed width when it was short again`);
    }
    assert(tried >= 8, `pages tried: ${tried}`);

    // a dialog's fields: one that opens past the dialog's height leaves
    // them as wide as they were
    const fields = await page.evaluate(() => {
      const m = document.createElement("div"); m.className = "modal";
      m.innerHTML = '<div class="dialog"><div class="editor framed"><div class="ehead">head</div><div class="ebody"><div id="f1">field</div></div><div class="bar">bar</div></div></div>';
      document.body.append(m);
      const body = m.querySelector(".ebody"), w0 = body.clientWidth, f0 = m.querySelector("#f1").getBoundingClientRect().width;
      const b = document.createElement("div"); b.style.cssText = "height:3000px;grid-column:1/-1"; body.append(b);
      const r = { w0, w1: body.clientWidth, f0, f1: m.querySelector("#f1").getBoundingClientRect().width, over: body.scrollHeight > body.clientHeight + 1, bar: body.offsetWidth - body.clientWidth };
      m.remove();
      return r;
    });
    assert(fields.over, "the dialog's fields ran past it");
    assert(fields.bar > 0, "the dialog draws the app's bar");
    assert.equal(fields.w1, fields.w0, `the dialog's fields narrowed by ${fields.w0 - fields.w1}px`);
    assert.equal(fields.f1, fields.f0, "a field narrowed when the dialog's fields ran past it");
  });

  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    if (platform !== "MacIntel" && lang !== "en") continue; // one language is enough for the other bar
    test(`${engine} ${platform} ${lang}: opening an agent's details past the window moves nothing sideways`, async (t) => {
      const browser = await launch();
      t.after(() => browser.close());
      const page = await onPlatform(browser, platform, { width: 1000, height: 330 });
      page.setDefaultTimeout(6000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      await page.goto("http://uni-switch.test/?view=agents");
      const row = page.locator('.row.agent[data-id="pi"]');
      await row.waitFor();
      if (await row.locator(".ag-exp").count()) await row.locator(".ag-link").click(); // start folded
      await page.waitForFunction(() => !document.querySelector('.row.agent[data-id="pi"] .ag-exp'));
      const edge = () => row.evaluate((r) => { const b = r.getBoundingClientRect(); return { left: b.left, right: b.right }; });
      const before = { ...(await shown(page)), edge: await edge() };
      assert(!before.over, `${lang}: the page fits before the details open`);
      await row.locator(".ag-link").click();
      await row.locator(".ag-exp").waitFor();
      const open = { ...(await shown(page)), edge: await edge() };
      assert(open.over, `${lang}: the details run the page past the window`);
      assert.equal(open.width, before.width, `${lang}: the page narrowed by ${before.width - open.width}px`);
      assert.deepEqual(open.edge, before.edge, `${lang}: the agent's row moved sideways`);
      await row.locator(".ag-link").click();
      await page.waitForFunction(() => !document.querySelector('.row.agent[data-id="pi"] .ag-exp'));
      assert.deepEqual(await edge(), before.edge, `${lang}: the row moved sideways when the details closed`);
      assert.deepEqual(errors, []);
    });
  }
}

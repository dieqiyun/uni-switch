// Run with Node's test runner and Playwright on the module path; see README.md.
// The sidebar's sub-list lights the row under the pointer as a torch would
// (navlamp.js, app.css .nav-s > span.lit): the word is brightest where the
// pointer is and falls off along the row, the rest of the row a little
// over the others; only that row is lit, the next row takes the light as
// the pointer moves to it, and it goes out when the pointer leaves. The
// selected row is never lit, a touch lights nothing, and with reduced
// motion the light is there at once. Read from the pixels: the ink near
// the pointer stands further off the ground than the word's far end, and
// both ends turn when the pointer goes to the other end. Both engines,
// light and dark, English and Chinese. The sidebar's page rows (Providers,
// Routing, Gateway…), Settings and the gateway's address under it are lit
// the same way.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function serve(lang, theme) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:${JSON.stringify(theme)},web:false,sideGateway:{url:"http://127.0.0.1:3425",running:true}};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme } });
    if (url.pathname === "/api/settings") return json({ lang, theme, tray: "panel", version: "0.1.400", dir: "~/.config/uni-switch", gateway: "http://127.0.0.1:3425", lanURLs: [], visionModels: [], imageGenModels: [], workbuddyCheckins: [], redactWords: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    if (!/^\/[\w./-]*$/.test(url.pathname) || url.host !== "uni-switch.test") return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

// each sub-row's word: its box, how lit it is, and whether it is the selection
const words = (page) => page.$$eval(".nav-sub > .nav-s > span", (all) => all.map((s) => {
  const b = s.getBoundingClientRect();
  return { x: b.left, y: b.top, w: b.width, h: b.height, lit: s.classList.contains("lit"), si: +(s.style.getPropertyValue("--si") || 0), cur: s.parentElement.classList.contains("cur") };
}));
// how far the ink stands off the ground in a word's first and last fifth:
// the most any pixel's luminance differs from the corner's
async function ink(page, w) {
  const png = await page.screenshot({ clip: { x: w.x, y: w.y, width: w.w, height: w.h } });
  return page.evaluate(async (src) => {
    const img = new Image();
    img.src = "data:image/png;base64," + src;
    await img.decode();
    const c = document.createElement("canvas");
    c.width = img.width; c.height = img.height;
    const g = c.getContext("2d");
    g.drawImage(img, 0, 0);
    const d = g.getImageData(0, 0, c.width, c.height).data;
    const lum = (i) => 0.2126 * d[i] + 0.7152 * d[i + 1] + 0.0722 * d[i + 2];
    const ground = lum(0), fifth = Math.max(1, Math.floor(c.width / 5));
    const part = (from) => { let m = 0; for (let y = 0; y < c.height; y++) for (let x = from; x < from + fifth; x++) m = Math.max(m, Math.abs(lum((y * c.width + x) * 4) - ground)); return m; };
    return { start: part(0), end: part(c.width - fifth) };
  }, png.toString("base64"));
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: the sub-list's row under the pointer is lit as by a torch`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const theme of ["dark", "light"]) for (const lang of ["en", "zh"]) await t.test(`${theme} ${lang}`, async () => {
      const context = await browser.newContext({ viewport: { width: 1024, height: 800 }, colorScheme: theme });
      t.after(() => context.close());
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, theme));
      await page.goto("http://uni-switch.test/?view=settings");
      await page.waitForSelector(".nav-sub > .nav-s + .nav-s");
      await page.mouse.move(40, 300); // over the sidebar, which loads the lamp
      await page.waitForFunction(() => document.documentElement.classList.contains("navlamp"));
      await page.mouse.move(700, 500);
      await page.waitForTimeout(600); // the list's own unrolling
      let all = await words(page);
      assert(all.every((w) => !w.lit), "nothing lit before the pointer comes");
      // the longest word not selected, and another
      const free = all.map((w, i) => ({ ...w, i })).filter((w) => !w.cur);
      const long = free.reduce((a, b) => (b.w > a.w ? b : a));
      const other = free.find((w) => w.i !== long.i);
      const lit = (i) => page.waitForFunction((i) => { const s = document.querySelectorAll(".nav-sub > .nav-s > span")[i]; return s.classList.contains("lit") && +s.style.getPropertyValue("--si") > 0.99; }, i);
      const out = (i) => page.waitForFunction((i) => !document.querySelectorAll(".nav-sub > .nav-s > span")[i].classList.contains("lit"), i);

      // at the word's start, the start stands off the ground more than its end
      await page.mouse.move(long.x + 2, long.y + long.h / 2);
      await lit(long.i);
      await page.waitForTimeout(400); // the light catching the pointer up
      const atStart = await ink(page, long);
      assert(atStart.start > atStart.end + 12, `brightest where the pointer is: ${JSON.stringify(atStart)}`);
      all = await words(page);
      assert.deepEqual(all.filter((w) => w.lit).length, 1, "only the row under the pointer is lit");
      // moved to its end, the light follows
      await page.mouse.move(long.x + long.w - 2, long.y + long.h / 2, { steps: 8 });
      await page.waitForTimeout(600);
      const atEnd = await ink(page, long);
      assert(atEnd.end > atEnd.start + 12, `the light follows the pointer: ${JSON.stringify(atEnd)}`);

      // to another row: it takes the light and the first goes out
      await page.mouse.move(other.x + 4, other.y + other.h / 2, { steps: 4 });
      await lit(other.i);
      await out(long.i);
      // off the list: all out
      await page.mouse.move(600, 500);
      await out(other.i);

      // the selection is never lit
      const cur = all.find((w) => w.cur);
      if (cur) {
        await page.mouse.move(cur.x + 4, cur.y + cur.h / 2);
        await page.waitForTimeout(200);
        assert((await words(page)).every((w) => !w.lit), "the selected row keeps its own ink");
      }

      // a touch lights nothing
      await page.mouse.move(600, 500);
      await page.evaluate(({ x, y }) => document.elementFromPoint(x, y).dispatchEvent(new PointerEvent("pointermove", { bubbles: true, clientX: x, clientY: y, pointerType: "touch" })), { x: long.x + 4, y: long.y + long.h / 2 });
      await page.waitForTimeout(200);
      assert((await words(page)).every((w) => !w.lit), "a touch lights no row");

      // reduced motion: lit in the first frame
      await page.emulateMedia({ reducedMotion: "reduce", colorScheme: theme });
      await page.mouse.move(long.x + 4, long.y + long.h / 2);
      await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
      const now = (await words(page))[long.i];
      assert(now.lit && now.si === 1, `lit at once with reduced motion: ${JSON.stringify(now)}`);
      assert.deepEqual(errors, []);
    });
  });
}

// a page row's word (Providers, Routing…, Settings): its box, whether lit,
// whether its page is open
const tops = (page) => page.$$eval("#side :is(#nav > button[data-view], #prefs, #sideGw) > span", (all) => all.filter((s) => s.offsetWidth).map((s) => {
  const b = s.getBoundingClientRect();
  return { x: b.left, y: b.top, w: b.width, h: b.height, lit: s.classList.contains("lit"), on: s.parentElement.classList.contains("on"), id: s.parentElement.dataset.view || s.parentElement.id };
}));

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: a page row in the sidebar is lit as by a torch`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const theme of ["dark", "light"]) for (const lang of ["en", "zh"]) await t.test(`${theme} ${lang}`, async () => {
      const context = await browser.newContext({ viewport: { width: 1024, height: 800 }, colorScheme: theme });
      t.after(() => context.close());
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, theme));
      await page.goto("http://uni-switch.test/?view=agents");
      await page.waitForSelector("body.side-nav #side #nav > button[data-view]");
      await page.mouse.move(40, 300); // over the sidebar, which loads the lamp
      await page.waitForFunction(() => document.documentElement.classList.contains("navlamp"));
      await page.mouse.move(700, 500);
      await page.waitForTimeout(400);
      let all = await tops(page);
      assert(all.length >= 5 && all.some((w) => w.id === "prefs") && all.some((w) => w.id === "sideGw"), `the page rows, Settings and the gateway's address: ${JSON.stringify(all.map((w) => w.id))}`);
      assert(all.every((w) => !w.lit), "nothing lit before the pointer comes");
      const at = (id) => all.findIndex((w) => w.id === id);
      const lit = (i) => page.waitForFunction((i) => { const s = [...document.querySelectorAll("#side :is(#nav > button[data-view], #prefs, #sideGw) > span")].filter((s) => s.offsetWidth)[i]; return s.classList.contains("lit") && +s.style.getPropertyValue("--si") > 0.99; }, i);
      const out = (i) => page.waitForFunction((i) => ![...document.querySelectorAll("#side :is(#nav > button[data-view], #prefs, #sideGw) > span")].filter((s) => s.offsetWidth)[i].classList.contains("lit"), i);
      const pv = at("providers"), gw = at("gateway"), prefs = at("prefs"), word = all[pv];

      // at the word's start, the start stands off the ground more than its end
      await page.mouse.move(word.x + 2, word.y + word.h / 2);
      await lit(pv);
      await page.waitForTimeout(400);
      const atStart = await ink(page, word);
      assert(atStart.start > atStart.end + 12, `brightest where the pointer is: ${JSON.stringify(atStart)}`);
      assert.equal((await tops(page)).filter((w) => w.lit).length, 1, "only the row under the pointer is lit");
      await page.mouse.move(word.x + word.w - 2, word.y + word.h / 2, { steps: 8 });
      await page.waitForTimeout(600);
      const atEnd = await ink(page, word);
      assert(atEnd.end > atEnd.start + 12, `the light follows the pointer: ${JSON.stringify(atEnd)}`);

      // to Gateway, then Settings: each takes the light from the one before
      await page.mouse.move(all[gw].x + 4, all[gw].y + all[gw].h / 2, { steps: 4 });
      await lit(gw);
      await out(pv);
      await page.mouse.move(all[prefs].x + 4, all[prefs].y + all[prefs].h / 2, { steps: 4 });
      await lit(prefs);
      await out(gw);
      // the gateway's address under Settings, brightest where the pointer is
      const addr = all[at("sideGw")];
      await page.mouse.move(addr.x + 2, addr.y + addr.h / 2, { steps: 4 });
      await lit(at("sideGw"));
      await out(prefs);
      await page.waitForTimeout(400);
      const gwInk = await ink(page, addr);
      assert(gwInk.start > gwInk.end + 12, `the address brightest where the pointer is: ${JSON.stringify(gwInk)}`);
      await page.mouse.move(700, 500);
      await out(at("sideGw"));

      // the open page's row keeps its own ink
      const cur = all.find((w) => w.on);
      assert(cur, "a page is open");
      await page.mouse.move(cur.x + 4, cur.y + cur.h / 2);
      await page.waitForTimeout(200);
      assert((await tops(page)).every((w) => !w.lit), "the open page's row is never lit");
      assert.deepEqual(errors, []);
    });
  });
}

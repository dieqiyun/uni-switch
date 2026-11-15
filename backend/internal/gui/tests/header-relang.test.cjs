// Run with Node's test runner and Playwright on the module path; see README.md.
// kiwiflysky on X: with a wide interface font, the macOS window's header fit
// in Chinese, but switched to English, Japanese or German its tabs ran left
// under the uni-switch and the traffic lights, and in German into the refresh
// button and the gear. fitTop draws the header in when it doesn't fit, but
// was only run again when the header, the uni-switch or the buttons changed size;
// another language or font changes only the tabs. Here a macOS window opens
// in Chinese, with the header's tabs picked over the sidebar, and is
// switched to each other language from Settings at the reporter's width. In
// English it takes a wide font (Menlo) at each width from 1000 to 640
// pixels and must be fitted as a fresh fit fits it. Each language is then
// swept from 1100 to 600 pixels, and the tabs keep 7.5px from the uni-switch
// (or the header's padding past the traffic lights) and from the buttons,
// which stay inside the header; drawn in all the way, they scroll in the
// room between. Chromium and WebKit; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const LANGS = [["en", "English"], ["zh-TW", "繁體中文"], ["ja", "日本語"], ["de", "Deutsch"], ["zh", "简体中文"]];

function server() {
  let settings = { lang: "zh", theme: "light" };
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"zh",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings } });
    if (url.pathname === "/api/settings") {
      if (req.method() === "POST") settings = { ...settings, ...JSON.parse(req.postData() || "{}") };
      return route.fulfill({ json: settings });
    }
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], presets: [], excluded: [], gateway: { running: true, window: true } } });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const settled = (page) => page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
const header = (page) => page.evaluate(() => {
  const top = document.querySelector(".top"), r = top.getBoundingClientRect(), brand = document.querySelector(".brand");
  const n = document.querySelector("#nav").getBoundingClientRect(), a = document.querySelector(".actions").getBoundingClientRect();
  const left = brand.offsetParent ? brand.getBoundingClientRect().right : r.left + parseFloat(getComputedStyle(top).paddingLeft);
  return {
    cls: top.className, packed: top.classList.contains("packed"),
    left: n.left - left, right: a.left - n.right, out: a.right - (r.right - parseFloat(getComputedStyle(top).paddingRight)),
  };
});
// drawn in all the way (packed) the tabs scroll in the room between, past the
// header's padding, which is past the traffic lights
const check = (h, at) => {
  const room = h.packed ? 0 : 7.5;
  assert(h.left >= room, "the tabs run under the uni-switch at " + at);
  assert(h.right >= room, "the tabs run into the buttons at " + at);
  assert(h.out <= 0.5, "the buttons run off the header at " + at);
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": the header fits again in another language and font", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    const ctx = await browser.newContext({ viewport: { width: 830, height: 600 } });
    await ctx.addInitScript(() => {
      Object.defineProperty(Navigator.prototype, "platform", { get: () => "MacIntel" });
      // the header's tabs picked over the sidebar (the title bar's button), so
      // the tabs are in the header at every width swept, not only under 1024px
      localStorage.setItem("uni-switch.navLayout", "top");
    });
    const page = await ctx.newPage();
    page.on("pageerror", (e) => errors.push(e.message));
    await page.route("**/*", server());
    await page.goto("http://uni-switch.test/?mode=window");
    await page.waitForSelector("#nav button.on");
    assert(await page.evaluate(() => document.body.classList.contains("mac") && document.body.classList.contains("window")), "not a macOS window");
    assert(await page.evaluate(() => !document.body.classList.contains("side-nav")), "the tabs aren't in the header");
    await settled(page);
    check(await header(page), "830px in Chinese");
    await page.click("#prefs");
    let wide = false;
    for (const [id, label] of LANGS) {
      await t.test(id, async () => {
        await page.setViewportSize({ width: 830, height: 600 });
        // Settings' language is one menu (1ooogle on X): opened, then the name picked
        await page.locator("#langPick").click();
        await page.locator(".proto-menu .pm-item", { hasText: new RegExp("^" + label + "$") }).first().click();
        await page.waitForFunction((id) => locale === id, id);
        await settled(page);
        check(await header(page), `830px just switched to ${id}: ${JSON.stringify(await header(page))}`);
        if (!wide) {
          // the reporter's wide interface font, picked once the header was
          // fitted in English (a Chinese name's glyphs aren't Menlo's), as
          // the host hands a font saved in the other webview, on the Agents
          // page, where nothing of Settings redraws: at each width the
          // header is fitted to the system font, then given Menlo, and must
          // be fitted as a fresh fit fits it
          wide = true;
          await page.click("#nav button[data-view=agents]");
          await page.waitForFunction(() => view === "agents");
          const MENLO = { family: "Menlo", name: "Regular", weight: 400, style: "normal", stretch: 100 };
          for (let w = 1000; w >= 640; w -= 20) {
            await page.evaluate(() => window.receiveFonts({ uiFont: null }));
            await page.setViewportSize({ width: w, height: 600 });
            await settled(page);
            await page.evaluate((f) => window.receiveFonts({ uiFont: f }), MENLO);
            await settled(page);
            const h = await header(page);
            check(h, `${w}px in ${id}, just given Menlo: ${JSON.stringify(h)}`);
            const fresh = await page.evaluate(() => { fitTop(); return document.querySelector(".top").className; });
            assert.equal(h.cls, fresh, `the header isn't fitted to Menlo at ${w}px in ${id}`);
          }
          assert(await page.evaluate(() => getComputedStyle(document.querySelector("#nav button")).fontFamily.includes("Menlo")), "the tabs aren't in Menlo");
          await page.setViewportSize({ width: 830, height: 600 });
          await page.click("#prefs");
          await page.waitForFunction(() => view === "settings");
        }
        for (let w = 1100; w >= 600; w -= 20) {
          await page.setViewportSize({ width: w, height: 600 });
          await settled(page);
          const h = await header(page);
          check(h, `${w}px in ${id}: ${JSON.stringify(h)}`);
        }
      });
    }
    await ctx.close();
    assert.deepEqual(errors, []);
  });
}

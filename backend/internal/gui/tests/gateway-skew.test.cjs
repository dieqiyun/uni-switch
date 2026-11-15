// Run with Node's test runner and Playwright on the module path; see README.md.
// A uni-switch of another version serving the gateway (matrix0487 on Discord:
// pi got 404 for a model the newer app had written it, and only the Gateway
// page named the older uni-switch) is said over the Agents list: an older copy
// of the app being taken over once idle, an older uni-switch serve or app with
// the Gateway page's button, a newer one, a build of its own. In English
// and Chinese, in both engines, at the panel's width, with nothing cut off.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function fixture(lang, other, posts) {
  let gone = false;
  return async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/gateway/take-over" && req.method() === "POST") {
      posts.push(url.pathname);
      gone = true;
      return json({ ok: true });
    }
    if (url.pathname === "/api/state") return json({
      agents: [{ id: "pi", name: "pi", path: "/test/models.json", icon: "pi", wired: true, fields: [] }],
      profiles: [], settings: { lang, theme: "light" },
      ...(gone || !other ? {} : { gatewayOther: other }),
    });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const base = { version: "0.1.2400", mine: "0.1.2500", pid: 4242 };
const cases = {
  en: [
    { other: { ...base, serves: "app", older: true, replacing: true, busy: 2 }, head: "uni-switch 0.1.2400, an older copy of this app, still serves the gateway", why: /once its 2 requests in flight finish/, quit: false },
    { other: { ...base, serves: "app", older: true, replacing: true, busy: 1 }, head: "uni-switch 0.1.2400, an older copy of this app, still serves the gateway", why: /once its 1 request in flight finishes/, quit: false },
    { other: { ...base, serves: "serve", older: true }, head: "The gateway is served by uni-switch 0.1.2400, older than this app (0.1.2500)", why: /uni-switch serve.*uni-switch daemon restart/, quit: true },
    { other: { ...base, serves: "app", older: true, stuck: "denied" }, head: "The gateway is served by uni-switch 0.1.2400, older than this app (0.1.2500)", why: /Quit it here and this app serves the gateway at once/, quit: true },
    { other: { ...base, version: "0.1.2600", serves: "app", newer: true }, head: "The gateway is served by uni-switch 0.1.2600, newer than this app (0.1.2500)", why: /Update this uni-switch/, quit: false },
    { other: { ...base, version: "0.1.2400-3-gabcdef1", serves: "app" }, head: "The gateway is served by uni-switch 0.1.2400-3-gabcdef1, a different build from this app (0.1.2500)", why: /Quit it there/, quit: false },
  ],
  zh: [
    { other: { ...base, serves: "app", older: true, replacing: true, busy: -1 }, head: "更旧的 uni-switch 0.1.2400（这个应用的旧副本）仍在提供网关", why: /没有进行中的请求时接管网关/, quit: false },
    { other: { ...base, serves: "serve", older: true }, head: "网关由 uni-switch 0.1.2400 提供，比这个应用（0.1.2500）旧", why: /uni-switch serve.*uni-switch daemon restart/, quit: true },
    { other: { ...base, version: "0.1.2600", serves: "app", newer: true }, head: "网关由 uni-switch 0.1.2600 提供，比这个应用（0.1.2500）新", why: /请更新当前 uni-switch/, quit: false },
  ],
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: another uni-switch's version serving the gateway is said on the Agents page`, async (t) => {
      assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch());
      t.after(() => browser.close());
      const errors = [];
      for (const c of [...cases[lang], { other: null }]) {
        const posts = [];
        const context = await browser.newContext({ viewport: { width: 380, height: 640 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", fixture(lang, c.other, posts));
        await page.goto("http://uni-switch.test/");
        await page.locator('.row.agent[data-id="pi"]').waitFor();
        const box = page.locator("#gwSkew");
        if (!c.other) {
          // none: nothing is said
          assert.equal(await box.count() ? await box.isVisible() : false, false, "a skew said with none");
          await context.close();
          continue;
        }
        await box.waitFor();
        assert.equal((await box.locator(".dep-head").textContent()).trim(), c.head);
        assert.match(await box.locator(".dep-why").first().textContent(), c.why);
        // above the list, the whole text readable, and nothing sideways
        const [skewY, listY] = await page.evaluate(() => [document.querySelector("#gwSkew").getBoundingClientRect().top, document.querySelector('.row.agent[data-id="pi"]').getBoundingClientRect().top]);
        assert(skewY < listY, "said under the list");
        assert(await box.evaluate((b) => [...b.querySelectorAll(".dep-head,.dep-why")].every((e) => e.scrollWidth <= e.clientWidth + 1)), "cut off");
        assert(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), "the page scrolls sideways");
        const button = box.locator("button");
        assert.equal(await button.count(), c.quit ? 1 : 0, "the button's there only for an older uni-switch this app can quit");
        if (c.quit) {
          await button.click();
          // the take-over is asked for, and once it's done the line goes
          await box.waitFor({ state: "hidden" });
          assert.deepEqual(posts, ["/api/gateway/take-over"]);
        }
        await context.close();
      }
      assert.deepEqual(errors, []);
    });
  }
}

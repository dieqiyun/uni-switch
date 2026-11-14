// Run with Node's test runner and Playwright on the module path; see README.md.
// The Gateway card's button (leslie_luo on Discord: the page said an older
// uni-switch served the gateway, with no way to quit it). With an older uni-switch
// on the port it reads "Quit uni-switch {v} and take over" and asks the backend
// to; once this one serves, it is "Restart gateway" and restarts it. A
// uni-switch of this version serving it gets no button. What stood in the way
// (a program that isn't uni-switch on the port) is said under it, naming the
// process. No click scrolls the page, nothing has a left border, in English
// and Chinese.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function fixture(lang, world, theme = "light") {
  const state = { agents: [], profiles: [], settings: { lang, theme } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"${theme}",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (route.request().method() === "POST" && url.pathname.startsWith("/api/gateway/")) {
      world.posts.push(url.pathname);
      return json(world.answer(url.pathname));
    }
    if (route.request().method() !== "GET" && url.pathname.startsWith("/api/")) return json({});
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") {
      // a long list under the card, so the page could scroll
      const models = Array.from({ length: 40 }, (_, i) => ({ id: `model-${i}`, name: `Model ${i}`, on: true }));
      return json({
        providers: [{ id: "fixture", name: "Fixture", icon: "generic", models, agents: [] }],
        gateway: { running: true, window: true, url: "http://127.0.0.1:3999", calls: [], groups: [], models: 40, ...world.gateway },
      });
    }
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) await new Promise((r) => setTimeout(r, 20e3));
      return json({ mine: false, now: new Date().toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: {
    quit: "Quit uni-switch 0.1.550 and take over", restart: "Restart gateway", running: "running",
    notUniSwitch: /Port 3999 is held by \/usr\/bin\/python3 \(pid 4242\), which isn't uni-switch/,
    container: /Port 3999 is forwarded by \/Applications\/OrbStack\.app\/.*OrbStack Helper \(pid 873\) to a container or a VM: the uni-switch 0\.1\.552 .*docker stop <name>.*another port in Settings/,
  },
  zh: {
    quit: "关闭 uni-switch 0.1.550 并接管网关", restart: "重启网关", running: "运行中",
    notUniSwitch: /端口 3999 被 \/usr\/bin\/python3（pid 4242）占用，它不是 uni-switch/,
    container: /端口 3999 由 \/Applications\/OrbStack\.app\/.*OrbStack Helper（pid 873）转发到一个容器或虚拟机：在那里应答的 uni-switch 0\.1\.552 .*docker stop <名称>.*换到其他端口/,
  },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    const w = words[lang];
    test(`${engine} ${lang}: the gateway card quits an older uni-switch, or restarts the gateway`, async (t) => {
      assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch());
      t.after(() => browser.close());
      const errors = [];
      const open = async (world, theme) => {
        const context = await browser.newContext({ viewport: { width: 420, height: 520 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", fixture(lang, world, theme));
        await page.goto("http://uni-switch.test/?view=gateway");
        await page.locator("#gateway .state").waitFor();
        return { context, page };
      };
      const scrolls = (page) => page.evaluate(() => [...document.querySelectorAll(".view, html, body")].map((e) => e.scrollTop).join(","));
      const noLeftBorder = (loc) => loc.evaluate((e) => parseFloat(getComputedStyle(e).borderLeftWidth) === 0);
      // a border all round, as a button's: no stripe down its left side
      const noLeftAccent = (loc) => loc.evaluate((e) => { const s = getComputedStyle(e); return s.borderLeftWidth === s.borderRightWidth && s.borderLeftColor === s.borderRightColor; });

      // a program that isn't uni-switch holds the port: said, by its path and pid
      {
        const world = {
          posts: [], gateway: { mine: false, version: "0.1.550", older: true },
          answer: () => ({ ok: false, reason: "not-uni-switch", port: "3999", pid: 4242, path: "/usr/bin/python3", version: "0.1.550" }),
        };
        const { context, page } = await open(world);
        const fix = page.locator("#gateway .gw-fix");
        assert.equal((await fix.textContent()).trim(), w.quit);
        assert(await noLeftAccent(fix), "the button has a left border");
        const before = await scrolls(page);
        await fix.click();
        await page.locator("#gateway .sub.err").waitFor();
        assert.deepEqual(world.posts, ["/api/gateway/take-over"]);
        assert.match(await page.locator("#gateway .sub.err").textContent(), w.notUniSwitch);
        assert(await noLeftBorder(page.locator("#gateway .sub.err")), "the error has a left border");
        assert.equal(await scrolls(page), before, "the click scrolled the page");
        assert(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), "the page scrolls sideways");
        await context.close();
      }

      // a uni-switch in a container (leslie_luo's OrbStack): said as a container's
      // to stop, in dark mode, where the button is bordered to read as one
      {
        const world = {
          posts: [], gateway: { mine: false, version: "0.1.552", older: true },
          answer: () => ({ ok: false, reason: "container", port: "3999", pid: 873, path: "/Applications/OrbStack.app/Contents/Frameworks/OrbStack Helper.app/Contents/MacOS/OrbStack Helper", version: "0.1.552" }),
        };
        const { context, page } = await open(world, "dark");
        assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), "dark");
        const fix = page.locator("#gateway .gw-fix");
        const look = await fix.evaluate((b) => {
          const s = getComputedStyle(b), card = getComputedStyle(b.closest(".gw") || document.body);
          return { top: parseFloat(s.borderTopWidth), left: parseFloat(s.borderLeftWidth), color: s.borderTopColor, bg: s.backgroundColor, card: card.backgroundColor };
        });
        assert(look.top >= 1 && look.left >= 1, `the button has no border in dark mode: ${JSON.stringify(look)}`);
        assert.notEqual(look.color, look.card, "the border is the card's colour");
        const before = await scrolls(page);
        await fix.click();
        await page.locator("#gateway .sub.err").waitFor();
        assert.match(await page.locator("#gateway .sub.err").textContent(), w.container);
        assert.equal(await scrolls(page), before, "the click scrolled the page");
        assert(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), "the page scrolls sideways");
        await context.close();
      }

      // an older uni-switch is quit and this one serves; the button restarts it then
      {
        const world = { posts: [], gateway: { mine: false, version: "0.1.550", older: true } };
        world.answer = (p) => {
          world.gateway = { mine: true, running: true };
          return { ok: true };
        };
        const { context, page } = await open(world);
        const fix = page.locator("#gateway .gw-fix");
        const before = await scrolls(page);
        await fix.click();
        await page.waitForFunction((r) => document.querySelector("#gateway .gw-fix")?.textContent.trim() === r, w.restart);
        assert.deepEqual(world.posts, ["/api/gateway/take-over"]);
        assert.equal((await page.locator("#gateway .state").textContent()).trim(), w.running);
        assert.equal(await page.locator("#gateway .sub.err").count(), 0);
        await fix.click();
        await page.waitForFunction(() => !document.querySelector("#gateway .gw-fix")?.disabled);
        assert.deepEqual(world.posts, ["/api/gateway/take-over", "/api/gateway/restart"]);
        assert.equal(await scrolls(page), before, "a click scrolled the page");
        await context.close();
      }

      // another uni-switch of this version: not this page's to quit
      {
        const world = { posts: [], gateway: { mine: false, version: "0.1.630" }, answer: () => ({ ok: true }) };
        const { context, page } = await open(world);
        assert.equal(await page.locator("#gateway .gw-fix").count(), 0);
        await context.close();
      }
      assert.deepEqual(errors, []);
    });
  }
}

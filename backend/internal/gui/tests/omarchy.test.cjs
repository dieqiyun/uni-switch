// Run with Node's test runner and Playwright on the module path; see README.md.
// Omarchy's look (omarchy.css, omarchy.js) and Settings → Bar icon: with no
// Omarchy theme in boot.js the page is as it always was (no class, no theme
// style, the Appearance choices, no Bar icon row, nothing asked of
// /api/omarchy); with one, the theme's colours and square corners, its name
// where the Appearance choices were, and the Bar icon row when the app says
// the bar is there, whose On and Off post and move nothing. English and
// Chinese; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

const theme = {
  name: "tokyo-night", mode: "dark", stamp: "1",
  vars: {
    "--bg": "#1a1b26", "--fg": "#a9b1d6", "--accent": "#7aa2f7", "--accent-fg": "#1a1b26", "--line": "#3b3d4d",
    "--pop-bg": "#1a1b26", "--font": "monospace", "--om-edge": "#7aa2f7", "--om-border": "2px", "--om-radius": "0px",
    "--om-size": "13px", "--om-hover": "rgba(169, 177, 214, 0.04)", "--om-hover-line": "rgba(169, 177, 214, 0.25)",
    "--om-sel": "rgba(122, 162, 247, 0.12)", "--om-sel-line": "rgba(122, 162, 247, 0.25)",
  },
};

function settingsPayload(lang) {
  return {
    theme: "system", lang, tray: "panel", quotaLeft: false, currency: "usd",
    dock: false, dockWindow: false, proxy: "", redact: false, redactPersonal: false, redactWords: [],
    codexWarmup: "", claudeWarmup: "", codexWarmAt: "", claudeWarmAt: "", workbuddyCheckin: false, noStats: false,
    trayUsage: "", trayUsageEvery: 3, vision: "", imageGen: "",
    version: "0.1.400", dir: "~/.config/uni-switch", gateway: "http://127.0.0.1:3425",
    proxyNow: "none", proxySource: "none", login: false,
    visionModels: [], imageGenModels: [], workbuddyCheckins: [], lanURLs: [],
    fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
  };
}

// om: the theme boot.js hands over, or none; bar: what /api/omarchy/widget says
function server(lang, om, bar, asked, posted) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") {
      const boot = { lang, theme: "system", web: false, ...(om ? { omarchy: om } : {}) };
      return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = ${JSON.stringify(boot)};` });
    }
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname.startsWith("/api/omarchy")) asked.push(req.method() + " " + url.pathname);
    if (url.pathname === "/api/omarchy") return json(om || null);
    if (url.pathname === "/api/omarchy/widget") {
      if (req.method() === "POST") {
        posted.push(req.postDataJSON());
        bar.on = req.postDataJSON().on;
      }
      return json(bar);
    }
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "system" } });
    if (url.pathname === "/api/settings") return json(settingsPayload(lang));
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const view = (page) => page.locator("#view-settings").evaluate((v) => v.scrollTop);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": Omarchy's look and Bar icon, only on Omarchy", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const pages = [];
    t.after(async () => {
      if (process.env.ARTIFACT_DIR) {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        for (const [i, p] of pages.entries()) await p.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-omarchy-${i}.png`) });
      }
      await browser.close();
    });

    for (const [lang, barName, on, off] of [["en", "Bar icon", "On", "Off"], ["zh", "状态栏图标", "开启", "关闭"]]) {
      await t.test(lang + ": elsewhere nothing changes", async () => {
        const errors = [], asked = [], posted = [];
        const context = await browser.newContext({ viewport: { width: 900, height: 420 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        pages.push(page);
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, null, { available: true, on: false }, asked, posted));
        await page.goto("http://uni-switch.test/");
        await page.locator("#prefs").click();
        await page.locator("#themeSegs .opt").first().waitFor();
        assert.equal(await page.evaluate(() => document.documentElement.classList.contains("omarchy")), false);
        assert.equal(await page.locator("#omarchy-theme").count(), 0, "no theme style");
        assert(await page.locator("#themeSegs").isVisible(), "the Appearance choices show");
        assert.equal(await page.locator(".om-theme-name").isVisible(), false);
        assert.equal(await page.locator("#barIconRow").isVisible(), false, "no Bar icon row");
        await page.waitForTimeout(3000); // past omarchy.js's poll
        assert.deepEqual(asked, [], "nothing is asked of /api/omarchy");
        assert.deepEqual(errors, []);
        await context.close();
      });

      await t.test(lang + ": on Omarchy", async () => {
        const errors = [], asked = [], posted = [];
        const context = await browser.newContext({ viewport: { width: 900, height: 420 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        pages.push(page);
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, theme, { available: true, on: false }, asked, posted));
        await page.goto("http://uni-switch.test/");
        assert.equal(await page.evaluate(() => document.documentElement.classList.contains("omarchy")), true);
        assert.equal(await page.evaluate(() => getComputedStyle(document.body).backgroundColor), "rgb(26, 27, 38)", "the theme's background");
        await page.locator("#prefs").click();
        await page.locator("#barIconSegs .opt").first().waitFor();
        assert.equal((await page.locator(".om-theme-name").textContent()).trim(), "tokyo-night");
        assert.equal(await page.locator("#themeSegs").isVisible(), false, "the theme is Omarchy's to pick");
        assert.equal(await page.locator("#textSizeSegs .opt").first().evaluate((b) => getComputedStyle(b).borderTopLeftRadius), "0px", "square");
        assert.equal((await page.locator("#barIconRow .name").textContent()).trim(), barName);

        // On, then Off: each posted, the pick drawn, the page not moved
        const segs = page.locator("#barIconSegs .opt");
        assert.deepEqual(await segs.allTextContents(), [off, on]);
        assert.equal(await segs.nth(0).evaluate((b) => b.classList.contains("on")), true, "off to start");
        // Font preferences add rows above the bar control. Bring it into
        // this short window with a reader's wheel before testing a click.
        await page.locator("#view-settings").hover();
        await page.mouse.wheel(0, 260);
        await page.waitForFunction(() => {
          const r = document.querySelector("#barIconSegs").getBoundingClientRect();
          const v = document.querySelector("#view-settings").getBoundingClientRect();
          return r.top >= v.top && r.bottom <= v.bottom;
        });
        const before = await view(page);
        await segs.nth(1).click();
        await page.locator("#barIconSegs .opt.on", { hasText: on }).waitFor();
        await segs.nth(0).click();
        await page.locator("#barIconSegs .opt.on", { hasText: off }).waitFor();
        assert.deepEqual(posted, [{ on: true }, { on: false }]);
        await page.waitForTimeout(300);
        assert.equal(await view(page), before, "a pick moves nothing");
        assert.deepEqual(errors, []);
        await context.close();
      });

      await t.test(lang + ": Omarchy without its bar (a browser's page)", async () => {
        const errors = [], asked = [], posted = [];
        const context = await browser.newContext({ viewport: { width: 900, height: 420 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, theme, { available: false, on: false }, asked, posted));
        await page.goto("http://uni-switch.test/");
        await page.locator("#prefs").click();
        await page.locator("#langPick span").first().waitFor();
        await page.waitForTimeout(300);
        assert(asked.includes("GET /api/omarchy/widget"));
        assert.equal(await page.locator("#barIconRow").isVisible(), false);
        assert.deepEqual(errors, []);
        await context.close();
      });
    }
  });
}

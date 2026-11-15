// Run with Node's test runner and Playwright on the module path; see README.md.
// registry.json gives Kilo Code, Search1API and PowerContext an https
// picture as their icon where the rest give a bundled icon's name. The
// server fetches and keeps such a picture (provider.MarketIcon) and the
// listing says file:…; Discover's card and the plugin's page draw it. A
// URL that still reaches the page is never taken for a bundled file's
// name (icons/https://….svg) nor loaded from its host: the card wears
// the generic cube at once. A kept picture that fails to load wears the
// cube too, never a blank square. Every language, both engines, wide and
// narrow; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const KILO = "@uni-switch-community/agent-kilo", S1 = "@uni-switch-community/tools-search1api";
const PC = "@uni-switch-community/tools-powercontext", COPILOT = "the retired opencode-copilot-auth";
const npm = { version: "0.1.0", weekly: 5, license: "MIT" };
const listings = [
  { package: KILO, name: "Kilo Code", kind: "agent", community: true, icon: "file:kilo.png", summary: { en: "Kilo Code." }, npm },
  { package: S1, name: "Search1API", kind: "tools", community: true, icon: "https://s1.dev/icon.png", summary: { en: "Search." }, npm },
  { package: PC, name: "PowerContext", kind: "tools", community: true, icon: "file:gone.png", summary: { en: "Context." }, npm },
  { package: COPILOT, name: "GitHub Copilot", community: true, icon: "githubcopilot", providers: ["github-copilot"], summary: { en: "Copilot." }, npm },
];
const state = { bun: true, bunVersion: "1.4.2", plugins: [] };
// a 1x1 picture
const PNG = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==", "base64");

function server(lang, asked) {
  return async (route) => {
    const url = new URL(route.request().url());
    asked.push(url.href);
    const json = (data) => route.fulfill({ json: data });
    if (url.host !== "uni-switch.test") return route.fulfill({ status: 404, body: "" });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/plugins") return json(state);
    if (url.pathname === "/api/plugins/listings") return json({ listings });
    if (url.pathname === "/api/plugins/market") return json({ listings, state });
    if (url.pathname === "/api/icons/kilo.png") return route.fulfill({ contentType: "image/png", body: PNG });
    if (url.pathname.startsWith("/api/icons/")) return route.fulfill({ status: 404, body: "" });
    if (url.pathname.startsWith("/api/")) return json({});
    if (!/^\/[\w./-]*$/.test(url.pathname)) return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a registry icon that is a picture draws it, else the cube", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) for (const width of [980, 420]) {
      await t.test(lang + " " + width, async () => {
        const ctx = await browser.newContext({ viewport: { width, height: 820 } });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], asked = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, asked));
        await page.goto("http://uni-switch.test/?view=plugins");
        const card = (pkg) => page.locator(`#view-plugins .pm-card[data-pkg="${pkg}"] .pm-logo`);
        // the list is drawn again as the market comes in: a card found a
        // moment ago can be gone by the scroll, so the scroll looks again
        const into = async (loc) => {
          for (let i = 0; ; i++) {
            try { return await loc.scrollIntoViewIfNeeded({ timeout: 1000 }); }
            catch (e) { if (i > 8 || !/not attached|Timeout/.test(e.message)) throw e; }
          }
        };
        await card(KILO).waitFor();
        await into(card(KILO));
        // the kept picture, drawn
        assert.equal(await card(KILO).locator("img").getAttribute("src"), "/api/icons/kilo.png");
        await page.waitForFunction((pkg) => document.querySelector(`#view-plugins .pm-card[data-pkg="${pkg}"] .pm-logo img`)?.naturalWidth > 0, KILO);
        // a URL: the cube at once, nothing asked for it
        await into(card(S1));
        assert.equal(await card(S1).locator(".ic.generic").count(), 1, "a URL wears the cube");
        // a kept picture that fails: the cube, not a blank square
        await into(card(PC));
        await card(PC).locator(".ic.generic").waitFor();
        assert.equal(await card(PC).locator("img").count(), 0);
        // a bundled name is still a bundled icon
        assert.equal(await card(COPILOT).locator('.ic[data-icon="githubcopilot"]').count(), 1);
        for (const pkg of [KILO, S1, PC]) {
          const box = await card(pkg).boundingBox();
          assert.ok(box && box.width > 0 && box.x >= 0 && box.x + box.width <= width, `${pkg}'s logo fits: ${JSON.stringify(box)}`);
        }

        // the plugin's page wears the same
        await page.locator(`#view-plugins .pm-card[data-pkg="${S1}"] .pm-name`).first().click();
        const dlg = page.locator("#modal .pm-detail");
        await dlg.waitFor();
        assert.equal(await dlg.locator(".pm-dhead .pm-logo .ic.generic").count(), 1, "the page of a URL wears the cube");
        await page.goto("http://uni-switch.test/?view=plugins");
        await page.locator(`#view-plugins .pm-card[data-pkg="${KILO}"] .pm-name`).first().click();
        await dlg.waitFor();
        assert.equal(await dlg.locator(".pm-dhead .pm-logo img").getAttribute("src"), "/api/icons/kilo.png");

        await page.waitForTimeout(0);
        const wrong = asked.filter((u) => !u.startsWith("http://uni-switch.test/") || /\/icons\/.*(https?:|s1\.dev)/.test(decodeURIComponent(u)));
        assert.deepEqual(wrong, [], "nothing asked of another host, nor a bundled file named by a URL");
        assert.deepEqual(errors, []);
        await ctx.close();
      });
    }
  });
}

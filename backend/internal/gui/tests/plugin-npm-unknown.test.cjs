// Run with Node's test runner and Playwright on the module path; see README.md.
// A suggested plugin npm said nothing of (npm, or the community repo's
// dist branch, not read in time) is unknown: its card offers Install, not
// a disabled Coming soon nothing confirmed (zhudabao8433 on Discord:
// PowerContext, only on the dist branch, installable the day before, read
// Coming soon). The page asks again, and the answer that comes shows its
// version. One npm said it doesn't have still reads Coming soon. Every
// language, both engines, wide and narrow; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const PC = "@uni-switch-community/gateway-powercontext", QODER = "the retired opencode-qoder-auth";
const listings = [
  { package: PC, name: "PowerContext", kind: "middleware", community: true, summary: { en: "Recall from your PowerContext service." } },
  { package: QODER, name: "Qoder", icon: "qoder", providers: ["qoder"], community: true, summary: { en: "Your Qoder subscription." }, npm: { weekly: 0 } },
];
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const state = { bun: true, bunVersion: "1.4.2", plugins: [] };

const L = {
  en: { install: "Install", soon: "Coming soon" },
  zh: { install: "安装", soon: "即将上线" },
  "zh-TW": { install: "安裝", soon: "即將上線" },
  ja: { install: "インストール", soon: "近日公開" },
  de: { install: "Installieren", soon: "Demnächst" },
};

function server(lang, asks) {
  return async (route) => {
    const url = new URL(route.request().url());
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
    if (url.pathname === "/api/plugins/npm") {
      const names = (url.searchParams.get("names") || "").split(",");
      asks.push(names);
      // the first ask: the dist branch not read in time, nothing said of
      // PowerContext; the next: the branch read, its 0.2.0
      return json({ npm: asks.length > 1 && names.includes(PC) ? { [PC]: { version: "0.2.0", weekly: 0 } } : {} });
    }
    if (url.pathname.startsWith("/api/")) return json({});
    if (!/^\/[\w./-]*$/.test(url.pathname)) return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a plugin npm said nothing of offers Install, not Coming soon", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of Object.keys(L)) for (const width of [980, 420]) {
      await t.test(lang + " " + width, async () => {
        const w = L[lang];
        const ctx = await browser.newContext({ viewport: { width, height: 820 } });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], asks = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.clock.install();
        await page.route("**/*", server(lang, asks));
        await page.goto("http://uni-switch.test/?view=plugins");
        const act = (pkg) => page.locator(`#view-plugins .pm-card[data-pkg="${pkg}"] .pm-act`);
        await act(PC).waitFor();
        // npm's first answer, with nothing of PowerContext, has come
        for (let i = 0; i < 100 && !asks.length; i++) await sleep(50);
        assert.equal(asks.length, 1, "npm asked once");
        await page.waitForFunction(([sel, word]) => document.querySelector(sel)?.textContent === word,
          [`#view-plugins .pm-card[data-pkg="${PC}"] .pm-act`, w.install]);
        assert.equal(await act(PC).isDisabled(), false, "Install can be clicked");
        assert.equal(await act(QODER).innerText(), w.soon, "one npm said it doesn't have is still Coming soon");
        assert.equal(await act(QODER).isDisabled(), true);
        const box = await act(PC).boundingBox();
        assert.ok(box && box.x >= 0 && box.x + box.width <= width, `Install fits: ${JSON.stringify(box)}`);

        // asked again, the branch now read: its version shows
        await page.clock.fastForward(16000);
        for (let i = 0; i < 100 && asks.length < 2; i++) await sleep(50);
        assert.ok(asks.length >= 2 && asks[1].includes(PC), "npm asked again of PowerContext: " + JSON.stringify(asks));
        await page.locator(`#view-plugins .pm-card[data-pkg="${PC}"] .pm-ver`).waitFor();
        assert.match(await page.locator(`#view-plugins .pm-card[data-pkg="${PC}"] .pm-ver`).innerText(), /0\.2\.0/);
        assert.equal(await act(PC).innerText(), w.install);
        assert.deepEqual(errors, []);
        await ctx.close();
      });
    }
  });
}

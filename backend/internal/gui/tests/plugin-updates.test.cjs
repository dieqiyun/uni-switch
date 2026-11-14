// Run with Node's test runner and Playwright on the module path; see README.md.
// Plugin updates: a plugin whose update waits for the reader (someone
// else's, or one pinned) puts a dot on Plugins, which goes once it's
// updated; a plugin uni-switch updated by itself says so on its row. The dot
// says itself in the sidebar: under Plugins, Installed reads "1 update" in
// its count's place while it waits, and Plugins opens on Installed, where
// the plugins with an update are named above the list. Every language, wide and narrow; the
// API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function server(lang) {
  const installed = [
    { spec: "opencode-copilot-auth", providers: ["GitHub Copilot"], version: "0.0.7", latest: "0.0.9" },
    { spec: "the retired opencode-zed-auth", providers: ["Zed"], version: "0.1.4", latest: "0.1.4",
      autoUpdated: { package: "the retired opencode-zed-auth", from: "0.1.3", to: "0.1.4", at: "2026-09-30T08:00:00Z" } },
  ];
  const waiting = () => installed.filter((e) => e.version !== e.latest).map((e) => ({ spec: e.spec, package: e.spec, version: e.version, latest: e.latest }));
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname === "/api/plugins/updates") return json({ checked: "2026-10-01T00:00:00Z", waiting: waiting(), updated: [] });
    // the page asks for the market in parts (#488)
    if (url.pathname === "/api/plugins/market" || url.pathname === "/api/plugins" || url.pathname === "/api/plugins/listings") { const m = { listings: [], state: { bun: true, bunVersion: "1.3.0", plugins: installed } }; return json(url.pathname === "/api/plugins" ? m.state : url.pathname === "/api/plugins/listings" ? { listings: m.listings } : m); }
    if (url.pathname === "/api/plugins/upgrade") { installed[0].version = "0.0.9"; return json({}); }
    if (url.pathname.startsWith("/api/")) return json({});
    if (!/^\/[\w./-]*$/.test(url.pathname) || url.host !== "uni-switch.test") return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const L = {
  en: { bar: "A newer version of opencode-copilot-auth is out", installed: "Installed", update: "Update", auto: "Auto-updated", out: "An update for opencode-copilot-auth is out", from: /from v0\.1\.3 to v0\.1\.4/, one: "1 update" },
  zh: { bar: "opencode-copilot-auth 有新版本", installed: "已安装", update: "更新", auto: "已自动更新", out: "opencode-copilot-auth 有新版本", from: /从 v0\.1\.3 更新到 v0\.1\.4/, one: "1 个更新" },
  "zh-TW": { bar: "opencode-copilot-auth 有新版本", out: "opencode-copilot-auth 有新版本", installed: "已安裝", one: "1 個更新" },
  ja: { bar: "opencode-copilot-auth の新しいバージョンがあります", out: "opencode-copilot-auth のアップデートが公開されました", installed: "インストール済み", one: "1 件のアップデート" },
  de: { bar: "Eine neue Version von opencode-copilot-auth ist da", out: "Ein Update für opencode-copilot-auth ist da", installed: "Installiert", one: "1 Update" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": plugin updates", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of Object.keys(L)) for (const width of [1024, 600]) {
      await t.test(lang + " " + width, async () => {
        const w = L[lang];
        const page = await (await browser.newContext({ viewport: { width, height: 820 } })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang));

        await page.goto("http://uni-switch.test/?view=providers");
        const nav = page.locator('#nav button[data-view="plugins"]');
        await page.waitForFunction(() => document.querySelector('#nav button[data-view="plugins"]')?.classList.contains("has-dot"));
        assert.equal(await nav.getAttribute("title"), w.out);
        // the dot is drawn, small, inside the button (a little larger at the
        // sidebar row's end than at a tab's corner)
        const dot = await nav.evaluate((b) => { const s = getComputedStyle(b, "::after"); return { w: s.width, pos: s.position }; });
        assert.deepEqual(dot, { w: await page.evaluate(() => document.body.classList.contains("side-nav")) ? "6px" : "5px", pos: "absolute" });

        await nav.click();
        // in the sidebar, the row the reader acts on says what the dot is
        const side = await page.evaluate(() => document.body.classList.contains("side-nav"));
        const up = page.locator(".nav-s em.up");
        assert.equal(side, width >= 1024, "a sidebar at " + width);
        if (side) {
          await up.waitFor();
          assert.equal(await up.textContent(), w.one);
          assert.match(await up.evaluate((e) => e.parentElement.textContent), new RegExp("^" + w.installed));
          // read where it is found, in one go: the rows are drawn again as
          // the page's counts come in, so a label found a moment ago may be
          // gone, and a gone one has no colour or box
          const label = await page.evaluate(() => {
            const e = document.querySelector(".nav-s em.up"), r = e.getBoundingClientRect(), b = e.parentElement.getBoundingClientRect();
            const p = document.createElement("i");
            p.style.color = "var(--accent)";
            document.body.append(p);
            const accent = getComputedStyle(p).color;
            p.remove();
            return { color: getComputedStyle(e).color, accent, fits: r.right <= b.right + 0.5 && r.left >= b.left };
          });
          assert.equal(label.color, label.accent, "in the dot's colour");
          assert.ok(label.fits, "the label fits its row");
        }
        // the dot, followed, lands on Installed (the page was last on
        // Discover), which names the plugins with an update first
        const view = page.locator("#view-plugins");
        const bar = view.locator(".pm-updates");
        await bar.waitFor();
        assert.equal(await bar.locator(".grow").textContent(), w.bar);
        assert.equal(await view.locator(".lib-tabs .opt.on").getAttribute("data-sub"), "installed");
        assert.ok(await bar.evaluate((b) => b.getBoundingClientRect().right <= b.closest("#view-plugins").getBoundingClientRect().right + 0.5), "the bar fits");
        assert.ok(await bar.evaluate((b) => b.compareDocumentPosition(b.parentElement.querySelector(".pm-list")) & Node.DOCUMENT_POSITION_FOLLOWING), "above the list");
        if (!w.update) { assert.deepEqual(errors, []); return; }
        const chip = view.locator(".pm-chip.soft", { hasText: w.auto });
        await chip.waitFor();
        assert.match(await chip.getAttribute("title"), w.from);

        // updating the waiting one takes the dot away
        await view.getByRole("button", { name: w.update, exact: true }).first().click();
        await page.waitForFunction(() => !document.querySelector('#nav button[data-view="plugins"]').classList.contains("has-dot"));
        assert.equal(await nav.getAttribute("title"), null);
        if (side) await page.locator(".nav-s", { hasText: w.installed }).locator("em:not(.up)").waitFor();
        assert.equal(await up.count(), 0);
        assert.equal(await bar.count(), 0);
        assert.deepEqual(errors, []);
      });
    }
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// Sign in on Plugins › Installed opens the Providers add sheet with that
// plugin's sign-in drawn at the sheet's foot, under every tile. With enough
// plugins the sheet is taller than the window, and the click left the reader
// at the top, the sign-in out of sight: the Sign in seemed to do nothing. The
// click that asked for it now takes the reader down to it.
// In Chromium and WebKit, en and zh; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const LAST = "opencode-zz-last-auth";

// 24 plugins signed in already, each with a tile in the sheet, and the one
// signed out last
const subs = [];
for (let i = 0; i < 24; i++) {
  const n = String(i).padStart(2, "0");
  subs.push({ id: `p${n}`, pid: `p${n}`, name: `Plugin ${n}`, icon: "generic", spec: `opencode-p${n}-auth`, signedIn: true, models: 2, methods: [{ type: "api", label: "API key" }] });
}
subs.push({ id: "zzlast", pid: "zzlast", name: "Last One", icon: "generic", spec: LAST, signedIn: false, models: 2, methods: [{ type: "api", label: "API key" }] });

function server(lang) {
  const providers = { providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: subs };
  const plugins = { plugins: subs.map((x) => ({ spec: x.spec, providers: [x.name], named: true, version: "0.1.0", moved: [] })), bun: true, bunVersion: "1.3.0", picker: false, mirror: false };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/plugins") return json(plugins);
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/plugin-signin/prompt") return json({});
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const words = { en: { signIn: "Sign in" }, zh: { signIn: "登录" } };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": Sign in on Installed takes the reader to the plugin's sign-in", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh"]) {
      await t.test(lang, async () => {
        const ctx = await browser.newContext({ viewport: { width: 900, height: 560 }, reducedMotion: "reduce" });
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.pluginTab", "installed"); } catch {} });
        const page = await ctx.newPage();
        t.after(() => ctx.close());
        page.setDefaultTimeout(15000); // a busy machine takes seconds over a click
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang));
        await page.goto("http://uni-switch.test/?view=plugins");
        const row = page.locator("#view-plugins .pm-row", { hasText: LAST });
        const button = row.locator("button", { hasText: new RegExp(`^${words[lang].signIn}$`) });
        await button.waitFor();
        // the reader scrolls down to the row: a scroll no reader made is put
        // back, Playwright's own among them
        await page.mouse.move(450, 300);
        for (let i = 0; i < 40 && !(await button.evaluate((b) => { const r = b.getBoundingClientRect(); return r.top >= 0 && r.bottom <= innerHeight; })); i++) {
          await page.mouse.wheel(0, 300);
          await page.waitForTimeout(50);
        }
        await button.click();
        const box = page.locator("#addSheet .signing");
        await box.locator(".callback-form input").waitFor();
        const at = await page.evaluate(() => {
          const v = document.querySelector("#view-providers"), b = document.querySelector("#addSheet .signing");
          const r = b.getBoundingClientRect(), w = v.getBoundingClientRect();
          return { top: v.scrollTop, height: v.scrollHeight, client: v.clientHeight, inSight: r.top >= w.top && r.top < w.bottom };
        });
        assert(at.height > 1.5 * at.client, `the sheet is taller than the window (${at.height} over ${at.client})`);
        assert(at.top > 0, "the page went down to the sign-in");
        assert(at.inSight, "the sign-in is in sight");
        assert.match(await box.innerText(), /Last One/);
        assert.deepEqual(errors, []);
      });
    }
  });
}

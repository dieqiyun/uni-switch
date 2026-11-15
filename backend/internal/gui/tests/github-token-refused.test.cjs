// Run with Node's test runner and Playwright on the module path; see README.md.
// #3926 (MYCF711): a GitHub token in Settings that GitHub refuses no longer
// fails every check; requests go without it. The page stays honest about
// it: Settings › Network still shows the token, masked, saying
// GitHub refused it and requests go without one, with Remove to replace
// it, rather than reading as no token set. English and Chinese, wide and
// 440px, Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn } = require("./reader.cjs");

const assets = path.resolve(process.env.UNI_SWITCH_REFUSED_ASSETS || path.resolve(__dirname, "../assets"));
const W = {
  en: { remove: "Remove", row: "GitHub refused this token: it may have expired or been revoked. Requests go without one (60 an hour) until it is replaced." },
  zh: { remove: "移除", row: "GitHub 拒绝了这个令牌：它可能已过期或被撤销。在换成新令牌之前，请求改为不带令牌发出（每小时 60 次）。" },
};

function serve(lang, refused) {
  const settings = () => ({ lang, theme: "light", searchVendors: [], searchAPIs: [], githubTokenMask: "ghp_…0123", githubTokenFrom: "settings", ...(refused ? { githubTokenRefused: true } : {}) });
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: settings() });
    if (url.pathname === "/api/settings") return json(settings());
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ models: [], groups: [], pools: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    if (!/^\/[\w./-]*$/.test(url.pathname) || url.host !== "uni-switch.test") return r.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await r.fulfill({ body: await fs.readFile(file), contentType }); } catch { await r.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a refused Settings token is said in Settings", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of Object.keys(W)) {
      for (const width of [1000, 440]) {
        await t.test(lang + " " + width, async () => {
          const w = W[lang];
          for (const refused of [true, false]) {
            const ctx = await browser.newContext({ viewport: { width, height: 860 } });
            const page = await ctx.newPage();
            page.setDefaultTimeout(5000);
            page.setDefaultNavigationTimeout(60000);
            const errors = [];
            page.on("pageerror", (e) => errors.push(e.message));
            await page.route("**/*", serve(lang, refused));

            // Settings: the token still shown, masked, and said refused
            await watchSettingsReads(page);
            await page.goto("http://uni-switch.test/?view=settings&tab=network");
            await settingsDrawn(page);
            const row = page.locator("#githubList .row.github-token");
            await row.waitFor();
            assert.equal(await row.locator("code").innerText(), "ghp_…0123");
            assert.equal(await row.locator("button.text", { hasText: w.remove }).count(), 1);
            const sub = row.locator(".sub");
            if (refused) {
              assert.equal(await sub.innerText(), w.row);
              assert.ok(await sub.evaluate((e) => e.classList.contains("err")), "said as an error");
            } else {
              assert.notEqual(await sub.innerText(), w.row);
              assert.ok(!(await sub.evaluate((e) => e.classList.contains("err"))));
            }

            assert.ok(await sub.evaluate((e) => e.scrollWidth <= e.clientWidth + 1 || getComputedStyle(e).textOverflow === "ellipsis"), "the line fits");
            assert.deepEqual(errors, []);
            await ctx.close();
          }
        });
      }
    }
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// The menu-bar quota picker lists one row per account, so a subscription with
// many accounts makes its proto menu taller than the window. The base
// .pop.proto-menu had max-height:none with no overflow, so the menu ran off
// the window's bottom edge — the last subscriptions were unreachable, and
// scrolling did nothing because the box itself never scrolled. It now clamps
// to min(360px,70vh) and scrolls, as its siblings (.sess-menu,
// .lan-address-menu) already did. This opens the picker with enough accounts
// to overflow, checks the menu stays inside the viewport and scrolls to the
// last row, and that a click still picks it. English and Chinese, Chromium
// and WebKit; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { openPart } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");

// one subscription with enough accounts that its rows overflow the window.
const N = 15;
const quotas = Array.from({ length: N }, (_, i) => ({
  provider: "claude", name: "Claude Code", icon: "claude-color",
  user: `acct${i}@b.c`, plan: "Max", windows: [{ name: "5-hour", used: i }],
}));

function server(lang, posts) {
  let cur = {
    theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd",
    dock: false, dockWindow: false, proxy: "", redact: false, redactPersonal: false, redactWords: [],
    codexWarmup: "", claudeWarmup: "", codexWarmAt: "", claudeWarmAt: "", workbuddyCheckin: false, noStats: false,
    trayUsage: "", trayUsages: [], trayUsageEvery: 3, vision: "", imageGen: "",
    version: "0.1.400", dir: "~/.config/uni-switch", gateway: "http://127.0.0.1:3425",
    proxyNow: "none", proxySource: "none", login: false,
    visionModels: [], imageGenModels: [], workbuddyCheckins: [], lanURLs: [],
    fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
  };
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/settings") {
      if (req.method() === "POST") {
        const body = req.postDataJSON();
        posts.push(body);
        cur = { ...cur, ...body, trayUsage: (body.trayUsages || [])[0] || "" };
      }
      return json(cur);
    }
    if (url.pathname === "/api/usage/quotas") return json(quotas);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const VIEWPORT = { width: 900, height: 600 };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a quota picker taller than the window clamps and scrolls`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const page = await (await browser.newContext({ viewport: VIEWPORT, reducedMotion: "reduce" })).newPage();
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-proto-overflow.png`) });
        }
        await browser.close();
      });
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [], posts = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(lang, posts));
      await page.goto("http://uni-switch.test/?view=usage");
      await page.locator("#prefs").click();
      await openPart(page, "usage");
      const pill = page.locator("#trayUsagePick button");
      await pill.waitFor();
      await pill.scrollIntoViewIfNeeded();

      await pill.click();
      const menu = page.locator(".proto-menu");
      const items = page.locator(".proto-menu .pm-item");
      await items.first().waitFor();
      assert.equal(await items.count(), N + 4, "off, the account in use, each account, then uni-switch's own two totals (#1984)");

      // the menu is clamped to the viewport, not grown past its bottom edge.
      const box = await menu.boundingBox();
      assert.ok(box, "the menu is open");
      assert.ok(box.y + box.height <= VIEWPORT.height + 1,
        `the menu's bottom (${(box.y + box.height).toFixed(0)}) stays inside the ${VIEWPORT.height}px window`);

      // it overflows and scrolls to the last account, which a click picks.
      const overflows = await menu.evaluate((e) => e.scrollHeight > e.clientHeight);
      assert.ok(overflows, "with this many accounts the menu must be scrollable");
      const last = items.nth(N + 1);
      await last.scrollIntoViewIfNeeded();
      const scrolled = await menu.evaluate((e) => e.scrollTop > 0);
      assert.ok(scrolled, "scrolling the menu moves it, so the last rows are reachable");
      await last.click();
      await page.keyboard.press("Escape");
      const saved = posts.at(-1)?.trayUsages;
      assert.ok(Array.isArray(saved) && saved.length === 1 && /acct\d+@b\.c$/.test(saved[0]),
        `the last account was picked and saved: ${JSON.stringify(saved)}`);
      assert.deepEqual(errors, []);
    });
  }
}

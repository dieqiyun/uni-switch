// Run with Node's test runner and Playwright on the module path; see README.md.
// Settings › About and the title bar hold still while the update's state
// comes in (#4057):
// - opened cold, with no state read yet, the Version row keeps the line
//   under its name for the answer, rather than growing 12px when it lands;
// - opened with a state read a minute ago ("Up to date") while a check has
//   since downloaded a newer version, that old state isn't drawn first and
//   swapped for "0.1.401 is downloaded";
// - the header's Update pill showing opens from nothing, so the Settings
//   search in the title bar beside the sidebar slides over with it rather
//   than jumping 80px left in one frame.
// English and Chinese; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const words = {
  en: { version: "Version", latest: "Up to date", ready: "0.1.401 is downloaded" },
  zh: { version: "版本", latest: "已是最新", ready: "0.1.401 已下载" },
};

function server(lang, ctl) {
  const settings = {
    theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd", proxy: "", redact: false, redactWords: [],
    version: "0.1.400", dir: "~/.config/uni-switch", gateway: "http://127.0.0.1:3425", proxyNow: "none", proxySource: "none",
    visionModels: [], imageGenModels: [], workbuddyCheckins: [], lanURLs: [], fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
  };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" }, fx: settings.fx });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/update") {
      if (ctl.hold) await ctl.hold;
      return json(ctl.update || { state: "latest", current: "0.1.400" });
    }
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    if (!/^\/[\w./-]*$/.test(url.pathname) || url.host !== "uni-switch.test") return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const ready = { state: "ready", current: "0.1.400", latest: "0.1.401" };

// every frame, the Version row's height and the line under its name
const watchRow = (version) => {
  window.__rows = [];
  const look = () => {
    const r = [...document.querySelectorAll("#about .row.pref")].find((r) => r.querySelector(".name")?.textContent === version);
    if (r && r.offsetParent && !r.dataset.left) window.__rows.push({ h: r.getBoundingClientRect().height, sub: r.querySelector(".who .sub")?.textContent ?? null });
    if (!window.__stop) requestAnimationFrame(look);
  };
  look();
};

async function open(browser, lang, ctl, width, url) {
  const errors = [];
  const page = await (await browser.newContext({ viewport: { width, height: 700 } })).newPage();
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  page.on("pageerror", (e) => { if (!/^ResizeObserver loop completed with undelivered notifications/.test(e.message)) errors.push(e.message); });
  await page.route("**/*", server(lang, ctl));
  await watchSettingsReads(page);
  await page.addInitScript(`(${watchRow})(${JSON.stringify(words[lang].version)})`);
  await page.goto(url);
  return { page, errors };
}

const versionRow = (page, lang) => page.locator("#about .row.pref", { has: page.locator(".name", { hasText: words[lang].version }) }).first();

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": the Version row and the title bar hold still while the update's state comes in (#4057)", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of Object.keys(words)) {
      const w = words[lang];

      await t.test(lang + ": opened cold, the row keeps its line for the answer", async () => {
        let release;
        const ctl = { hold: new Promise((go) => (release = go)) };
        const { page, errors } = await open(browser, lang, ctl, 900, "http://uni-switch.test/?view=settings&tab=about");
        await settingsDrawn(page);
        await page.waitForFunction(() => window.__rows.length > 20);
        release();
        const row = versionRow(page, lang);
        await row.locator(".sub", { hasText: w.latest }).waitFor();
        await page.evaluate(() => (window.__stop = true));
        const tall = (await row.boundingBox()).height;
        const rows = await page.evaluate(() => window.__rows.slice());
        const off = rows.filter((s) => Math.abs(s.h - tall) > 0.5);
        assert.deepEqual(off.slice(0, 3), [], `the row is ${tall}px with its answer; ${off.length} of ${rows.length} frames drew it otherwise`);
        assert.deepEqual(errors, []);
        await page.context().close();
      });

      await t.test(lang + ": a state read a minute ago isn't drawn before the answer", async () => {
        const ctl = {};
        const { page, errors } = await open(browser, lang, ctl, 900, "http://uni-switch.test/?view=settings&tab=about");
        await settingsDrawn(page);
        const row = versionRow(page, lang);
        await row.locator(".sub", { hasText: w.latest }).waitFor();
        const tall = (await row.boundingBox()).height;
        await page.evaluate(() => show("agents"));
        // a minute on, a check has downloaded 0.1.401; its read is held
        let release;
        ctl.hold = new Promise((go) => (release = go));
        ctl.update = ready;
        // the row left on screen is drawn again when Settings is: only the
        // new row's frames count
        await page.evaluate((version) => {
          for (const r of document.querySelectorAll("#about .row.pref")) r.dataset.left = "1";
          const now = Date.now;
          Date.now = () => now() + 60e3;
          window.__rows = [];
          show("settings");
        }, w.version);
        await page.waitForFunction(() => window.__rows.length > 20);
        release();
        await row.locator(".sub", { hasText: w.ready }).waitFor();
        await page.evaluate(() => (window.__stop = true));
        const rows = await page.evaluate(() => window.__rows.slice());
        const stale = rows.filter((s) => s.sub === w.latest);
        assert.equal(stale.length, 0, `"${w.latest}", a minute old, was drawn in ${stale.length} of ${rows.length} frames before "${w.ready}"`);
        const off = rows.filter((s) => Math.abs(s.h - tall) > 0.5);
        assert.deepEqual(off.slice(0, 3), [], `the row is ${tall}px; ${off.length} of ${rows.length} frames drew it otherwise`);
        assert.deepEqual(errors, []);
        await page.context().close();
      });

      await t.test(lang + ": the Update pill opens from nothing, the search beside the sidebar sliding with it", async () => {
        const ctl = {};
        const { page, errors } = await open(browser, lang, ctl, 1200, "http://uni-switch.test/?view=settings&tab=about");
        await settingsDrawn(page);
        await versionRow(page, lang).locator(".sub", { hasText: w.latest }).waitFor();
        assert.ok(await page.evaluate(() => document.body.classList.contains("side-nav")), "a sidebar at 1200px");
        const frames = () => page.evaluate(() => new Promise((go) => requestAnimationFrame(() => requestAnimationFrame(() => requestAnimationFrame(go)))));
        await frames();
        const at = () => page.evaluate(() => $("#setQ").getBoundingClientRect().left);
        const before = await at();
        // the pill's opening is held at its first frame, as it is shown
        ctl.update = ready;
        await page.evaluate(() => {
          const b = $("#update");
          new MutationObserver(() => {
            if (b.hidden) return;
            window.__opening = b.getAnimations();
            for (const a of window.__opening) { a.pause(); a.currentTime = 0; }
          }).observe(b, { attributes: true, attributeFilter: ["hidden"] });
          renderUpdateBadge();
        });
        await page.waitForFunction(() => !$("#update").hidden && window.__opening);
        await frames();
        const first = await at();
        assert.ok(Math.abs(first - before) <= 1, `the search moved ${before - first}px in the pill's first frame`);
        await page.evaluate(() => { for (const a of window.__opening) a.finish(); });
        await frames();
        const after = await at();
        assert.ok(before - after > 30, `the pill took its room once open: the search at ${before}, then ${after}`);
        assert.deepEqual(errors, []);
        await page.context().close();
      });
    }
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// #2696 (LoKi, mufanmu): a Qoder campaign can open later in the Beijing day,
// so uni-switch asks Qoder's check-in again, the built-in's and the Qoder and
// Qoder CN plugins', on a press. A Qoder account whose check-in found no
// campaign (inactive) today keeps Check in now on its card, built-in
// (checkinBy "qoder") and plugin ("plugin:qoder-cn") alike, and a press
// that finds none again says it is not available yet rather than
// "Checked in" (#3326). Another vendor's inactive, a plugin's included,
// is settled for the day and has no press. After LoKi's patch on the issue.
// English, Simplified and Traditional Chinese, Japanese and German, the
// window and the panel's 440px; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn, reach } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const today = new Date(Date.now() + 8 * 3600e3).toISOString().slice(0, 10);
const card = (provider, name, user, by) => ({
  provider, name, plan: "Pro", user, windows: [{ name: "Credits", used: 1 }],
  checkins: true, checkinBy: by, checkin: { user, by, day: today, outcome: "inactive" },
});
const quotas = () => [
  card("qoder", "Qoder", "arno", "qoder"),
  card("plugin:qoder-cn", "Qoder CN", "lei", "plugin:qoder-cn"),
  card("plugin:fakeco", "FakeCo", "ann", "plugin:fakeco"),
  card("workbuddy", "WorkBuddy", "lucy", ""),
];

function serve(lang, asked) {
  const settings = { theme: "light", lang, quotaLeft: false, currency: "usd", qoderCheckin: true, workbuddyCheckin: true, pluginCheckins: { "qoder-cn": true, fakeco: true } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/usage/qoder-checkin" || url.pathname === "/api/usage/plugin-checkin") {
      asked.push([url.pathname, route.request().postDataJSON()]);
      return json([{ outcome: "inactive" }]);
    }
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") return json(quotas());
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const refreshed = { en: "Not available yet: 1", zh: "暂不可签到：1", "zh-TW": "暫不可簽到：1", ja: "まだチェックインできません：1", de: "Noch nicht verfügbar: 1" };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [900, 440]) {
      test(`${engine} ${lang} ${width}px: an inactive Qoder check-in can be pressed again`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await (await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], asked = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, asked));
        await watchSettingsReads(page);
        await page.goto("http://uni-switch.test/?view=usage");
        await settingsDrawn(page);
        const of = (name) => page.locator(".subscription-card", { hasText: name });
        // each card by its account: "Qoder" is in Qoder CN's name too
        for (const user of ["arno", "lei", "ann", "lucy"]) await of(user).locator(".wb-checkin").first().waitFor();
        assert.equal(await of("ann").locator(".ci-now").count(), 0, "another plugin's inactive is settled for the day");
        assert.equal(await of("lucy").locator(".ci-now").count(), 0, "another vendor's inactive is settled for the day");

        for (const [user, want] of [
          ["arno", ["/api/usage/qoder-checkin", {}]],
          ["lei", ["/api/usage/plugin-checkin", { provider: "qoder-cn" }]],
        ]) {
          const now = of(user).locator(".ci-now");
          assert.equal(await now.count(), 1, user + "'s inactive Qoder check-in has Check in now");
          asked.length = 0;
          await page.evaluate(() => { document.querySelector("#status").textContent = ""; });
          await reach(page, now);
          await now.click();
          await page.waitForFunction((want) => document.querySelector("#status")?.textContent === want, refreshed[lang]);
          assert.deepEqual(asked, [want]);
        }
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "no sideways scroll");
        assert.deepEqual(errors, []);
      });
    }
  }
}

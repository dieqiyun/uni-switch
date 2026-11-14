// Run with Node's test runner and Playwright on the module path; see README.md.
// #3326 (connoryang331): AutoClaw CN, WorkBuddy, Trae and others answered
// "already checked in" at 00:00 Beijing, before their own day had turned,
// and uni-switch showed "Checked in today" and never asked again. An answer
// uni-switch asks again (again: true) is not shown as today's check-in: its
// card says the vendor said already, uni-switch asks again later, and keeps
// Check in now, whose press asks the vendor. A done answer uni-switch keeps
// (no again) is still "Checked in today" with no press. The press says
// what came of it: claimed now (+credits), already checked in today, not
// available yet, failed. English,
// Simplified and Traditional Chinese, Japanese and German, the window and
// the panel's 440px; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn, reach } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const today = new Date(Date.now() + 8 * 3600e3).toISOString().slice(0, 10);
const card = (provider, name, user, by, checkin) => ({
  provider, name, plan: "Pro", user, windows: [{ name: "Credits", used: 1 }],
  checkins: true, checkinBy: by, checkin: { user, by, day: today, ...checkin },
});
const quotas = () => [
  card("plugin:autoclaw-cn", "AutoClaw CN", "hu", "plugin:autoclaw-cn", { vendor: "AutoClaw CN", outcome: "done", again: true }),
  card("plugin:fakeco", "FakeCo", "ann", "plugin:fakeco", { vendor: "FakeCo", outcome: "done", credit: 50 }),
];

function serve(lang, asked) {
  const settings = { theme: "light", lang, quotaLeft: false, currency: "usd", pluginCheckins: { "autoclaw-cn": true, fakeco: true } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/usage/plugin-checkin") {
      asked.push(route.request().postDataJSON());
      return json([{ user: "hu", outcome: "claimed", credit: 200, asked: true }]);
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

const again = {
  en: "AutoClaw CN says already checked in; its day may not have turned yet, so uni-switch asks again later",
  zh: "AutoClaw CN 回复已签到，但它的签到日可能还没翻页，uni-switch 稍后会再问",
  "zh-TW": "AutoClaw CN 回覆已簽到，但它的簽到日可能還沒換日，uni-switch 稍後會再問",
  ja: "AutoClaw CN はチェックイン済みと答えましたが、まだ日付が切り替わっていない可能性があるため、uni-switch は後でもう一度確認します",
  de: "AutoClaw CN meldet bereits eingecheckt; sein Tag hat vielleicht noch nicht gewechselt, daher fragt uni-switch später erneut",
};
const said = {
  en: { claimed: "Claimed: 1 (+200 credits)", already2: "Already checked in today: 2", notYet3: "Not available yet: 3", mixed: "Claimed: 2 (+150 credits) · Already checked in today: 1 · Check-in failed for 1 account(s)" },
  zh: { claimed: "已领取：1（+200 积分）", already2: "今日已签到过：2", notYet3: "暂不可签到：3", mixed: "已领取：2（+150 积分） · 今日已签到过：1 · 1 个账号签到失败" },
  "zh-TW": { claimed: "已領取：1（+200 點數）", already2: "今日已簽到過：2", notYet3: "暫不可簽到：3", mixed: "已領取：2（+150 點數） · 今日已簽到過：1 · 1 個帳號簽到失敗" },
  ja: { claimed: "受け取り済み：1（+200 クレジット）", already2: "今日はすでにチェックイン済み：2", notYet3: "まだチェックインできません：3", mixed: "受け取り済み：2（+150 クレジット） · 今日はすでにチェックイン済み：1 · 1 件のアカウントでチェックインに失敗しました" },
  de: { claimed: "Eingelöst: 1 (+200 Credits)", already2: "Heute bereits eingecheckt: 2", notYet3: "Noch nicht verfügbar: 3", mixed: "Eingelöst: 2 (+150 Credits) · Heute bereits eingecheckt: 1 · Check-in für 1 Konto/Konten fehlgeschlagen" },
};
const done = { en: "Checked in today +50", zh: "今日已签到 +50", "zh-TW": "今日已簽到 +50", ja: "今日はチェックイン済み +50", de: "Heute eingecheckt +50" };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [900, 440]) {
      test(`${engine} ${lang} ${width}px: an early "already checked in" is not today's and can be pressed`, async (t) => {
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
        const of = (user) => page.locator(".subscription-card", { hasText: user });
        for (const user of ["hu", "ann"]) await of(user).locator(".wb-checkin").first().waitFor();

        const hu = of("hu").locator(".wb-checkin").first();
        assert.equal((await hu.locator(".ci-say").textContent()).trim(), again[lang]);
        assert.equal(await hu.getAttribute("data-state"), "wait", "not shown as checked in");
        const ann = of("ann").locator(".wb-checkin").first();
        assert.equal((await ann.locator(".ci-say").textContent()).trim(), done[lang]);
        assert.equal(await of("ann").locator(".ci-now").count(), 0, "a kept done is settled, no press");

        const now = of("hu").locator(".ci-now");
        assert.equal(await now.count(), 1, "an answer uni-switch asks again keeps Check in now");
        await reach(page, now);
        await now.click();
        await page.waitForFunction((want) => document.querySelector("#status")?.textContent === want, said[lang].claimed);
        assert.deepEqual(asked, [{ provider: "autoclaw-cn" }], "the press asks the vendor");
        // what a press says, each kind (the coordinator's ask on #3326)
        const say = (rs) => page.evaluate((rs) => checkinPressSaid(rs).text, rs);
        assert.equal(await say([{ outcome: "claimed", credit: 200 }, { outcome: "done" }]), said[lang].already2);
        assert.equal(await say([{ outcome: "done", again: true }, { outcome: "inactive" }, { outcome: "ineligible" }]), said[lang].notYet3);
        assert.equal(await say([{ outcome: "claimed", credit: 100, asked: true }, { outcome: "claimed", credit: 50, asked: true }, { outcome: "done" }, { outcome: "failed" }]), said[lang].mixed);
        assert.equal(await page.evaluate(() => checkinPressSaid([{ outcome: "failed" }]).bad), 1);
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "no sideways scroll");
        assert.deepEqual(errors, []);
      });
    }
  }
}

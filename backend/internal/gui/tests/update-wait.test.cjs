// Run with Node's test runner and Playwright on the module path; see README.md.
// #577: a restart to update with the gateway busy — an agent's reply
// streaming, a Claude Code turn waiting on its tool results — waits for it to
// be idle instead of cutting them off. Settings → Version says what is in
// flight, the restart then waits with "Restart now" and "Cancel", and the
// counts follow the gateway. The header's Update pill does the same: its
// click waits, its next click (not one straight after, a double click)
// restarts at once. A wait that ran out says so. In the window the restart
// is asked first, with what changed (#844). No click moves the page; no
// coloured left border. English and Chinese; no backend, the API is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");

const words = {
  en: {
    version: "Version", restart: "Restart to update", now: "Restart now", cancel: "Cancel", restarting: "Restarting…",
    busy: "0.1.401 is downloaded · 2 in flight · 1 awaiting tool results",
    waiting: "0.1.401 is downloaded · restarts once the gateway is idle · 2 in flight · 1 awaiting tool results",
    waitingLess: "0.1.401 is downloaded · restarts once the gateway is idle · 1 in flight",
    gaveUp: "0.1.401 is downloaded · the gateway stayed busy for an hour, so uni-switch didn't restart; it updates when you restart or quit it",
    pill: "Update", pillWaiting: "Waiting to update",
    pillTitle: "uni-switch restarts to update to 0.1.401 once the agents' requests through the gateway have finished: 2 in flight · 1 awaiting tool results. Click to restart now.",
    handoverWaiting: "0.1.401 is downloaded · restarts once the Claude subscription's replies have finished · 1 in flight",
    handoverPillTitle: "uni-switch restarts to update to 0.1.401 once the replies through a Claude subscription have finished: 1 in flight. Other requests carry on across the restart. Click to restart now.",
  },
  zh: {
    version: "版本", restart: "重启以更新", now: "立即重启", cancel: "取消", restarting: "正在重启…",
    busy: "0.1.401 已下载 · 2 个请求进行中 · 1 个工具调用待返回",
    waiting: "0.1.401 已下载 · 网关空闲后自动重启 · 2 个请求进行中 · 1 个工具调用待返回",
    waitingLess: "0.1.401 已下载 · 网关空闲后自动重启 · 1 个请求进行中",
    gaveUp: "0.1.401 已下载 · 网关忙碌了一小时，uni-switch 未重启；重启或退出 uni-switch 时完成更新",
    pill: "更新", pillWaiting: "等待更新",
    pillTitle: "网关中 Agent 的请求结束后，uni-switch 将重启以更新到 0.1.401：2 个请求进行中 · 1 个工具调用待返回。点击立即重启。",
    handoverWaiting: "0.1.401 已下载 · Claude 订阅的回复结束后自动重启 · 1 个请求进行中",
    handoverPillTitle: "Claude 订阅的回复结束后，uni-switch 将重启以更新到 0.1.401：1 个请求进行中。其他请求不受重启影响。点击立即重启。",
  },
};

function settingsPayload(lang) {
  return {
    theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd",
    dock: false, dockWindow: false, proxy: "", redact: false, redactPersonal: false, redactWords: [],
    codexWarmup: "", claudeWarmup: "", codexWarmAt: "", claudeWarmAt: "", workbuddyCheckin: false, noStats: false,
    trayUsage: "", trayUsageEvery: 3, vision: "", imageGen: "",
    version: "0.1.400", dir: "~/.config/uni-switch", gateway: "http://127.0.0.1:3425",
    proxyNow: "none", proxySource: "none", login: false,
    visionModels: [], imageGenModels: [], workbuddyCheckins: [], lanURLs: [],
    fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
  };
}

// ctl.update is the backend's update state; an install waits while it is
// busy, unless asked to restart now
function server(lang, ctl) {
  const cur = settingsPayload(lang);
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: cur, fx: cur.fx });
    if (url.pathname === "/api/settings") return json(cur);
    if (url.pathname === "/api/update") return json({ ...ctl.update });
    if (url.pathname === "/api/update/install") {
      const body = req.postDataJSON() || {};
      ctl.installs.push(body);
      if (body.when === "cancel") {
        delete ctl.update.waiting;
        return json({ ...ctl.update });
      }
      if (body.when !== "now" && ctl.update.busy) {
        ctl.update.waiting = true;
        return json({ ...ctl.update });
      }
      ctl.restarted = true;
      return route.fulfill({ status: 204 });
    }
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const fresh = (over) => ({
  update: { state: "ready", current: "0.1.400", latest: "0.1.401", busy: { requests: 2, tools: 1 }, ...over },
  installs: [], restarted: false,
});
// the window asks first, showing what changed (#844)
const confirmAsk = (page) => page.locator("#modal .update-ask button.primary").click();
const scrolls = (page) => page.evaluate(() => [window.scrollY, document.scrollingElement.scrollTop, ...[...document.querySelectorAll(".view")].map((v) => v.scrollTop)].join(","));

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a restart to update waits for the gateway", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const open = async (lang, ctl, query, width) => {
      const errors = [];
      const page = await (await browser.newContext({ viewport: { width, height: 560 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      // WebKit's notice when a layout outruns its observers, not a fault
      page.on("pageerror", (e) => { if (!/^ResizeObserver loop completed with undelivered notifications/.test(e.message)) errors.push(e.message); });
      await page.route("**/*", server(lang, ctl));
      await watchSettingsReads(page);
      await page.goto("http://uni-switch.test/" + query);
      await settingsDrawn(page); // rows found are the ones that stay (#1659)
      return { page, errors };
    };
    const subIs = (page, row, text) => page.waitForFunction(({ text }) => {
      const r = [...document.querySelectorAll("#about .row.pref")].find((r) => r.querySelector(".val button") && r.querySelector(".sub")?.textContent.includes(text));
      return !!r;
    }, { text });

    for (const lang of ["en", "zh"]) {
      const w = words[lang];
      await t.test(lang + ": Settings says what is in flight, waits, and can restart now or cancel", async () => {
        const ctl = fresh();
        const { page, errors } = await open(lang, ctl, "?view=settings&tab=about", 900);
        const row = page.locator("#about .row.pref", { has: page.locator(".name", { hasText: w.version }) }).first();
        const sub = row.locator(".sub");
        await row.locator("button", { hasText: w.restart }).waitFor();
        assert.equal((await sub.textContent()).trim(), w.busy);
        await row.scrollIntoViewIfNeeded();
        await page.waitForTimeout(250);
        const before = await scrolls(page);

        await row.locator("button", { hasText: w.restart }).click();
        await confirmAsk(page);
        await row.locator("button", { hasText: w.now }).waitFor();
        assert.equal((await sub.textContent()).trim(), w.waiting);
        assert.deepEqual(ctl.installs, [{ view: "settings" }], "the click asks to restart, not to restart now");
        assert.equal(ctl.restarted, false);
        assert.deepEqual(await row.locator(".val button").allTextContents(), [w.now, w.cancel]);
        // the counts follow the gateway while it waits
        ctl.update.busy = { requests: 1 };
        await page.waitForFunction((t) => document.querySelector("#about .row.pref .sub")?.textContent.trim() === t || [...document.querySelectorAll("#about .row.pref .sub")].some((s) => s.textContent.trim() === t), w.waitingLess);
        // no coloured stripe down its side
        for (const el of [row, sub]) assert.equal(await el.evaluate((e) => getComputedStyle(e).borderLeftStyle), "none");

        await row.locator("button", { hasText: w.cancel }).click();
        await row.locator("button", { hasText: w.restart }).waitFor();
        assert.deepEqual(ctl.installs.at(-1), { view: "settings", when: "cancel" });
        assert.equal(ctl.restarted, false);

        await row.locator("button", { hasText: w.restart }).click();
        await confirmAsk(page);
        await row.locator("button", { hasText: w.now }).click();
        for (let i = 0; i < 50 && !ctl.restarted; i++) await page.waitForTimeout(20);
        assert.deepEqual(ctl.installs.at(-1), { view: "settings", when: "now" });
        assert.equal(ctl.restarted, true);
        await subIs(page, row, w.restarting).catch(() => {});
        assert.equal((await sub.textContent()).trim(), w.restarting);
        assert.equal(await scrolls(page), before, "no click moves the page");
        assert.deepEqual(errors, []);
      });

      await t.test(lang + ": a wait that ran out says so", async () => {
        const ctl = fresh({ busy: { requests: 1 }, gaveUp: true });
        const { page, errors } = await open(lang, ctl, "?view=settings&tab=about", 900);
        const row = page.locator("#about .row.pref", { has: page.locator(".name", { hasText: w.version }) }).first();
        await row.locator("button", { hasText: w.restart }).waitFor();
        assert.equal((await row.locator(".sub").textContent()).trim(), w.gaveUp);
        assert.deepEqual(errors, []);
      });

      await t.test(lang + ": the Update pill waits, and its next click restarts now", async () => {
        const ctl = fresh();
        const { page, errors } = await open(lang, ctl, "?mode=panel", 420);
        const pill = page.locator("#update"), label = pill.locator("span").first();
        await pill.waitFor({ state: "visible" });
        assert.equal((await label.textContent()).trim(), w.pill);
        const before = await scrolls(page);
        await pill.click();
        await page.waitForFunction((t) => document.querySelector("#update span").textContent.trim() === t, w.pillWaiting);
        assert.deepEqual(ctl.installs, [{}]);
        assert.equal(await pill.getAttribute("title"), w.pillTitle);
        // a double click's second half is not "restart now"
        await pill.click();
        await page.waitForTimeout(100);
        assert.equal(ctl.installs.length, 1);
        await page.waitForTimeout(1500);
        await pill.click();
        for (let i = 0; i < 50 && !ctl.restarted; i++) await page.waitForTimeout(20);
        assert.deepEqual(ctl.installs.at(-1), { when: "now" });
        assert.equal(ctl.restarted, true);
        assert.equal(await scrolls(page), before, "no click moves the page");
        assert.deepEqual(errors, []);
      });

      // xuanwo on X: a restart that hands the gateway to the new version
      // waits only for the Claude subscription's replies, and says so
      await t.test(lang + ": handing over, the wait names the Claude subscription's replies", async () => {
        const ctl = fresh({ handover: true, busy: { requests: 1 } });
        const { page, errors } = await open(lang, ctl, "?view=settings&tab=about", 900);
        const row = page.locator("#about .row.pref", { has: page.locator(".name", { hasText: w.version }) }).first();
        await row.locator("button", { hasText: w.restart }).click();
        await confirmAsk(page);
        await row.locator("button", { hasText: w.now }).waitFor();
        assert.equal((await row.locator(".sub").textContent()).trim(), w.handoverWaiting);
        const panel = await open(lang, fresh({ handover: true, busy: { requests: 1 } }), "?mode=panel", 420);
        const pill = panel.page.locator("#update");
        await pill.click();
        await panel.page.waitForFunction((t) => document.querySelector("#update span").textContent.trim() === t, w.pillWaiting);
        assert.equal(await pill.getAttribute("title"), w.handoverPillTitle);
        assert.deepEqual(errors, []);
        assert.deepEqual(panel.errors, []);
      });

      await t.test(lang + ": with nothing in flight the click restarts at once", async () => {
        const ctl = fresh({ busy: undefined });
        const { page, errors } = await open(lang, ctl, "?view=settings&tab=about", 900);
        const row = page.locator("#about .row.pref", { has: page.locator(".name", { hasText: w.version }) }).first();
        await row.locator("button", { hasText: w.restart }).waitFor();
        assert.equal((await row.locator(".sub").textContent()).trim(), w.busy.split(" · ")[0]);
        await row.locator("button", { hasText: w.restart }).click();
        await confirmAsk(page);
        for (let i = 0; i < 50 && !ctl.restarted; i++) await page.waitForTimeout(20);
        assert.equal(ctl.restarted, true);
        assert.deepEqual(errors, []);
      });
    }
  });
}

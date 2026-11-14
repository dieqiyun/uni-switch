// Run with Node's test runner and Playwright on the module path; see README.md.
// fflake33 on Discord: a window's Pace as a number, "Pace +20%" on its
// reset's line, against an even pace through the window (the gateway gives
// its start, provider.WithWindowStarts): amber when more is used than an
// even pace would have, its sign turned with the meter when it shows what
// is left, what it means in its tooltip, nothing for a window whose start
// isn't known; whole at 1100 and 420px in every language. And Settings ›
// Menu bar pace: off unless asked for, On posts trayPace, another save
// keeps it, no row on Windows (its tray shows no text), no click moves the
// page. No backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { openPart } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const at = (h) => new Date(Date.now() + h * 3600e3).toISOString();
// half of each window gone by: 70% used is 20 over, 20% used 30 under
const quotas = () => [
  { provider: "claude", name: "Claude Code", icon: "claude-color", plan: "max", user: "dee@example.com", windows: [
    { name: "5 hours", used: 70, startsAt: at(-2.5), resetsAt: at(2.5) },
    { name: "7 days", used: 20, startsAt: at(-84), resetsAt: at(84) },
    { name: "Credits", used: 40, resetsAt: at(10) },
  ] },
];

function settingsPayload(over) {
  return {
    theme: "light", tray: "panel", quotaLeft: false, currency: "usd",
    dock: false, dockWindow: false, proxy: "", redact: false, redactPersonal: false, redactWords: [],
    codexWarmup: "", claudeWarmup: "", codexWarmAt: "", claudeWarmAt: "", workbuddyCheckin: false, noStats: false,
    trayUsage: "claude|dee@example.com", trayUsages: ["claude|dee@example.com"], trayUsageEvery: 3, vision: "", imageGen: "",
    version: "0.1.400", dir: "~/.config/uni-switch", gateway: "http://127.0.0.1:3999",
    proxyNow: "none", proxySource: "none", login: false,
    visionModels: [], imageGenModels: [], workbuddyCheckins: [], lanURLs: [],
    fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
    ...over,
  };
}

function serve(lang, posts, over) {
  let cur = settingsPayload({ lang, ...over });
  return async (r) => {
    const req = r.request(), url = new URL(req.url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light", quotaLeft: cur.quotaLeft }, fx: cur.fx });
    if (url.pathname === "/api/settings") {
      if (req.method() === "POST") {
        const body = req.postDataJSON();
        posts.push(body);
        cur = { ...cur, ...body, trayUsage: (body.trayUsages || [])[0] || "" };
      }
      return json(cur);
    }
    if (url.pathname === "/api/gateway/trace") return url.searchParams.get("wait") ? new Promise(() => {}) : json({ mine: true, now: new Date().toISOString(), seq: 0, totals: {}, routes: [] });
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ models: [], groups: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname === "/api/usage/quotas") return json(quotas());
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await r.fulfill({ body: await fs.readFile(file), contentType }); } catch { await r.fulfill({ status: 404, body: "" }); }
  };
}

const want = {
  en: { pace: "Pace", over: "20% more of this window is used", under: "30% less of this window is used", row: "Menu bar pace", off: "Off", on: "On" },
  zh: { pace: "配速", over: "比匀速多用了 20%", under: "比匀速少用了 30%", row: "菜单栏配速", off: "关闭", on: "开启" },
  "zh-TW": { pace: "配速", over: "比勻速多用了 20%", under: "比勻速少用了 30%", row: "選單列配速" },
  ja: { pace: "ペース", over: "均等なペースより 20% 多く", under: "均等なペースより 30% 少なく", row: "メニューバーのペース" },
  de: { pace: "Tempo", over: "20% mehr von diesem Fenster", under: "30% weniger von diesem Fenster", row: "Tempo in der Menüleiste" },
};

async function open(browser, lang, width, over = {}, posts = [], platform = "MacIntel", view = "usage") {
  const context = await browser.newContext({ viewport: { width, height: view === "usage" ? 900 : 420 }, reducedMotion: "reduce" });
  await context.addInitScript((p) => Object.defineProperty(Navigator.prototype, "platform", { get: () => p }), platform);
  const page = await context.newPage();
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  await page.route("**/*", serve(lang, posts, over));
  await page.goto("http://uni-switch.test/?view=usage");
  return { context, page };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a window's Pace number on its card", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of Object.keys(want)) {
      for (const width of [1100, 420]) {
        for (const left of [false, true]) {
          const where = `${lang} ${width}px ${left ? "left" : "used"}`;
          const errors = [];
          const { context, page } = await open(browser, lang, width, { quotaLeft: left });
          page.on("pageerror", (e) => errors.push(e.message));
          await page.locator(".subscription-card .quota-pace").nth(1).waitFor();
          if (left) await page.waitForFunction(() => document.querySelector(".quota-pace")?.textContent.includes("−"));
          const got = await page.evaluate(() => [...document.querySelectorAll(".quota")].map((q) => {
            const p = q.querySelector(".quota-pace");
            if (!p) return null;
            const card = p.closest(".subscription-card").getBoundingClientRect(), b = p.getBoundingClientRect();
            return {
              text: p.textContent, title: p.title, over: p.classList.contains("over"),
              color: getComputedStyle(p).color, line: p.closest(".quota-reset")?.textContent,
              inCard: b.left >= card.left - 1 && b.right <= card.right + 1 && p.scrollWidth <= p.clientWidth + 1,
            };
          }).concat([{ amber: (() => { const s = document.createElement("span"); s.style.color = "var(--amber)"; document.body.append(s); const c = getComputedStyle(s).color; s.remove(); return c; })(), scroll: document.documentElement.scrollWidth > document.documentElement.clientWidth + 1 }]));
          const meta = got.pop();
          const [five, week, credits] = got;
          const w = want[lang];
          assert.equal(credits, null, `${where}: a window whose start isn't known has no Pace`);
          assert.equal(five.text, `${w.pace} ${left ? "−" : "+"}20%`, where);
          assert.equal(week.text, `${w.pace} ${left ? "+" : "−"}30%`, where);
          assert.ok(five.over && !week.over, `${where}: over is by use, whichever way the meter reads`);
          assert.equal(five.color, meta.amber, `${where}: over is amber`);
          assert.notEqual(week.color, meta.amber, where);
          assert.ok(five.title.includes(w.over), `${where}: ${five.title}`);
          assert.ok(week.title.includes(w.under), `${where}: ${week.title}`);
          assert.match(five.line, /· \S+ [+−]20%$/u, `${where}: on the reset's line after its wait: ${five.line}`);
          for (const p of [five, week]) assert.ok(p.inCard, `${where}: "${p.text}" is cut or past its card`);
          assert.equal(meta.scroll, false, `${where}: the page scrolls sideways`);
          // a click on it moves nothing
          const before = await page.evaluate(() => [scrollY, document.querySelector("#view-usage")?.scrollTop]);
          await page.locator(".quota-pace").first().click();
          await page.waitForTimeout(100);
          assert.deepEqual(await page.evaluate(() => [scrollY, document.querySelector("#view-usage")?.scrollTop]), before, where);
          assert.deepEqual(errors, [], where);
          await context.close();
        }
      }
    }
  });

  test(engine + ": Settings › Menu bar pace", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const prefs = async (page) => {
      await page.locator("#prefs").click();
      await openPart(page, "usage");
      await page.locator("#trayUsagePick button").waitFor();
    };
    const scrolled = (page) => page.locator("#view-settings").evaluate((v) => v.scrollTop);
    for (const lang of Object.keys(want)) {
      for (const width of [1100, 420]) {
        const where = `${lang} ${width}px`;
        const errors = [], posts = [];
        const { context, page } = await open(browser, lang, width, {}, posts, "MacIntel", "settings");
        page.on("pageerror", (e) => errors.push(e.message));
        await prefs(page);
        const row = page.locator("#trayPaceRow");
        await row.waitFor();
        assert.equal((await row.locator(".name").textContent()).trim(), want[lang].row, where);
        assert.equal(await row.evaluate((r) => r.previousElementSibling.id), "trayBirdRow", `${where}: under the uni-switch icon row`);
        assert.equal(await row.evaluate((r) => getComputedStyle(r).borderLeftWidth), "0px");
        assert.equal(await row.evaluate((r) => r.scrollWidth <= r.clientWidth + 1 && r.getBoundingClientRect().right <= innerWidth), true, `${where}: the row is cut`);
        const opts = row.locator("#trayPaceSegs .opt");
        if (want[lang].off) assert.deepEqual((await opts.allTextContents()).map((s) => s.trim()), [want[lang].off, want[lang].on]);
        assert.equal(await opts.nth(0).evaluate((b) => b.classList.contains("on")), true, `${where}: off unless asked for`);

        // scrolled down with a real wheel, the row in sight; then clicked: no scroll
        await page.waitForTimeout(500);
        await page.locator("#trayUsagePick button").hover();
        for (let i = 0; i < 20 && !(await scrolled(page)); i++) { await page.mouse.wheel(0, 120); await page.waitForTimeout(20); }
        await row.scrollIntoViewIfNeeded();
        await page.waitForTimeout(300);
        const before = await scrolled(page);
        assert(before > 0, `${where}: the settings list must be scrolled`);
        await opts.nth(1).click();
        for (let i = 0; i < 50 && !posts.length; i++) await page.waitForTimeout(50);
        assert.equal(posts.at(-1).trayPace, true, `${where}: On posts trayPace`);
        await page.waitForFunction(() => document.querySelectorAll("#trayPaceSegs .opt")[1].classList.contains("on"));
        await page.waitForTimeout(300);
        assert.equal(await scrolled(page), before, `${where}: On moved the page`);

        // another setting saved keeps it
        const sent = posts.length;
        await page.locator("#currencySegs .opt").nth(1).evaluate((b) => b.click());
        for (let i = 0; i < 50 && posts.length === sent; i++) await page.waitForTimeout(50);
        assert.equal(posts.at(-1).currency, "cny");
        assert.equal(posts.at(-1).trayPace, true, `${where}: a save of another setting keeps it`);
        assert.deepEqual(errors, [], where);
        await context.close();
      }
    }
    // no card in the menu bar, or on Windows: no row; on Linux it's there
    for (const [platform, over, shown] of [["MacIntel", { trayUsage: "", trayUsages: [] }, false], ["Win32", {}, false], ["Linux x86_64", {}, true]]) {
      const { context, page } = await open(browser, "en", 1100, over, [], platform, "settings");
      await prefs(page);
      await page.waitForTimeout(200);
      assert.equal(await page.locator("#trayPaceRow").isVisible(), shown, platform + " " + JSON.stringify(over));
      await context.close();
    }
  });
}

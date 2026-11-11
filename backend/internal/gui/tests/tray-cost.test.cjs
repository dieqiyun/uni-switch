// Run with Node's test runner and Playwright on the module path; see README.md.
// Settings › Menu bar cost (BussanQ on Discord: the today card in the menu
// bar with the tokens alone): an On/Off row under the pace row, on by
// default; Off posts trayNoCost and On takes it back; a save of another
// setting keeps it; the row is there on every OS and with no card, as the
// tooltip says the cost too; it fits at 1100 and 420 and no click moves
// the page. Every language; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { openPart, reach } = require("./reader.cjs"); // reach: wheeled to as a reader would, rows taller since their lines wrap whole (arculongus on Discord); a Playwright scroll is undone

const assets = path.resolve(__dirname, "../assets");

const quotas = [
  { provider: "claude", name: "Claude Code", icon: "claude-color", user: "a@b.c", plan: "Max",
    windows: [{ name: "5-hour", used: 42 }, { name: "Weekly", used: 18 }] },
];

function settingsPayload(over) {
  return {
    theme: "light", lang: "en", tray: "panel", quotaLeft: false, currency: "usd",
    dock: false, dockWindow: false, proxy: "", redact: false, redactPersonal: false, redactWords: [],
    codexWarmup: "", claudeWarmup: "", codexWarmAt: "", claudeWarmAt: "", workbuddyCheckin: false, noStats: false,
    trayUsage: "claude|a@b.c", trayUsages: ["claude|a@b.c"], trayUsageEvery: 3, vision: "", imageGen: "",
    version: "0.1.400", dir: "~/.config/uni-switch", gateway: "http://127.0.0.1:3425",
    proxyNow: "none", proxySource: "none", login: false,
    visionModels: [], imageGenModels: [], workbuddyCheckins: [], lanURLs: [],
    fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
    ...over,
  };
}

function server(lang, posts, over) {
  let cur = settingsPayload({ lang, ...over });
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" }, fx: cur.fx });
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
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const view = (page) => page.locator("#view-settings").evaluate((v) => v.scrollTop);

async function open(browser, lang, posts, platform, width, over) {
  const context = await browser.newContext({ viewport: { width, height: 700 }, reducedMotion: "reduce" });
  await context.addInitScript((p) => Object.defineProperty(Navigator.prototype, "platform", { get: () => p }), platform);
  const page = await context.newPage();
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  await page.route("**/*", server(lang, posts, over));
  await page.goto("http://uni-switch.test/?view=usage");
  await page.locator("#prefs").click();
  await openPart(page, "usage");
  await page.locator("#trayUsagePick button").waitFor();
  return { context, page };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": the menu bar's cost on or off", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const pages = [];
    t.after(async () => {
      if (process.env.ARTIFACT_DIR) {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        for (const [i, p] of pages.entries()) await p.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-tray-cost-${i}.png`) });
      }
      await browser.close();
    });

    for (const [lang, name, sub] of [
      ["en", "Menu bar cost", "What uni-switch's requests cost, beside their tokens in the menu bar and its tooltip; off, the tokens alone"],
      ["zh", "菜单栏金额", "uni-switch 请求的花费，显示在菜单栏和提示中的 token 数旁；关闭则只显示 token 数"],
      ["zh-TW", "選單列金額", "uni-switch 請求的花費，顯示在選單列與提示中的 token 數旁；關閉則只顯示 token 數"],
      ["ja", "メニューバーの金額", "uni-switch のリクエストの費用を、メニューバーとツールチップのトークン数の横に表示します。オフにするとトークン数だけになります"],
      ["de", "Kosten in der Menüleiste", "Was die Anfragen über uni-switch kosten, neben ihren Tokens in der Menüleiste und ihrem Tooltip; aus, nur die Tokens"],
    ]) {
      for (const width of [1100, 420]) {
        await t.test(lang + " at " + width, async () => {
          const errors = [], posts = [];
          const { context, page } = await open(browser, lang, posts, "MacIntel", width, { trayUsage: "usage:today", trayUsages: ["usage:today"] });
          pages.push(page);
          page.on("pageerror", (e) => errors.push(e.message));
          const row = page.locator("#trayCostRow");
          await row.waitFor();
          assert.equal((await row.locator(".name").textContent()).trim(), name);
          assert.equal((await row.locator(".sub").textContent()).trim(), sub);
          assert.equal(await row.evaluate((r) => r.previousElementSibling.id), "trayPaceRow", "under the pace row");
          const opts = row.locator("#trayCostSegs .opt");
          assert.equal(await opts.count(), 2);
          // the cost shown, as before
          assert.equal(await opts.nth(1).evaluate((b) => b.classList.contains("on")), true, "the cost on by default");
          // the row fits: nothing cut off nor pushed past the page
          await reach(page, row);
          const fit = await row.evaluate((r) => {
            const b = r.getBoundingClientRect(), s = r.querySelector("#trayCostSegs").getBoundingClientRect();
            return { over: r.scrollWidth - r.clientWidth, right: s.right - b.right, page: document.documentElement.scrollWidth - document.documentElement.clientWidth };
          });
          assert(fit.over <= 1 && fit.right <= 1 && fit.page <= 0, JSON.stringify(fit));

          await page.waitForTimeout(300);
          const before = await view(page);
          await opts.nth(0).click();
          for (let i = 0; i < 50 && !posts.length; i++) await page.waitForTimeout(50);
          assert.equal(posts.at(-1).trayNoCost, true, "Off posts trayNoCost");
          await page.waitForFunction(() => document.querySelector("#trayCostSegs .opt").classList.contains("on"));
          await page.waitForTimeout(300);
          assert.equal(await view(page), before, "Off moved the page");

          // another setting saved keeps it
          let sent = posts.length;
          await page.locator("#currencySegs .opt").nth(1).evaluate((b) => b.click());
          for (let i = 0; i < 50 && posts.length === sent; i++) await page.waitForTimeout(50);
          assert.equal(posts.at(-1).currency, "cny");
          assert.equal(posts.at(-1).trayNoCost, true, "a save of another setting keeps the cost off");

          // and On takes it back
          sent = posts.length;
          await page.locator("#trayCostSegs .opt").nth(1).click();
          for (let i = 0; i < 50 && posts.length === sent; i++) await page.waitForTimeout(50);
          assert.equal(posts.at(-1).trayNoCost, false, "On posts the cost back");
          assert.deepEqual(errors, []);
          await context.close();

          // every OS's tray says the cost, with no card too (its tooltip)
          for (const [platform, over] of [["MacIntel", { trayUsage: "", trayUsages: [] }], ["Win32", {}], ["Linux x86_64", {}]]) {
            const { context, page } = await open(browser, lang, [], platform, width, { trayNoCost: true, ...over });
            const row = page.locator("#trayCostRow");
            await row.waitFor();
            assert.equal(await row.locator("#trayCostSegs .opt").nth(0).evaluate((b) => b.classList.contains("on")), true, platform + ": off as saved");
            await context.close();
          }
        });
      }
    }
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// Settings → Download updates automatically (#2296, meifu2027): on by
// default; off, a newer version found is only shown, with its size, and is
// downloaded when Download update is clicked — for metered networks and
// proxies billed by traffic. The switch is saved as noAutoDownload and kept
// across a reload, the other update choices left as they were. The version
// row then says what is out and how big it is, and its Download update
// asks for the download (update/install), which the Check beside it never
// does; the header's Update pill shows too. No click moves the page, and
// the rows fit a narrow window. Every language, both engines, at 1100 and
// 420 wide; no backend, the API is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn, openPart } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");

const words = {
  en: { name: "Download updates automatically", on: "Downloads a newer version as soon as it is found. Turn off on a metered network or a proxy billed by traffic", off: "Off: a newer version is only shown, with its size; it downloads when you click Download update. For metered networks and proxies billed by traffic", out: "0.1.401 is out · 52.4 MB", get: "Download update", got: "0.1.401 is downloaded" },
  zh: { name: "自动下载更新", on: "发现新版本后立即下载。使用按流量计费的网络或代理时可以关闭", off: "关闭：发现新版本时只显示版本和大小，点击「下载更新」后才下载。适合按流量计费的网络或代理", out: "0.1.401 已发布 · 52.4 MB", get: "下载更新", got: "0.1.401 已下载" },
  "zh-TW": { name: "自動下載更新", on: "發現新版本後立即下載。使用按流量計費的網路或代理時可以關閉", off: "關閉：發現新版本時只顯示版本和大小，點擊「下載更新」後才下載。適合按流量計費的網路或代理", out: "0.1.401 已釋出 · 52.4 MB", get: "下載更新", got: "0.1.401 已下載" },
  ja: { name: "アップデートを自動でダウンロード", on: "新しいバージョンが見つかるとすぐにダウンロードします。従量課金のネットワークやプロキシではオフにできます", off: "オフ：新しいバージョンはサイズとともに表示するだけで、「アップデートをダウンロード」をクリックしたときにダウンロードします。従量課金のネットワークやプロキシ向け", out: "0.1.401 が公開されました · 52.4 MB", get: "アップデートをダウンロード", got: "0.1.401 をダウンロードしました" },
  de: { name: "Updates automatisch herunterladen", on: "Lädt eine neue Version herunter, sobald sie gefunden wird. Bei getakteten Netzen oder nach Datenvolumen abgerechneten Proxys ausschalten", off: "Aus: Eine neue Version wird nur mit ihrer Größe angezeigt und erst heruntergeladen, wenn Sie auf „Update herunterladen“ klicken. Für getaktete Netze und nach Datenvolumen abgerechnete Proxys", out: "0.1.401 ist erschienen · 52.4 MB", get: "Update herunterladen", got: "0.1.401 ist heruntergeladen" },
};

function settingsPayload(lang, ctl) {
  const s = {
    theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd",
    dock: false, dockWindow: false, proxy: "", redact: false, redactPersonal: false, redactWords: [],
    codexWarmup: "", claudeWarmup: "", codexWarmAt: "", claudeWarmAt: "", workbuddyCheckin: false, noStats: false,
    trayUsage: "", trayUsageEvery: 3, vision: "", imageGen: "",
    version: "0.1.400", dir: "~/.config/uni-switch", gateway: "http://127.0.0.1:3425",
    proxyNow: "none", proxySource: "none", login: false,
    visionModels: [], imageGenModels: [], workbuddyCheckins: [], lanURLs: [],
    fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
  };
  // as the backend sends it: left out while it is its default
  if (ctl.held) s.noAutoDownload = true;
  return s;
}

function server(lang, ctl) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: settingsPayload(lang, ctl), fx: settingsPayload(lang, ctl).fx });
    if (url.pathname === "/api/settings") {
      if (req.method() === "POST") {
        const body = req.postDataJSON();
        ctl.held = !!body.noAutoDownload;
        ctl.saves.push(body);
      }
      return json(settingsPayload(lang, ctl));
    }
    const out = { current: "0.1.400", latest: "0.1.401", notes: "- faster" };
    if (url.pathname === "/api/update/check") {
      ctl.checks++;
      return json({ ...out, state: ctl.state, ...(ctl.state === "found" ? { size: 52400000 } : {}) });
    }
    if (url.pathname === "/api/update/install") {
      ctl.installs++;
      ctl.state = "downloading";
      return json({ ...out, state: "downloading", done: 1000000, total: 52400000 });
    }
    if (url.pathname === "/api/update") {
      if (ctl.state === "downloading") ctl.state = "ready"; // the next read: it's in
      return json(ctl.state ? { ...out, state: ctl.state, ...(ctl.state === "found" ? { size: 52400000 } : {}) } : { state: "", current: "0.1.400" });
    }
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// wheel a row into sight, as a user would; a click must then not move the page
async function showRow(page, row) {
  await page.locator("#view-settings").hover();
  for (let i = 0; i < 40; i++) {
    const box = await row.boundingBox();
    const view = await page.locator("#view-settings").boundingBox();
    if (box && view && box.y >= view.y + 4 && box.y + box.height <= view.y + view.height - 4) break;
    await page.mouse.wheel(0, 180);
    await page.waitForTimeout(16);
  }
  await page.waitForTimeout(250);
}
const scrolls = (page) => page.evaluate(() => [window.scrollY, document.scrollingElement.scrollTop, ...[...document.querySelectorAll(".view")].map((v) => v.scrollTop)].join(","));

async function fits(page, row, width) {
  let box, segs;
  for (let i = 0; i < 50 && !box; i++) {
    await page.waitForTimeout(40);
    box = await row.boundingBox().catch(() => null);
  }
  assert.ok(box, "the row isn't drawn");
  assert.ok(box.x >= 0 && box.x + box.width <= width + 0.5, `row ${box.x}+${box.width} past ${width}`);
  for (const b of await row.locator("button, .segs").all()) {
    segs = await b.boundingBox();
    if (segs) assert.ok(segs.x >= box.x - 0.5 && segs.x + segs.width <= box.x + box.width + 0.5, "a control past its row");
  }
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true, "the page scrolls sideways");
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": downloads wait for a click", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const width of [1100, 420]) for (const lang of Object.keys(words)) {
      const w = words[lang];
      await t.test(`${lang} at ${width}: on by default, saved off, kept; the version row offers the download`, async () => {
        const ctl = { held: false, state: "", checks: 0, installs: 0, saves: [] };
        const errors = [];
        const page = await (await browser.newContext({ viewport: { width, height: 640 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => { if (!/^ResizeObserver loop completed with undelivered notifications/.test(e.message)) errors.push(e.message); });
        await page.route("**/*", server(lang, ctl));
        await watchSettingsReads(page);
        await page.goto("http://uni-switch.test/?view=settings");
        await settingsDrawn(page);
        const aboutTab = async () => {
          await openPart(page, "about");
          await page.locator("#setPage-about").waitFor({ state: "visible" });
        };
        await aboutTab();
        const rowOf = (name) => page.locator("#about .row.pref", { has: page.locator(".name", { hasText: new RegExp("^" + name + "$") }) });
        const dl = rowOf(w.name);
        await dl.waitFor();
        await dl.locator(".sub", { hasText: w.on }).waitFor();
        assert.equal(await dl.locator(".opt").count(), 2);
        assert.equal(await dl.locator(".opt").nth(1).evaluate((o) => o.classList.contains("on")), true, "on by default");

        // off: saved, nothing else changed, the row says what off does
        // the click scrolls nothing but to keep what was clicked where it
        // was: the row's line says what off does, and wrapped whole
        // (arculongus on Discord) it can take another line, the switch
        // beside it held under the pointer
        await showRow(page, dl);
        let before = await scrolls(page);
        const y0 = (await dl.locator(".opt").nth(0).boundingBox()).y;
        await dl.locator(".opt").nth(0).click();
        await page.waitForFunction(() => document.querySelectorAll(".update-download-row .opt")[0]?.classList.contains("on"));
        assert.equal(ctl.saves.at(-1).noAutoDownload, true);
        assert.equal(ctl.saves.at(-1).noAutoUpdate, false);
        assert.equal(ctl.saves.at(-1).updateEvery, 360);
        assert.equal(ctl.saves.at(-1).autoRestart, false);
        await dl.locator(".sub", { hasText: w.off }).waitFor();
        await page.waitForTimeout(400);
        const y1 = (await dl.locator(".opt").nth(0).boundingBox()).y;
        if (await scrolls(page) !== before) assert.ok(Math.abs(y1 - y0) <= 1, `the click scrolled the page, and the switch moved ${y1 - y0}px`);
        await fits(page, dl, width);

        // kept after a reload; Check now finds 0.1.401 and only says so
        ctl.state = "found";
        await page.reload();
        await aboutTab();
        await dl.waitFor();
        assert.equal(await dl.locator(".opt").nth(0).evaluate((o) => o.classList.contains("on")), true, "kept off");
        const ver = page.locator("#about .row.pref").first();
        await ver.locator(".sub", { hasText: w.out }).waitFor();
        const get = ver.locator("button", { hasText: new RegExp("^" + w.get + "$") });
        await get.waitFor();
        await fits(page, ver, width);
        assert.equal(await page.locator("#update").isVisible(), true, "the header's Update pill is out");
        assert.equal(ctl.installs, 0, "downloaded without a click");

        // Download update: the download is asked for, and the row follows it
        await showRow(page, ver);
        before = await scrolls(page);
        await get.click();
        await ver.locator(".sub", { hasText: w.got }).waitFor();
        assert.equal(ctl.installs, 1);
        await page.waitForTimeout(400);
        assert.equal(await scrolls(page), before, "the click must not scroll the page");

        // and on again
        await showRow(page, dl);
        await dl.locator(".opt").nth(1).click();
        await page.waitForFunction(() => document.querySelectorAll(".update-download-row .opt")[1]?.classList.contains("on"));
        assert.equal(ctl.saves.at(-1).noAutoDownload, false);
        assert.deepEqual(errors, []);
        await page.context().close();
      });
    }
  });
}

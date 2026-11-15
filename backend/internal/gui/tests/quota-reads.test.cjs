// Run with Node's test runner and Playwright on the module path; see README.md.
// Settings → Allowances → Allowance reads (#1518, RooobinYe): uni-switch
// started at login before the proxy app read allowances by itself, direct.
// The row offers Automatic (the default) or When I ask; When I ask posts
// settings/quota-reads {reads:"asked"} and is drawn chosen, Automatic takes
// it back; the row fits a narrow window without the page scrolling
// sideways, and no click moves the page. Every language; no backend, the
// API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { openPart, reach } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");

function settingsPayload(over) {
  return {
    theme: "light", lang: "en", tray: "panel", quotaLeft: false, currency: "usd",
    dock: false, dockWindow: false, proxy: "", redact: false, redactPersonal: false, redactWords: [],
    codexWarmup: "", claudeWarmup: "", codexWarmAt: "", claudeWarmAt: "", workbuddyCheckin: false, noStats: false,
    trayUsage: "", trayUsages: [], trayUsageEvery: 3, vision: "", imageGen: "",
    version: "0.1.400", dir: "~/.config/uni-switch", gateway: "http://127.0.0.1:3425",
    proxyNow: "none", proxySource: "none", login: false,
    visionModels: [], imageGenModels: [], workbuddyCheckins: [], lanURLs: [],
    fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
    ...over,
  };
}

function server(lang, posts) {
  let cur = settingsPayload({ lang });
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" }, fx: cur.fx });
    if (url.pathname === "/api/settings/quota-reads") {
      const body = req.postDataJSON();
      posts.push(body);
      cur = { ...cur, quotaReads: body.reads || undefined };
      return json(cur);
    }
    if (url.pathname === "/api/settings") {
      // the save of every other setting leaves this one as it is (api.go)
      if (req.method() === "POST") cur = { ...cur, ...req.postDataJSON(), quotaReads: cur.quotaReads };
      return json(cur);
    }
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const view = (page) => page.locator("#view-settings").evaluate((v) => v.scrollTop);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": allowances read by uni-switch, or only when asked", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());

    for (const [lang, name, auto, asked] of [
      ["en", "Allowance reads", "Automatic", "When I ask"],
      ["zh", "额度刷新", "自动", "仅手动"],
      ["zh-TW", "額度重新整理", "自動", "僅手動"],
      ["ja", "利用枠の読み込み", "自動", "手動のみ"],
      ["de", "Kontingente abrufen", "Automatisch", "Nur auf Anfrage"],
    ]) {
      for (const width of [900, 360]) {
        await t.test(lang + " " + width, async () => {
          const errors = [], posts = [];
          const context = await browser.newContext({ viewport: { width, height: 420 }, reducedMotion: "reduce" });
          const page = await context.newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", server(lang, posts));
          await page.goto("http://uni-switch.test/?view=usage");
          await page.locator("#prefs").click();
          await openPart(page, "usage");
          const row = page.locator("#quotaReadsRow");
          const opts = row.locator("#quotaReadsSegs .opt");
          await opts.first().waitFor();
          assert.equal((await row.locator(".name").textContent()).trim(), name);
          const sub = (await row.locator(".sub").textContent()).trim();
          assert(sub.includes("uni-switch quota"), "the sub says what counts as asking: " + sub);
          if (lang !== "en") assert(!sub.startsWith("When I ask"), lang + " sub not translated");
          assert.deepEqual((await opts.allTextContents()).map((s) => s.trim()), [auto, asked]);
          assert.equal(await opts.nth(0).evaluate((b) => b.classList.contains("on")), true, "Automatic by default");

          // in a narrow window the row fits, and the page doesn't scroll sideways
          // wheeled to, as a reader does: the app puts back a scroll from
          // code, and in WebKit de 360 the button sits just under the view's
          // foot, where Playwright's own scroll is put back before it presses
          await reach(page, opts.nth(1));
          // the Usage page drawn from a reply with no cost says no error
          // (fmtCost threw on it, and the error's line covered this row, #3406)
          assert.equal(await page.locator("#status.err").count(), 0,
            "an error is shown: " + await page.locator("#status").getAttribute("title"));
          const fit = await row.evaluate((r) => {
            const b = r.getBoundingClientRect(), segs = r.querySelector("#quotaReadsSegs").getBoundingClientRect();
            return { right: b.right, segsRight: segs.right, vw: document.documentElement.clientWidth,
              sideways: document.scrollingElement.scrollWidth > document.documentElement.clientWidth };
          });
          assert(fit.segsRight <= fit.vw + 0.5 && fit.right <= fit.vw + 0.5, "the row runs out of the window: " + JSON.stringify(fit));
          assert.equal(fit.sideways, false, "the page scrolls sideways");

          const before = await view(page);
          await opts.nth(1).click();
          for (let i = 0; i < 50 && !posts.length; i++) await page.waitForTimeout(50);
          assert.deepEqual(posts.at(-1), { reads: "asked" });
          await page.waitForFunction(() => document.querySelectorAll("#quotaReadsSegs .opt")[1]?.classList.contains("on"));
          await page.waitForTimeout(200);
          assert.equal(await view(page), before, "When I ask moved the page");

          // another setting saved keeps it
          await page.locator("#currencySegs .opt").nth(1).evaluate((b) => b.click());
          await page.waitForTimeout(300);
          assert.equal(await page.locator("#quotaReadsSegs .opt").nth(1).evaluate((b) => b.classList.contains("on")), true,
            "a save of another setting put Automatic back");

          await page.locator("#quotaReadsSegs .opt").nth(0).click();
          for (let i = 0; i < 50 && posts.length < 2; i++) await page.waitForTimeout(50);
          assert.deepEqual(posts.at(-1), { reads: "" });
          await page.waitForFunction(() => document.querySelectorAll("#quotaReadsSegs .opt")[0]?.classList.contains("on"));
          assert.deepEqual(errors, []);
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await row.scrollIntoViewIfNeeded();
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-quota-reads-${lang}-${width}.png`) });
          }
          await context.close();
        });
      }
    }
  });
}

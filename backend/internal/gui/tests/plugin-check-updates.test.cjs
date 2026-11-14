// Run with Node's test runner and Playwright on the module path; see README.md.
// Check for updates on Plugins (the owner: 插件的检查更新按钮还是得做): the
// button asks npm now, says it is checking, and each row then says what it
// found — an update with the version installed and the one on npm, up to
// date, or why it couldn't be checked; Update installs the one found. npm
// not reached says so. The click never scrolls the page. English and
// Chinese; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const COPILOT = "opencode-copilot-auth", ZED = "the retired opencode-zed-auth", KIRO = "the retired opencode-kiro-auth";

function server(lang, calls) {
  // what npm said within the hour: nothing newer than what's installed
  const installed = [
    { spec: COPILOT, providers: ["GitHub Copilot"], version: "0.0.7", latest: "0.0.7", moved: [] },
    { spec: ZED, providers: ["Zed"], version: "0.1.4", latest: "0.1.4", moved: [] },
    { spec: KIRO, providers: ["Kiro"], version: "0.1.2", latest: "0.1.2", moved: [] },
    { spec: "github:me/opencode-x-auth", package: "opencode-x-auth", providers: ["X"], version: "1.0.0", moved: [] },
  ];
  for (let i = 0; i < 14; i++) installed.push({ spec: "opencode-filler-" + String(i).padStart(2, "0"), providers: ["F"], version: "1.0.0", latest: "1.0.0", moved: [] });
  let offline = false, gate = null;
  const state = () => ({ bun: true, bunVersion: "1.3.0", plugins: installed, movable: [] });
  const fn = async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname === "/api/plugins/updates") return json({ checked: "2026-10-01T00:00:00Z", waiting: installed.filter((e) => e.spec === COPILOT && e.latest !== e.version).map((e) => ({ spec: e.spec, package: e.spec, version: e.version, latest: e.latest })), updated: [] });
    if (url.pathname === "/api/plugins/npm") return json({ npm: {} });
    if (url.pathname === "/api/plugins") return json(state());
    if (url.pathname === "/api/plugins/listings") return json({ listings: [] });
    if (url.pathname === "/api/plugins/check") {
      calls.push("check");
      if (gate) await gate;
      const at = "2026-10-02T09:30:00Z";
      if (offline) return json({ at, state: state(), plugins: installed.filter((e) => !e.spec.startsWith("github:")).map((e) => ({ spec: e.spec, package: e.spec, version: e.version, status: "unknown", why: "offline", error: "dial tcp: no route to host", auto: false, off: false })) });
      installed[0].latest = "0.0.9";
      const plugins = installed.map((e) => e.spec.startsWith("github:") ? { spec: e.spec, package: e.package, version: e.version, status: "git", auto: false, off: false }
        : e.spec === KIRO ? { spec: e.spec, package: e.spec, version: e.version, status: "unknown", why: "limited", error: "429 Too Many Requests", auto: true, off: false }
        : { spec: e.spec, package: e.spec, version: e.version, latest: e.latest, status: e.latest === e.version ? "current" : "update", auto: e.spec === ZED, off: false });
      return json({ at, state: state(), plugins });
    }
    if (url.pathname === "/api/plugins/upgrade") {
      const body = route.request().postDataJSON();
      calls.push("upgrade " + body.spec);
      installed[0].version = "0.0.9";
      return json(state());
    }
    if (url.pathname.startsWith("/api/")) return json({});
    if (!/^\/[\w./-]*$/.test(url.pathname) || url.host !== "uni-switch.test") return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
  fn.offline = () => { offline = true; };
  fn.hold = () => { let open; gate = new Promise((r) => { open = r; }); return () => { gate = null; open(); }; };
  return fn;
}

const L = {
  en: {
    installed: "Installed", check: "Check for updates", checking: "Checking…", update: "Update", out: "v0.0.9 out", outTitle: "v0.0.7 installed, v0.0.9 on npm",
    current: "Up to date", couldnt: "Couldn't check", limited: /too many/, found: /1 plugin has an update · 1 couldn't be checked: npm is turning requests away/,
    updated: "opencode-copilot-auth updated to v0.0.9", offline: /^Couldn't check for updates: npm couldn't be reached — check your connection or proxy$/,
  },
  zh: {
    installed: "已安装", check: "检查更新", checking: "检查中…", update: "更新", out: "v0.0.9 可用", outTitle: "已安装 v0.0.7，npm 上有 v0.0.9",
    current: "已是最新", couldnt: "无法检查", limited: /请求过多/, found: /1 个插件有更新 · 1 个无法检查：npm 暂时拒绝请求/,
    updated: /opencode-copilot-auth/, offline: /^无法检查更新：无法连接 npm，请检查网络或代理$/,
  },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": plugins check for updates", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh"]) {
      await t.test(lang, async () => {
        const w = L[lang];
        const calls = [];
        const page = await (await browser.newContext({ viewport: { width: 980, height: 560 } })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const srv = server(lang, calls);
        await page.route("**/*", srv);

        await page.goto("http://uni-switch.test/?view=plugins");
        const view = page.locator("#view-plugins");
        await view.page().locator(":is(.lib-tabs .opt, .nav-s):visible", { hasText: w.installed }).click();
        const row = (spec) => view.locator(".pm-row").filter({ has: page.locator(".name span", { hasText: new RegExp("^" + spec.replace(/[/@-]/g, "\\$&") + "$") }) });
        await row(COPILOT).waitFor();
        // nothing newer known yet: no Update offered
        assert.equal(await row(COPILOT).getByRole("button", { name: w.update, exact: true }).count(), 0);

        // the reader scrolls down to the button at the list's foot
        const check = view.locator(".pm-foot .pm-check");
        const box = await view.boundingBox();
        await page.mouse.move(box.x + box.width / 2, box.y + 60);
        for (let i = 0; i < 80; i++) { await page.mouse.wheel(0, 60); await page.waitForTimeout(15); }
        await page.waitForTimeout(200);
        const top = await view.evaluate((v) => v.scrollTop);
        assert(top > 0, "the view must have scrolled");
        assert.equal((await check.textContent()).trim(), w.check);

        const open = srv.hold();
        await check.click();
        await page.waitForFunction((s) => document.querySelector("#view-plugins .pm-foot .pm-check")?.textContent.trim() === s, w.checking);
        assert(await check.isDisabled());
        open();
        await page.waitForFunction((s) => document.querySelector("#view-plugins .pm-foot .pm-check")?.textContent.trim() === s, w.check);
        assert.match(await page.locator("#status").textContent(), w.found);
        assert.equal(await view.evaluate((v) => v.scrollTop), top, "the check must not scroll the page");

        // each row says what npm said now
        const out = row(COPILOT).locator(".pm-chip.up");
        assert.equal(await out.textContent(), w.out);
        assert.equal(await out.getAttribute("title"), w.outTitle);
        assert.equal(await row(ZED).locator(".pm-chip.soft").textContent(), w.current);
        const warn = row(KIRO).locator(".pm-chip.warn");
        assert.equal(await warn.textContent(), w.couldnt);
        assert.match(await warn.getAttribute("title"), w.limited);
        assert.equal(await warn.locator(".dot").count(), 1);
        // a git one isn't npm's to check: nothing said of it
        assert.equal(await row("opencode-x-auth").locator(".pm-chip.soft, .pm-chip.warn").count(), 0);
        // the dot on Plugins: someone else's update waits
        await page.waitForFunction(() => document.querySelector('#nav button[data-view="plugins"]')?.classList.contains("has-dot"));

        // Update installs the one found, as before: the reader scrolls back up to it
        for (let i = 0; i < 80; i++) { await page.mouse.wheel(0, -60); await page.waitForTimeout(15); }
        await page.waitForTimeout(200);
        const top2 = await view.evaluate((v) => v.scrollTop);
        await row(COPILOT).getByRole("button", { name: w.update, exact: true }).click();
        await row(COPILOT).locator(".pm-chip.soft", { hasText: w.current }).waitFor();
        assert.deepEqual(calls, ["check", "upgrade " + COPILOT]);
        assert.match(await page.locator("#status").textContent(), w.updated instanceof RegExp ? w.updated : new RegExp(w.updated.replace(/\./g, "\\.")));
        assert.equal(await view.evaluate((v) => v.scrollTop), top2, "updating must not scroll the page");

        // npm not reached: said so, plainly
        for (let i = 0; i < 80; i++) { await page.mouse.wheel(0, 60); await page.waitForTimeout(15); }
        await page.waitForTimeout(200);
        const top3 = await view.evaluate((v) => v.scrollTop);
        srv.offline();
        await check.click();
        await page.waitForFunction(() => /npm/.test(document.querySelector("#status")?.textContent || ""));
        assert.match(await page.locator("#status").textContent(), w.offline);
        assert(await page.locator("#status").evaluate((s) => s.classList.contains("err")));
        assert.equal(await view.evaluate((v) => v.scrollTop), top3);
        assert.deepEqual(errors, []);
      });
    }
  });
}

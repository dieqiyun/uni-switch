// Run with Node's test runner and Playwright on the module path; see README.md.
// Plugins from a git repository (Lemon on Discord: can a plugin be installed
// straight from GitHub?): the market's field says it takes a GitHub repo,
// and what is typed (github:owner/repo) is what Install sends. Installed
// names the plugin by the package its repository holds, not the spec, with
// the commit installed. It has no npm version to compare, so it is out of
// date only when its repository is at another commit (ARNO on Discord:
// github:yuweihao17/codearts showed Update all the time, and still did
// right after updating it): at the same commit there is no Update, nor
// Update all; once the repository moves on, Check for updates shows New
// commits and an Update that fetches it again, and after it there is none,
// even when the repository moved on again after the check and the update
// installed a commit newer than the one it found (matrix0487, #4802: New
// commits and Update all stayed, and Update said it was up to date; these
// are the reporter's commits). Its
// page is the README it was installed with, with no npm link and a Source
// link to the repository. A click scrolls nothing. English and Chinese; the
// API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

const SPEC = "github:owner/repo";
const HAVE = "fdaa691";
const NEXT = "3b411a5"; // what Check for updates finds
const LATER = "ba898e7"; // pushed after it: what Update installs
const README = ["# my-oc-plugin", "", "A plugin installed from its repository."].join("\n");

function server(lang, asked, repo) {
  const installed = [];
  const market = () => ({ listings: [], state: { bun: true, bunVersion: "1.3.0", plugins: installed, picker: false } });
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    const body = () => route.request().postDataJSON();
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    // the page asks for the market in parts (#488)
    if (url.pathname === "/api/plugins/market" || url.pathname === "/api/plugins" || url.pathname === "/api/plugins/listings") { const m = market(); return json(url.pathname === "/api/plugins" ? m.state : url.pathname === "/api/plugins/listings" ? { listings: m.listings } : m); }
    if (url.pathname === "/api/plugins/search") return json({ hits: [] });
    if (url.pathname === "/api/plugins/updates") return json({ waiting: [], updated: [] });
    // the commit the repository is at now
    if (url.pathname === "/api/plugins/git") { asked.push(["git"]); return json({ heads: { [SPEC]: repo.head } }); }
    if (url.pathname === "/api/plugins/check") {
      asked.push(["check"]);
      const m = market();
      return json({ at: "2026-10-11T00:00:00Z", state: m.state, plugins: installed.map((e) => ({ spec: e.spec, package: e.package, version: e.version, status: e.commit === repo.head ? "current" : "update", commit: e.commit, head: repo.head, auto: false, off: false })) });
    }
    if (url.pathname === "/api/plugins/page") {
      asked.push(["page", url.searchParams.get("name")]);
      return json({ readme: README, updated: "2026-10-01T10:00:00Z" });
    }
    if (url.pathname === "/api/plugins/add") {
      const b = body();
      asked.push(["add", b]);
      installed.push({ spec: b.spec, package: "my-oc-plugin", providers: [], version: "1.0.0", commit: repo.head, moved: [] });
      return json({});
    }
    if (url.pathname === "/api/plugins/upgrade") {
      asked.push(["upgrade", body()]);
      // bun installs what the repository is at now, and the update asks
      // the repository again, as the plugins' own head says; the version
      // in its package.json stays
      if (repo.after) repo.head = repo.after;
      installed[0].commit = installed[0].head = repo.head;
      return json({});
    }
    if (url.pathname === "/api/open") { asked.push(["open", body()]); return route.fulfill({ status: 204 }); }
    if (url.pathname.startsWith("/api/")) return json({});
    if (!/^\/[\w./-]*$/.test(url.pathname) || url.host !== "uni-switch.test") { asked.push(["fetched", url.href]); return route.fulfill({ status: 404, body: "" }); }
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const L = {
  en: { hint: "npm package, GitHub repo (github:owner/repo) or a folder", install: "Install", installed: "Installed", update: "Update", check: "Check for updates", all: "Update all", fresh: "New commits", done: "my-oc-plugin updated to " + LATER, source: "Source" },
  zh: { hint: "npm 包名、GitHub 仓库（github:owner/repo）或本机文件夹", install: "安装", installed: "已安装", update: "更新", check: "检查更新", all: "全部更新", fresh: "有新提交", done: "my-oc-plugin 已更新到 " + LATER, source: "源码" },
};

const shot = async (loc, name) => {
  if (!process.env.ARTIFACT_DIR) return;
  await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
  await loc.screenshot({ path: path.join(process.env.ARTIFACT_DIR, name + ".png") });
};

const scrolls = (page) => page.evaluate(() => [window.scrollY, ...[...document.querySelectorAll("*")].filter((e) => e.scrollTop > 0).map((e) => (e.id || e.className) + ":" + e.scrollTop)].join(","));

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a plugin from a git repository", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh"]) {
      await t.test(lang, async () => {
        const w = L[lang];
        const asked = [];
        const page = await (await browser.newContext({ viewport: { width: 980, height: 820 } })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const repo = { head: HAVE };
        await page.route("**/*", server(lang, asked, repo));

        await page.goto("http://uni-switch.test/?view=plugins");
        const view = page.locator("#view-plugins");
        await view.waitFor({ state: "visible" });

        // Discover: the field says it takes a GitHub repo, and sends it as typed
        const manual = view.locator(".pm-manual");
        await manual.waitFor();
        const field = manual.locator(".pm-minput");
        assert.equal(await field.getAttribute("placeholder"), w.hint);
        await shot(manual, `plugins-git-field-${engine}-${lang}`);
        await field.fill("  " + SPEC + " ");
        const before = await scrolls(page);
        await manual.locator("button", { hasText: new RegExp("^" + w.install + "$") }).click();
        await page.waitForFunction(() => !!document.querySelector("#view-plugins .pm-minput"));
        assert.deepEqual(asked.find(([k]) => k === "add")[1], { spec: SPEC });
        assert.equal(await scrolls(page), before, "Install scrolls nothing");

        // Installed: known by its package, at the commit installed, and
        // with the repository at that commit, nothing to update
        await view.page().locator(":is(.lib-tabs .opt, .nav-s):visible", { hasText: w.installed }).click();
        const row = view.locator(".pm-row").first();
        await row.waitFor();
        for (let i = 0; i < 100 && !asked.some(([k]) => k === "git"); i++) await new Promise((r) => setTimeout(r, 20));
        assert.ok(asked.some(([k]) => k === "git"), "the repository's commit is asked for");
        await page.waitForTimeout(100);
        assert.equal((await row.locator(".name span").first().innerText()).trim(), "my-oc-plugin", "the package names it, not the spec");
        const vers = () => row.locator(".pm-ver").allInnerTexts().then((v) => v.map((x) => x.trim()));
        assert.deepEqual(await vers(), ["v1.0.0", HAVE]);
        assert.match(await row.locator(".pm-ver", { hasText: HAVE }).getAttribute("title"), /github:owner\/repo/);
        const up = row.locator(".val button", { hasText: new RegExp("^" + w.update + "$") });
        const foot = view.locator(".pm-foot");
        // Update all is with the plugins it names, above the list
        const bar = view.locator(".pm-updates");
        assert.equal(await up.count(), 0, "at its repository's commit, a git plugin has no Update");
        assert.equal(await row.locator(".pm-chip", { hasText: w.fresh }).count(), 0);
        assert.equal(await bar.locator("button", { hasText: w.all }).count(), 0, "nor is it in Update all");
        await shot(view, `plugins-git-current-${engine}-${lang}`);

        // the repository moves on, its package.json's version the same:
        // Check for updates finds it
        repo.head = NEXT;
        await foot.locator("button", { hasText: w.check }).click();
        await row.locator(".pm-chip", { hasText: w.fresh }).waitFor();
        assert.match(await row.locator(".pm-chip", { hasText: w.fresh }).getAttribute("title"), new RegExp(HAVE + ".*github:owner/repo.*" + NEXT));
        assert.equal(await up.count(), 1, "a git plugin behind its repository can be updated");
        assert.match(await up.getAttribute("title"), /github:owner\/repo/);
        assert.match(await bar.locator("button", { hasText: w.all }).innerText(), /1/);
        await shot(view, `plugins-git-installed-${engine}-${lang}`);
        const before2 = await scrolls(page);
        // the repository moves on again after the check, before Update
        repo.after = LATER;
        await up.click();
        await page.waitForFunction((n) => [...document.querySelectorAll("#view-plugins .pm-row .pm-ver")].some((x) => x.textContent.trim() === n), LATER);
        assert.deepEqual(asked.find(([k]) => k === "upgrade")[1], { spec: SPEC });
        assert.equal(await scrolls(page), before2, "Update scrolls nothing");
        assert.match(await page.locator("body").innerText(), new RegExp(w.done));
        // updated: no Update stays behind (it did, before)
        assert.deepEqual(await vers(), ["v1.0.0", LATER]);
        assert.equal(await up.count(), 0, "updated, there is nothing to update");
        assert.equal(await row.locator(".pm-chip", { hasText: w.fresh }).count(), 0);
        assert.equal(await bar.locator("button", { hasText: w.all }).count(), 0);

        // its page: the README it came with, no npm link, the repository as Source
        await row.locator(".who").click();
        const dlg = page.locator("#modal .pm-detail");
        await dlg.locator(".pm-md", { hasText: "A plugin installed from its repository." }).waitFor();
        assert.deepEqual(asked.filter(([k]) => k === "page").pop(), ["page", SPEC], "the plugin's own folder is what's asked for");
        assert.equal(await dlg.locator(".pm-links a", { hasText: "npm" }).count(), 0, "npm has no page for it");
        const src = dlg.locator(".pm-links a", { hasText: w.source });
        assert.equal(await src.getAttribute("href"), "https://github.com/owner/repo");
        await shot(page.locator("#modal .dialog"), `plugins-git-page-${engine}-${lang}`);
        await src.click();
        for (let i = 0; i < 50 && !asked.some(([k]) => k === "open"); i++) await new Promise((r) => setTimeout(r, 20));
        assert.deepEqual(asked.find(([k]) => k === "open")[1], { url: "https://github.com/owner/repo" });
        assert.equal(asked.filter(([k]) => k === "fetched").length, 0, "nothing fetched from outside");
        assert.deepEqual(errors, []);
      });
    }
  });
}

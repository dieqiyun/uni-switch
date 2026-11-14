// Run with Node's test runner and Playwright on the module path; see README.md.
// A machine whose `uni-switch` command is a copied file (an older installer, a
// hand cp) keeps running that build after a GUI update moves the app on —
// the stale command that wedged a WebDAV sync before #531's check. The
// window can't tell a stale copy from a current one without running the old
// binary (whose start-up migrates settings), so /api/state reports the copy
// and the window shows advice with a "Hide until the next version" that
// POSTs /api/cli-behind/quiet. The panel is too small for a dialog, so it
// stays out of it. English and Chinese; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

const HEAD = {
  en: "The `uni-switch` command is a copy",
  zh: "终端里的 `uni-switch` 命令是一份拷贝",
};

function server(lang, behind, quiet) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") {
      const s = { agents: [], profiles: [], settings: { lang, theme: "light" } };
      if (behind) s.cliBehind = "~/.local/bin/uni-switch";
      return json(s);
    }
    if (url.pathname === "/api/cli-behind/quiet") { quiet.calls++; return route.fulfill({ status: 204 }); }
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/update" || url.pathname === "/api/drift") return json({});
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a copied command gets advice, once, dismissable`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(async () => { await browser.close(); });
      const quiet = { calls: 0 };
      const page = await (await browser.newContext({ viewport: { width: 900, height: 700 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(lang, true, quiet));
      await page.goto("http://uni-switch.test/");
      const dialog = page.locator("#modal:not([hidden])");
      await dialog.waitFor();
      await assert.match(await dialog.textContent(), new RegExp(HEAD[lang].replace(/[.*+?^${}()|[\]\\]/g, "\\$&")), "the dialog names the copy");
      assert.match(await dialog.textContent(), /~\/\.local\/bin\/uni-switch/, "the dialog names the path");
      // Hide until the next version posts the dismiss and closes
      await dialog.getByRole("button", { name: lang === "zh" ? "隐藏，直到下一个版本" : "Hide until the next version" }).click();
      await page.waitForFunction(() => document.querySelector("#modal").hidden);
      assert.equal(quiet.calls, 1, "the dismiss reached the API");
      // the panel, too small for a dialog, says nothing
      const panel = await (await browser.newContext({ viewport: { width: 380, height: 600 }, reducedMotion: "reduce" })).newPage();
      panel.setDefaultTimeout(5000);
      panel.setDefaultNavigationTimeout(60000);
      panel.on("pageerror", (e) => errors.push(e.message));
      await panel.route("**/*", server(lang, true, quiet));
      await panel.goto("http://uni-switch.test/?mode=panel");
      await panel.locator("#view-agents, #nav, body").first().waitFor();
      await panel.waitForTimeout(500);
      assert.ok(await panel.locator("#modal").evaluate((m) => m.hidden), "the panel stays quiet");
      // and no advice when the command is the link (no cliBehind)
      const ok = await (await browser.newContext({ viewport: { width: 900, height: 700 }, reducedMotion: "reduce" })).newPage();
      ok.setDefaultTimeout(5000);
      ok.setDefaultNavigationTimeout(60000);
      ok.on("pageerror", (e) => errors.push(e.message));
      await ok.route("**/*", server(lang, false, quiet));
      await ok.goto("http://uni-switch.test/");
      await ok.waitForTimeout(500);
      assert.ok(await ok.locator("#modal").evaluate((m) => m.hidden), "a following command is not mentioned");
      const keys = [
        "The `uni-switch` command is a copy",
        "The `uni-switch` command at {path} is a copied file, not the installer's link to the app: it won't follow the app's updates, and an old copy can break what a new one fixed. Re-run the installer, or link it by hand.",
      ];
      const missing = await ok.evaluate((ks) => ks.filter((x) => !I18N.zh[x]), keys);
      assert.deepEqual(missing, [], "the advice has its Chinese");
      assert.deepEqual(errors, []);
    });
  }
}

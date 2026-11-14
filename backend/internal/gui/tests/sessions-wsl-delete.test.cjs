// Run with Node's test runner and Playwright on the module path; see README.md.
// A session in a WSL distro is deleted from the Sessions page as this
// computer's are (TJHHHH: 请问是否可以增加wsl内对于会话的删除呢): its row has
// its box beside the WSL badge, its folder's box picks it, and the bar's
// Delete asks in uni-switch's own dialog and posts sessions/delete with its id;
// no note says WSL's can't be deleted. In English and Chinese; no backend,
// the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const at = (min) => new Date(Date.now() - min * 60e3).toISOString();
const sess = (id, title, min, wsl) => ({
  agent: "claude", id, cwd: "/work/app", title, start: at(min + 5), last: at(min), models: [], wsl,
  resume: wsl ? `wsl.exe -d 'Ubuntu' --cd '/work/app' -e sh -lc 'exec \${SHELL:-sh} -lic ''claude --resume ${id}'''` : `cd /work/app && claude --resume ${id}`,
  path: wsl ? `\\\\wsl.localhost\\Ubuntu\\home\\me\\.claude\\projects\\-work-app\\${id}.jsonl` : `~/.claude/projects/-work-app/${id}.jsonl`,
  size: 4096, messages: 6, files: 1, deletable: true,
});

function serve(lang, calls) {
  let sessions = [sess("w-1", "in the distro", 30, "Ubuntu"), sess("l-1", "on windows", 60)];
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/sessions/manage") {
      return json({ agents: [{ agent: "claude", count: sessions.length, deletable: true, name: "Claude Code", icon: "claude" }],
        agent: "claude", sessions, terminal: false, trash: [], trashDir: "C:\\Users\\me\\AppData\\Roaming\\uni-switch\\trash\\sessions" });
    }
    if (url.pathname === "/api/sessions/delete") {
      const body = req.postDataJSON();
      calls.push(body);
      sessions = sessions.filter((s) => !body.ids.includes(s.id));
      return json({ deleted: body.ids, refused: [] });
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const words = {
  en: { nav: "Sessions", del: "Delete", picked: "1 selected", note: "Sessions in WSL" },
  zh: { nav: "会话", del: "删除", picked: "已选 1 个", note: "WSL 中的会话" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    const w = words[lang];
    test(`${engine} ${lang}: a session in WSL is deleted as this computer's are`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const page = await (await browser.newContext({ viewport: { width: 900, height: 560 }, reducedMotion: "reduce" })).newPage();
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-sessions-wsl-delete.png`) });
        await browser.close();
      });
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      page.on("dialog", (d) => { errors.push("a browser dialog: " + d.message()); d.dismiss(); });
      const calls = [];
      await page.route("**/*", serve(lang, calls));
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator("#nav").getByRole("button", { name: w.nav, exact: true }).click();
      const view = page.locator("#view-sessions");
      const row = view.locator('.row.sm-sess[data-id="w-1"]');
      await view.locator(".row.sm-folder").first().waitFor();
      if (!(await row.isVisible())) await view.locator(".row.sm-folder .fold").first().click();
      await row.waitFor();

      assert.equal(await row.locator(".badge.wsl").textContent(), "WSL Ubuntu");
      assert.equal(await view.locator(".sm-note", { hasText: w.note }).count(), 0, "no note that WSL's can't be deleted");
      assert.equal(await view.locator(".sm-bar").isHidden(), true, "no bar before Select");
      await view.locator(".sm-select").click();
      assert.equal(await view.locator(".sm-bar").isHidden(), false);
      const folderBox = view.locator(".row.sm-folder input.sm-folder-check");
      assert.equal(await folderBox.count(), 1);

      // its own box picks it, and Delete posts it after the dialog
      const box = row.locator("input.sm-check");
      assert.equal(await box.count(), 1, "the WSL session has its box");
      await box.check();
      assert.equal((await view.locator(".sm-bar .sm-count").textContent()).trim(), w.picked);
      await view.locator(".sm-bar .sm-delete").click();
      const ask = page.locator("#modal .sm-ask");
      await ask.waitFor();
      assert.deepEqual(calls, []);
      await ask.getByRole("button", { name: w.del, exact: true }).click();
      await ask.waitFor({ state: "detached" });
      await page.waitForFunction(() => !document.querySelector('.row.sm-sess[data-id="w-1"]'));
      assert.deepEqual(calls, [{ agent: "claude", ids: ["w-1"] }]);
      assert.equal(await view.locator('.row.sm-sess[data-id="l-1"]').count(), 1);
      assert.deepEqual(errors, []);
    });
  }
}

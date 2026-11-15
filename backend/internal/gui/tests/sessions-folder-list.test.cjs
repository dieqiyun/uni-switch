// Run with Node's test runner and Playwright on the module path; see README.md.
// The Sessions page lists project folders as one list, newest first: each
// row has the folder's name, where it is (home said as ~), its newest
// session's title, when and how many. Temporary folders, the disk's root and
// apps' hidden folders wait folded under Other folders at the end. Boxes and
// the Delete bar come with Select; a folder's Delete all is in its ⋯ menu and
// asks in uni-switch's own dialog. In every language, Chromium and WebKit, wide
// and narrow; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const at = (min) => new Date(Date.now() - min * 60e3).toISOString();
const sess = (id, cwd, title, min) => ({
  agent: "claude", id, cwd, title, start: at(min + 5), last: at(min), models: [],
  resume: `cd ${cwd} && claude --resume ${id}`, path: `~/.claude/projects/x/${id}.jsonl`, size: 1024, messages: 4, files: 1, deletable: true,
});

function serve(lang, calls) {
  let sessions = [
    sess("t-1", "/private/var/folders/15/p0yq/T", "make a commit message", 10),
    sess("a-1", "/home/me/work/app", "fix the login", 20),
    sess("r-1", "/", "summarize a file", 25),
    sess("a-2", "/home/me/work/app", "add tests", 40),
    sess("h-1", "/home/me/.yansu-agent/chats/1784", "hello", 50),
    sess("b-1", "/home/me/blog", "write the post", 300),
  ];
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
        agent: "claude", home: "/home/me", sessions, terminal: false, trash: [], trashDir: "~/.local/share/uni-switch/trash/sessions" });
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

const keys = ["Other folders", "Temporary folders, the disk's root and apps' hidden folders", "Delete all sessions", "Select", "Done", "More", "{n} folders"];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [1000, 420]) {
      test(`${engine} ${lang} ${width}px: folders are one list, the temporary ones folded at the end`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const page = await (await browser.newContext({ viewport: { width, height: 700 }, reducedMotion: "reduce" })).newPage();
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-sessions-folder-list.png`) });
          }
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
        await page.evaluate(() => show("sessions"));
        const view = page.locator("#view-sessions");
        const tr = (k, vars) => page.evaluate(([k, vars]) => t(k, vars), [k, vars]);
        const folders = view.locator(".sm-folders .row.sm-folder");
        await folders.first().waitFor();

        if (lang !== "en") {
          const missing = await page.evaluate(([lang, keys]) => keys.filter((k) => !I18N[lang][k]), [lang, keys]);
          assert.deepEqual(missing, [], `${lang} has every string`);
        }

        // the projects, newest first; the newest open
        assert.deepEqual(await folders.locator(".fold .name").allTextContents(), ["app", "blog"]);
        const app = folders.nth(0);
        assert.equal(await app.locator(".fold").getAttribute("aria-expanded"), "true");
        assert.equal(await app.locator(".sm-latest").textContent(), "fix the login");
        assert.notEqual((await app.locator(".sm-ago").textContent()).trim(), "");
        assert.equal(await app.getAttribute("title"), "/home/me/work/app");
        if (width > 560) assert.equal(await app.locator(".sm-path").textContent(), "~/work");

        // the rest folded at the end, saying how many
        const others = view.locator(".sm-others");
        assert.equal(await others.locator(".sm-others-fold .name").textContent(), await tr("Other folders"));
        assert.equal((await others.locator(".sm-others-row > .note").textContent()).trim(), await tr("{n} folders", { n: 3 }));
        assert.equal(await others.locator(".sm-others-fold").getAttribute("aria-expanded"), "false");
        assert.equal(await others.locator(".row.sm-folder").count(), 0);
        await others.locator(".sm-others-fold").click();
        assert.deepEqual(await others.locator(".row.sm-folder .fold .name").allTextContents(), ["T", "/", "1784"]);

        // no boxes, no bar until Select; Done puts them away and the pick
        assert.equal(await view.locator(".sm-check, .sm-folder-check").count(), 0);
        assert(!(await view.locator(".sm-bar").isVisible()));
        const sel = view.locator(".sm-select");
        assert.equal((await sel.textContent()).trim(), await tr("Select"));
        await sel.click();
        assert.equal((await sel.textContent()).trim(), await tr("Done"));
        assert.equal(await sel.getAttribute("aria-pressed"), "true");
        assert(await view.locator(".sm-bar").isVisible());
        await app.locator(".sm-folder-check").check();
        await sel.click();
        assert.equal(await view.locator(".sm-check, .sm-folder-check").count(), 0);
        await sel.click();
        assert.equal(await app.locator(".sm-folder-check").isChecked(), false, "Done let the pick go");
        await sel.click();

        // ⋯ → Delete all sessions asks, then deletes the folder's sessions
        await app.hover();
        await app.locator(".sm-folder-more").click();
        await page.locator(".row-menu").getByRole("menuitem", { name: await tr("Delete all sessions"), exact: true }).click();
        const ask = page.locator("#modal .sm-ask");
        await ask.waitFor();
        assert.equal((await ask.locator(".sm-ask-path").textContent()).trim(), "/home/me/work/app");
        assert.deepEqual(calls, []);
        await ask.getByRole("button", { name: await tr("Delete"), exact: true }).click();
        await ask.waitFor({ state: "detached" });
        await page.waitForFunction(() => !document.querySelector('#view-sessions .row.sm-sess[data-id="a-1"]'));
        assert.deepEqual(calls.map((c) => [...c.ids].sort()), [["a-1", "a-2"]]);

        const wide = await page.evaluate(() => document.documentElement.scrollWidth > innerWidth);
        assert.equal(wide, false, "nothing runs past the window");
        assert.deepEqual(errors, []);
      });
    }
  }
}

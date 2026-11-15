// Run with Node's test runner and Playwright on the module path; see README.md.
// #2311 (betacatZ): with the Sessions page switched off (Plugins › Built-in),
// a row of Usage › Sessions' latest sessions looked like a link, a chevron
// and a pointer, and a click did nothing, as there was no page to open it on.
// Now such a row unfolds the session's details in place (time, folder, id,
// the command that resumes it), by click or by key, and folds again; with
// the page on, a row still opens there. English and Chinese, Chromium and
// WebKit, and a narrow window.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const at = (min) => new Date(Date.now() - min * 60e3).toISOString();
const sess = (id, title, min, n) => ({
  agent: "codex", id, cwd: "/work/tool", title, start: at(min + 5), last: at(min), name: "Codex", icon: "openai",
  models: [{ model: "gpt-5.5", input: 1000 * n, output: 100 * n, cache_read: 500 * n, cache_write: 0, cost: 0.5 * n, priced: true }],
  input: 1000 * n, output: 100 * n, cache_read: 500 * n, cache_write: 0, cost: 0.5 * n, unpriced: 0,
  resume: `cd /work/tool && codex resume ${id}`, path: `~/.codex/sessions/${id}.jsonl`,
});

function serve(lang, off) {
  const list = [sess("c-1", "你好", 5, 3), sess("c-2", "port the parser", 50, 2)];
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = ${JSON.stringify({ lang, theme: "light", web: true, featuresOff: off })};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/sessions") return json({ sessions: list, terminal: true, dirs: ["~/.codex/sessions"] });
    if (url.pathname === "/api/sessions/stats") {
      const today = new Date().toISOString().slice(0, 10);
      const usage = list.map((s) => ({ agent: s.agent, cwd: s.cwd, model: s.models[0].model, input: s.input, output: s.output, cache_read: s.cache_read, cache_write: 0, cost: s.cost, priced: true }));
      return json({ from: today, to: today, days: [{ date: today, usage, active: [] }], agents: { codex: "Codex" } });
    }
    if (url.pathname === "/api/sessions/overview") return json({ count: 2, median: 5000, p90: 5000, days: [1], top: { tokens: [], cost: [], active: [] } });
    if (url.pathname === "/api/sessions/manage") {
      return json({ agents: [{ agent: "codex", count: list.length, deletable: true, name: "Codex", icon: "openai" }], agent: "codex", sessions: list.map((s) => ({ ...s, size: 4096, messages: 6, files: 1, deletable: true })), terminal: true, trash: [], trashDir: "~/uni-switch/trash/sessions" });
    }
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/plugins/builtins") return json(off.map((id) => ({ id, on: false })));
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    const body = await fs.readFile(file).catch(() => null);
    await (body ? route.fulfill({ body, contentType }) : route.fulfill({ status: 404, body: "" }));
  };
}

// the reader's wheel brings a row into sight (a scroll by code is put back)
async function reach(page, l) {
  const v = page.locator("#view-usage");
  const box = await v.boundingBox();
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  for (let k = 0; k < 120; k++) {
    const b = await l.boundingBox();
    if (b.y >= box.y + 40 && b.y + b.height <= box.y + box.height - 40) break;
    await page.mouse.wheel(0, b.y < box.y + 40 ? -60 : 60);
    await page.waitForTimeout(20);
  }
  await page.waitForTimeout(300);
}

const words = { en: { id: "Session ID", resume: "Resume" }, zh: { id: "会话 ID", resume: "继续" } };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    for (const width of [900, 420]) {
      test(`${engine} ${lang} ${width}px: with the Sessions page off, a latest session unfolds its details in place`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const page = await (await browser.newContext({ viewport: { width, height: 700 }, reducedMotion: "reduce" })).newPage();
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-sessions-usage-off.png`) });
          }
          await browser.close();
        });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, ["sessions"]));
        await page.addInitScript(() => { localStorage.setItem("uni-switch.usageTab", "sessions"); localStorage.setItem("uni-switch.sessRange", "all"); });
        await page.goto("http://uni-switch.test/?view=usage");

        const list = page.locator("#sessList");
        const row = list.locator(".row.sess", { hasText: "你好" });
        await row.waitFor();
        assert.equal(await row.getAttribute("role"), "button", "not a link to a page that isn't there");
        assert.equal(await row.getAttribute("title"), null);
        assert.equal(await row.getAttribute("aria-expanded"), "false");

        await reach(page, row);
        await row.click();
        const det = list.locator(".sess-item.open .sess-detail");
        await det.waitFor();
        const text = await det.innerText();
        assert.match(text, /c-1/);
        assert.match(text, /codex resume c-1/);
        assert(text.includes(words[lang].id) && text.includes(words[lang].resume), text);
        assert.equal(await list.locator(".row.sess", { hasText: "你好" }).getAttribute("aria-expanded"), "true");
        assert(await page.locator("#view-usage").isVisible(), "the page stays on Usage");
        assert(await det.evaluate((d) => d.getBoundingClientRect().right <= innerWidth + 1), "the details fit the window");

        // one at a time, and the keyboard folds it again
        await list.locator(".row.sess", { hasText: "port the parser" }).click();
        await list.locator(".sess-item.open", { hasText: "port the parser" }).locator(".sess-detail").waitFor();
        assert.equal(await list.locator(".sess-item.open").count(), 1);
        await page.keyboard.press("Enter");
        await list.locator(".sess-item.open").waitFor({ state: "detached" });
        assert.deepEqual(errors, []);
      });
    }

    test(`${engine} ${lang}: with the Sessions page on, a latest session still opens there`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const page = await (await browser.newContext({ viewport: { width: 900, height: 700 }, reducedMotion: "reduce" })).newPage();
      t.after(() => browser.close());
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      await page.route("**/*", serve(lang, []));
      await page.addInitScript(() => { localStorage.setItem("uni-switch.usageTab", "sessions"); localStorage.setItem("uni-switch.sessRange", "all"); });
      await page.goto("http://uni-switch.test/?view=usage");
      const row = page.locator("#sessList .row.sess", { hasText: "你好" });
      await row.waitFor();
      assert.equal(await row.getAttribute("role"), "link");
      await reach(page, row);
      await row.click();
      await page.locator("#view-sessions .sm-item.open .row.sm-sess[data-id=\"c-1\"]").waitFor();
      assert.equal(await page.locator("#sessList .sess-item.open").count(), 0);
    });
  }
}

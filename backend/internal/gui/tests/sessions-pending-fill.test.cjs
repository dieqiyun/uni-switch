// Run with Node's test runner and Playwright on the module path; see README.md.
// hey_boyce on X: the Sessions page stood on its placeholder rows for a
// long time on its first open. uni-switch now answers before it has read a
// long history whole: rows it hasn't read yet come from their files' first
// lines and say they are pending, and the page asks again until none is.
// Here the first answer has every row pending and no messages counted; the
// page shows its rows at once, asks again on its own clock, and draws the
// counts in without taking the filter's focus, then stops asking.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const nap = (ms) => new Promise((r) => setTimeout(r, ms));
const assets = path.resolve(__dirname, "../assets");
const at = (min) => new Date(Date.now() - min * 60e3).toISOString();
const sess = (id, cwd, title, min, extra) => ({
  agent: "claude", id, cwd, title, start: at(min + 5), last: at(min), models: [],
  resume: `cd ${cwd} && claude --resume ${id}`, path: `~/.claude/projects/x/${id}.jsonl`, size: 4096, files: 1, deletable: true, ...extra,
});

function serve(lang, seen) {
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/sessions/manage") {
      seen.manage++;
      const read = seen.manage > 1; // read whole by the second ask
      const extra = read ? { messages: 6 } : { pending: true };
      return json({
        agents: [{ agent: "claude", count: 2, deletable: true, name: "Claude Code", icon: "claudecode-color" }],
        agent: "claude", sessions: [sess("a-1", "/work/app", "fix the login form", 3, extra), sess("a-2", "/work/app", "older task", 90, extra)],
        terminal: true, trash: [], trashDir: "~/trash",
      });
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    const body = await fs.readFile(file).catch(() => null);
    return body ? route.fulfill({ body, contentType }) : route.fulfill({ status: 404, body: "" });
  };
}

const words = {
  en: { nav: "Sessions", six: "6 messages" },
  zh: { nav: "会话", six: "6 条消息" },
  "zh-TW": { nav: "會話", six: "6 條訊息" },
  ja: { nav: "セッション", six: "6 件のメッセージ" },
  de: { nav: "Sitzungen", six: "6 Nachrichten" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const [lang, width] of [["en", 900], ["zh", 900], ["zh-TW", 700], ["ja", 700], ["de", 420]]) {
    const w = words[lang];
    test(`${engine} ${lang} ${width}px: pending rows show at once and fill in`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width, height: 560 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [], seen = { manage: 0 };
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, seen));
      await page.clock.install();
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator("#nav").getByRole("button", { name: w.nav, exact: true }).click();
      const view = page.locator("#view-sessions");
      const row = view.locator('.row.sm-sess[data-id="a-1"]');
      await row.waitFor();
      assert.equal(seen.manage, 1);
      assert.ok(!(await row.textContent()).includes(w.six), "counted before the read");
      // the reader is typing in the filter when the counts come in
      const filter = view.locator(".sm-filter");
      await filter.click();
      await filter.pressSequentially("fi");
      await page.clock.runFor(2000);
      for (let i = 0; i < 100 && !(await row.textContent()).includes(w.six); i++) await nap(20);
      assert.ok((await row.textContent()).includes(w.six), "the counts were not drawn in");
      assert.equal(seen.manage, 2);
      assert.equal(await page.evaluate(() => document.activeElement?.classList.contains("sm-filter")), true, "the filter lost its focus");
      assert.equal(await filter.inputValue(), "fi");
      await filter.pressSequentially("x");
      assert.equal(await filter.inputValue(), "fix");
      // none pending: no more asks
      await page.clock.runFor(6000);
      await nap(100);
      assert.equal(seen.manage, 2, "asked again with nothing pending");
      assert.deepEqual(errors, []);
    });
  }
}

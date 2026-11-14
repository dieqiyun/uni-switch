// Run with Node's test runner and Playwright on the module path; see README.md.
// A Sessions row keeps room for its title (cybercaius on Discord: in the
// Windows app, in Chinese, at a narrow window, a session's title showed
// only "7" while its tokens, cost, 继续 and 换 Agent 继续 took the row).
// At every width the window can be, the title gets at least a readable
// share of the row and ellipsizes, and nothing on the row is cut off or
// pushed past its edge. In English and Chinese, Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const at = (min) => new Date(Date.now() - min * 60e3).toISOString();
const title = "7 月的发布说明：把会话列表在窄窗口里的标题、用量和操作重新排好";
const sess = (id, min) => ({
  agent: "claude", id, cwd: "/work/app", title, start: at(min + 5), last: at(min),
  models: [{ model: "claude-opus-5-5", priced: true, input: 6500000, output: 200000, calls: 40 }],
  input: 6500000, output: 200000, cost: 17.55, messages: 120, size: 4096 * 1024, files: 1, deletable: true,
  resume: `cd /work/app && claude --resume ${id}`, path: `~/.claude/projects/x/${id}.jsonl`,
  carry: [{ agent: "codex", command: "codex resume x" }, { agent: "opencode", command: "opencode -s x" }],
});

function serve(lang) {
  const sessions = [sess("s-1", 1), sess("s-2", 30)];
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
      return json({
        agents: [{ agent: "claude", count: sessions.length, deletable: true, name: "Claude Code", icon: "claudecode-color" }],
        agent: "claude", sessions, terminal: true, trash: [], trashDir: "~/Library/Application Support/uni-switch/trash/sessions",
      });
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const nav = { en: "Sessions", zh: "会话" };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a Sessions row keeps room for its title at every width`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const page = await (await browser.newContext({ viewport: { width: 1100, height: 600 }, reducedMotion: "reduce" })).newPage();
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-sessions-row-narrow.png`) });
        }
        await browser.close();
      });
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator("#nav").getByRole("button", { name: nav[lang], exact: true }).click();
      const row = page.locator('#view-sessions .row.sm-sess[data-id="s-1"]');
      await row.waitFor();
      const seen = [];
      for (const width of [1100, 960, 860, 780, 720, 660, 600, 560, 500, 440, 400, 360]) {
        await page.setViewportSize({ width, height: 600 });
        await page.waitForFunction(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(() => r(true)))));
        const m = await row.evaluate((r) => {
          const box = r.getBoundingClientRect(), name = r.querySelector(".who .name");
          const out = [...r.children].filter((c) => c.offsetParent && (c.getBoundingClientRect().right > box.right + 1 || c.getBoundingClientRect().left < box.left - 1)).map((c) => c.className);
          const cut = [...r.querySelectorAll("button")].filter((b) => b.offsetParent && b.scrollWidth > b.clientWidth + 1).map((b) => b.className);
          return { row: Math.round(box.width), name: Math.round(name.getBoundingClientRect().width), ellipsis: getComputedStyle(name).textOverflow, out, cut };
        });
        seen.push({ width, ...m });
      }
      for (const s of seen) {
        assert.ok(s.name >= Math.min(140, s.row * 0.35), `at ${s.width}px the title has ${s.name}px of a ${s.row}px row: ${JSON.stringify(seen)}`);
        assert.equal(s.ellipsis, "ellipsis", `at ${s.width}px`);
        assert.deepEqual(s.out, [], `at ${s.width}px past the row's edge`);
        assert.deepEqual(s.cut, [], `at ${s.width}px buttons cut`);
      }
      assert.deepEqual(errors, []);
    });
  }
}

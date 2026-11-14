// Run with Node's test runner and Playwright on the module path; see README.md.
// #2278: over a long history the server takes a long time over a sessions
// overview, and the app grew to 85 GB while the Sessions page asked for
// the same overview again before the last was answered. A filter's read of
// the overview and the page's fifteen-second reload ask for the same one
// here, and it is asked for once while the first is under way. The API is
// faked, holding the filtered overview; the fifteen seconds are the page
// clock's, run forward.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const today = new Date().toISOString().slice(0, 10);
const at = today + "T08:00:00Z";
const session = (agent, id, name) => ({ agent, id, name, title: name + " work", cwd: "/work", start: at, last: at, models: [], input: 10, output: 1, cost: 0, resume: "", path: "/test/" + id });

function serve(lang, seen, held) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs={lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window={};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/sessions/progress") return json({ indexing: false });
    if (url.pathname === "/api/sessions") return json({ sessions: [session("claude", "a", "Claude Code"), session("codex", "b", "Codex")], dirs: [] });
    if (url.pathname === "/api/sessions/stats") {
      seen.stats++;
      return json({ from: today, to: today, days: [], agents: { claude: "Claude Code", codex: "Codex" } });
    }
    if (url.pathname === "/api/sessions/overview") {
      const agent = url.searchParams.get("agent") || "all";
      seen.overview[agent] = (seen.overview[agent] || 0) + 1;
      const body = { count: agent === "all" ? 2 : 1, days: [], top: { tokens: [], cost: [], active: [] } };
      if (agent === "codex") return new Promise((resolve) => held.push(() => resolve(json(body))));
      return json(body);
    }
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const body = await fs.readFile(file).catch(() => null);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml" }[path.extname(file)];
    return body ? route.fulfill({ body, contentType }) : route.fulfill({ status: 404, body: "" });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const [lang, width] of [["en", 1100], ["zh", 1100], ["zh-TW", 900], ["ja", 900], ["de", 420]]) {
    test(`${engine} ${lang} ${width}px: an overview under way is not asked for again`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await browser.newPage({ viewport: { width, height: 760 } });
      page.setDefaultTimeout(8000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [], seen = { stats: 0, overview: {} }, held = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, seen, held));
      await page.clock.install();
      await page.addInitScript(() => { localStorage.setItem("uni-switch.usageTab", "sessions"); localStorage.setItem("uni-switch.sessRange", "today"); });
      await page.goto("http://uni-switch.test/?view=usage");
      await page.locator("#sessAgent").click();
      await page.locator('.sess-menu .pm-item[title="codex"]').click();
      for (let i = 0; i < 100 && !held.length; i++) await new Promise((r) => setTimeout(r, 20));
      assert.equal(held.length, 1, "the filter asked for its overview");
      const stats = seen.stats;
      // the page's reload, while the filter's overview is still under way
      await page.clock.runFor(16000);
      for (let i = 0; i < 200 && seen.stats === stats; i++) await new Promise((r) => setTimeout(r, 20));
      assert.ok(seen.stats > stats, "the page read the sessions again");
      await new Promise((r) => setTimeout(r, 200));
      assert.equal(seen.overview.codex, 1, "the same overview was asked for again while the first was under way");
      for (const answer of held.splice(0)) answer();
      await new Promise((r) => setTimeout(r, 200));
      assert.equal(seen.overview.codex, 1);
      assert.deepEqual(errors, []);
    });
  }
}

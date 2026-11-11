// Run with Node's test runner and Playwright on the module path; see README.md.
// Hide accounts hides accounts, not every text with an account's name in it
// (#1973: with the account lingting hidden on Usage, the project
// lingting-react-ui on Usage's Sessions showed as ••••••••-react-ui). The
// Sessions tab, read from the agents' own logs, names no account: a
// project there keeps its name, even one called just as the account is,
// while an address is still hidden. Elsewhere a name inside a word joined
// by a hyphen or a dot isn't the account either, and the account itself
// still is. In every language, Chromium and WebKit, at 1100 and 420 wide.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const iso = (d) => [d.getFullYear(), d.getMonth() + 1, d.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const to = new Date();
const days = Array.from({ length: 5 }, (_, i) => { const d = new Date(to); d.setDate(d.getDate() - 4 + i); return d; }).map((d) => ({
  date: iso(d),
  usage: [
    { agent: "claude", cwd: "/Users/me/code/lingting-react-ui", model: "claude-opus-5-5", input: 30000, output: 4000, cache_read: 0, cache_write: 0, cost: 1.5, priced: true },
    { agent: "claude", cwd: "/Users/me/code/lingting", model: "claude-opus-5-5", input: 20000, output: 2000, cache_read: 0, cache_write: 0, cost: 1, priced: true },
  ],
  active: [],
}));
const quotas = [{ provider: "claude", name: "Claude", icon: "claude-color", plan: "Max", user: "lingting", windows: [{ name: "5 hours", used: 20 }] }];

function serve(lang) {
  const state = { agents: [{ id: "claude", name: "Claude Code", path: "/test/settings.json", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") return json(quotas);
    if (url.pathname === "/api/sessions/progress") return json({ indexing: false });
    if (url.pathname === "/api/sessions") return json({ sessions: [], dirs: ["/test/sessions"] });
    if (url.pathname === "/api/sessions/stats") return json({ from: days[0].date, to: iso(to), days, agents: { claude: "Claude Code" } });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    const body = await fs.readFile(file).catch(() => null);
    await (body ? route.fulfill({ body, contentType }) : route.fulfill({ status: 404, body: "" }));
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [1100, 420]) {
      test(`${engine} ${lang} ${width}: a project named like the account isn't hidden as one`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await (await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        await page.addInitScript(() => { try { localStorage.setItem("uni-switch.usageTab", "usage"); localStorage.setItem("uni-switch.sessRange", "all"); } catch {} });
        await page.goto("http://uni-switch.test/?view=usage");
        await page.waitForSelector(".subscription-account .user");

        // the reporter's steps: Usage, Hide accounts on, then Sessions
        await page.evaluate(() => {
          const d = document.createElement("div");
          d.id = "sample";
          d.textContent = "lingting-react-ui · lingting.dev · lingting · lingting@example.com";
          document.querySelector("#usagePane").append(d);
        });
        await page.locator("#usageMask").click();
        await page.waitForFunction(() => document.querySelector(".subscription-account .user .pii")?.dataset.raw === "lingting");
        await page.waitForFunction(() => document.querySelectorAll("#sample .pii").length === 2);
        assert.deepEqual(await page.evaluate(() => [...document.querySelectorAll("#sample .pii")].map((e) => e.dataset.raw)), ["lingting", "lingting@example.com"]);
        assert.match(await page.locator("#sample").textContent(), /^lingting-react-ui · lingting\.dev · \S+ · \S+$/);

        await page.locator(":is(#usageTab .opt, .nav-s):visible").nth(2).click();
        await page.locator("#sessGrid:not([hidden])").waitFor();
        await page.waitForFunction(() => document.querySelectorAll("#sessProjects .sess-bar").length === 2);
        // an address in a session is still hidden
        await page.evaluate(() => {
          const d = document.createElement("div");
          d.id = "sessSample";
          d.textContent = "lingting asked lingting@example.com";
          document.querySelector("#sessionsPane").append(d);
        });
        await page.waitForFunction(() => document.querySelector("#sessSample .pii")?.dataset.raw === "lingting@example.com");
        const got = await page.evaluate(() => ({
          projects: [...document.querySelectorAll("#sessProjects .sess-bar .n")].map((n) => n.textContent).sort(),
          hidden: [...document.querySelectorAll("#sessionsPane .pii")].map((e) => e.dataset.raw),
          titles: [...document.querySelectorAll("#sessionsPane [title]")].map((e) => e.title).filter((s) => s.includes("•")),
        }));
        assert.deepEqual(got.projects, ["lingting", "lingting-react-ui"]);
        assert.deepEqual(got.hidden, ["lingting@example.com"]);
        assert.deepEqual(got.titles, []);
        assert.match(await page.locator("#sessSample").textContent(), /^lingting asked \S+$/);
        assert.deepEqual(errors, []);
      });
    }
  }
}

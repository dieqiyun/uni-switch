// Run with Node's test runner and Playwright on the module path; see README.md.
// cybercaius and young02036 (Discord, Windows): with only Claude Desktop
// installed, the Agents page listed Claude Code too, by its ~/.claude, and
// nothing said which card Desktop reads. With no Claude Code of its own
// found, its CLI is the one Desktop includes for its Code tab, and the
// card says so ("In Claude Desktop"), its tip naming the version, the
// settings file and that a Code session in WSL runs the distro's card.
// A Claude Code of its own is shown as before. In every language, wide and
// narrow. No backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const models = ["model-a"].map((m) => ({ value: m, label: m }));
const AGENTS = [{ id: "claude", name: "Claude Code", path: "C:\\Users\\me\\.claude\\settings.json", fields: [{ key: "model", label: "model", value: "model-a", options: models }] }];

function server(lang, clis) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: AGENTS, profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/agents/cli" && req.method() === "GET") return route.fulfill({ json: { agents: clis, pending: false } });
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const row = '.row.agent[data-id="claude"]';
const WORDS = {
  en: ["In Claude Desktop", /^Claude Code 2\.1\.310, included in Claude Desktop\nNo Claude Code of its own is installed here.*C:\\Users\\me\\\.claude\\settings\.json.*WSL/s],
  zh: ["Claude Desktop 内置", /^Claude Desktop 内置的 Claude Code 2\.1\.310\n这里没有单独安装的 Claude Code.*settings\.json.*WSL/s],
  "zh-TW": ["Claude Desktop 內建", /^Claude Desktop 內建的 Claude Code 2\.1\.310\n這裡沒有單獨安裝的 Claude Code.*settings\.json.*WSL/s],
  ja: ["Claude Desktop に内蔵", /^Claude Desktop に内蔵の Claude Code 2\.1\.310\nここには単体の Claude Code.*settings\.json.*WSL/s],
  de: ["In Claude Desktop enthalten", /^Claude Code 2\.1\.310, in Claude Desktop enthalten\nHier ist kein eigenes Claude Code.*settings\.json.*WSL/s],
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const [lang, [tag, tip]] of Object.entries(WORDS)) {
    for (const width of [980, 560]) {
      test(`${engine} ${lang} ${width}: Claude Code only in Claude Desktop says so`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const errors = [];
        const open = async (clis) => {
          const page = await (await browser.newContext({ viewport: { width, height: 640 } })).newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", server(lang, clis));
          await page.goto("http://uni-switch.test/");
          await page.locator(row).waitFor();
          return page;
        };
        const page = await open({ claude: { inDesktop: true, desktopVersion: "2.1.310" } });
        const v = page.locator(`${row} .ag-app`);
        await v.waitFor();
        assert.equal(await v.textContent(), tag);
        assert.match(await v.getAttribute("title"), tip);
        // on the name's line, and nothing wider than the window
        const mid = (s) => page.evaluate((s) => { const b = document.querySelector(s).getBoundingClientRect(); return b.top + b.height / 2; }, s);
        assert(Math.abs((await mid(`${row} .name`)) - (await mid(`${row} .ag-app`))) <= 2, "not on the name's line");
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "the page scrolls sideways");
        // Desktop's not fetched yet: the words, with no version line
        const early = await open({ claude: { inDesktop: true } });
        await early.locator(`${row} .ag-app`).waitFor();
        assert.doesNotMatch(await early.locator(`${row} .ag-app`).getAttribute("title"), /2\.1\.310|\n/);
        // its own Claude Code: its version, no Desktop words
        const own = await open({ claude: { version: "2.1.299" } });
        await own.locator(`${row} .ag-ver`).waitFor();
        assert.equal(await own.locator(`${row} .ag-app`).count(), 0);
        assert.deepEqual(errors, []);
      });
    }
  }
}

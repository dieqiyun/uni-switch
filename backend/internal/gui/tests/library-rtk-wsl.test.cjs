// Run with Node's test runner and Playwright on the module path; see README.md.
// lex on Discord: Claude Code in WSL couldn't have the RTK of uni-switch on
// Windows. An agent in a WSL distro is listed, with its distro's own RTK in
// a card of its own: one not installed there has Install RTK in WSL
// <distro> (RTK's own Linux installer, run in the distro), and the agent's
// switch waits for it; a distro that isn't running says so, and its agents'
// switches are off limits, since uni-switch never starts it. In every language,
// both engines, at a phone's width too. No backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "C:\\Users\\lex";
const lib = () => ({
  dir: `${HOME}\\.uni-switch\\library`, backups: `${HOME}\\.uni-switch\\backups`, home: HOME,
  agents: [{ id: "claude", name: "Claude Code", icon: "", skills: `${HOME}\\.claude\\skills`, mcp: `${HOME}\\.claude.json` }],
  instructions: { agents: [], sets: [] }, foundServers: [], projects: [], foundSkills: [], skills: [], servers: [],
});
const INSTALL = "curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh | sh";
const rtkView = (installed) => ({
  path: `${HOME}\\.local\\bin\\rtk.exe`, version: "0.52.0", url: "https://www.rtk-ai.app",
  agents: [
    { id: "claude", name: "Claude Code", icon: "", on: false },
    { id: "claude@wsl:Ubuntu", name: "Claude Code · WSL Ubuntu", icon: "", on: false, wsl: "Ubuntu" },
    { id: "codex@wsl:Arch", name: "Codex · WSL Arch", icon: "", on: false, wsl: "Arch", asleep: true },
  ],
  wsl: [
    installed ? { name: "Ubuntu", path: "/home/lex/.local/bin/rtk", version: "0.52.0" } : { name: "Ubuntu", install: INSTALL },
    { name: "Arch", asleep: true },
  ],
});

function server(lang, calls) {
  let installed = false;
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/library") return route.fulfill({ json: lib() });
    if (url.pathname === "/api/library/rtk") return route.fulfill({ json: rtkView(installed) });
    if (url.pathname === "/api/library/rtk/install") {
      calls.push(req.postDataJSON());
      installed = true;
      return route.fulfill({ json: rtkView(true) });
    }
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { title: "RTK in WSL Ubuntu", button: "Install RTK in WSL Ubuntu", asleep: "Not running", stopped: "WSL Arch isn't running", done: "RTK is installed in WSL Ubuntu — switch it on for its agents below" },
  zh: { title: "WSL Ubuntu 中的 RTK", button: "在 WSL Ubuntu 中安装 RTK", asleep: "未运行", stopped: "WSL Arch 未运行", done: "RTK 已安装到 WSL Ubuntu — 在下方为其中的 Agent 打开它" },
  "zh-TW": { title: "WSL Ubuntu 中的 RTK", button: "在 WSL Ubuntu 中安裝 RTK", asleep: "未執行", stopped: "WSL Arch 未執行", done: "RTK 已安裝到 WSL Ubuntu — 在下方為其中的 Agent 開啟它" },
  ja: { title: "WSL Ubuntu の RTK", button: "WSL Ubuntu に RTK をインストール", asleep: "停止中", stopped: "WSL Arch は起動していません", done: "WSL Ubuntu に RTK をインストールしました — 下でそのエージェントをオンにしてください" },
  de: { title: "RTK in WSL Ubuntu", button: "RTK in WSL Ubuntu installieren", asleep: "Läuft nicht", stopped: "WSL Arch läuft nicht", done: "RTK ist in WSL Ubuntu installiert — schalte es unten für ihre Agents ein" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": an agent in WSL gets its distro's own RTK", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const lang of Object.keys(words)) for (const width of [980, 380]) {
      await t.test(`${lang} at ${width}px`, async () => {
        const w = words[lang], calls = [];
        const ctx = await browser.newContext({ viewport: { width, height: 900 } });
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "rtk"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, calls));
        await page.goto("http://uni-switch.test/");
        await page.locator('button[data-view="library"]').click();
        const v = page.locator("#view-library");
        const cards = v.locator(".lib-rtk-wsl");
        await cards.first().waitFor();
        assert.equal(await cards.count(), 2);
        const ubuntu = cards.nth(0), arch = cards.nth(1);
        assert.equal(await ubuntu.locator(".lib-cardhead b").textContent(), w.title);
        assert.equal(await ubuntu.locator(".lib-rtk-cmd").textContent(), INSTALL);
        assert((await arch.locator(".lib-rtk-gain").textContent()).startsWith(w.stopped), await arch.locator(".lib-rtk-gain").textContent());
        // a stopped distro offers no install: that would start it
        assert.equal(await arch.getByRole("button", { name: w.button.replace("Ubuntu", "Arch"), exact: true }).count(), 0);

        const row = (name) => v.locator(".lib-row").filter({ has: page.locator(".name", { hasText: name }) });
        const sw = (name) => row(name).getByRole("switch");
        // Windows' rtk is there for Windows' Claude Code, not the distro's
        assert.equal(await sw("Claude Code").first().isDisabled(), false);
        assert.equal(await sw("Claude Code · WSL Ubuntu").isDisabled(), true);
        assert.equal(await sw("Codex · WSL Arch").isDisabled(), true);
        assert.deepEqual(await row("Codex · WSL Arch").locator(".lib-tag").allTextContents(), [w.asleep]);

        await ubuntu.getByRole("button", { name: w.button, exact: true }).click();
        await page.waitForFunction((want) => document.querySelector("#status").textContent === want, w.done).catch(() => {});
        assert.equal(await page.locator("#status").textContent(), w.done);
        assert.deepEqual(calls, [{ distro: "Ubuntu" }]);
        assert.equal(await sw("Claude Code · WSL Ubuntu").isDisabled(), false);
        assert.equal(await sw("Codex · WSL Arch").isDisabled(), true);
        assert.equal(await ubuntu.locator(".note.mono").textContent(), "v0.52.0");
        // nothing runs off the side at a phone's width
        const over = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
        assert(over <= 0, `the page scrolls sideways by ${over}px`);
        await ctx.close();
      });
    }
    assert.deepEqual(errors, []);
  });
}

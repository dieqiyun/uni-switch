// Run with Node's test runner and Playwright on the module path; see README.md.
// 新v学员 on Discord: "RTK and other actions that need elevation all fail".
// RTK winget installed for every user (under Program Files) only an
// administrator can upgrade. Upgrade's title now says Windows asks for an
// administrator first; a prompt declined, or not to be had, says so on the
// card with winget's fix (no npm advice), in the reader's language. No
// backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "C:/Users/aimer";
const DIR = "C:\\Program Files\\WinGet\\Links";
const UPGRADE = "winget upgrade --id rtk-ai.rtk --exact --silent --accept-package-agreements --accept-source-agreements --disable-interactivity";
const agent = (id, name) => ({ id, name, icon: "", skills: `${HOME}/.${id}/skills`, mcp: `${HOME}/.${id}/mcp.json` });
const lib = () => ({
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: [agent("codex", "Codex")],
  instructions: { agents: [], sets: [] }, foundServers: [], projects: [], foundSkills: [], skills: [], servers: [],
});
const rtkView = (admin) => ({
  path: `${DIR}\\rtk.exe`, version: "0.43.0", latest: "0.51.0", upgrade: UPGRADE, url: "https://www.rtk-ai.app", ...(admin ? { admin: DIR } : {}),
  agents: [{ id: "codex", name: "Codex", icon: "", on: true }],
});
const base = { error: "RTK is installed in …", code: "needs_admin", agent: "RTK", prefix: DIR, command: UPGRADE, via: "winget" };

function server(lang, state) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/library") return route.fulfill({ json: lib() });
    if (url.pathname === "/api/library/rtk") return route.fulfill({ json: rtkView(state.admin) });
    if (url.pathname === "/api/library/rtk/upgrade") return route.fulfill({ status: 400, json: state.answer });
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const CASES = {
  en: {
    title: `Runs ${UPGRADE}, as administrator: Windows asks first`,
    plain: `Runs ${UPGRADE}`,
    declined: `The upgrade failed: RTK is installed in ${DIR}, which only an administrator can change, and the administrator prompt was declined. Try again and allow it, or run ${UPGRADE} in a terminal opened as administrator.`,
    none: `The upgrade failed: RTK needs an administrator here, and uni-switch couldn't ask Windows for one (no administrator prompt: The service cannot be started). Try again and allow it, or run ${UPGRADE} in a terminal opened as administrator.`,
  },
  zh: {
    title: `将以管理员身份运行 ${UPGRADE}：Windows 会先询问`,
    plain: `将运行 ${UPGRADE}`,
    declined: `升级失败：RTK 安装在 ${DIR}，这个目录只有管理员能修改，而管理员授权提示被拒绝了。 请再试一次并允许授权，或在以管理员身份打开的终端里运行 ${UPGRADE}。`,
    none: `升级失败：RTK 在这里需要管理员权限，而 uni-switch 没能向 Windows 请求管理员权限（no administrator prompt: The service cannot be started）。 请再试一次并允许授权，或在以管理员身份打开的终端里运行 ${UPGRADE}。`,
  },
  // the other languages: translated, never npm's advice
  ja: {}, de: {}, "zh-TW": {},
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": RTK under Program Files asks for an administrator", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    const open = async (lang, state) => {
      const ctx = await browser.newContext({ viewport: { width: 440, height: 800 } });
      await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "rtk"); } catch {} });
      const page = await ctx.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(lang, state));
      await page.goto("http://uni-switch.test/");
      await page.locator('button[data-view="library"]').click();
      await page.locator("#view-library .lib-cardhead button").waitFor();
      return page;
    };
    const failure = async (page) => {
      await page.locator("#view-library .lib-cardhead button").click();
      const err = page.locator("#view-library .lib-rtk-err");
      await err.waitFor();
      return err.textContent();
    };
    for (const [lang, want] of Object.entries(CASES)) {
      await t.test(lang, async () => {
        // a folder the user can change says nothing of an administrator
        let page = await open(lang, { admin: false });
        const plain = await page.locator("#view-library .lib-cardhead button").getAttribute("title");
        if (want.plain) assert.equal(plain, want.plain);
        await page.context().close();

        page = await open(lang, { admin: true, answer: { ...base, canceled: "1" } });
        const title = await page.locator("#view-library .lib-cardhead button").getAttribute("title");
        if (want.title) assert.equal(title, want.title);
        assert.notEqual(title, plain);
        let said = await failure(page);
        if (want.declined) assert.equal(said, want.declined);
        assert(said.includes(DIR) && said.includes(UPGRADE), said);
        assert(!/npm/.test(said), said);
        if (lang !== "en") assert(!/administrator|declined/.test(said), said);
        await page.context().close();

        page = await open(lang, { admin: true, answer: { ...base, prefix: "", reason: "no administrator prompt: The service cannot be started" } });
        said = await failure(page);
        if (want.none) assert.equal(said, want.none);
        assert(said.includes("The service cannot be started") && !said.includes(DIR) && !/npm/.test(said), said);
        if (lang !== "en") assert(!/needs an administrator|couldn't ask/.test(said), said);
        await page.context().close();
      });
    }
    assert.deepEqual(errors, []);
  });
}

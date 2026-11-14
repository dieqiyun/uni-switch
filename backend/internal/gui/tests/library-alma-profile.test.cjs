// Run with Node's test runner and Playwright on the module path; see README.md.
// Alma's instructions file is its ~/.config/alma/USER.md, its profile of
// its owner (#2315, Jie211): the Library says so on its row, and offers no
// "Move its own into the shared text", which would take the profile, front
// matter and all, into every agent's file. Chromium and WebKit, every
// language; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/foobra";
const library = {
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: [{ id: "alma", name: "Alma", icon: "alma", instructions: `${HOME}/.config/alma/USER.md` }],
  instructions: {
    shared: "Be brief.",
    sets: [{ id: "default", name: "", active: true, text: "Be brief." }],
    agents: [{ agent: "alma", name: "Alma", icon: "alma", path: `${HOME}/.config/alma/USER.md`, on: true, extra: "", own: 6, profile: true }],
  },
  foundServers: [], projects: [], foundSkills: [], skills: [], servers: [],
};

function server(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/library") return route.fulfill({ json: library });
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const view = "#view-library";
const move = {
  en: "Move its own into the shared text",
  zh: "把它的内容移入共享指令",
  "zh-TW": "把它的內容移入共享指令",
  ja: "固有の内容を共有テキストに移す",
  de: "Eigenes in den gemeinsamen Text verschieben",
};
const note = {
  en: "This file is Alma's profile of you: uni-switch adds its part after yours and leaves your own lines in it. A USER.md in a workspace is read instead of this one.",
  zh: "该文件是 Alma 对你的个人资料：uni-switch 会把它的部分加在你的内容之后，你自己的内容保留在文件里。工作区中的 USER.md 会代替此文件被读取。",
  "zh-TW": "該檔案是 Alma 對你的個人資料：uni-switch 會把它的部分加在你的內容之後，你自己的內容保留在檔案裡。工作區中的 USER.md 會取代此檔案被讀取。",
  ja: "このファイルは Alma にとってのあなたのプロフィールです。uni-switch は自分の部分をあなたの内容の後に追加し、あなた自身の行はそのまま残します。ワークスペースに USER.md があると、こちらの代わりにそれが読み込まれます。",
  de: "Diese Datei ist das Profil, das Alma von dir hat: uni-switch fügt seinen Teil nach deinem an und lässt deine eigenen Zeilen darin. Eine USER.md in einem Workspace wird stattdessen gelesen.",
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": Alma's instructions row says USER.md is its profile of the user, and offers no move into the shared text", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const lang of Object.keys(note)) {
      await t.test(lang, async () => {
        const ctx = await browser.newContext({ viewport: { width: 980, height: 860 }, reducedMotion: "reduce" });
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "instructions"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang));
        await page.goto("http://uni-switch.test/");
        await page.locator('button[data-view="library"]').click();
        const row = page.locator(`${view} .lib-row`, { hasText: "Alma" });
        await row.first().click();
        await page.locator(`${view} p.lib-aside`, { hasText: note[lang] }).waitFor();
        assert.equal(await page.locator(`${view} button`, { hasText: move[lang] }).count(), 0, "a move into the shared text is offered for Alma's profile");
        assert.deepEqual(errors, []);
        await ctx.close();
      });
    }
  });
}

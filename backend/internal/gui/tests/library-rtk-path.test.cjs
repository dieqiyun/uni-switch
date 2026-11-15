// Run with Node's test runner and Playwright on the module path; see README.md.
// #601: an rtk uni-switch found off the PATH the agents get (in ~/.local/bin,
// which the user's shell doesn't have) was shown as working, while Pi's
// extension said "RTK disabled: rtk binary not found in PATH". The RTK card
// now says rtk isn't on PATH and what Put RTK on PATH will do, an agent that
// has it is marked Not on PATH, and the button puts it there; in English
// and Chinese. No backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/aimer";
const agent = (id, name) => ({ id, name, icon: "", skills: `${HOME}/.${id}/skills`, mcp: `${HOME}/.${id}/mcp.json` });
const lib = () => ({
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: [agent("pi", "Pi"), agent("codex", "Codex")],
  instructions: { agents: [], sets: [] }, foundServers: [], projects: [], foundSkills: [], skills: [], servers: [],
});
const rtkView = (off) => ({
  path: `${HOME}/.local/bin/rtk`, version: "0.50.0", url: "https://www.rtk-ai.app",
  agents: [{ id: "pi", name: "Pi", icon: "", on: true }, { id: "codex", name: "Codex", icon: "", on: false }],
  ...(off ? { offPath: true, pathDir: `${HOME}/bin`, pathLink: true } : { restart: ["pi"] }),
});

function server(lang, calls) {
  let off = true;
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/library") return route.fulfill({ json: lib() });
    if (url.pathname === "/api/library/rtk") return route.fulfill({ json: rtkView(off) });
    if (url.pathname === "/api/library/rtk/path") {
      calls.push(req.method());
      off = false;
      return route.fulfill({ json: rtkView(false) });
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
  en: { note: "RTK is in ~/.local/bin, which isn't on your PATH", how: "Put RTK on PATH links it into ~/bin.", tag: "Not on PATH", button: "Put RTK on PATH", done: "RTK is on your PATH — restart your agents, and the terminals they run in, to use it" },
  zh: { note: "RTK 位于 ~/.local/bin，该目录不在 PATH 中", how: "“加到 PATH”会在 ~/bin 里建一个指向它的链接。", tag: "不在 PATH 中", button: "加到 PATH", done: "RTK 已在 PATH 中，重启 Agent 及其所在的终端后生效" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": rtk off PATH is said, and put on it", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const lang of ["en", "zh"]) {
      await t.test(lang, async () => {
        const w = words[lang], calls = [];
        const ctx = await browser.newContext({ viewport: { width: 980, height: 800 } });
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "rtk"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, calls));
        await page.goto("http://uni-switch.test/");
        await page.locator('button[data-view="library"]').click();
        const v = page.locator("#view-library");
        const note = v.locator(".lib-rtk-offpath");
        await note.waitFor();
        assert((await note.textContent()).startsWith(w.note), await note.textContent());
        assert.equal(await v.locator(".lib-rtk-cmd").textContent(), w.how);
        const row = (name) => v.locator(".lib-row").filter({ has: page.locator(".name", { hasText: name }) });
        assert.deepEqual(await row("Pi").locator(".lib-tag").allTextContents(), [w.tag]);
        // Codex hasn't got it: nothing to say there
        assert.equal(await row("Codex").locator(".lib-tag").count(), 0);

        await v.getByRole("button", { name: w.button, exact: true }).click();
        await page.waitForFunction((want) => document.querySelector("#status").textContent === want, w.done).catch(() => {});
        assert.equal(await page.locator("#status").textContent(), w.done);
        assert.deepEqual(calls, ["POST"]);
        assert.equal(await note.count(), 0);
        assert.equal(await row("Pi").locator(".lib-tag").count(), 0);
        await ctx.close();
      });
    }
    assert.deepEqual(errors, []);
  });
}

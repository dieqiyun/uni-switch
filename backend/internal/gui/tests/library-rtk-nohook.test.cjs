// Run with Node's test runner and Playwright on the module path; see README.md.
// Discord (lc): "rtk 没有识别 deepseek harness". RTK has no hook for
// DeepSeek Harness (dsh's hooks can't rewrite a command, and rtk init has no
// --agent dsh), and the RTK tab left it out, as if uni-switch hadn't seen it.
// It is listed now, tagged No RTK hook with why, and its switch can't be
// turned on; in English and Chinese. No backend: the API is faked here.
// Discord (新v学员): Grok Build's tag said DeepSeek Harness's reason and
// linked dsh's rtk-ai/rtk#3847. Each agent's tag says its own reason now:
// Grok Build's cites rtk-ai/rtk#2136, the request for Grok Build, and says
// nothing about its hooks.
// Discord (lml0664): Qoder, Cline and Kimi Code, which RTK has nothing
// global for, are listed each with its own reason (Qoder's cites
// rtk-ai/rtk#1327), and Droid, which it has a hook for from RTK 0.44,
// says that version when this RTK is older, not Codex's 0.50.
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
  agents: [agent("pi", "Pi"), agent("dsh", "DeepSeek Harness"), agent("grok", "Grok Build"), agent("qoder", "Qoder"), agent("kimi", "Kimi Code"), agent("droid", "Droid")],
  instructions: { agents: [], sets: [] }, foundServers: [], projects: [], foundSkills: [], skills: [], servers: [],
});
const rtkView = () => ({
  path: `${HOME}/.local/bin/rtk`, version: "0.51.0", url: "https://www.rtk-ai.app",
  agents: [
    { id: "pi", name: "Pi", icon: "", on: false },
    { id: "dsh", name: "DeepSeek Harness", icon: "", on: false, noHook: true, blocked: "RTK has no hook for DeepSeek Harness yet" },
    { id: "grok", name: "Grok Build", icon: "", on: false, noHook: true, blocked: "RTK has no hook for Grok Build yet" },
    { id: "qoder", name: "Qoder", icon: "", on: false, noHook: true, blocked: "RTK has no hook for Qoder yet" },
    { id: "kimi", name: "Kimi Code", icon: "", on: false, noHook: true, blocked: "RTK has no hook for Kimi Code" },
    { id: "droid", name: "Droid", icon: "", on: false, blocked: "Droid's hook needs RTK 0.44 or newer" },
  ],
});

function server(lang, calls) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/library") return route.fulfill({ json: lib() });
    if (url.pathname === "/api/library/rtk") {
      if (req.method() !== "GET") calls.push(req.postData());
      return route.fulfill({ json: rtkView() });
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
  en: { tag: "No RTK hook", why: "DeepSeek Harness's hooks can only allow or deny a command, not change it", grok: "RTK's installer has no option for Grok Build", qoder: "RTK's installer has no option for Qoder", kimi: "AGENTS.md", update: "Update RTK", droid: "RTK 0.44 or newer" },
  zh: { tag: "RTK 暂不支持", why: "DeepSeek Harness 的 hook 只能放行或拦截命令，不能改写", grok: "RTK 的安装程序还没有 Grok Build 的选项", qoder: "RTK 的安装程序还没有 Qoder 的选项", kimi: "AGENTS.md", update: "需更新 RTK", droid: "RTK 0.44 或更高版本" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": an agent RTK has no hook for is listed with why", async (t) => {
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
        const row = (name) => v.locator(".lib-row").filter({ has: page.locator(".name", { hasText: name }) });
        await row("DeepSeek Harness").waitFor();
        const tag = row("DeepSeek Harness").locator(".lib-tag");
        assert.deepEqual(await tag.allTextContents(), [w.tag]);
        assert((await tag.getAttribute("title")).includes(w.why), await tag.getAttribute("title"));
        assert((await tag.getAttribute("title")).includes("rtk-ai/rtk/issues/3847"));
        // Grok Build's tag is its own: its issue, not dsh's, and nothing
        // said about its hooks
        const gtag = row("Grok Build").locator(".lib-tag");
        assert.deepEqual(await gtag.allTextContents(), [w.tag]);
        const gwhy = await gtag.getAttribute("title");
        assert(gwhy.includes(w.grok) && gwhy.includes("rtk-ai/rtk/issues/2136"), gwhy);
        assert(!gwhy.includes("3847") && !gwhy.includes("DeepSeek") && !gwhy.includes("hook"), gwhy);
        assert.equal(await row("Grok Build").locator(".lib-switch").isDisabled(), true);
        // Qoder's and Kimi Code's are their own too
        const qwhy = await row("Qoder").locator(".lib-tag").getAttribute("title");
        assert(qwhy.includes(w.qoder) && qwhy.includes("rtk-ai/rtk/issues/1327") && !qwhy.includes("2136"), qwhy);
        const kwhy = await row("Kimi Code").locator(".lib-tag").getAttribute("title");
        assert(kwhy.includes(w.kimi) && !kwhy.includes("issues/"), kwhy);
        // Droid with an RTK before 0.44: its own version, not Codex's
        const dtag = row("Droid").locator(".lib-tag");
        assert.deepEqual(await dtag.allTextContents(), [w.update]);
        const dwhy = await dtag.getAttribute("title");
        assert(dwhy.includes(w.droid) && !dwhy.includes("0.50"), dwhy);
        assert.equal(await row("Droid").locator(".lib-switch").isDisabled(), true);
        const sw = row("DeepSeek Harness").locator(".lib-switch");
        assert.equal(await sw.isDisabled(), true);
        // Pi has a hook: nothing to say, and it can be switched on
        assert.equal(await row("Pi").locator(".lib-tag").count(), 0);
        assert.equal(await row("Pi").locator(".lib-switch").isDisabled(), false);
        await sw.click({ force: true });
        assert.deepEqual(calls, []);
        await ctx.close();
      });
    }
    assert.deepEqual(errors, []);
  });
}

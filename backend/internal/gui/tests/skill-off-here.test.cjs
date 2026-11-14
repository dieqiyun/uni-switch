// Run with Node's test runner and Playwright on the module path; see README.md.
// hanslin1666 on Discord: turn a library skill off on this computer only,
// without the other computers losing it, and without a sync turning it on
// here again. Each skill's row has a power button that sends
// skills/here with on:false; a skill off here says "Off on this computer"
// with why, its agents' chips are faded and can't be changed here, and a
// click on the tag sends on:true. Neither click scrolls the page. In every
// language, in Chromium and WebKit, wide and narrow. No backend: the API
// is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/hans";
const agent = (id, name) => ({ id, name, icon: "", skills: `${HOME}/.${id}/skills` });
const sk = (name, off) => ({ name, description: name + " skill", kind: "folder", source: `${HOME}/skills/${name}`, agents: ["claude", "codex"], ...(off ? { offHere: true } : {}) });
const lib = (st) => ({
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: [agent("claude", "Claude Code"), agent("codex", "Codex")],
  instructions: { agents: [] }, servers: [], foundServers: [], projects: [], foundSkills: [], newSkills: [],
  checkDue: false,
  skills: [sk("pdf", st.off), sk("slides", false)],
});

function server(lang, posts, st) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/library") return route.fulfill({ json: lib(st) });
    if (url.pathname.startsWith("/api/library/skills/")) {
      const what = url.pathname.split("/").pop(), body = req.postDataJSON();
      posts.push({ what, body });
      if (what === "here") st.off = !body.on;
      return route.fulfill({ json: { ...lib(st), result: { changed: ["claude", "codex"] } } });
    }
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const scrolls = (page) => page.evaluate(() => [...document.querySelectorAll("*")].filter((e) => e.scrollTop).map((e) => (e.id || e.className) + ":" + e.scrollTop).concat("doc:" + document.scrollingElement.scrollTop).join(" "));

const words = {
  en: { off: "Turn off on this computer only: your other computers keep it", on: "Turn on on this computer", tag: "Off on this computer",
    tip: "No agent on this computer gets it. Your other computers keep their own choice, and a sync doesn't turn it on here. Click to turn it on here." },
  zh: { off: "只在本机关闭：其他电脑照常使用", on: "在本机打开", tag: "本机已关闭",
    tip: "这台电脑上的 Agent 都不会用到它。其他电脑保持各自的设置，同步也不会在这里把它重新打开。点一下在本机打开。" },
  "zh-TW": { off: "只在本機關閉：其他電腦照常使用", on: "在本機開啟", tag: "本機已關閉",
    tip: "這台電腦上的 Agent 都不會用到它。其他電腦保持各自的設定，同步也不會在這裡把它重新開啟。點一下在本機開啟。" },
  ja: { off: "このコンピュータだけでオフにする：ほかのコンピュータはそのまま", on: "このコンピュータでオンにする", tag: "このコンピュータでオフ",
    tip: "このコンピュータのエージェントには渡されません。ほかのコンピュータはそれぞれの設定のままで、同期してもここでオンに戻りません。クリックするとここでオンにします。" },
  de: { off: "Nur auf diesem Computer ausschalten: deine anderen Computer behalten ihn", on: "Auf diesem Computer einschalten", tag: "Auf diesem Computer aus",
    tip: "Kein Agent auf diesem Computer bekommt ihn. Deine anderen Computer behalten ihre Wahl, und ein Sync schaltet ihn hier nicht wieder ein. Klicken, um ihn hier einzuschalten." },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a skill is turned off on this computer alone, and on again", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      await browser.close();
    });
    for (const width of [860, 400]) for (const lang of Object.keys(words)) {
      await t.test(`${lang} at ${width}px`, async () => {
        const w = words[lang], posts = [], st = { off: false };
        const ctx = await browser.newContext({ viewport: { width, height: 900 } });
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "skills"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, posts, st));
        await page.goto("http://uni-switch.test/");
        await page.locator('button[data-view="library"]').click();
        const v = page.locator("#view-library");
        const row = (n) => v.locator(".lib-skill", { has: page.locator(".name", { hasText: new RegExp("^" + n) }) });
        await row("pdf").waitFor();

        // on: no tag, chips as they are, the button says what it does
        assert.equal(await row("pdf").locator(".lib-offhere").count(), 0);
        const btn = row("pdf").locator(".lib-here");
        assert.equal(await btn.getAttribute("title"), w.off);
        assert.equal(await btn.getAttribute("aria-pressed"), "false");
        assert.equal(await row("pdf").locator(".lib-agents").evaluate((e) => getComputedStyle(e).pointerEvents), "auto");
        const before = await scrolls(page);
        await btn.click();
        await row("pdf").locator(".lib-tag.lib-offhere").waitFor();
        assert.deepEqual(posts, [{ what: "here", body: { name: "pdf", on: false } }]);
        assert.equal(await scrolls(page), before, "turning it off scrolled the page");

        // off here: said, with why; the ticks stay but can't be changed here
        const tag = row("pdf").locator(".lib-tag.lib-offhere");
        assert.equal((await tag.textContent()).trim(), w.tag);
        assert.equal(await tag.getAttribute("title"), w.tip);
        assert.equal(await row("pdf").locator(".lib-here").getAttribute("title"), w.on);
        assert.equal(await row("pdf").locator(".lib-here").getAttribute("aria-pressed"), "true");
        assert.equal(await row("pdf").locator(".lib-agents").evaluate((e) => getComputedStyle(e).pointerEvents), "none");
        assert.equal(await row("slides").locator(".lib-offhere").count(), 0, "another skill went off too");
        // the tag sits in the row, not over its edge
        const rb = await row("pdf").boundingBox(), tb = await tag.boundingBox();
        assert(tb.x >= rb.x && tb.x + tb.width <= rb.x + rb.width + 1, `the tag overflows its row at ${width}px`);

        await tag.click();
        await row("pdf").locator(".lib-offhere").waitFor({ state: "detached" });
        assert.deepEqual(posts.at(-1), { what: "here", body: { name: "pdf", on: true } });
        assert.equal(await scrolls(page), before, "turning it on scrolled the page");
        await ctx.close();
      });
    }
    assert.deepEqual(errors, []);
  });
}

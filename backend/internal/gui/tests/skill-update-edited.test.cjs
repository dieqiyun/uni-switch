// Run with Node's test runner and Playwright on the module path; see README.md.
// #1449 (YangZhiqiang98): Library → Skills should find which skills have
// updates and update them. The page now checks by itself when the server
// says a check is due (none since uni-switch started, or not for 12 hours),
// once, and says only what there is to update. A skill changed here since it
// was fetched from GitHub carries "Changed here"; its row's Update asks in
// the page before it replaces the change (Keep mine sends nothing, Update
// sends replace), and "Update N" leaves it out. A click never scrolls. In
// English and Chinese, in Chromium and WebKit, wide and narrow. No backend:
// the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/lionel";
const REPO = "anthropics/skills";
const agent = (id, name) => ({ id, name, icon: "", skills: `${HOME}/.${id}/skills` });
const sk = (name, extra) => ({ name, description: name + " skill", kind: "github", source: `https://github.com/${REPO}/tree/HEAD/skills/${name}`, agents: ["claude"], ...extra });
const mine = (name) => ({ name, description: name + " of mine", kind: "folder", source: `${HOME}/skills/${name}`, agents: ["claude"] });
const lib = (st) => ({
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: [agent("claude", "Claude Code"), agent("codex", "Codex")],
  instructions: { agents: [] }, servers: [], foundServers: [], projects: [], foundSkills: [], newSkills: [],
  checkDue: !st.checked,
  skills: [
    sk("pdf", { edited: true, check: st.checked ? { name: "pdf", status: "update", commit: "c2", message: "pdf: forms" } : undefined }),
    sk("docx", { check: st.checked ? { name: "docx", status: "update", commit: "c2", message: "docx: tables" } : undefined }),
    sk("xlsx", { check: st.checked ? { name: "xlsx", status: "current", commit: "c1" } : undefined }),
    ...Array.from({ length: 10 }, (_, i) => mine("note-" + i)),
  ],
});

function server(lang, posts, st) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/library") return route.fulfill({ json: lib(st) });
    if (url.pathname.startsWith("/api/library/skills/")) {
      const what = url.pathname.split("/").pop();
      posts.push({ what, body: req.postDataJSON() });
      if (what === "check") st.checked = true;
      const result = what === "update-some" ? { updated: ["docx"], changed: [] } : { changed: ["claude"] };
      return route.fulfill({ json: { ...lib(st), result } });
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

// a mouse press where the button is: a dialog still coming in moves it, so
// the press waits for the dialog to stop, or it lands beside the button
const press = async (page, loc) => {
  await page.waitForFunction(() => !document.getAnimations().some((a) => a.playState === "running" && a.effect?.target?.closest?.("#modal")));
  const b = await loc.boundingBox();
  await page.mouse.click(b.x + b.width / 2, b.y + b.height / 2);
};

// every scrolled box's offset, to tell a click moved none of them
const scrolls = (page) => page.evaluate(() => [...document.querySelectorAll("*")].filter((e) => e.scrollTop).map((e) => (e.id || e.className) + ":" + e.scrollTop).concat("doc:" + document.scrollingElement.scrollTop).join(" "));

const words = {
  en: { said: "2 skills have updates", edited: "Changed here", updN: "Update 1", head: "Replace your changes to pdf?", keep: "Keep mine", update: "Update", done: "pdf is up to date; your version is in the backups", tip: "You changed it since it was fetched from GitHub. Updating asks before it replaces your changes." },
  zh: { said: "2 个技能有更新", edited: "本地已改动", updN: "更新 1 个", head: "用 GitHub 的版本替换你对 pdf 的改动？", keep: "保留我的", update: "更新", done: "pdf 已是最新，你的版本已存入备份", tip: "从 GitHub 获取后你在本地改动过它。更新前会先询问，不会直接覆盖你的改动。" },
};

const shots = process.env.ARTIFACT_DIR;

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": skills are checked as the page opens, and a skill changed here asks before an update", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const width of [860, 400]) for (const lang of ["en", "zh"]) {
      await t.test(`${lang} at ${width}px`, async () => {
        const w = words[lang], posts = [], st = { checked: false };
        const ctx = await browser.newContext({ viewport: { width, height: 1100 } });
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "skills"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, posts, st));
        await page.goto("http://uni-switch.test/");
        await page.locator('button[data-view="library"]').click();
        const v = page.locator("#view-library");
        // checked by itself, once, with no click
        await v.locator(".lib-skill .lib-tag.lib-new").first().waitFor();
        await page.waitForTimeout(300);
        assert.deepEqual(posts.map((p) => p.what), ["check"], "one check as the page opened");
        assert.equal((await page.locator("#status").textContent()).trim(), w.said);

        const pdf = v.locator(".lib-skill", { has: page.locator(".name", { hasText: /^pdf/ }) });
        const tag = pdf.locator(".lib-tag.lib-edited");
        assert.equal((await tag.textContent()).trim(), w.edited);
        assert.equal(await tag.getAttribute("title"), w.tip);
        assert.equal(await v.locator(".lib-skill .lib-tag.lib-edited").count(), 1, "only pdf was changed here");
        // Update N is for docx alone: pdf asks from its own row
        const updN = v.locator(".row-head button.lib-updall", { hasText: w.updN });
        assert.equal(await updN.count(), 1, "Update N counts pdf");
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true, "no sideways scroll");

        if (shots) await page.screenshot({ path: path.join(shots, `skill-edited-${engine}-${lang}-${width}.png`) });
        const before = await scrolls(page);
        await press(page, pdf.locator(".lib-rowacts button.lib-icon").first());
        const modal = page.locator(".lib-update-edited");
        await modal.waitFor();
        assert.equal((await modal.locator(".ehead b").textContent()).trim(), w.head);
        for (const b of await modal.locator(".bar button").all()) {
          const box = await b.boundingBox();
          assert(box && box.x >= 0 && box.x + box.width <= width, `a button out of view: ${JSON.stringify(box)}`);
        }
        await page.waitForTimeout(300);
        if (shots) await page.screenshot({ path: path.join(shots, `skill-edited-ask-${engine}-${lang}-${width}.png`) });
        assert.deepEqual(posts.map((p) => p.what), ["check"], "nothing sent before the answer");
        await press(page, modal.locator(".bar button", { hasText: w.keep }));
        await modal.waitFor({ state: "detached" });
        assert.deepEqual(posts.map((p) => p.what), ["check"], "Keep mine sends nothing");
        assert.equal(await scrolls(page), before, "asking moved the page");

        await press(page, pdf.locator(".lib-rowacts button.lib-icon").first());
        await modal.waitFor();
        await press(page, modal.locator(".bar button.primary", { hasText: w.update }));
        await modal.waitFor({ state: "detached" });
        assert.deepEqual(posts[1], { what: "update", body: { name: "pdf", replace: true } });
        assert.equal((await page.locator("#status").textContent()).trim(), w.done);

        await press(page, updN);
        await page.waitForTimeout(300);
        assert.deepEqual(posts[2], { what: "update-some", body: { names: ["docx"] } });
        await ctx.close();
      });
    }
    assert.deepEqual(errors, []);
  });
}

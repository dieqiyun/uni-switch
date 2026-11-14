// Run with Node's test runner and Playwright on the module path; see README.md.
// スライム on Discord: a repository took some of its skills out, Update all
// said each of them failed, and they could only be taken out one by one.
// A skill an update or a check found its source without carries "Removed
// upstream" (no Update available, no update icon), Update all says them
// apart from failures, and Clean up N, in the group's heading or, with one
// group only, in the bar, asks first, naming each one and saying which was
// changed here, then sends skills/remove-gone with their names. Nothing is
// sent before the answer, and Cancel sends nothing. In English and
// Chinese, in Chromium and WebKit, wide and narrow. No backend: the API is
// faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/lionel";
const REPO = "leonxlnx/taste-skill";
const agent = (id, name) => ({ id, name, icon: "", skills: `${HOME}/.${id}/skills` });
const sk = (name, extra) => ({ name, description: name + " skill", kind: "github", source: `https://github.com/${REPO}/tree/HEAD/skills/${name}`, agents: ["claude"], ...extra });
const mine = (name) => ({ name, description: name + " of mine", kind: "folder", source: `${HOME}/skills/${name}`, agents: ["claude"] });
const goneCheck = (name) => ({ name, status: "gone", error: `${REPO} has no ${name} any more: it was removed from there` });
const lib = (st) => ({
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: [agent("claude", "Claude Code"), agent("codex", "Codex")],
  instructions: { agents: [] }, servers: [], foundServers: [], projects: [], foundSkills: [], newSkills: [],
  checkDue: false,
  skills: [
    ...(st.removed ? [] : [
      sk("taste", { edited: true, gone: "2026-10-22T21:00:00Z", check: goneCheck("taste") }),
      sk("brutal", { gone: "2026-10-22T21:00:00Z", check: goneCheck("brutal") }),
    ]),
    sk("minimal", { check: { name: "minimal", status: st.updated ? "current" : "update", commit: "c2", message: "minimal: spacing" } }),
    sk("soft", { check: { name: "soft", status: "current", commit: "c1" } }),
    ...(st.single ? [] : Array.from({ length: 4 }, (_, i) => mine("note-" + i))),
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
      let result = { changed: ["claude"] };
      if (what === "update-some") {
        st.updated = true;
        result = { updated: ["minimal"], gone: ["brutal", "taste"], changed: [] };
      }
      if (what === "remove-gone") {
        st.removed = true;
        result = { removed: ["brutal", "taste"], changed: ["claude"] };
      }
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

const press = async (page, loc) => {
  await page.waitForFunction(() => !document.getAnimations().some((a) => a.playState === "running" && a.effect?.target?.closest?.("#modal")));
  const b = await loc.boundingBox();
  await page.mouse.click(b.x + b.width / 2, b.y + b.height / 2);
};

const scrolls = (page) => page.evaluate(() => [...document.querySelectorAll("*")].filter((e) => e.scrollTop).map((e) => (e.id || e.className) + ":" + e.scrollTop).concat("doc:" + document.scrollingElement.scrollTop).join(" "));

const words = {
  en: { tag: "Removed upstream", tip: `${REPO} no longer has this skill: it was removed there. It stays here until you remove it.`, clean: "Clean up 2", updN: "Update 1",
    updated: "2 skills were removed from their sources: brutal, taste · 1 up to date", head: "Remove 2 skills removed upstream?", edited: "Changed here",
    cancel: "Cancel", go: "Remove 2", done: "2 skills removed" },
  zh: { tag: "上游已删除", tip: `${REPO} 里已经没有这个技能了：它在那里被删除了。在你移除之前，它会一直留在这里。`, clean: "清理 2 个", updN: "更新 1 个",
    updated: "2 个技能已从来源中删除：brutal, taste · 1 个已是最新", head: "移除 2 个上游已删除的技能？", edited: "本地已改动",
    cancel: "取消", go: "删除 2 个", done: "已删除 2 个技能" },
};

const shots = process.env.ARTIFACT_DIR;

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": skills removed upstream are said apart and cleaned up at once, asked first", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const width of [860, 400]) for (const lang of ["en", "zh"]) for (const single of [false, true]) {
      await t.test(`${lang} at ${width}px, ${single ? "one group" : "two groups"}`, async () => {
        const w = words[lang], posts = [], st = { single };
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
        await v.locator(".lib-skill .lib-tag.lib-gone").first().waitFor();

        const row = (n) => v.locator(".lib-skill", { has: page.locator(".name", { hasText: new RegExp("^" + n) }) });
        for (const n of ["taste", "brutal"]) {
          const tag = row(n).locator(".lib-tag.lib-gone");
          assert.equal((await tag.textContent()).trim(), w.tag);
          assert.equal(await tag.getAttribute("title"), w.tip);
          assert.equal(await row(n).locator(".lib-tag.lib-new").count(), 0, n + " says Update available");
          assert.equal(await row(n).locator(".lib-rowacts button.lib-icon").count(), await row("soft").locator(".lib-rowacts button.lib-icon").count() - 1, n + " has an update icon");
        }
        assert.equal(await v.locator(".lib-skill .lib-tag.lib-gone").count(), 2);
        // Clean up: in the bar with one group, in the group's heading with more
        const clean = v.locator("button.lib-cleanup");
        assert.equal(await clean.count(), 1, "one Clean up");
        assert.equal((await clean.textContent()).trim(), w.clean);
        assert.equal(await v.locator(".row-head button.lib-cleanup").count(), single ? 1 : 0, "where Clean up is");
        // Update N leaves the gone ones out
        const updN = v.locator("button.lib-updall:not(.lib-cleanup)", { hasText: w.updN });
        assert(await updN.count() >= 1, "Update 1");
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true, "no sideways scroll");
        if (shots) await page.screenshot({ path: path.join(shots, `skill-gone-${engine}-${lang}-${width}-${single ? 1 : 2}.png`) });

        // Update N says the gone ones apart from failures
        await press(page, updN.first());
        await page.waitForFunction((s) => document.querySelector("#status")?.textContent.trim() === s, w.updated).catch(() => {});
        assert.deepEqual(posts[0], { what: "update-some", body: { names: ["minimal"] } });
        assert.equal((await page.locator("#status").textContent()).trim(), w.updated);

        const before = await scrolls(page);
        await press(page, clean);
        const modal = page.locator(".lib-remove-gone");
        await modal.waitFor();
        assert.equal((await modal.locator(".ehead b").textContent()).trim(), w.head);
        const items = modal.locator("ul.lib-gonelist li");
        assert.equal(await items.count(), 2);
        const listed = await items.evaluateAll((ls) => ls.map((l) => [l.firstChild.textContent, l.querySelector(".lib-edited")?.textContent || ""]));
        assert.deepEqual(listed.sort(), [["brutal", ""], ["taste", w.edited]]);
        for (const b of await modal.locator(".bar button").all()) {
          const box = await b.boundingBox();
          assert(box && box.x >= 0 && box.x + box.width <= width, `a button out of view: ${JSON.stringify(box)}`);
        }
        await page.waitForTimeout(300);
        if (shots) await page.screenshot({ path: path.join(shots, `skill-gone-ask-${engine}-${lang}-${width}-${single ? 1 : 2}.png`) });
        assert.equal(posts.length, 1, "nothing sent before the answer");
        await press(page, modal.locator(".bar button", { hasText: w.cancel }));
        await modal.waitFor({ state: "detached" });
        assert.equal(posts.length, 1, "Cancel sends nothing");
        assert.equal(await scrolls(page), before, "asking moved the page");

        await press(page, clean);
        await modal.waitFor();
        await press(page, modal.locator(".bar button.primary", { hasText: w.go }));
        await modal.waitFor({ state: "detached" });
        assert.equal(posts[1].what, "remove-gone");
        assert.deepEqual([...posts[1].body.names].sort(), ["brutal", "taste"]);
        await v.locator(".lib-skill .lib-tag.lib-gone").first().waitFor({ state: "detached" });
        assert.equal(await v.locator("button.lib-cleanup").count(), 0, "Clean up after cleaning up");
        await ctx.close();
      });
    }
    assert.deepEqual(errors, []);
  });
}

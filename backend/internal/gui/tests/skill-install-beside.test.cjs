// Run with Node's test runner and Playwright on the module path; see README.md.
// mora1n on Discord: anthropics/skills' skill-creator and openai/skills' are
// two skills by one name, and only the first could be added. Now the second
// goes in beside it under a name of its own, and the list of what a
// repository has says so before the click: "adds as skill-creator-openai",
// with why in its title, and one had already under such a name says that
// name. The skill stays pickable, the tag is cut short rather than pushing
// the row wider, in English and Chinese, wide and narrow. No backend: the
// API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/mora1n";
const long = "a-skill-whose-name-is-as-long-as-the-library-lets-it-be-openai";
const probe = {
  source: "openai/skills", kind: "github", candidates: [
    { path: "skills/skill-creator", name: "skill-creator", description: "Guide for creating effective skills.", have: false, as: "skill-creator-openai" },
    { path: "skills/pdf", name: "pdf", description: "PDFs", have: true, as: "pdf-openai" },
    { path: "skills/docx", name: "docx", description: "Word files", have: false },
    { path: "skills/long", name: "a-skill-whose-name-is-as-long-as-the-library-lets-it-be", description: "Long", have: false, as: long },
  ],
};
const lib = () => ({
  dir: `${HOME}/.config/uni-switch/library`, backups: `${HOME}/.config/uni-switch/backups`, home: HOME,
  agents: [{ id: "claude", name: "Claude Code", icon: "", skills: `${HOME}/.claude/skills` }],
  instructions: { agents: [] }, servers: [], skills: [], foundServers: [], projects: [], foundSkills: [],
});

function server(lang, posts) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/library") return route.fulfill({ json: lib() });
    if (url.pathname === "/api/library/skills/probe") return route.fulfill({ json: probe });
    if (url.pathname === "/api/library/skills/install") {
      posts.push(req.postDataJSON().paths);
      return route.fulfill({ json: { ...lib(), result: { changed: ["claude"], problems: [], installed: ["skill-creator-openai", "docx"], had: [], skipped: [] } } });
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

const words = {
  en: { adds: "adds as skill-creator-openai", had: "In the library as pdf-openai", why: "The library has another skill called skill-creator, from elsewhere. This one goes in beside it as skill-creator-openai." },
  zh: { adds: "将添加为 skill-creator-openai", had: "已在资源库中，名为 pdf-openai", why: "资源库里已有一个来自别处、同样叫 skill-creator 的技能。这一个会以 skill-creator-openai 为名，和它并存。" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a skill by a name the library has from elsewhere says the name it goes in by", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    const run = async (lang, width) => {
      const posts = [];
      const ctx = await browser.newContext({ viewport: { width, height: 800 } });
      await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "skills"); } catch {} });
      const page = await ctx.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(lang, posts));
      await page.goto("http://uni-switch.test/");
      await page.locator('button[data-view="library"]').click();
      const src = page.locator('#view-library input[data-lib="source"]');
      await src.fill("openai/skills");
      await src.press("Enter");
      const rows = page.locator("#view-library .lib-pick");
      await rows.first().waitFor();
      const w = words[lang];
      const creator = rows.filter({ hasText: "Guide for creating" });
      const adds = creator.locator(".lib-tag");
      assert.equal((await adds.textContent()).trim(), w.adds);
      assert.equal(await adds.getAttribute("title"), w.why);
      assert.equal(await creator.locator("input").isDisabled(), false, "it can be picked");
      const had = rows.filter({ hasText: "PDFs" });
      assert.equal((await had.locator(".lib-tag").textContent()).trim(), w.had);
      assert.equal(await had.locator("input").isDisabled(), true);
      assert.equal(await rows.filter({ hasText: "Word files" }).locator(".lib-tag").count(), 0, "a name nothing has gets no tag");
      // the longest name a skill can have is cut short inside its row,
      // never pushing the list wider
      const fit = await rows.filter({ hasText: "Long" }).evaluate((r) => {
        const tg = r.querySelector(".lib-tag"), box = r.getBoundingClientRect(), b = tg.getBoundingClientRect();
        const list = r.closest(".lib-picks");
        return { inside: b.right <= box.right + 0.5 && b.left >= box.left - 0.5, cut: tg.scrollWidth > tg.clientWidth, over: list.scrollWidth - list.clientWidth, name: r.querySelector(".name").getBoundingClientRect().width };
      });
      assert.equal(fit.inside, true, "the tag stays in its row");
      assert.equal(fit.over, 0, "the list doesn't scroll sideways");
      assert(fit.name > 60, `the skill's own name keeps room (${fit.name}px)`);
      if (width < 600) assert.equal(fit.cut, true, "at this width the long tag is cut short");
      // three to add are picked for you, the one beside another among them
      assert.equal(await creator.locator("input").isChecked(), true);
      await page.locator("#view-library .lib-probefoot button.primary").click();
      await page.waitForTimeout(300);
      assert.deepEqual(posts.map((p) => [...p].sort()), [["skills/docx", "skills/long", "skills/skill-creator"]]);
      await ctx.close();
    };

    await t.test("in English", () => run("en", 980));
    await t.test("in Chinese, narrow", () => run("zh", 440));

    assert.deepEqual(errors, []);
  });
}

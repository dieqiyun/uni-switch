// Run with Node's test runner and Playwright on the module path; see README.md.
// #1862 (WardLu): a group's chips and its rows' say the same thing of a
// skill kept in ~/.agents/skills, which an agent (Codex here) reads itself
// whatever is ticked. The group's chip counts, gives and takes only the
// skills uni-switch decides for that agent: turned off, it isn't sent the one
// Codex reads itself, and says Codex still has it. The row's chip for that
// one is lit with a lock, not greyed like a switch. A group whose skills
// Codex reads every one of itself has a locked chip too, which sends
// nothing. The heading says how many are in ~/.agents/skills, before any
// click. Five languages, narrow and wide; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/me";
const agent = (id, name, icon) => ({ id, name, icon, skills: `${HOME}/.${id}/skills`, mcp: `${HOME}/.${id}/mcp.json` });
const gh = (repo, name, agents, always) => ({ name, kind: "github", source: `https://github.com/${repo}/tree/main/skills/${name}`, description: "d", agents, ...(always ? { always } : {}) });

function server(lang, state, posts) {
  return async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/library") return json(state.lib);
    if (url.pathname === "/api/library/skills/agents-some") {
      const b = req.postDataJSON();
      posts.push(b);
      for (const s of state.lib.skills) {
        if (!b.names.includes(s.name)) continue;
        const kept = s.agents.filter((a) => !b.agents.includes(a));
        s.agents = (b.on ? [...kept, ...b.agents] : kept).sort();
      }
      return json({ ...state.lib, result: { changed: b.agents } });
    }
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname.startsWith("/api/")) return json({});
    if (url.host !== "uni-switch.test") return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

// what the page says, as i18n.js has it in each language
const L = {
  en: { note: "1 in ~/.agents/skills", still: "Codex still reads 1 of them from ~/.agents/skills itself", part: "It reads 1 of them from ~/.agents/skills itself", whole: "Codex reads ~/.agents/skills itself, where all of these are kept" },
  zh: { note: "1 个在 ~/.agents/skills", still: "Codex 仍会自行从 ~/.agents/skills 读取其中 1 个", part: "其中 1 个由它自行从 ~/.agents/skills 读取", whole: "Codex 会自行读取这些技能所在的 ~/.agents/skills" },
  "zh-TW": { note: "1 個在 ~/.agents/skills", still: "Codex 仍會自行從 ~/.agents/skills 讀取其中 1 個", part: "其中 1 個由它自行從 ~/.agents/skills 讀取", whole: "Codex 會自行讀取這些技能所在的 ~/.agents/skills" },
  ja: { note: "1 個が ~/.agents/skills に", still: "Codex はそのうち 1 個を ~/.agents/skills から自分で読み続けます", part: "そのうち 1 個は ~/.agents/skills から自分で読みます", whole: "Codex はこれらがすべて置かれている ~/.agents/skills を自分で読みます" },
  de: { note: "1 in ~/.agents/skills", still: "Codex liest 1 davon weiterhin selbst aus ~/.agents/skills", part: "1 davon liest es selbst aus ~/.agents/skills", whole: "Codex liest ~/.agents/skills selbst, wo all diese liegen" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a group's chips count only what uni-switch decides", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    t.after(() => browser.close());
    for (const lang of Object.keys(L)) for (const width of [640, 1180]) {
      await t.test(lang + " " + width, async () => {
        const w = L[lang];
        const state = {
          lib: {
            dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
            agents: [agent("claude", "Claude Code", "claudecode-color"), agent("codex", "Codex", "openai")],
            instructions: { agents: [], sets: [] }, servers: [], foundServers: [], projects: [], foundSkills: [], problems: [],
            skills: [
              // mango is kept in ~/.agents/skills: Codex has it whatever is ticked
              gh("acme/kit", "mango", ["claude"], ["codex"]), gh("acme/kit", "cherry", ["claude", "codex"]),
              gh("solo/one", "pear", ["claude"], ["codex"]), gh("solo/one", "plum", [], ["codex"]),
            ],
          },
        };
        const posts = [];
        const ctx = await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" });
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "skills"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, state, posts));
        await page.goto("http://uni-switch.test/");
        await page.locator('button[data-view="library"]').click();
        const headOf = (repo) => page.locator("#view-library .lib-grouphead").filter({ has: page.locator(".name", { hasText: new RegExp("^" + repo.replace("/", "\\/") + "$") }) });
        const head = headOf("acme/kit");
        await head.waitFor();
        const chip = (h, id) => h.locator(`.lib-groupchips .lib-ag[data-agent="${id}"]`);
        const rowChip = (name, id) => page.locator("#view-library .lib-row.lib-skill").filter({ has: page.locator(".name", { hasText: new RegExp("^" + name + "$") }) }).locator(`.lib-agents .lib-ag[data-agent="${id}"]`);

        // the heading says it before any click
        assert.match(await head.locator(".who .sub").textContent(), new RegExp(w.note.replace(/[.*+?^${}()|[\]\\/]/g, "\\$&")));
        // Codex has the one uni-switch decides (cherry): lit, and its tip says
        // it reads the other itself
        assert.equal(await chip(head, "codex").getAttribute("aria-pressed"), "true");
        assert.equal(await chip(head, "codex").evaluate((c) => c.classList.contains("some")), false);
        assert.ok((await chip(head, "codex").getAttribute("title")).includes(w.part));
        // the row's chip for mango is lit and locked, with its lock showing
        const lock = rowChip("mango", "codex");
        assert.equal(await lock.getAttribute("aria-pressed"), "true");
        assert.equal(await lock.getAttribute("aria-disabled"), "true");
        const look = await lock.evaluate((c) => ({ op: getComputedStyle(c).opacity, badge: getComputedStyle(c, "::after").content }));
        assert.equal(look.op, "1", "a chip that has it isn't greyed out like one that can't");
        assert.notEqual(look.badge, "none");
        assert.equal(await rowChip("cherry", "codex").evaluate((c) => getComputedStyle(c, "::after").content), "none", "a switch has no lock");

        // solo/one: Codex reads both itself, so its chip there is locked
        const solo = headOf("solo/one");
        const c = chip(solo, "codex");
        assert.equal(await c.getAttribute("aria-pressed"), "true");
        assert.equal(await c.getAttribute("aria-disabled"), "true");
        assert.ok((await c.getAttribute("title")).includes(w.whole));
        // scrolled to as a user does, with the wheel: the page puts back a
        // scroll no one asked for
        await page.mouse.move(width / 2, 300);
        await page.mouse.wheel(0, 400);
        await page.waitForTimeout(300);
        await c.hover({ force: true }); // the chips fan out under the pointer first, as they do for a user
        await page.waitForTimeout(300);
        const hit = await c.evaluate((x) => { const r = x.getBoundingClientRect(); const e = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2); return e === x || x.contains(e) ? "" : (e?.outerHTML || "").slice(0, 200); });
        assert.equal(hit, "", "the locked chip is under another");
        await c.click({ force: true }); // aria-disabled: Playwright waits for it to be enabled
        await page.locator("#status").filter({ hasText: w.whole }).waitFor();
        await page.waitForTimeout(150);
        assert.deepEqual(posts, [], "a locked chip sends nothing");
        await page.mouse.move(width / 2, 300);
        await page.mouse.wheel(0, -400);
        await page.waitForTimeout(300);

        // turned off for Codex: only cherry is sent, and it says mango stays
        await chip(head, "codex").click();
        await page.locator("#status").filter({ hasText: w.still }).waitFor();
        assert.deepEqual(posts.shift(), { names: ["cherry"], agents: ["codex"], on: false });
        assert.deepEqual(state.lib.skills.find((s) => s.name === "mango").agents, ["claude"], "mango is left as it was");
        await page.waitForFunction(() => document.querySelector('#view-library .lib-grouphead .lib-groupchips .lib-ag[data-agent="codex"]')?.getAttribute("aria-pressed") === "false");
        // parent and rows agree: the only lit Codex chip under it is locked
        assert.equal(await rowChip("cherry", "codex").getAttribute("aria-pressed"), "false");
        assert.equal(await rowChip("mango", "codex").getAttribute("aria-pressed"), "true");
        assert.equal(await rowChip("mango", "codex").getAttribute("aria-disabled"), "true");

        // All gives it back, again of cherry alone
        await head.locator(".lib-groupchips .lib-ag.all").click();
        await page.waitForFunction(() => document.querySelector('#view-library .lib-grouphead .lib-groupchips .lib-ag[data-agent="codex"]')?.getAttribute("aria-pressed") === "true");
        assert.deepEqual(posts.shift(), { names: ["cherry"], agents: ["codex"], on: true });

        // nothing wider than the window
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "the page scrolls sideways");
        assert.deepEqual(errors, []);
        await ctx.close();
      });
    }
  });
}

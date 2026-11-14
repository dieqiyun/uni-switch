// Run with Node's test runner and Playwright on the module path; see README.md.
// Hipple (#1624): on Windows an instruction set made or removed took about
// a minute, with nothing on the page to say so and its buttons still
// pressable, so a second click made a second set or removed twice. While a
// change is on its way the page's buttons can't be pressed, the one pressed
// says it is saving, and a double click sends one change. The answer to an
// instruction change is the instructions alone ({part: "instructions"}),
// and the page keeps the rest of the library it has. Chromium and WebKit,
// every language; no backend, the API is faked here and holds each change
// until the test lets it go.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/hipple";
const sets = (extra) => [{ id: "default", name: "", active: true, text: "Be brief." }, ...extra];
const instructions = (extra) => ({
  shared: "Be brief.",
  sets: sets(extra),
  agents: [{ agent: "claude", name: "Claude Code", icon: "", path: `${HOME}/.claude/CLAUDE.md`, on: true, extra: "" }],
});
const library = {
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: [{ id: "claude", name: "Claude Code", icon: "", instructions: `${HOME}/.claude/CLAUDE.md` }],
  instructions: instructions([{ id: "work", name: "Work", active: false, text: "Company rules." }]),
  foundServers: [], projects: [], foundSkills: [], skills: [], servers: [],
};

function server(lang, sent, held) {
  return async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/library") return route.fulfill({ json: library });
    if (url.pathname.startsWith("/api/library/")) {
      const body = route.request().postDataJSON();
      sent.push({ path: url.pathname, body });
      await new Promise((go) => held.push(go));
      const extra = body.create ? [{ id: "work", name: "Work", active: false, text: "Company rules." }, { id: body.create.id, name: body.create.name, active: false, text: "" }] : [];
      return route.fulfill({ json: { part: "instructions", instructions: instructions(extra), result: { changed: ["claude"], problems: [] } } });
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

const view = "#view-library";
const saying = { en: "Saving…", zh: "保存中…", "zh-TW": "儲存中…", ja: "保存中…", de: "Wird gespeichert…" };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": an instruction change on its way holds the page's buttons, and a double click sends one", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const lang of Object.keys(saying)) {
      await t.test(lang, async () => {
        const ctx = await browser.newContext({ viewport: { width: 980, height: 860 }, reducedMotion: "reduce" });
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "instructions"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        const sent = [], held = [];
        await page.route("**/*", server(lang, sent, held));
        await page.goto("http://uni-switch.test/");
        await page.locator('button[data-view="library"]').click();
        await page.locator(`${view} .lib-setadd button`).click();
        await page.locator(`${view} input[data-lib="newset"]`).fill("Home");
        const add = page.locator(`${view} .lib-set button.primary`);

        // made: two clicks, one set
        await add.dblclick();
        await page.waitForFunction(() => document.querySelector("#view-library").getAttribute("aria-busy") === "true");
        await page.waitForTimeout(200);
        assert.equal(sent.length, 1, "a double click sent " + sent.length + " changes");
        assert.equal((await add.textContent()).trim(), saying[lang], "the button pressed says it is saving");
        const free = await page.locator(`${view} button:not([disabled])`).count();
        assert.equal(free, 0, "a button can be pressed while the change is on its way");
        assert.equal((await page.locator("#status").textContent()).trim(), saying[lang]);
        held.shift()();
        await page.waitForFunction(() => !document.querySelector("#view-library").hasAttribute("aria-busy"));
        // the answer was the instructions alone: the rest of the page stays
        await page.locator(`${view} textarea[data-lib="set:${sent[0].body.create.id}"]`).waitFor();
        assert.equal(await page.locator(`${view} .lib-set`).count(), 3);
        assert(await page.locator(`${view} button:not([disabled])`).count() > 0, "the buttons came back");
        assert.notEqual((await page.locator("#status").textContent()).trim(), saying[lang]);

        // removed: the second, sure click pressed twice removes once
        await page.locator(`${view} .lib-detail .lib-acts button.danger`).click();
        const sure = page.locator(`${view} .lib-detail .lib-acts button.danger-fill`);
        await sure.dblclick();
        await page.waitForTimeout(200);
        assert.equal(sent.length, 2, "removing sent " + (sent.length - 1) + " changes");
        assert.equal(sent[1].body.remove, sent[0].body.create.id);
        held.shift()();
        await page.waitForFunction(() => !document.querySelector("#view-library").hasAttribute("aria-busy"));
        assert.equal(sent.length, 2);
        await ctx.close();
      });
    }
    assert.deepEqual(errors, []);
  });
}

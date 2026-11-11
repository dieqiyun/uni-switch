// Run with Node's test runner and Playwright on the module path; see README.md.
// 新v学员 on Discord: one ~/.agents/AGENTS.md kept by hand, shared by every
// agent. A new set of instructions can be kept in a file of the user's own
// (Keep in a file…, ~/.agents/AGENTS.md filled in), which sends the file
// with the set; a set kept so says where, is tagged Your file, warns that
// Droid reads that file itself too, and its Remove says the file stays.
// Chromium and WebKit, at 1100 and 420, in every language; no backend, the
// API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/xv";
const FILE = `${HOME}/.agents/AGENTS.md`;
const library = () => ({
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: [
    { id: "claude", name: "Claude Code", icon: "", instructions: `${HOME}/.claude/CLAUDE.md` },
    { id: "droid", name: "Droid", icon: "", instructions: `${HOME}/.factory/AGENTS.md` },
  ],
  instructions: {
    shared: "Mine.",
    sets: [{ id: "default", name: "", active: true, text: "Mine." }, { id: "agents", name: "Everyone", active: false, text: "# Shared\nRules.", file: FILE }],
    agents: [
      { agent: "claude", name: "Claude Code", icon: "", path: `${HOME}/.claude/CLAUDE.md`, on: true, extra: "" },
      { agent: "droid", name: "Droid", icon: "", path: `${HOME}/.factory/AGENTS.md`, on: true, extra: "" },
    ],
  },
  foundServers: [], projects: [], foundSkills: [], skills: [], servers: [],
});

function server(lang, lib, sent) {
  return async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/library") return route.fulfill({ json: lib });
    if (url.pathname.startsWith("/api/library/")) {
      sent.push({ path: url.pathname, body: route.request().postDataJSON() });
      return route.fulfill({ json: { ...lib, result: { changed: [] } } });
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

const KEYS = [
  "Your file",
  "Kept in {path}, not in the library: edits there reach the agents, and Save writes there",
  "Kept in",
  "{agents} read this file themselves too: switch them off below so they don't read it twice.",
  "{agents} reads this file itself too: switch it off below so it doesn't read it twice.",
  "Remove {name} — the file stays",
  "Keep in a file…",
  "Full path of a Markdown file",
];

const view = "#view-library";

// nothing of the page is wider than the window, or cut off at its right
async function fits(page, sel, what) {
  const r = await page.evaluate((s) => {
    const out = [];
    for (const e of document.querySelectorAll(s)) {
      const b = e.getBoundingClientRect();
      if (b.width && b.right > innerWidth + 0.5) out.push(`${e.className} ${Math.round(b.right)} > ${innerWidth}`);
    }
    return out;
  }, sel);
  assert.deepEqual(r, [], what + " runs past the window");
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a set of instructions kept in a file of the user's own", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const width of [1100, 420]) {
      for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
        await t.test(`${width} ${lang}`, async () => {
          const ctx = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
          await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "instructions"); } catch {} });
          const page = await ctx.newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          page.on("pageerror", (e) => errors.push(e.message));
          const sent = [];
          await page.route("**/*", server(lang, library(), sent));
          await page.goto("http://uni-switch.test/");
          await page.locator('button[data-view="library"]').click();
          await page.locator(`${view} .lib-set`).nth(1).waitFor();
          const missing = await page.evaluate(([keys, lang]) => lang === "en" ? [] : keys.filter((k) => !I18N[lang]?.[k]), [KEYS, lang]);
          assert.deepEqual(missing, [], `every string has its ${lang}`);
          const tr = (k, vars) => page.evaluate(([k, vars]) => t(k, vars), [k, vars]);

          // the linked set is tagged, and opened says where it is kept
          const row = page.locator(`${view} .lib-set`).nth(1);
          assert((await row.locator(".lib-tags").textContent()).includes(await tr("Your file")), "no Your file tag");
          await row.click();
          const where = page.locator(`${view} .lib-setfile`);
          await where.waitFor();
          assert.equal((await where.textContent()).trim(), (await tr("Kept in")) + " ~/.agents/AGENTS.md");
          assert.equal(await page.locator(`${view} textarea[data-lib="set:agents"]`).inputValue(), "# Shared\nRules.");
          // Droid, on, reads ~/.agents/AGENTS.md itself: it is named
          const twice = page.locator(`${view} .lib-twice`);
          assert.equal((await twice.textContent()).trim(), await tr("{agents} reads this file itself too: switch it off below so it doesn't read it twice.", { agents: "Droid" }));
          // its Remove lets go of the file, and says so
          await page.locator(`${view} .lib-detail button.danger`).click();
          const sure = page.locator(`${view} .lib-detail button.danger-fill`);
          assert.equal((await sure.textContent()).trim(), await tr("Remove {name} — the file stays", { name: "Everyone" }));
          await fits(page, `${view} .lib-detail, ${view} .lib-detail *`, "the linked set");
          await page.locator(`${view} .lib-set`).nth(1).click(); // closed

          // a new set kept in a file
          await page.locator(`${view} .lib-setadd button`).click();
          const link = page.locator(`${view} .lib-setlink`);
          assert.equal((await link.textContent()).trim(), await tr("Keep in a file…"));
          await link.click();
          const file = page.locator(`${view} input[data-lib="newsetfile"]`);
          assert.equal(await file.inputValue(), "~/.agents/AGENTS.md");
          assert.equal(await page.locator(`${view} input[data-lib="newset"]`).inputValue(), "AGENTS.md");
          await fits(page, `${view} .lib-setnew, ${view} .lib-setnew *`, "the new set's row");
          await file.fill("~/notes/AGENTS.md");
          await file.press("Enter");
          await page.waitForFunction(() => true);
          await page.waitForTimeout(300);
          const s = sent.find((x) => x.path === "/api/library/instructions/save" && x.body.create);
          assert(s, "nothing sent");
          assert.equal(s.body.create.name, "AGENTS.md");
          assert.equal(s.body.create.file, "~/notes/AGENTS.md");
          assert.match(s.body.create.id, /^s/);
          await ctx.close();
        });
      }
    }
    assert.deepEqual(errors, []);
  });
}

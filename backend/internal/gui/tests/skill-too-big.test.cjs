// Run with Node's test runner and Playwright on the module path; see README.md.
// #2062 (mikubaka88/CCFA-Skills): one skill in the repository, ccf-paper-writer,
// is 297 MB of exemplar PDFs, over the 200 MB a skill may be, and the whole
// repository refused to install. Now the probe lists the other skills and names
// the one left out, with its size, under the list; when every skill is too big
// it names them in place of "No skills there". In every language, at a narrow
// width, in both engines; nothing runs past the page's width. No backend: the
// API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/summit";
const cand = (name) => ({ path: "skills/" + name, name, description: name + " for papers", have: false });
const big = { path: "skills/ccf-paper-writer", size: 296736735 };
const probes = {
  some: { source: "mikubaka88/CCFA-Skills", kind: "github", candidates: [cand("ccf-reviewer"), cand("ccf-rebuttal")], left: [big] },
  none: { source: "mikubaka88/CCFA-Skills", kind: "github", candidates: [], left: [big] },
};
const lib = () => ({
  dir: `${HOME}/.config/uni-switch/library`, backups: `${HOME}/.config/uni-switch/backups`, home: HOME,
  agents: [{ id: "claude", name: "Claude Code", icon: "", skills: `${HOME}/.claude/skills` }], instructions: { agents: [] }, servers: [], skills: [], foundServers: [], projects: [], foundSkills: [],
});

function server(lang, probe) {
  return async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/library") return route.fulfill({ json: lib() });
    if (url.pathname === "/api/library/skills/probe") return route.fulfill({ json: probe });
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

const notes = {
  en: "ccf-paper-writer is too big to install: 283 MB, over the 200 MB a skill may be",
  zh: "ccf-paper-writer 太大，无法安装：283 MB，超过单个技能 200 MB 的上限",
  "zh-TW": "ccf-paper-writer 太大，無法安裝：283 MB，超過單個技能 200 MB 的上限",
  ja: "ccf-paper-writer は大きすぎてインストールできません：283 MB（1 つのスキルの上限 200 MB を超えています）",
  de: "ccf-paper-writer ist zu groß zum Installieren: 283 MB, über den 200 MB, die ein Skill haben darf",
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a skill too big to install is named and the rest are listed", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    const probe = async (lang, width, which) => {
      const ctx = await browser.newContext({ viewport: { width, height: 800 } });
      await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "skills"); } catch {} });
      const page = await ctx.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(lang, probes[which]));
      await page.goto("http://uni-switch.test/");
      await page.locator('button[data-view="library"]').click();
      const src = page.locator('#view-library input[data-lib="source"]');
      await src.fill("mikubaka88/CCFA-Skills");
      await src.press("Enter");
      const note = page.locator("#view-library .lib-probe .lib-left");
      await note.waitFor();
      const got = {
        notes: (await note.allTextContents()).map((s) => s.trim()),
        none: await page.locator("#view-library .lib-probe .lib-none:not(.lib-left)").count(),
        rows: await page.locator("#view-library .lib-probefoot button.primary").count(),
        overflow: await note.evaluate((e) => e.getBoundingClientRect().right > document.documentElement.clientWidth || e.scrollWidth > e.clientWidth + 1),
      };
      await ctx.close();
      return got;
    };

    for (const [lang, width] of [["en", 980], ["zh", 440], ["zh-TW", 440], ["ja", 440], ["de", 420]]) {
      await t.test(`${lang} at ${width}, beside other skills`, async () => {
        assert.deepEqual(await probe(lang, width, "some"), { notes: [notes[lang]], none: 0, rows: 1, overflow: false });
      });
    }
    await t.test("en, every skill too big: named in place of No skills there", async () => {
      assert.deepEqual(await probe("en", 440, "none"), { notes: [notes.en], none: 0, rows: 0, overflow: false });
    });

    assert.deepEqual(errors, []);
  });
}

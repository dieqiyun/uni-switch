// Run with Node's test runner and Playwright on the module path; see README.md.
// Codex sessions made with another provider (CC Switch's "custom") are
// hidden from Codex's history once Codex uses uni-switch (#887). The Sessions
// page's Codex tab says how many and which provider, tags each one, and a
// Move button opens uni-switch's own dialog listing them, each ticked: Cancel
// sends nothing, an unticked one isn't moved, and Move posts
// sessions/codex-provider with each id and Codex's provider. Undo then posts
// each back to the provider it had. No click moves the page, nothing has a
// left border. In English and Chinese, Chromium and WebKit; no backend, the
// API is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const at = (min) => new Date(Date.now() - min * 60e3).toISOString();
const sess = (id, title, provider, min) => ({
  agent: "codex", id, title, cwd: "/work/app", start: at(min + 30), last: at(min), models: [], cost: 0, unpriced: 0,
  resume: "codex resume " + id, path: "~/.codex/sessions/x/" + id + ".jsonl", size: 2048, messages: 4, files: 1, deletable: true,
  provider, uses_provider: "uni-switch",
});

function serve(lang, calls) {
  const store = { sessions: [sess("a1", "fix the login form", "custom", 5), sess("b2", "write the post", "custom", 10), sess("c3", "new work", "uni-switch", 1)] };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/sessions/manage") {
      return json({
        agents: [{ agent: "codex", count: store.sessions.length, deletable: true, name: "Codex", icon: "codex-color" }],
        agent: "codex", sessions: store.sessions, terminal: false, trash: [], trashDir: "~/Library/Application Support/uni-switch/trash/sessions",
      });
    }
    if (url.pathname === "/api/sessions/codex-provider") {
      const body = route.request().postDataJSON();
      calls.push(body);
      const moved = body.moves.map((m) => {
        const s = store.sessions.find((x) => x.id === m.id);
        const from = s.provider;
        s.provider = m.to;
        return { id: m.id, from, to: m.to, files: [s.path], backup: "~/Library/Application Support/uni-switch/trash/codex-provider/x" };
      });
      return json({ moved, refused: [] });
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: {
    nav: "Sessions", cancel: "Cancel", move: "Move", undo: "Undo",
    note: "2 Codex sessions were made with another provider (custom): Codex lists only uni-switch's sessions in its history now, so they don't show there.",
    button: "Move to uni-switch…", ask: "Move sessions to uni-switch?", moved: "Moved 1 session to uni-switch", back: "Moved 1 session back",
  },
  zh: {
    nav: "会话", cancel: "取消", move: "迁移", undo: "撤销",
    note: "有 2 个 Codex 会话是用其他供应商（custom）创建的。Codex 现在的历史列表只显示 uni-switch 的会话，所以看不到它们。",
    button: "迁移到 uni-switch…", ask: "把会话迁移到 uni-switch？", moved: "已把 1 个会话迁移到 uni-switch", back: "已把 1 个会话迁移回去",
  },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    const w = words[lang];
    test(`${engine} ${lang}: Codex sessions of another provider are moved to Codex's, after uni-switch's own dialog, and back`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 900, height: 560 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      page.on("dialog", (d) => { errors.push("a browser dialog: " + d.message()); d.dismiss(); });
      const calls = [];
      await page.route("**/*", serve(lang, calls));
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator("#nav").getByRole("button", { name: w.nav, exact: true }).click();
      const view = page.locator("#view-sessions");
      const bar = view.locator(".sm-provider");
      await bar.waitFor();
      assert.equal((await bar.locator(".sm-provider-text").textContent()).trim(), w.note);
      await view.locator(".row.sm-sess").first().waitFor();
      // the two of another provider are tagged with it, the other isn't
      assert.deepEqual(await view.locator(".row.sm-sess .sm-prov").allTextContents(), ["custom", "custom"]);
      const said = (text) => page.waitForFunction((x) => document.querySelector("#status")?.textContent === x, text)
        .catch(async () => assert.equal(await page.locator("#status").textContent(), text));
      const top = (l) => l.evaluate((e) => e.getBoundingClientRect().top);

      const go = bar.getByRole("button", { name: w.button, exact: true });
      const before = await top(bar);
      await go.click();
      const ask = page.locator("#modal .sm-provider-ask");
      await ask.waitFor();
      assert.equal((await ask.locator(".ehead b").textContent()).trim(), w.ask);
      assert.deepEqual(await ask.locator(".sm-provider-list .name").allTextContents(), ["fix the login form", "write the post"]);
      const border = await page.evaluate(() => [...document.querySelectorAll("#view-sessions, #view-sessions *, #modal .sm-ask, #modal .sm-ask *")]
        .filter((e) => parseFloat(getComputedStyle(e).borderLeftWidth) > 1 && getComputedStyle(e).borderLeftColor !== getComputedStyle(e).borderRightColor).map((e) => e.className));
      assert.deepEqual(border, [], "no left-border accent");
      await ask.getByRole("button", { name: w.cancel, exact: true }).click();
      await ask.waitFor({ state: "detached" });
      assert.equal(calls.length, 0, "Cancel moves nothing");
      assert.equal(await top(bar), before, "the click moved the page");

      // one unticked stays; Move posts the other, to Codex's provider
      await go.click();
      await ask.waitFor();
      await ask.locator(".sm-provider-list li", { hasText: "write the post" }).locator("input").uncheck();
      await ask.getByRole("button", { name: w.move, exact: true }).click();
      await ask.waitFor({ state: "detached" });
      assert.deepEqual(calls[0], { moves: [{ id: "a1", to: "uni-switch" }] });
      await said(w.moved);
      await page.waitForFunction(() => document.querySelectorAll("#view-sessions .row.sm-sess .sm-prov").length === 1);

      // Undo moves it back to the provider it had
      const undo = view.locator(".sm-provider-undo");
      await undo.click();
      await said(w.back);
      assert.deepEqual(calls[1], { moves: [{ id: "a1", to: "custom" }] });
      await page.waitForFunction(() => document.querySelectorAll("#view-sessions .row.sm-sess .sm-prov").length === 2);
      assert.equal(await view.locator(".sm-provider-undo").count(), 0, "nothing left to undo");

      const missing = await page.evaluate(() => [
        "1 Codex session was made with another provider ({from}): Codex lists only {to}'s sessions in its history now, so it doesn't show there.",
        "{n} Codex sessions were made with another provider ({from}): Codex lists only {to}'s sessions in its history now, so they don't show there.",
        "Move to {to}…", "Moved 1 session to {to}", "Moved {n} sessions to {to}", "Moved 1 session back", "Moved {n} sessions back",
        "Move sessions to {to}?", "Made with {from}: Codex's history lists only {to}'s sessions now",
        "Only the provider each one names is changed, in its files and in Codex's database: its messages, id, title and archive stay as they are. A copy of its files is kept in uni-switch's trash folder first, and Undo moves them back. A session written to in the last minute is left alone.",
      ].filter((k) => !I18N.zh[k] || !I18N.ja[k] || !I18N.de[k]));
      assert.deepEqual(missing, [], "every string has its translations");
      assert.deepEqual(errors, []);
    });
  }
}

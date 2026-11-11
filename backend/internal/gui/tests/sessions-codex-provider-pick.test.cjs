// Run with Node's test runner and Playwright on the module path; see README.md.
// The Move sessions to {to}? dialog (#887) lists every Codex session made with
// another provider, each ticked. Over a list of more than one, a quiet row
// says how many are picked, with Select all (Deselect all once all are) and
// Invert selection (Cyrus on Discord). Each press re-ticks the boxes, the
// count and Move follow (none picked: Move is off), and Move posts the ones
// picked. At a narrow width the row stays inside the dialog. In every
// language, Chromium and WebKit; no backend, the API is faked.
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
  const sessions = [sess("a1", "fix the login form", "custom", 5), sess("b2", "write the post", "custom", 10), sess("c3", "plan the trip", "custom", 15), sess("d4", "new work", "uni-switch", 1)];
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
        agents: [{ agent: "codex", count: sessions.length, deletable: true, name: "Codex", icon: "codex-color" }],
        agent: "codex", sessions, terminal: false, trash: [], trashDir: "~/Library/Application Support/uni-switch/trash/sessions",
      });
    }
    if (url.pathname === "/api/sessions/codex-provider") {
      const body = route.request().postDataJSON();
      calls.push(body);
      return json({ moved: body.moves.map((m) => ({ id: m.id, from: "custom", to: m.to, files: [], backup: "" })), refused: [] });
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { nav: "Sessions", button: "Move to uni-switch…", move: "Move", all: "Select all", none: "Deselect all", invert: "Invert selection", n: (n) => `${n} selected` },
  zh: { nav: "会话", button: "迁移到 uni-switch…", move: "迁移", all: "全选", none: "取消全选", invert: "反选", n: (n) => `已选 ${n} 个` },
  "zh-TW": { nav: "會話", button: "遷移到 uni-switch…", move: "遷移", all: "全選", none: "取消全選", invert: "反選", n: (n) => `已選 ${n} 個` },
  ja: { nav: "セッション", button: "uni-switch に移行…", move: "移行", all: "すべて選択", none: "すべての選択を解除", invert: "選択を反転", n: (n) => `${n} 件選択中` },
  de: { nav: "Sitzungen", button: "Zu uni-switch umziehen…", move: "Umziehen", all: "Alle auswählen", none: "Alle abwählen", invert: "Auswahl umkehren", n: (n) => `${n} ausgewählt` },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(words)) {
    const w = words[lang];
    test(`${engine} ${lang}: the move dialog picks all, deselects all and inverts, and Move follows`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 900, height: 640 }, reducedMotion: "reduce" })).newPage();
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
      await view.locator(".sm-provider").waitFor();
      await page.setViewportSize({ width: 420, height: 640 });
      await view.locator(".sm-provider").getByRole("button", { name: w.button, exact: true }).click();
      const ask = page.locator("#modal .sm-provider-ask");
      await ask.waitFor();
      const pick = ask.locator(".sm-provider-pick");
      const move = ask.getByRole("button", { name: w.move, exact: true });
      const state = () => ask.evaluate((e) => ({
        ticked: [...e.querySelectorAll(".sm-provider-list li")].filter((li) => li.querySelector("input").checked).map((li) => li.querySelector(".name").textContent),
        count: e.querySelector(".sm-provider-pick .sm-count").textContent,
        all: e.querySelector(".sm-provider-pick .sm-pick-all").textContent,
        off: e.querySelector(".bar .primary").disabled,
      }));
      const every = ["fix the login form", "write the post", "plan the trip"];

      // the row is above the list, inside the dialog at 420px
      const box = await pick.evaluate((e) => {
        const r = e.getBoundingClientRect(), d = e.closest(".sm-ask").getBoundingClientRect(), l = e.parentElement.querySelector(".sm-provider-list").getBoundingClientRect();
        return { inside: r.left >= d.left - 0.5 && r.right <= d.right + 0.5, above: r.bottom <= l.top + 0.5, fits: e.scrollWidth <= e.clientWidth + 1 };
      });
      assert.deepEqual(box, { inside: true, above: true, fits: true });
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, "no sideways scroll");

      // every one ticked to start: Deselect all
      assert.deepEqual(await state(), { ticked: every, count: w.n(3), all: w.none, off: false });
      await pick.getByRole("button", { name: w.none, exact: true }).click();
      assert.deepEqual(await state(), { ticked: [], count: w.n(0), all: w.all, off: true });
      await pick.getByRole("button", { name: w.all, exact: true }).click();
      assert.deepEqual(await state(), { ticked: every, count: w.n(3), all: w.none, off: false });

      // one unticked by hand, then inverted: only it is picked
      await ask.locator(".sm-provider-list li", { hasText: "write the post" }).locator("input").uncheck();
      assert.deepEqual(await state(), { ticked: ["fix the login form", "plan the trip"], count: w.n(2), all: w.all, off: false });
      await pick.getByRole("button", { name: w.invert, exact: true }).click();
      assert.deepEqual(await state(), { ticked: ["write the post"], count: w.n(1), all: w.all, off: false });
      // inverting all gives none, and Move is off
      await pick.getByRole("button", { name: w.all, exact: true }).click();
      await pick.getByRole("button", { name: w.invert, exact: true }).click();
      assert.deepEqual(await state(), { ticked: [], count: w.n(0), all: w.all, off: true });
      await pick.getByRole("button", { name: w.invert, exact: true }).click();
      await ask.locator(".sm-provider-list li", { hasText: "plan the trip" }).locator("input").uncheck();
      await ask.locator(".sm-provider-list li", { hasText: "fix the login form" }).locator("input").uncheck();
      await pick.getByRole("button", { name: w.invert, exact: true }).click();
      assert.deepEqual(await state(), { ticked: ["fix the login form", "plan the trip"], count: w.n(2), all: w.all, off: false });

      // Move posts what is picked
      assert.equal(calls.length, 0, "picking moves nothing");
      await move.click();
      await ask.waitFor({ state: "detached" });
      assert.deepEqual(calls, [{ moves: [{ id: "a1", to: "uni-switch" }, { id: "c3", to: "uni-switch" }] }]);
      assert.deepEqual(errors, []);
    });
  }
}

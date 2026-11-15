// Run with Node's test runner and Playwright on the module path; see README.md.
// 0xZaTe on Discord (#feedback): the Agents page listed the agents hidden by
// hand first in its fold, right under the agent in use and above the ones
// nothing is set on, each with its Show. Hidden read as pinned (看着反而像是
// 一个置顶功能了 … 现在反而更显眼了). Now they come last, after Not set up,
// behind a disclosure of their own, 「已隐藏（2）」, shut until it is opened,
// and the fold's button counts only the others. With nothing else folded,
// the fold is theirs and opens straight onto them. The window at a narrow
// width and the tray panel, Chromium and WebKit, every language; no backend.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const options = [{ value: "relay/m1", label: "m1", ref: "relay/m1", note: "Relay" }];
const agent = (id, name, value) => ({ id, name, icon: "generic", path: "/fixture/" + id, fields: [{ key: "model", label: "model", value, options }] });
// the reporter's list: Codex in use, two hidden, three with nothing set
const agents = [
  { ...agent("codex", "Codex", "relay/m1"), wired: true },
  agent("agy", "Antigravity CLI", ""), agent("commandcode", "Command Code", ""),
  agent("dsh", "DeepSeek Harness", ""), agent("zed", "Zed", ""), agent("alma", "Alma", ""),
];

function server(lang, hidden) {
  const settings = { lang, theme: "dark", agentsHidden: hidden };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"dark",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents, profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/usage") return json({ calls: 0, cost: 0, days: [], agents: [], models: [], keys: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/window/fit") return route.fulfill({ status: 204 });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const words = {
  en: { unset: "Not set up", hidden: "Hidden", line: "Hidden (2)", more: "Show 3 more", only: "2 hidden agents" },
  zh: { unset: "未设置", hidden: "已隐藏", line: "已隐藏（2）", more: "显示其余 3 个", only: "已隐藏的 Agent（2 个）" },
  "zh-TW": { unset: "未設定", hidden: "已隱藏", line: "已隱藏（2）", more: "顯示其餘 3 個", only: "已隱藏的 Agent（2 個）" },
  ja: { unset: "未設定", hidden: "非表示", line: "非表示（2）", more: "さらに 3 件表示", only: "非表示のエージェント（2）" },
  de: { unset: "Nicht eingerichtet", hidden: "Ausgeblendet", line: "Ausgeblendet (2)", more: "3 weitere anzeigen", only: "2 ausgeblendete Agenten" },
};

// the fold top to bottom: each line, and each row as its id, with where it
// is drawn and how tall it shows (0 when its fold is shut)
const fold = (page) => page.evaluate(() => {
  const out = [];
  for (const n of document.querySelectorAll(".agent-fold-inner > *, .agent-hid-inner > *")) {
    const b = n.getBoundingClientRect();
    if (n.classList.contains("agent-fold-cap")) out.push({ cap: n.textContent.trim(), y: b.top });
    else if (n.dataset.id) {
      const clip = n.closest(".agent-hid-fold");
      out.push({ id: n.dataset.id, y: b.top, seen: clip ? Math.min(b.bottom, clip.getBoundingClientRect().bottom) - b.top : b.height, show: !!n.querySelector(".ag-show") });
    }
  }
  return out;
});
const settle = (page) => page.locator(".agent-fold").evaluate((f) => Promise.all(f.getAnimations({ subtree: true }).map((a) => a.finished)));

const surfaces = [
  ["window, narrow", "http://uni-switch.test/?view=agents", { width: 420, height: 900 }],
  ["tray panel", "http://uni-switch.test/?mode=panel", { width: 380, height: 900 }],
];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: hidden agents come last, behind a disclosure shut until opened`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of Object.keys(words)) for (const [where, url, viewport] of surfaces) {
      await t.test(`${lang}, ${where}`, async () => {
        const w = words[lang];
        const ctx = await browser.newContext({ viewport, colorScheme: "dark" });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, ["agy", "commandcode"]));
        await page.goto(url);
        const more = page.locator(".agent-more");
        await more.waitFor();
        const said = (await more.textContent()).trim();
        await more.click();
        await settle(page);

        const rows = await fold(page);
        const at = (k) => rows.findIndex((r) => r.cap === k || r.id === k);
        // Not set up first, its three rows, then the hidden ones' line, last
        assert.deepEqual(rows.map((r) => r.cap || r.id), [w.unset, "dsh", "zed", "alma", w.line, "agy", "commandcode"]);
        // the button counted the ones nothing is set on, not the hidden ones
        assert.equal(said, w.more);
        for (const id of ["dsh", "zed", "alma"]) assert.ok(rows[at(id)].y < rows[at(w.line)].y, `${id} is drawn above ${w.line}`);
        // shut: the hidden rows take no room and can't be reached
        for (const id of ["agy", "commandcode"]) assert.ok(rows[at(id)].seen <= 0.5, `${id} is out of sight while ${w.line} is shut: ${rows[at(id)].seen}`);
        const line = page.locator(".agent-hid-line");
        assert.equal(await line.getAttribute("aria-expanded"), "false");
        assert.equal(await page.locator(".agent-hid-inner").evaluate((e) => e.inert), true);

        // opened: the two come in under their line, each with its Show
        await line.click();
        await page.locator(".agent-hid-fold.open").waitFor();
        await page.locator(".agent-hid-fold").evaluate((f) => new Promise((r) => {
          const done = () => f.getBoundingClientRect().height >= f.firstChild.scrollHeight - 0.5;
          if (done()) return r();
          f.addEventListener("transitionend", r, { once: true });
          setTimeout(r, 1500);
        }));
        assert.equal(await line.getAttribute("aria-expanded"), "true");
        const open = await fold(page);
        for (const id of ["agy", "commandcode"]) {
          const r = open.find((x) => x.id === id);
          assert.ok(r.seen > 20 && r.show, `${id} shows with its Show once opened: ${JSON.stringify(r)}`);
          assert.ok(r.y > open.find((x) => x.cap === w.line).y, `${id} is under its line`);
        }
        await page.locator('.agent-hid-inner .row.agent[data-id="agy"] .ag-show').click({ trial: true });
        // nothing runs off the side at this width
        assert.equal(await page.evaluate(() => document.scrollingElement.scrollWidth <= window.innerWidth), true, "no sideways scroll");
        assert.ok(await line.evaluate((e) => e.scrollWidth <= e.clientWidth + 1), "the line's words fit");
        assert.deepEqual(errors, []);
        await ctx.close();
      });
    }

    // only hidden ones folded: the fold is theirs, opened straight onto them
    await t.test("only hidden ones", async () => {
      for (const lang of ["en", "zh"]) {
        const w = words[lang];
        const ctx = await browser.newContext({ viewport: { width: 420, height: 900 }, colorScheme: "dark" });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        await page.route("**/*", server(lang, ["agy", "commandcode", "dsh", "zed", "alma"]));
        await page.goto("http://uni-switch.test/?view=agents");
        const more = page.locator(".agent-more");
        await more.waitFor();
        assert.equal((await more.textContent()).trim(), w.only.replace("2", "5"));
        await more.click();
        await settle(page);
        assert.equal(await page.locator(".agent-hid-line").count(), 0, "no second disclosure inside the first");
        const rows = await fold(page);
        assert.equal(rows[0].cap, w.hidden);
        for (const r of rows.slice(1)) assert.ok(r.seen > 20 && r.show, `${r.id} shows with its Show`);
        await ctx.close();
      }
    });
  });
}

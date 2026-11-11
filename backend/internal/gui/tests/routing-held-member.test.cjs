// Run with Node's test runner and Playwright on the module path; see README.md.
// A routing group's member whose account is held — used up and set not to
// spend credits, or at the usage cap set on it — stays on the Routing
// page's stage, faded and saying why, in its place among the group's
// members, as a resting one does (#2129, wyq0x55: d-task's gpt-sol-6.1,
// used up, wasn't shown at all while a used-up Claude was). One whose plan
// doesn't list the model is still left off the stage. A fold of a
// provider's accounts counts the held ones apart from those available.
// English, Chinese (both), Japanese and German; 1100 and 420 wide; no
// backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const ago = (s) => new Date(now.getTime() - s * 1e3).toISOString();
const ahead = (m) => new Date(Date.now() + m * 60e3).toISOString();
const acct = (p, name, user, model, extra = {}) => ({ id: `${p}@${user}`, provider: p, name, who: user, kind: "account", model, plan: "Pro", ...extra });

const group = { id: "d-task", name: "d-task", routing: "order",
  members: ["codex/gpt-6.1-sol", "claude/claude-opus-5-5", "grok/grok-4.7", "team/glm-5"] };
const claude = acct("claude", "Claude", "c@x.test", "claude-opus-5-5", { rest: { why: "quota", status: 429, until: ahead(60), key: "claude@c@x.test", for: 3600e9 } });
const grok = acct("grok", "Grok", "g@x.test", "grok-4.7", { known: true, used: 10 });
const team = ["t1@x.test", "t2@x.test", "t3@x.test"];
const route = {
  id: 1, seq: 1, time: ago(30), agent: "codex", model: "d-task", provider: "grok", done: true, status: 200, ms: 900, tokens: 1200, group,
  order: [claude, grok, acct("team", "Team", team[0], "glm-5")],
  left: [
    // used up and set not to spend its credits: the member's only account
    acct("codex", "Codex", "o@x.test", "gpt-6.1-sol", { unlisted: true, noCredits: true, capped: 100, used: 100 }),
    // two of Team's three at the cap set on them
    acct("team", "Team", team[1], "glm-5", { unlisted: true, capped: 90, used: 95 }),
    acct("team", "Team", team[2], "glm-5", { unlisted: true, capped: 90, used: 97 }),
    // its plan doesn't list the model: never asked, never drawn
    acct("grok", "Grok", "free@x.test", "grok-4.7", { unlisted: true }),
  ],
  tries: [{ id: grok.id, model: "grok-4.7", start: ago(30), done: true, status: 200, ms: 900 }],
};

function serve(lang) {
  return async (request) => {
    const url = new URL(request.request().url());
    const json = (data) => request.fulfill({ json: data });
    if (url.pathname === "/boot.js") return request.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return request.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) { await new Promise((r) => setTimeout(r, 20e3)); return json({}); }
      return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 1, rerouted: 0, errors: 0 }, routes: [route] });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await request.fulfill({ body: await fs.readFile(file), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [1100, 420]) {
      test(`${engine} ${lang} ${width}: a group member held at its cap or used up stays on the stage, saying so`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await browser.newPage({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (error) => errors.push(error.message));
        await page.route("**/*", serve(lang));
        await page.goto("http://uni-switch.test/?view=routing");
        await page.locator(".rt-accts li.rt-fold").first().waitFor();
        await page.waitForFunction(() => document.querySelector(".rt-accts li.rt-fold em")?.textContent.trim());
        const say = (key, vars = {}) => page.evaluate(([lang, key, vars]) => {
          const s = (lang !== "en" && I18N[lang]?.[key]) || key;
          return s.replace(/\{(\w+)\}/g, (_, k) => vars[k] ?? `{${k}}`);
        }, [lang, key, vars]);
        // top to bottom, in the group's order: the held Codex member first
        const rows = await page.locator(".rt-accts > li").evaluateAll((lis) => lis.map((li) => ({
          who: li.classList.contains("rt-fold") ? "fold:" + li.querySelector(".who").textContent : li.querySelector(".who")?.textContent,
          st: li.querySelector("em")?.textContent.trim(), left: li.classList.contains("left"), rest: li.classList.contains("rest"),
        })));
        assert.deepEqual(rows.map((r) => r.who), ["o@x.test", "c@x.test", "g@x.test", "fold:Team", ...team], JSON.stringify(rows));
        const codex = rows[0];
        assert.equal(codex.st, await say("held: its allowance used up, set not to spend credits"));
        assert(codex.left, "the held member is drawn faded");
        assert(rows[1].rest, "the resting Claude is faded too");
        assert.equal(rows[5].st, await say("held at its {cap}% usage cap", { cap: 90 }));
        // free@x.test, whose plan doesn't list grok-4.7, isn't drawn
        assert(!rows.some((r) => r.who === "free@x.test"));
        assert.equal(rows[3].st, [await say("{n} available", { n: 1 }), await say("{n} held", { n: 2 })].join(" · "));
        // nothing spills at either width, measured once the narrow layout
        // is on: client.js sets its classes a frame after it observes a
        // width, the box's first and then the list's, and the list can be
        // drawn the wide way at 0px until then (#2252)
        if (width < 560) await page.locator(".rt.max560 .rt-accts.max460").waitFor();
        const spills = await page.locator(".rt-accts li").evaluateAll((els) => els.filter((e) => e.getBoundingClientRect().right > document.documentElement.clientWidth + 0.5).map((e) => e.textContent.slice(0, 40)));
        assert.deepEqual(spills, []);
        assert.deepEqual(errors, []);
      });
    }
  }
}

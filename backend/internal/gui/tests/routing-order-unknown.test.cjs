// Run with Node's test runner and Playwright on the module path; see README.md.
// A routing group set In order tries its members in the order the user
// gave, each over its own accounts as its provider routes them
// (planLevel): an account whose allowance isn't known doesn't go after
// another member's known one. The Routing page said it did (bitssly on
// X: "opencode：剩余额度未知，排在已知的之后。" under an In order group),
// as if uni-switch had moved the member the user put there. Now an In order
// group's first says In order, and the unknown-allowance line is said
// only of an account weighed against a known one of its own member, as
// a Smart group still says it of each; a group By weight says the
// round picked its first. English, Chinese (both),
// Japanese and German; 1100 and 420 wide; no backend, the API is faked
// here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const ago = (s) => new Date(now.getTime() - s * 1e3).toISOString();
const ahead = (h) => new Date(Date.now() + h * 3600e3).toISOString();
const acct = (p, name, user, model, extra = {}) => ({ id: `${p}@${user}`, provider: p, name, who: user, kind: "account", agent: p, model, routing: "", ...extra });

const codex = acct("codex", "Codex", "c@x.test", "gpt-5.5", { plan: "Plus", known: true, used: 30, renews: [ahead(50)] });
const zen = acct("opencode-zen-free", "OpenCode Zen Free", "opencode", "big-pickle");
const wbKnown = acct("workbuddy-ai", "WorkBuddy AI", "w2@x.test", "glm-5", { known: true, used: 20, renews: [ahead(100)] });
const wbUnknown = acct("workbuddy-ai", "WorkBuddy AI", "腾讯-国际", "glm-5");

const members = ["codex/gpt-5.5", "opencode-zen-free/big-pickle", "workbuddy-ai/glm-5"];
const routeOf = (id, group, order) => ({
  id, seq: id, time: ago(60 - id), agent: "codex", model: group.id, provider: "codex", done: true, status: 200, ms: 900, tokens: 1200, group, order,
  tries: [{ id: order[0].id, model: order[0].model, start: ago(60 - id), done: true, status: 200, ms: 900 }],
});
// In order: Codex first as the user put it; WorkBuddy AI's two accounts,
// one member, Smart between themselves: the known one first
const ordered = routeOf(1, { id: "coding", name: "coding", routing: "order", members }, [codex, zen, wbKnown, wbUnknown]);
// Smart: every member's accounts weighed as one, the unknown after
const smart = routeOf(2, { id: "mix", name: "mix", routing: "", members }, [codex, wbKnown, zen, wbUnknown]);
// By weight: the round's pick first, each member whole after it
const weighted = routeOf(3, { id: "spread", name: "spread", routing: "weight", members }, [zen, codex, wbKnown, wbUnknown]);

function serve(lang) {
  return async (request) => {
    const url = new URL(request.request().url());
    const json = (data) => request.fulfill({ json: data });
    if (url.pathname === "/boot.js") return request.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return request.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) { await new Promise((r) => setTimeout(r, 20e3)); return json({}); }
      return json({ mine: true, now: now.toISOString(), seq: 3, totals: { requests: 3, rerouted: 0, errors: 0 }, routes: [ordered, smart, weighted] });
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
      test(`${engine} ${lang} ${width}: an In order group's later members aren't said to go after a known allowance`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await browser.newPage({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (error) => errors.push(error.message));
        await page.route("**/*", serve(lang));
        await page.goto("http://uni-switch.test/?view=routing");
        const say = (key, vars = {}) => page.evaluate(([lang, key, vars]) => {
          const s = (lang !== "en" && I18N[lang]?.[key]) || key;
          return s.replace(/\{(\w+)\}/g, (_, k) => vars[k] ?? `{${k}}`);
        }, [lang, key, vars]);
        const unknown = (who) => say("{who}: what it has left isn't known yet, so it goes after those known.", { who });
        // the story of the request with the group named
        const story = async (name) => {
          const row = page.locator(".rt-req", { hasText: name }).first();
          await row.waitFor();
          await row.dispatchEvent("click"); // the list may run past a narrow window
          await page.locator(".rt-steps li").first().waitFor();
          await page.waitForFunction((name) => document.querySelector(".rt-steps")?.textContent.includes(name), name);
          return page.locator(".rt-steps li").evaluateAll((ls) => ls.map((l) => [l.className, l.textContent.trim()]));
        };

        let got = await story("coding");
        const why = got.filter(([c]) => c === "why").map(([, s]) => s);
        assert.deepEqual(why, [await say("In order: {who} is first, and answers everything while it can.", { who: "c@x.test" })], JSON.stringify(got));
        const asides = got.filter(([c]) => c === "aside").map(([, s]) => s);
        // OpenCode Zen Free is its own member, after Codex by the order given
        assert(!asides.includes(await unknown("opencode")), JSON.stringify(asides));
        // WorkBuddy AI's unknown account, weighed against its own known one,
        // is told with its member: after Codex by the order given
        assert(asides.includes(await say("{member} isn't passed over for being near its limit: it comes after {first} in {group}'s order.", { member: "WorkBuddy AI", first: "Codex", group: "coding" })), JSON.stringify(asides));
        assert(!asides.some((s) => s.startsWith("OpenCode Zen Free")), JSON.stringify(asides));

        got = await story("mix");
        const smartAsides = got.filter(([c]) => c === "aside").map(([, s]) => s);
        assert(smartAsides.includes(await unknown("opencode")), JSON.stringify(smartAsides));
        assert(smartAsides.includes(await unknown("腾讯-国际")), JSON.stringify(smartAsides));

        // by weight: the first is the round's pick, not one weighed by
        // its allowance against Codex's
        got = await story("spread");
        assert.deepEqual(got.filter(([c]) => c === "why").map(([, s]) => s), [await say("By weight: it's {who}'s share — each model takes requests as the weight set beside it says.", { who: "opencode" })], JSON.stringify(got));
        const wAsides = got.filter(([c]) => c === "aside").map(([, s]) => s);
        assert(!wAsides.some((s) => s.startsWith("c@x.test")), JSON.stringify(wAsides));
        assert(!wAsides.includes(await unknown("opencode")), JSON.stringify(wAsides));
        assert(wAsides.includes(await unknown("腾讯-国际")), JSON.stringify(wAsides));

        const spills = await page.locator(".rt-steps li").evaluateAll((els) => els.filter((e) => e.getBoundingClientRect().right > document.documentElement.clientWidth + 0.5).map((e) => e.textContent.slice(0, 40)));
        assert.deepEqual(spills, []);
        assert.deepEqual(errors, []);
      });
    }
  }
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// One model resting alone rests on its own rows only (#1960, WardLu): an
// Antigravity account out of its Claude & GPT pool (claude-opus-5.5 refused
// until Sunday) still answers gemini-3.8-flash from its Gemini pool. Before,
// Routing's stage marked the account's gemini-3.8-flash row "quota used up ·
// back at Sun 17:53", and its row under Accounts and keys said the account
// was out. Now the stage marks the Opus row alone, and the account's row
// says what it has left with a line of its own naming the model that rests,
// whose Try now lifts that model's rest. An account resting as a whole
// still rests on every row. In every language, at a wide and a narrow
// window, in Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
// a reader wheels down to it, and it is the button under the pointer
const { reach } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const start = new Date(now.getTime() - 60e3).toISOString();
const sunday = new Date(now.getTime() + 5 * 24 * 3600e3).toISOString();
const renews = new Date(now.getTime() + 3 * 3600e3).toISOString();
const seat = (user, model, used, extra = {}) => ({
  id: "antigravity@" + user, provider: "antigravity", name: "Antigravity", kind: "account", icon: "antigravity",
  who: user, plan: "Google AI Pro", model, known: true, used, renews: [renews], ...extra,
});
// the issue's: the Claude & GPT pool used up rests claude-opus-5.5 alone,
// as the gateway names it (Rest.Model) — and one from a kept day before it
// did, the model on the end of the key only
const opusRest = { why: "quota", status: 429, until: sunday, by: "window", key: "antigravity@me@x.test/claude-opus-5.5", model: "claude-opus-5.5" };
const oldRest = { why: "quota", status: 429, until: sunday, by: "window", key: "antigravity@old@x.test/claude-opus-5.5" };
// an account out as a whole
const allRest = { why: "quota", status: 429, until: sunday, by: "window", key: "antigravity@out@x.test" };

// one request weighing the three accounts' seats, as the gateway does:
// each seat with its own rest, the account's on every seat of it. The
// stage shows the latest request's seats.
function routes() {
  const time = start, order = [], tries = [];
  for (const [user, rest] of [["me@x.test", opusRest], ["old@x.test", oldRest], ["out@x.test", allRest]]) {
    const opus = seat(user, "claude-opus-5.5", 100, { rest });
    const flash = seat(user, "gemini-3.8-flash", 46, rest === allRest ? { rest } : {});
    order.push(opus, flash);
    tries.push({ id: opus.id, model: opus.model, start: time, done: true, status: 429, ms: 300, fail: "quota", rest, error: "quota exhausted" });
  }
  tries.push({ id: order[1].id, model: order[1].model, start: time, done: true, status: 200, ms: 900 });
  return [{ id: 1, seq: 1, time, agent: "codex", model: "group/g", provider: "antigravity", order, tries, done: true, status: 200, ms: 2000 }];
}

function serve(lang, lifted) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.has("wait")) return;
      return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 1, rerouted: 1, errors: 0 }, routes: routes() });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/gateway/unrest") {
      lifted.push(JSON.parse(route.request().postData() || "{}"));
      return json({ lifted: true });
    }
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file).catch(() => ""), contentType });
  };
}

const go = { en: "Try now", zh: "立即重试", "zh-TW": "立即重試", ja: "今すぐ試す", de: "Jetzt versuchen" };
const posted = async (lifted) => {
  for (let i = 0; i < 60 && !lifted.length; i++) await new Promise((r) => setTimeout(r, 50));
  return lifted;
};
const sideways = (page) => page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(go)) {
    for (const width of [1100, 420]) {
      test(`${engine} ${lang} ${width}px: one model resting leaves the account's other models and the account in`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], lifted = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, lifted));
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-model-rest.png`), fullPage: true });
          }
          await browser.close();
        });
        await page.goto("http://uni-switch.test/?view=routing");
        await page.locator(".rt-act").first().waitFor();

        // the stage: a row per account and model
        const stage = (user, model) => page.locator("li", { has: page.locator("code.mdl", { hasText: new RegExp(`^${model.replace(/\./g, "\\.")}$`) }) })
          .filter({ has: page.locator(".who", { hasText: user }) });
        // old@x.test's gemini-3.8-flash wasn't asked, as the reporter's
        // wasn't; me@x.test's answered after its Opus was refused
        for (const user of ["old@x.test", "me@x.test"]) {
          const opus = stage(user, "claude-opus-5.5"), flash = stage(user, "gemini-3.8-flash");
          await opus.first().waitFor({ state: "attached" });
          // a row is in before the frame that says how it rests: under load
          // the class was read before that frame came (#2115)
          await opus.first().evaluate((e) => new Promise((done) => { const ok = () => e.classList.contains("rest") ? done() : requestAnimationFrame(ok); setTimeout(done, 5000); ok(); }));
          assert.equal(await opus.count(), 1, `${user}: one Opus row`);
          assert.equal(await flash.count(), 1, `${user}: one gemini-3.8-flash row`);
          assert(!(await flash.evaluate((e) => e.classList.contains("rest"))), `${user}: gemini-3.8-flash rests with Opus`);
          assert(await opus.evaluate((e) => e.classList.contains("rest")), `${user}: the Opus row doesn't rest`);
          const said = (await opus.locator("em").textContent()).trim();
          assert(said, `${user}: the Opus row says nothing`);
          assert.notEqual((await flash.locator("em").textContent()).trim(), said, `${user}: gemini-3.8-flash says Opus's rest`);
        }
        for (const model of ["claude-opus-5.5", "gemini-3.8-flash"]) {
          assert(await stage("out@x.test", model).evaluate((e) => e.classList.contains("rest")), `the account out as a whole: ${model} doesn't rest`);
        }

        // Accounts and keys: the account isn't out; the model resting is named
        for (const user of ["me@x.test", "old@x.test"]) {
          const row = page.locator(".rt-act", { hasText: user });
          assert.equal(await row.count(), 1);
          assert.equal(await row.locator(".st.rest:not(.part)").count(), 0, `${user}: the account reads as out`);
          const part = row.locator(".st.rest.part");
          assert.equal(await part.count(), 1, `${user}: no line names the model resting`);
          const text = await part.textContent();
          assert(text.startsWith("claude-opus-5.5 · "), `${user}: the line says ${text}`);
          assert(!text.includes("gemini"), `${user}: the line names gemini-3.8-flash`);
        }
        const out = page.locator(".rt-act", { hasText: "out@x.test" });
        assert.equal(await out.locator(".st.rest:not(.part)").count(), 1, "the account out as a whole doesn't read as out");
        assert.equal(await out.locator(".st.part").count(), 0);
        assert(!(await sideways(page)), "the page scrolls sideways");

        // Try now lifts the model's own rest
        const row = page.locator(".rt-act", { hasText: "me@x.test" });
        const btn = row.locator(".fix button", { hasText: go[lang] });
        await reach(page, btn);
        await btn.click();
        assert.deepEqual(await posted(lifted), [{ key: opusRest.key }]);
        await row.locator(".st.part").waitFor({ state: "detached" });
        assert.deepEqual(errors, []);
      });
    }
  }
}

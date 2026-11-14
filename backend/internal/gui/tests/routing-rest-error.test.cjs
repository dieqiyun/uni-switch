// Run with Node's test runner and Playwright on the module path; see README.md.
// An account resting after a failure that names no kind says the status
// it answered, and the vendor's words are over the row, on the stage and
// in the Accounts table (GanHei37443 on X: three WorkBuddy accounts each
// "failed · back in 16s", and nothing on the page said why). A rest that
// names its kind (quota used up) reads as before. In every language, at a
// wide and a narrow window, in both engines.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const start = new Date(now.getTime() - 60e3).toISOString();
const soon = new Date(now.getTime() + 16e3).toISOString();
const later = new Date(now.getTime() + 10 * 3600e3).toISOString();
const said = "WorkBuddy: model deepseek-v4.1-flash is not available right now";
const seat = (user, rest) => ({
  id: "workbuddy@" + user, provider: "workbuddy", name: "WorkBuddy", kind: "account", icon: "workbuddy",
  who: user, plan: "Free", model: "deepseek-v4.1-flash", rest,
});
const failed = (user) => ({ why: "other", status: 500, until: soon, by: "cooldown", key: "workbuddy@" + user, error: said });
const spent = { why: "quota", status: 429, until: later, by: "window", key: "workbuddy@d@x.test" };

function routes() {
  const order = [], tries = [];
  for (const user of ["a@x.test", "b@x.test", "c@x.test"]) {
    const rest = failed(user), s = seat(user, rest);
    order.push(s);
    tries.push({ id: s.id, model: s.model, start, done: true, status: 500, ms: 300, fail: "other", rest, error: said });
  }
  const d = seat("d@x.test", spent);
  order.push(d);
  tries.push({ id: d.id, model: d.model, start, done: true, status: 429, ms: 300, fail: "quota", rest: spent, error: "quota" });
  return [{ id: 1, seq: 1, time: start, agent: "codex", model: "group/g", provider: "workbuddy", order, tries, done: true, status: 500, ms: 2000, error: said }];
}

function serve(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.has("wait")) return;
      return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 1, rerouted: 1, errors: 1 }, routes: routes() });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file).catch(() => ""), contentType });
  };
}

const sideways = (page) => page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [1100, 420]) {
      test(`${engine} ${lang} ${width}px: a plain failure's rest says its status and the vendor's words`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-rest-error.png`), fullPage: true });
          }
          await browser.close();
        });
        await page.goto("http://uni-switch.test/?view=routing");
        await page.locator(".rt-act").first().waitFor();

        const stage = (user) => page.locator("li", { has: page.locator("code.mdl") }).filter({ has: page.locator(".who", { hasText: user }) });
        for (const user of ["a@x.test", "b@x.test", "c@x.test"]) {
          const li = stage(user);
          await li.first().waitFor({ state: "attached" });
          await li.first().evaluate((e) => new Promise((done) => { const ok = () => e.classList.contains("rest") ? done() : requestAnimationFrame(ok); setTimeout(done, 5000); ok(); }));
          assert.equal(await li.count(), 1, `${user}: one stage row`);
          const em = li.locator("em");
          const text = (await em.textContent()).trim();
          assert(text.startsWith("500 · "), `${user}: the stage row says ${text}`);
          assert.equal(await em.getAttribute("title"), said, `${user}: the stage row's words`);

          const act = page.locator(".rt-act", { hasText: user });
          assert.equal(await act.count(), 1);
          const st = act.locator(".st.rest");
          assert((await st.textContent()).startsWith("500 · "), `${user}: the Accounts table says ${await st.textContent()}`);
          assert.equal(await st.getAttribute("title"), said, `${user}: the Accounts table's words`);
        }
        const d = stage("d@x.test").locator("em");
        assert(!(await d.textContent()).trim().startsWith("429"), `a quota rest says its status: ${await d.textContent()}`);
        assert.equal(await d.getAttribute("title"), null, "a rest without words has a tooltip");
        assert(!(await sideways(page)), "the page scrolls sideways");
        assert.deepEqual(errors, []);
      });
    }
  }
}

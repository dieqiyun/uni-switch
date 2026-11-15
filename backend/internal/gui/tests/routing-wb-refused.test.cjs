// Run with Node's test runner and Playwright on the module path; see README.md.
// WorkBuddy refusing Codex's chat "from an unapproved channel" (its system
// prompt, #182): the Routing page's story gives the vendor's words as they
// were, and what to do about it apart from them, in the page's language
// (Discord, v0.1.478: WorkBuddy AI: Illegal API invocation from an
// unapproved channel). Told as the agent's prompt turned away, which no
// reasoning level changes: the level it went at is left out of it (Discord,
// lemon: "at low reasoning … low is the model's nearest to the none LCode
// asked for" read as the level's fault).
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const at = (i) => new Date(now.getTime() - (i + 1) * 60e3).toISOString();
const hint = "WorkBuddy refuses chats from Codex and Claude Code (their system prompt); use it from Hermes, OpenCode or Pi, or add another provider to this group";
const vendor = "WorkBuddy AI: Illegal API invocation from an unapproved channel";
const key = { id: "workbuddy-ai", provider: "workbuddy-ai", name: "WorkBuddy AI", kind: "provider", model: "deepseek-v4.1-flash" };
// newest first: the refusal (sent at low, none asked), then another 400
// with nothing to add
const errs = [vendor + " — " + hint, "WorkBuddy AI: first message is not system prompt", "", "", "", ""];
const routes = errs.map((error, i) => {
  const status = error ? 400 : 200;
  return {
    id: 100 - i, seq: 100 - i, time: at(i), agent: "codex", model: "workbuddy-ai/deepseek-v4.1-flash", provider: "workbuddy-ai",
    order: [key], tries: [{ id: key.id, model: key.model, start: at(i), done: true, status, ms: 400, ...(error ? { error } : {}),
      ...(i === 0 ? { fail: "prompt", effort: "low" } : {}) }],
    done: true, status, ms: 400, ...(error ? { error } : {}), ...(i === 0 ? { effort: "none" } : {}),
  };
});

function serve(lang) {
  const state = { agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) await new Promise((r) => setTimeout(r, 20e3));
      return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname === "/api/gateway/history") {
      const d = url.searchParams.get("day");
      return json({ cut: false, days: [{ day, requests: routes.length }], routes: d ? routes : [] });
    }
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const want = {
  en: { said: "It said: " + vendor, hint, why: "the vendor turns away Codex's system prompt whichever account it goes to", level: "low reasoning" },
  zh: { said: "原话：" + vendor, hint: "WorkBuddy 会因系统提示词拒绝 Codex 和 Claude Code 的对话；请在 Hermes、OpenCode 或 Pi 中使用，或在此分组中再加一个供应商", why: "服务商不接受 Codex 的系统提示词，换哪个账号都一样", level: "low 推理" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: WorkBuddy's refusal of Codex says what to do`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const context = await browser.newContext({ viewport: { width: 1100, height: 760 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-wb-refused.png`), fullPage: true });
        }
        await browser.close();
      });
      await page.goto("http://uni-switch.test/?view=routing");
      await page.locator(".rt-days .rt-day").nth(1).click();
      await page.locator(".rt-req").nth(errs.length - 1).waitFor();

      const steps = async () => page.locator(".rt-steps li").evaluateAll((ls) => ls.map((l) => [l.className, l.textContent]));
      const row = page.locator(".rt-req").nth(0);
      // measure once the page holds still: the day's rows arriving grow the
      // page above the list, and the day button's click is kept in place by
      // scrolling the view, which can land after the last row is in
      const was = await row.evaluate(async (e) => {
        let last = NaN;
        for (let i = 0; i < 60; i++) {
          await new Promise((r) => requestAnimationFrame(r));
          const top = e.getBoundingClientRect().top;
          if (top === last) return top;
          last = top;
        }
        return last;
      });
      await row.click();
      await page.waitForTimeout(400);
      assert(Math.abs((await row.evaluate((e) => e.getBoundingClientRect().top)) - was) <= 1, "picking the request moved the page");
      let got = await steps();
      // the vendor's words as they were, without uni-switch's hint in them
      assert.deepEqual(got.filter(([c]) => c === "aside said").map(([, s]) => s), [want[lang].said]);
      // and the hint on its own line, in the page's language
      assert.deepEqual(got.filter(([, s]) => s === want[lang].hint).map(([c]) => c), ["aside"]);
      // the try told as the prompt turned away, not at a level
      const told = got.map(([, s]) => s).filter((s) => s.includes(want[lang].why));
      assert.equal(told.length, 1, JSON.stringify(got));
      assert(!told[0].includes(want[lang].level), told[0]);

      // another 400 gets no hint
      await page.locator(".rt-req").nth(1).click();
      await page.waitForTimeout(300);
      got = await steps();
      assert.equal(got.filter(([c]) => c === "aside said").length, 1);
      assert(!got.some(([, s]) => s.includes("Hermes")), JSON.stringify(got));
      assert.deepEqual(errors, []);
    });
  }
}

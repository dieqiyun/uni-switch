// Run with Node's test runner and Playwright on the module path; see README.md.
// Codex's "Codex auto-review model" (#938, guitaoliu: Codex's auto-review
// runs on the conversation's model, with no way to pick a cheaper one): by
// default Codex's own pick; the app's menu (no native select) offers
// uni-switch's models and groups; a pick posts settings/codex-auto-review on its
// own, its button shows the choice under a name alone (1ooogle on X), a refusal is said and the row keeps
// what it had, and no click scrolls the Agents page, where Codex's row holds it.
// "Same as the session" runs it on the session's own model (cybercaius on
// Discord). Every language, Chromium and WebKit; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, wheelTo, agentRow, openAgentSettings } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");

function settingsPayload(over) {
  return {
    theme: "light", lang: "en", tray: "panel", quotaLeft: false, currency: "usd",
    dock: false, dockWindow: false, proxy: "", redact: false, redactPersonal: false, redactWords: [],
    codexWarmup: "", claudeWarmup: "", codexWarmAt: "", claudeWarmAt: "", workbuddyCheckin: false, noStats: false,
    trayUsage: "", trayUsageEvery: 3, vision: "", imageGen: "",
    version: "0.1.400", dir: "~/.config/uni-switch", gateway: "http://127.0.0.1:3425",
    proxyNow: "none", proxySource: "none", login: false,
    visionModels: [], imageGenModels: [], workbuddyCheckins: [], lanURLs: [],
    titleModels: [
      { id: "deepseek/deepseek-v4-flash", name: "DeepSeek V4 Flash", provider: "deepseek", providerName: "DeepSeek", icon: "deepseek" },
      { id: "group/cheap", name: "cheap", provider: "", providerName: "Routing groups" },
    ],
    fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
    ...over,
  };
}

function server(lang, posted) {
  let cur = settingsPayload({ lang });
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    // agents below Codex, so its opened row is not the list's end: a choice
    // that rewraps its row at the very bottom must move the list
    if (url.pathname === "/api/state") return json({ agents: [agentRow("codex", "Codex"), agentRow("claude", "Claude Code"), agentRow("pi", "Pi")], profiles: [], settings: { lang, theme: "light" }, fx: cur.fx });
    if (url.pathname === "/api/settings/codex-auto-review") {
      const body = req.postDataJSON();
      posted.push(["codex-auto-review", body]);
      if (body.model === "group/cheap" && posted.filter(([k]) => k === "codex-auto-review").length === 2)
        return route.fulfill({ status: 400, json: { error: "no model group/cheap for Codex's auto-review" } });
      cur = { ...cur, codexAutoReview: body.model };
      return json(cur);
    }
    if (url.pathname === "/api/settings") {
      if (req.method() === "POST") posted.push(["settings", req.postDataJSON()]);
      return json(cur);
    }
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try {
      await route.fulfill({ body: await fs.readFile(file), contentType });
    } catch {
      await route.fulfill({ status: 404, body: "" });
    }
  };
}

const want = {
  en: { name: "Codex auto-review model", own: "Codex’s own pick", session: "Same as the session" },
  zh: { name: "Codex 自动审批模型", own: "由 Codex 决定", session: "跟随当前会话" },
  "zh-TW": { name: "Codex 自動審批模型", own: "由 Codex 決定", session: "跟隨目前工作階段" },
  ja: { name: "Codex 自動レビューのモデル", own: "Codex に任せる", session: "セッションと同じ" },
  de: { name: "Modell für Codex’ Auto-Review", own: "Wie Codex es wählt", session: "Wie die Sitzung" },
};
const view = (page) => page.locator("#view-agents").evaluate((v) => v.scrollTop);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(want)) {
    test(`${engine} ${lang}: Codex's auto-review runs on the model Codex's settings pick`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const context = await browser.newContext({ viewport: { width: 900, height: 480 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [], posted = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(lang, posted));
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-codex-auto-review.png`) });
        }
        await browser.close();
      });
      await watchSettingsReads(page);
      await page.goto("http://uni-switch.test/?view=agents");
      await openAgentSettings(page); // on Codex's opened row, not in Settings (the owner, 10-20)
      const row = page.locator("#codexAutoReviewRow");
      const pick = page.locator("#codexAutoReviewPick button");
      await pick.waitFor();
      assert.equal(await row.locator("select").count(), 0, "no native select");
      assert.equal((await row.locator(".name").textContent()).trim(), want[lang].name);
      assert.equal((await pick.textContent()).trim(), want[lang].own, "Codex's own pick by default");
      assert.equal(await row.locator(".sub").count(), 0, "the row says its name alone, its button the choice (1ooogle on X)");

      // scrolled by a wheel till the row is mid-view, so a click that
      // moved the page would show
      await wheelTo(page, pick, 200); // each wheel landed before the next (#2370)
      const before = await view(page);
      assert(before > 0, "the agents list must scroll to the row");

      // the app's menu: Codex's own pick and uni-switch's models and groups
      const choose = async (text) => {
        await pick.click();
        await page.locator("#pop:not([hidden]) #list li").first().waitFor();
        await page.locator("#list li:not(.group)").filter({ hasText: text }).first().click();
      };
      await pick.click();
      await page.locator("#pop:not([hidden]) #list li").first().waitFor();
      const items = (await page.locator("#list li:not(.group)").allTextContents()).join("|");
      for (const s of [want[lang].own, want[lang].session, "DeepSeek V4 Flash", "cheap"]) assert(items.includes(s), `${s} in ${items}`);
      await page.locator("#list li:not(.group)").filter({ hasText: "DeepSeek V4 Flash" }).first().click();
      await page.locator("#codexAutoReviewPick button", { hasText: "DeepSeek V4 Flash" }).waitFor();
      assert.deepEqual(posted, [["codex-auto-review", { model: "deepseek/deepseek-v4-flash" }]]);
      assert.equal(await row.locator(".sub").count(), 0);
      await page.waitForTimeout(300);
      assert.equal(await view(page), before, "picking a model scrolled the page");

      // refused: the row keeps the model it had
      await choose("cheap");
      await page.waitForTimeout(300);
      assert.deepEqual(posted.at(-1), ["codex-auto-review", { model: "group/cheap" }]);
      assert.match(await pick.textContent(), /DeepSeek V4 Flash/, "a refused pick changed the row");

      // the session's own model
      await choose(want[lang].session);
      await page.locator("#codexAutoReviewPick button", { hasText: want[lang].session }).waitFor();
      assert.deepEqual(posted.at(-1), ["codex-auto-review", { model: "session" }]);

      // and back to Codex's own pick
      await choose(want[lang].own);
      await page.locator("#codexAutoReviewPick button", { hasText: want[lang].own }).waitFor();
      assert.deepEqual(posted.at(-1), ["codex-auto-review", { model: "" }]);
      assert.equal(await row.locator(".sub").count(), 0);
      await page.waitForTimeout(300);
      assert.equal(await view(page), before, "the picks scrolled the page");
      assert(!posted.some(([k]) => k === "settings"), "set on its own, not with the other preferences");
      assert.deepEqual(errors, []);
    });
  }
}

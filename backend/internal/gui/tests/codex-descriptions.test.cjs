// Run with Node's test runner and Playwright on the module path; see README.md.
// Codex's "Codex thread descriptions" (#1198, congee949): Codex's
// thread_description requests go where this says, apart from the titles'
// setting. By default the session's model (cybercaius); the app's menu (no native
// select) offers Off, as the titles' (cybercaius again), and uni-switch's models; a pick posts settings/codex-descriptions
// on its own, the titles' setting untouched, its button shows the choice
// under a name with no line of its own (as the titles' row, 1ooogle on X),
// and no click scrolls the Agents page, where Codex's row holds it.
// English and Chinese, Chromium and WebKit; no backend, the API is faked here.
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
    if (url.pathname === "/api/settings/codex-descriptions") {
      const body = req.postDataJSON();
      posted.push(["codex-descriptions", body]);
      cur = { ...cur, codexDescriptions: body.model };
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
  en: { name: "Codex thread descriptions", own: "Same as the session", off: "Off", offNote: "no description, nothing sent" },
  zh: { name: "Codex 会话描述", own: "跟随当前会话", off: "关闭", offNote: "不生成描述，不发出请求" },
};
const view = (page) => page.locator("#view-agents").evaluate((v) => v.scrollTop);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: Codex's description requests go where Codex's settings say`, async (t) => {
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
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-codex-descriptions.png`) });
        }
        await browser.close();
      });
      await watchSettingsReads(page);
      await page.goto("http://uni-switch.test/?view=agents");
      await openAgentSettings(page); // on Codex's opened row, not in Settings (the owner, 10-20)
      const row = page.locator("#codexDescriptionsRow");
      const pick = page.locator("#codexDescriptionsPick button");
      await pick.waitFor();
      assert.equal(await page.locator("#codexDescriptionsRow select").count(), 0, "no native select");
      assert.equal((await row.locator(".name").textContent()).trim(), want[lang].name);
      assert.equal((await pick.textContent()).trim(), want[lang].own, "the session's by default (cybercaius)");
      assert.equal(await row.locator(".sub").count(), 0, "the row says its name alone, its button the choice (1ooogle on X)");
      // the chip's icon is the chip's size, not the row's 18–22px (duanckham on X)
      assert.deepEqual(await pick.locator(".ic").evaluate((e) => [e.offsetWidth, e.offsetHeight]), [14, 14]);

      // scrolled by a wheel till the row is mid-view, so a click that
      // moved the page would show
      await wheelTo(page, pick, 200); // each wheel landed before the next (#2370)
      const before = await view(page);
      assert(before > 0, "the agents list must scroll to the row");

      // the app's menu: the session's, Off and uni-switch's models
      await pick.click();
      await page.locator("#pop:not([hidden]) #list li").first().waitFor();
      const items = (await page.locator("#list li:not(.group)").allTextContents()).join("|");
      for (const s of [want[lang].own, want[lang].off, want[lang].offNote, "DeepSeek V4 Flash", "cheap"]) assert(items.includes(s), `${s} in ${items}`);
      await page.keyboard.press("Escape");
      await page.locator("#pop:not([hidden])").waitFor({ state: "hidden" });

      // a model of uni-switch's
      await pick.click();
      await page.locator("#pop:not([hidden]) #list li").first().waitFor();
      await page.locator("#list li:not(.group)").filter({ hasText: "DeepSeek V4 Flash" }).first().click();
      await page.locator("#codexDescriptionsPick button", { hasText: "DeepSeek V4 Flash" }).waitFor();
      assert.deepEqual(posted, [["codex-descriptions", { model: "deepseek/deepseek-v4-flash" }]]);
      assert.equal((await page.locator("#codexTitlesPick button").textContent()).trim(), want[lang].own, "the titles' setting is its own");
      await page.waitForTimeout(300);
      assert.equal(await view(page), before, "picking a model scrolled the page");
      assert.equal(await row.locator(".sub").count(), 0);

      // Off, as the titles' has (cybercaius on Discord): the titles' stays
      await pick.click();
      await page.locator("#pop:not([hidden]) #list li").first().waitFor();
      await page.locator("#list li:not(.group)").filter({ hasText: want[lang].offNote }).first().click();
      await page.locator("#codexDescriptionsPick button", { hasText: want[lang].off }).waitFor();
      assert.equal((await pick.textContent()).trim(), want[lang].off);
      assert.deepEqual(posted.at(-1), ["codex-descriptions", { model: "off" }]);
      assert.equal((await page.locator("#codexTitlesPick button").textContent()).trim(), want[lang].own, "the titles' setting is its own");
      await page.waitForTimeout(300);
      assert.equal(await view(page), before, "picking Off scrolled the page");

      // and back to the session's
      await pick.click();
      await page.locator("#pop:not([hidden]) #list li").first().waitFor();
      await page.locator("#list li:not(.group)").filter({ hasText: want[lang].own }).first().click();
      await page.locator("#codexDescriptionsPick button", { hasText: want[lang].own }).waitFor();
      assert.deepEqual(posted.at(-1), ["codex-descriptions", { model: "" }]);
      await page.waitForTimeout(300);
      assert.equal(await view(page), before, "the picks scrolled the page");
      assert(!posted.some(([k]) => k === "settings"), "set on its own, not with the other preferences");
      assert.deepEqual(errors, []);
    });
  }
}

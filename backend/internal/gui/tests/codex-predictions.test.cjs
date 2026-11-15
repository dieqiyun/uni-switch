// Run with Node's test runner and Playwright on the module path; see README.md.
// Codex's "Codex composer predictions" (#1970, pluo): the Codex app's
// guess at the user's next message, a hidden turn that reads the whole
// conversation again, goes where this says. By default the
// session's model (cybercaius); Off answers it with no prediction; the app's menu (no native
// select) offers uni-switch's models; a pick posts settings/codex-predictions
// on its own, the titles' and descriptions' settings untouched, its button
// shows the choice under a name with no line of its own (as its siblings,
// 1ooogle on X), and no click scrolls the Agents page, where Codex's row holds it.
// Five languages, 1100 and 420 wide, Chromium and WebKit; no backend, the
// API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, agentRow, openAgentSettings } = require("./reader.cjs");

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
    if (url.pathname === "/api/settings/codex-predictions") {
      const body = req.postDataJSON();
      posted.push(["codex-predictions", body]);
      cur = { ...cur, codexPredictions: body.model };
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
  en: { name: "Codex composer predictions", own: "Same as the session", off: "Off" },
  zh: { name: "Codex 输入框预测", own: "跟随当前会话", off: "关闭" },
  "zh-TW": { name: "Codex 輸入框預測", own: "跟隨目前工作階段", off: "關閉" },
  ja: { name: "Codex の入力予測", own: "セッションと同じ", off: "オフ" },
  de: { name: "Codex-Eingabevorhersagen", own: "Wie die Sitzung", off: "Aus" },
};
const view = (page) => page.locator("#view-agents").evaluate((v) => v.scrollTop);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const width of [1100, 420]) {
    for (const lang of Object.keys(want)) {
      test(`${engine} ${lang} ${width}: Codex's composer predictions go where Codex's settings say`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const context = await browser.newContext({ viewport: { width, height: 480 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], posted = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, posted));
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-codex-predictions.png`) });
          }
          await browser.close();
        });
        await watchSettingsReads(page);
        await page.goto("http://uni-switch.test/?view=agents");
        await openAgentSettings(page); // on Codex's opened row, not in Settings (the owner, 10-20)
        const row = page.locator("#codexPredictionsRow");
        const pick = page.locator("#codexPredictionsPick button");
        const shows = (s) => page.locator("#codexPredictionsPick button", { hasText: s }).waitFor();
        await pick.waitFor();
        assert.equal(await row.locator("select").count(), 0, "no native select");
        assert.equal((await row.locator(".name").textContent()).trim(), want[lang].name);
        assert.equal((await pick.textContent()).trim(), want[lang].own, "the session's by default (cybercaius)");
        assert.equal(await row.locator(".sub").count(), 0, "the row says its name alone, its button the choice (1ooogle on X)");
        assert.deepEqual(await pick.locator(".ic").evaluate((e) => [e.offsetWidth, e.offsetHeight]), [14, 14]);
        // the chip stays inside its row, the row inside the window
        const fit = await row.evaluate((r) => {
          const b = r.querySelector("#codexPredictionsPick button").getBoundingClientRect(), rr = r.getBoundingClientRect();
          return { inRow: b.right <= rr.right + 0.5 && b.left >= rr.left - 0.5, inWindow: rr.right <= document.documentElement.clientWidth + 0.5 };
        });
        assert(fit.inRow && fit.inWindow, JSON.stringify(fit));

        // scrolled by a wheel till the row is mid-view, so a click that
        // moved the page would show
        const box = await page.locator("#view-agents").boundingBox();
        await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
        const top = () => pick.evaluate((e) => e.getBoundingClientRect().top);
        for (let i = 0; i < 80 && (await top()) > 200; i++) { await page.mouse.wheel(0, 100); await page.waitForTimeout(30); }
        const before = await view(page);
        assert(before > 0, "the agents list must scroll to the row");
        const pickItem = async (s) => {
          await pick.click();
          await page.locator("#pop:not([hidden]) #list li").first().waitFor();
          await page.locator("#list li:not(.group)").filter({ hasText: s }).first().click();
        };

        // the app's menu: the session's, Off and uni-switch's models
        await pick.click();
        await page.locator("#pop:not([hidden]) #list li").first().waitFor();
        const items = (await page.locator("#list li:not(.group)").allTextContents()).join("|");
        for (const s of [want[lang].own, want[lang].off, "DeepSeek V4 Flash", "cheap"]) assert(items.includes(s), `${s} in ${items}`);
        await page.keyboard.press("Escape");
        await page.locator("#pop:not([hidden])").waitFor({ state: "hidden" });

        // off: answered by uni-switch with no prediction
        await pickItem(want[lang].off);
        await shows(want[lang].off);
        assert.deepEqual(posted, [["codex-predictions", { model: "off" }]]);

        // a model of uni-switch's
        await pickItem("DeepSeek V4 Flash");
        await shows("DeepSeek V4 Flash");
        assert.deepEqual(posted.at(-1), ["codex-predictions", { model: "deepseek/deepseek-v4-flash" }]);
        for (const other of ["#codexTitlesPick button", "#codexDescriptionsPick button"])
          assert.equal((await page.locator(other).textContent()).trim(), want[lang].own, `${other} is its own setting`);
        await page.waitForTimeout(300);
        assert.equal(await view(page), before, "picking a model scrolled the page");
        assert.equal(await row.locator(".sub").count(), 0);

        // and back to the session's
        await pickItem(want[lang].own);
        await shows(want[lang].own);
        assert.deepEqual(posted.at(-1), ["codex-predictions", { model: "" }]);
        await page.waitForTimeout(300);
        assert.equal(await view(page), before, "the picks scrolled the page");
        assert(!posted.some(([k]) => k === "settings"), "set on its own, not with the other preferences");
        assert.deepEqual(errors, []);
      });
    }
  }
}

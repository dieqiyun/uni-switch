// Run with Node's test runner and Playwright on the module path; see README.md.
// Codex's sheet says each picked model with its provider, "Name · Provider",
// in every model row (nb5p on Discord): "Codex composer predictions" and
// "Codex auto-review model" did, from Settings' model list, while "Codex
// memories model" and "Codex subagent model", which are Codex's own fields
// and pick from its options, said the name alone ("GPT-6 Luna", no
// "· MyGateway"). A routing group is said by its name alone, and a value no
// option has as it is. Chromium and WebKit, every language, 440px and wide;
// no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, openAgentSettings } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
// Codex's field options as internal/agent builds them: a uni-switch model's
// group is its provider's name, a routing group's "Routing groups"
const opts = [
  { value: "gpt-5.5", label: "GPT-5.5", note: "OpenAI · via uni-switch", group: "OpenAI", ref: "openai/gpt-5.5", icon: "openai" },
  { value: "mygw/gpt-6-luna", label: "GPT-6 Luna", note: "MyGateway · via uni-switch", group: "MyGateway", ref: "mygw/gpt-6-luna", icon: "openai" },
  { value: "group/cheap", label: "cheap", note: "routing group · via uni-switch", group: "Routing groups", ref: "group/cheap" },
];
const agents = (memories, sub) => [{
  id: "codex", name: "Codex", path: "/test/config.toml", icon: "codex-color", wired: true,
  fields: [
    { key: "model", label: "model", value: "gpt-5.5", options: opts },
    { key: "subagent_model", label: "subagent model", value: sub, options: opts },
    { key: "memories", label: "memories", value: memories, options: opts },
  ],
}];
// Settings' list, as /api/settings has it
const titleModels = [{ id: "mygw/gpt-6-luna", name: "GPT-6 Luna", provider: "mygw", providerName: "MyGateway", icon: "openai" }];
const settings = (lang) => ({
  theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd",
  dock: false, dockWindow: false, proxy: "", redact: false, redactPersonal: false, redactWords: [],
  codexWarmup: "", claudeWarmup: "", codexWarmAt: "", claudeWarmAt: "", workbuddyCheckin: false, noStats: false,
  trayUsage: "", trayUsageEvery: 3, vision: "", imageGen: "",
  version: "0.1.400", dir: "~/.config/uni-switch", gateway: "http://127.0.0.1:3425",
  proxyNow: "none", proxySource: "none", login: false,
  visionModels: [], imageGenModels: [], workbuddyCheckins: [], lanURLs: [], titleModels,
  codexPredictions: "mygw/gpt-6-luna", codexAutoReview: "mygw/gpt-6-luna",
  fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
});

function server(lang, memories, sub) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: agents(memories, sub), profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/settings") return json(settings(lang));
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
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

// the rows' names as i18n.js has them
const W = {
  en: { mem: "Codex memories model", pred: "Codex composer predictions" },
  zh: { mem: "Codex 记忆整理模型", pred: "Codex 输入框预测" },
  "zh-TW": { mem: "Codex 記憶整理模型", pred: null },
  ja: { mem: "Codex メモリ整理のモデル", pred: null },
  de: { mem: "Codex-Modell für Erinnerungen", pred: null },
};

async function open(engine, width, lang, memories, sub, t) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  const context = await browser.newContext({ viewport: { width, height: 700 }, reducedMotion: "reduce" });
  const page = await context.newPage();
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/*", server(lang, memories, sub));
  t.after(async () => {
    if (process.env.ARTIFACT_DIR) {
      await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
      await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${width}-${lang}-codex-sheet-providers.png`) });
    }
    await browser.close();
  });
  await watchSettingsReads(page);
  await page.goto("http://uni-switch.test/?view=agents");
  await openAgentSettings(page);
  await page.locator("#codexMemoriesPick button").waitFor();
  return { page, errors };
}
const said = (page, id) => page.locator(`#${id} button`).evaluate((b) => b.textContent.trim());

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const width of [440, 1100]) {
    for (const lang of Object.keys(W)) {
      test(`${engine} ${width}px ${lang}: every model row in Codex's sheet names its provider`, async (t) => {
        const { page, errors } = await open(engine, width, lang, "mygw/gpt-6-luna", "mygw/gpt-6-luna", t);
        assert.equal((await page.locator("#codexMemoriesRow .name").textContent()).trim(), W[lang].mem);
        if (W[lang].pred) assert.equal((await page.locator("#codexPredictionsRow .name").textContent()).trim(), W[lang].pred);
        for (const id of ["codexPredictionsPick", "codexAutoReviewPick", "codexMemoriesPick", "codexSubagentModelPick"])
          assert.equal(await said(page, id), "GPT-6 Luna · MyGateway", id);
        // each row's name and button fit it, the page no wider than the window
        const fit = await page.evaluate(() => ["#codexMemoriesRow", "#codexSubagentModelRow"].every((s) => {
          const r = document.querySelector(s), box = r.getBoundingClientRect();
          return [...r.querySelectorAll(".name, button")].every((e) => {
            const b = e.getBoundingClientRect();
            return b.width > 0 && b.left >= box.left - 1 && b.right <= box.right + 1;
          });
        }) && document.documentElement.scrollWidth <= innerWidth);
        assert(fit, "a row overflows");
        assert.deepEqual(errors, []);
      });
    }
  }

  test(`${engine}: a routing group by its name alone, a value with no option as it is`, async (t) => {
    const { page, errors } = await open(engine, 900, "en", "group/cheap", "gone/old-model", t);
    assert.equal(await said(page, "codexMemoriesPick"), "cheap");
    assert.equal(await said(page, "codexSubagentModelPick"), "gone/old-model");
    assert.deepEqual(errors, []);
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// Claude Desktop's connection check names its model itself, the first
// model listed to it when none is Claude-named, while uni-switch sends it to
// the model chosen in Desktop's menu. Its row in the Routing page's
// Requests list says it is the check, so "requested deepseek-flash" with
// gpt-6.1-sol answering reads as it is (cybercaius on Discord: 对话里选的
// 是 Anyrouter 的 gpt-6.1-sol，路由页显示 Claude Desktop 请求的是
// DeepSeek 的 deepseek-flash).
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const at = (i) => new Date(now.getTime() - (i + 1) * 60e3).toISOString();
const seat = { id: "anyrouter", provider: "anyrouter", name: "AnyRouter", who: "API key", kind: "key", model: "gpt-6.1-sol" };
const rows = [
  { kind: "connection_check", max: 1, status: 400 },
  { kind: "", max: 32000, status: 200 },
];
const routes = rows.map((x, i) => ({
  id: 100 - i, seq: 100 - i, time: at(i), agent: "claude-desktop", model: "deepseek/deepseek-flash", provider: "anyrouter", ...(x.kind ? { kind: x.kind } : {}),
  order: [seat], tries: [{ id: seat.id, model: "gpt-6.1-sol", start: at(i), done: true, status: x.status, ms: 900 }],
  done: true, status: x.status, ms: 900, tokens: 12,
}));

function serve(lang) {
  const state = { agents: [{ id: "claude-desktop", name: "Claude Desktop", path: "/test/claude_desktop_config.json", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) await new Promise((r) => setTimeout(r, 20e3));
      return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname === "/api/gateway/history") {
      const d = url.searchParams.get("day");
      return json({ cut: false, days: [{ day, requests: routes.length }], routes: d ? routes : [] });
    }
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const want = {
  en: { tag: "Connection check", story: /names this check's model itself.*\(deepseek\/deepseek-flash\), not the model chosen in the conversation/ },
  zh: { tag: "连接检查", story: /按模型列表里的名字选出（deepseek\/deepseek-flash），不是对话里选的模型/ },
  "zh-TW": { tag: "連線檢查", story: /不是對話裡選的模型/ },
  ja: { tag: "接続チェック", story: /会話で選んだモデルではなく/ },
  de: { tag: "Verbindungstest", story: /nicht das im Gespräch gewählte/ },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(want)) {
    test(`${engine} ${lang}: Claude Desktop's connection check is tagged and says whose model it names`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const context = await browser.newContext({ viewport: { width: 1100, height: 860 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-desktop-check.png`), fullPage: true });
        }
        await browser.close();
      });
      await page.goto("http://uni-switch.test/?view=routing");
      await page.locator(".rt-days .rt-day").nth(1).click();
      await page.locator(".rt-req").nth(rows.length - 1).waitFor();

      const tags = await page.locator(".rt-req").evaluateAll((rs) => rs.map((r) => r.querySelector(".asked .kind")?.textContent || ""));
      assert.deepEqual(tags, [want[lang].tag, ""]);
      await page.locator(".rt-req").nth(0).click();
      const story = page.locator(".rt-steps li.kind");
      await story.waitFor();
      assert.equal(await story.locator(".kind").textContent(), want[lang].tag);
      assert.match(await story.textContent(), want[lang].story);
      // a turn of the conversation has no tag in its story
      await page.locator(".rt-req").nth(1).click();
      await page.waitForTimeout(300);
      assert.equal(await page.locator(".rt-steps li.kind").count(), 0);
      assert.deepEqual(errors, []);
    });
  }
}

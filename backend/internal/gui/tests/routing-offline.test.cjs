// Run with Node's test runner and Playwright on the module path; see README.md.
// With this computer off the network (usrname1833 on Discord), a group
// member that couldn't connect hands the request to the next and doesn't
// rest: the Routing page's story says the computer had no network and that
// nothing is wrong with the member, in the page's language, rather than
// "an error another account wouldn't fix".
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const at = (i) => new Date(now.getTime() - (i + 1) * 60e3).toISOString();
const deepseek = { id: "deepseek", provider: "deepseek", name: "DeepSeek", kind: "provider", model: "deepseek-chat" };
const kimi = { id: "kimi", provider: "kimi", name: "Kimi", kind: "provider", model: "kimi-k2" };
const err = (p, host) => `${p}: Post "https://${host}/v1/chat/completions": dial tcp: lookup ${host}: no such host`;
const routes = [{
  id: 100, seq: 100, time: at(0), agent: "codex", model: "group/pair", provider: "kimi", order: [deepseek, kimi],
  tries: [
    { id: deepseek.id, model: deepseek.model, start: at(0), done: true, status: 502, ms: 3, fail: "offline", error: err("DeepSeek", "api.deepseek.com") },
    { id: kimi.id, model: kimi.model, start: at(0), done: true, status: 502, ms: 3, fail: "offline", error: err("Kimi", "api.moonshot.cn") },
  ],
  done: true, status: 502, ms: 10, error: err("Kimi", "api.moonshot.cn"),
}];

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

// what the first member's story, the last's, and the tag say in each language
const want = {
  en: { next: "this computer had no network, so the request never reached the vendor and goes on to the next", last: "nobody is left to try", tag: "502 · network down" },
  zh: { next: "这台电脑没有网络，请求未到达厂商，已转给下一个", last: "且没有其他可尝试的", tag: "502 · 没有网络" },
  "zh-TW": { next: "這台電腦沒有網路，請求未到達廠商，已轉給下一個", last: "且沒有其他可嘗試的", tag: "502 · 沒有網路" },
  ja: { next: "このコンピュータがネットワークにつながっていなかったため、リクエストはベンダーに届かず次へ進みます", last: "ほかに試せる相手もいない", tag: "502 · ネットワークなし" },
  de: { next: "Dieser Computer hatte kein Netzwerk, daher erreichte die Anfrage den Anbieter nie und geht an den nächsten", last: "niemand sonst ist übrig", tag: "502 · kein Netzwerk" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(want)) {
    test(`${engine} ${lang}: a member that couldn't reach the network doesn't rest`, async (t) => {
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
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-offline.png`), fullPage: true });
        }
        await browser.close();
      });
      await page.goto("http://uni-switch.test/?view=routing");
      await page.locator(".rt-days .rt-day").nth(1).click();
      await page.locator(".rt-req").first().waitFor();
      await page.locator(".rt-req").first().click();
      await page.locator(".rt-steps li").first().waitFor();
      const steps = await page.locator(".rt-steps li").allTextContents();
      const first = steps.find((s) => s.includes(want[lang].next) && s.includes("DeepSeek"));
      assert(first, JSON.stringify(steps));
      const last = steps.find((s) => s.includes(want[lang].last) && s.includes("Kimi"));
      assert(last, JSON.stringify(steps));
      assert(!steps.some((s) => s.includes("wouldn't fix") || s.includes("换账号也无法解决")), JSON.stringify(steps));
      const text = await page.locator("body").innerText();
      assert(text.includes(want[lang].tag), "no tag " + want[lang].tag);
      assert.deepEqual(errors, []);
    });
  }
}

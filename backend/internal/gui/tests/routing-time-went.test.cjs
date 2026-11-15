// Run with Node's test runner and Playwright on the module path; see README.md.
// Where a try's time went (gateway.TryNet; Discord, Magic_zero: the gateway
// looked slow): a request's story says, under each try, uni-switch's time, the
// network's and the vendor's, and each part that took any, as one stacked
// bar and named values, the queue for a slot among them. A part that took
// none isn't named. A try sent through a plugin's own fetch, or traced
// before uni-switch measured it, has no network timing and no breakdown; the
// plugin's says it sent the request itself, so the missing split doesn't
// read as lost (Discord, lute). Both engines, a narrow window, every language; no
// part runs out of the story, no left-border accent. The API is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const at = (i) => new Date(now.getTime() - (i + 1) * 60e3).toISOString();
const key = { id: "deepseek", provider: "deepseek", name: "DeepSeek", kind: "provider", model: "deepseek-chat" };
const plug = { id: "kiro", provider: "kiro", name: "Kiro", kind: "provider", model: "claude-sonnet-4.5" };
// a new connection, queued 40 ms for a slot, the vendor 1.8 s before its
// headers; reused connections spend no DNS, connect or TLS
const net = { own: 2.4, asks: 1, dns: 3.1, connect: 21.5, tls: 48.2, send: 0.6, wait: 1800, head: 1916 };
const routes = [
  { id: 101, seq: 101, time: at(0), agent: "codex", model: "deepseek/deepseek-chat", provider: "deepseek", order: [key],
    tries: [{ id: key.id, model: "deepseek-chat", start: at(0), done: true, status: 200, ms: 4200, ttft: 2100, queued: 40, net }],
    done: true, status: 200, ms: 4200, ttft: 2100, tokens: 900, served: "deepseek-chat" },
  { id: 100, seq: 100, time: at(1), agent: "codex", model: "kiro/claude-sonnet-4.5", provider: "kiro", order: [plug],
    tries: [{ id: plug.id, model: "claude-sonnet-4.5", start: at(1), done: true, status: 200, ms: 3000, ttft: 1200, sentBy: "plugin" }],
    done: true, status: 200, ms: 3000, ttft: 1200, tokens: 900, served: "claude-sonnet-4.5" },
];

function serve(lang) {
  const state = { agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
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
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/requests") return json({ period: url.searchParams.get("period"), rows: [], offset: 0, total: 0, calls: 0, errors: 0, input: 600, output: 80, cache_read: 0, cache_write: 0, reasoning: 0, cost: 0, unpriced: 2, agents: [{ id: "codex", name: "Codex", icon: "codex-color" }] });
    if (url.pathname === "/api/usage/requests/content") return json({ found: false, why: "read" });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/usage") return json({ calls: 2, errors: 0, input: 600, output: 80, cache_read: 0, cache_write: 0, reasoning: 0, unpriced: 2, cost: 0, bucket: "day", series: [], agents: [], models: [], path: "~/.config/uni-switch/usage.jsonl" });
    if (url.pathname === "/api/sessions") return json({ sessions: [], dirs: [] });
    if (url.pathname === "/api/sessions/stats") return json({ from: "", to: "", days: [], agents: {} });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const want = {
  en: { head: "Where the time went", vendor: "Vendor (first byte)", slot: "Waiting for a slot", plugin: "Its plugin sent this request itself, so uni-switch can't split its time" },
  zh: { head: "时间花在哪里", vendor: "供应商（首字节）", slot: "排队等空位", plugin: "此请求由插件自行发出，uni-switch 无法拆分它的耗时" },
  "zh-TW": { head: "時間花在哪裡", vendor: "供應商（首位元組）", slot: "排隊等空位", plugin: "此請求由外掛自行發出，uni-switch 無法拆分它的耗時" },
  ja: { head: "時間の内訳", vendor: "ベンダー（最初のバイト）", slot: "空き枠待ち", plugin: "このリクエストはプラグインが自ら送信したため、uni-switch では時間の内訳を出せません" },
  de: { head: "Wohin die Zeit ging", vendor: "Anbieter (erstes Byte)", slot: "Warten auf einen freien Platz", plugin: "Diese Anfrage hat die Erweiterung selbst gesendet, daher kann uni-switch ihre Zeit nicht aufschlüsseln" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(want)) {
    test(`${engine} ${lang}: a try says where its time went`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const context = await browser.newContext({ viewport: { width: 440, height: 900 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-time-went.png`), fullPage: true });
        }
        await browser.close();
      });
      await page.goto("http://uni-switch.test/?view=routing");
      await page.locator(".rt-day").nth(1).waitFor({ state: "visible", timeout: 15000 });
      await page.locator(".rt-day").nth(1).click();
      await page.locator(".rt-req").nth(routes.length - 1).waitFor();
      await page.locator(".rt-req").nth(0).click();
      const box = page.locator(".rt-steps li.time .rt-time");
      await box.waitFor();
      assert.equal(await box.count(), 1);
      const seen = await box.evaluate((b) => {
        const story = b.closest(".rt-steps").getBoundingClientRect();
        const out = [...b.querySelectorAll("*")].filter((e) => {
          const r = e.getBoundingClientRect();
          return r.width > 0 && (r.left < story.left - 0.5 || r.right > story.right + 0.5);
        }).map((e) => e.className);
        const segs = [...b.querySelectorAll(".rt-time-bar span")].map((s) => ({ who: s.className, w: s.getBoundingClientRect().width }));
        return {
          head: b.querySelector(".rt-time-head").firstChild.textContent,
          who: [...b.querySelectorAll(".rt-time-who")].map((e) => e.className.split(" ")[1]),
          parts: [...b.querySelectorAll(".rt-time-parts li")].map((li) => [li.className, li.querySelector(".k").textContent, li.querySelector(".v").textContent]),
          segs, out, border: [...b.querySelectorAll("*")].map((e) => getComputedStyle(e).borderLeftWidth).filter((w) => w !== "0px"),
          barW: b.querySelector(".rt-time-bar").getBoundingClientRect().width,
        };
      });
      assert.equal(seen.head, want[lang].head);
      assert.deepEqual(seen.who, ["uni-switch", "net", "vendor"]);
      // queue, own, DNS, connect, TLS, send, the vendor's wait, first
      // token, streaming: no pool, no requests before it
      assert.deepEqual(seen.parts.map((p) => p[0]), ["uni-switch", "uni-switch", "net", "net", "net", "net", "vendor", "vendor", "vendor"]);
      assert.equal(seen.parts[0][1], want[lang].slot);
      assert.equal(seen.parts[6][1], want[lang].vendor);
      assert.match(seen.parts[0][2], /40/);
      assert.match(seen.parts[1][2], /2\.4/);
      assert.match(seen.parts[6][2], /1\.8/);
      assert.equal(seen.segs.length, 9);
      // the bar is to scale: the vendor's wait is the widest
      const widest = seen.segs.reduce((a, s) => (s.w > a.w ? s : a));
      assert.equal(widest.who, "vendor");
      assert(seen.segs[6].w > seen.barW * 0.3, JSON.stringify(seen));
      assert.deepEqual(seen.out, [], "out of the story");
      assert.deepEqual(seen.border, []);
      if (process.env.ARTIFACT_DIR) await page.locator(".rt-steps").screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-time-went-story.png`) });

      // the plugin's try: no network timing, no breakdown, the try's own
      // line still there
      await page.locator(".rt-req").nth(1).click();
      await page.locator(".rt-req").nth(1).and(page.locator('[aria-pressed="true"]')).waitFor();
      await page.locator(".rt-steps li.ok").first().waitFor();
      assert.equal(await page.locator(".rt-time").count(), 0);
      const why = page.locator(".rt-steps li.aside", { hasText: want[lang].plugin });
      await why.waitFor();
      assert.equal(await why.count(), 1);
      // and the routed try, which has its split, says nothing of the kind
      await page.locator(".rt-req").nth(0).click();
      await page.locator(".rt-steps li.time .rt-time").waitFor();
      assert.equal(await page.locator(".rt-steps li.aside", { hasText: want[lang].plugin }).count(), 0);
      assert.deepEqual(errors, []);
    });
  }
}

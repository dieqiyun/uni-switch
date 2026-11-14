// Run with Node's test runner and Playwright on the module path; see README.md.
// A provider's decision address field (Outvi V on Discord): only Jev's own
// presets (TypeSafe, Vercel, Cloudflare) call it Jev's endpoint and give
// Cloudflare's …/accounts/<account id>/ai/run as the example. A provider
// that also serves conversations and asks its own decision models on
// System One (OpenRouter, OpenCode Zen, Ollama Cloud, Bailian's Token Plan,
// Ollama, oMLX, MLX-Serve) shows System One's title and a hint of its own,
// added or edited, in every language. The API is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

// the presets with a decision API, as internal/provider/presets.go gives them
const presets = [
  { id: "openrouter", name: "OpenRouter", icon: "openrouter", kind: "relay", chat: "https://openrouter.ai/api/v1", anthropic: "https://openrouter.ai/api", decide: "https://openrouter.ai/api/v1", added: false },
  { id: "opencode-zen", name: "OpenCode Zen", icon: "opencode", kind: "relay", noKey: true, chat: "https://opencode.ai/zen/v1", responses: "https://opencode.ai/zen/v1", anthropic: "https://opencode.ai/zen", decide: "https://opencode.ai/zen/v1", added: false },
  { id: "ollama-cloud", name: "Ollama Cloud", icon: "ollama", kind: "vendor", chat: "https://ollama.com/v1", anthropic: "https://ollama.com", decide: "https://ollama.com/v1", added: false },
  { id: "bailian-token-plan", name: "Bailian Token Plan", icon: "bailian-color", kind: "vendor", chat: "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", anthropic: "https://token-plan.cn-beijing.maas.aliyuncs.com/apps/anthropic", decide: "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", added: false },
  { id: "ollama", name: "Ollama", icon: "ollama", kind: "local", noKey: true, chat: "http://localhost:11434/v1", anthropic: "http://localhost:11434", decide: "http://localhost:11434/v1", added: false },
  { id: "omlx", name: "oMLX", icon: "omlx", kind: "local", noKey: true, chat: "http://localhost:8000/v1", responses: "http://localhost:8000/v1", anthropic: "http://localhost:8000", decide: "http://localhost:8000/v1", added: false },
  { id: "mlx-serve", name: "MLX-Serve", icon: "mlx-serve", kind: "local", noKey: true, chat: "http://localhost:11234/v1", responses: "http://localhost:11234/v1", anthropic: "http://localhost:11234", decide: "http://localhost:11234/v1", added: false },
  { id: "typesafe", name: "TypeSafe Jev", icon: "typesafe", kind: "vendor", decide: "https://api.typesafe.ai/v1", note: "routes groups · picks model and effort", added: false },
  { id: "vercel-jev", name: "Jev · Vercel AI Gateway", icon: "vercel", kind: "relay", decide: "https://ai-gateway.vercel.sh/typesafe", note: "routes groups · picks model and effort", added: false },
  { id: "cloudflare-jev", name: "Cloudflare Workers AI · Jev, Clef", icon: "cloudflare-color", kind: "relay", decide: "https://api.cloudflare.com/client/v4", note: "routes groups · picks model and effort", added: false },
];
const jevs = ["TypeSafe Jev", "Jev · Vercel AI Gateway", "Cloudflare Workers AI · Jev, Clef"];

const providers = [{ id: "openrouter", name: "OpenRouter", icon: "openrouter", preset: "openrouter",
  chat: "https://openrouter.ai/api/v1", anthropic: "https://openrouter.ai/api", decide: "https://openrouter.ai/api/v1",
  models: [], agents: [], key: { set: true, masked: "sk-…ab12" } }];

function server(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    const all = { providers, presets, excluded: [], gateway: { running: true, window: true } };
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json(all);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

// each language's two titles, and a word of each hint
const L = {
  en: { jev: "Jev endpoint", one: "System One endpoint", own: "The address its decision models are asked at, POST …/systemone under it" },
  zh: { jev: "Jev 端点", one: "System One 端点", own: "该供应商决策模型的请求地址，在其下 POST …/systemone" },
  "zh-TW": { jev: "Jev 端點", one: "System One 端點", own: "該供應商決策模型的請求位址，在其下 POST …/systemone" },
  ja: { jev: "Jev エンドポイント", one: "System One エンドポイント", own: "このプロバイダーの判定モデルに問い合わせるアドレスで、その下の POST …/systemone を使います" },
  de: { jev: "Jev-Endpunkt", one: "System-One-Endpunkt", own: "Die Adresse, unter der die Entscheidungsmodelle dieses Anbieters abgefragt werden, darunter POST …/systemone" },
};

// the decision address field's title and hint, by its input
const decideField = (ed) => ed.locator("input.decide-url").evaluate((input) => {
  const wrap = input.closest("div");
  return { label: wrap.previousElementSibling?.textContent || "", hint: wrap.querySelector(".hint")?.textContent || "" };
});

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": only Jev's presets call the decision address Jev's endpoint", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of Object.keys(L)) {
      await t.test(lang, async () => {
        const w = L[lang];
        const page = await (await browser.newContext({ viewport: { width: 900, height: 760 } })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang));
        const sheet = page.locator("#addSheet");
        const ed = page.locator(".editor.new");

        for (const { name } of presets) {
          await page.goto("http://uni-switch.test/?view=providers");
          await page.locator("#addProvider").click();
          await sheet.locator(".tile .n", { hasText: new RegExp("^" + name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + "$") }).click();
          await ed.locator(".ehead b", { hasText: name }).waitFor();
          const got = await decideField(ed);
          if (jevs.includes(name)) {
            assert.equal(got.label, w.jev, name);
            assert.match(got.hint, /Cloudflare/, name);
          } else {
            assert.equal(got.label, w.one, name);
            assert.equal(got.hint, w.own, name);
            assert.doesNotMatch(got.hint, /Cloudflare|Jev/, name);
          }
        }

        // editing a saved OpenRouter shows the same
        const saved = page.locator(".editor");
        await page.goto("http://uni-switch.test/?view=providers");
        await page.locator('#providers .row[data-id="openrouter"]').click();
        await saved.locator(".ehead b", { hasText: "OpenRouter" }).waitFor();
        const got = await decideField(saved);
        assert.equal(got.label, w.one);
        assert.equal(got.hint, w.own);
        assert.equal(await saved.locator("input.decide-url").getAttribute("placeholder"), "https://openrouter.ai/api/v1");
        assert.deepEqual(errors, []);
        await page.context().close();
      });
    }
  });
}

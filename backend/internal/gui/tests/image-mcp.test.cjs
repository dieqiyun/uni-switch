// Run with Node's test runner and Playwright on the module path; see README.md.
// uni-switch's image generation as an MCP server (JonahFu on X): Settings ›
// Models has an "Image generation MCP server" row, after image
// recognition's, with the gateway's address of it, /mcp/uni-switch/image, to
// copy into any agent, and says what its tools give back and that a
// gateway key goes with it from another computer. In every language, in
// both engines, wide and narrow.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const address = "http://127.0.0.1:3999/mcp/uni-switch/image";
const tools = /generate_image.*generate_video.*generation_result.*Authorization: Bearer <key>/;
const words = {
  en: { name: "Image generation MCP server", sub: tools },
  zh: { name: "生图 MCP 服务器", sub: tools },
  "zh-TW": { name: "生圖 MCP 伺服器", sub: tools },
  ja: { name: "画像生成 MCP サーバー", sub: tools },
  de: { name: "Bilderzeugung als MCP-Server", sub: tools },
};

function serve(lang) {
  const settings = { lang, theme: "light", gateway: "http://127.0.0.1:3999", port: 3999, vision: "", visionAuto: "codex/gpt-6.1-sol",
    visionModels: [{ id: "codex/gpt-6.1-sol", name: "GPT-6.1 Sol", provider: "codex", providerName: "Codex", icon: "openai" }],
    imageGen: "", imageGenAuto: "codex/gpt-image-2", imageGenModels: [{ id: "codex/gpt-image-2", name: "GPT Image 2", provider: "codex", providerName: "Codex", icon: "openai" }] };
  const state = { agents: [], profiles: [], settings, clients: [] };
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) await new Promise((res) => setTimeout(res, 20e3));
      return json({ mine: true, now: new Date().toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname === "/api/groups") return json({ models: [], groups: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await r.fulfill({ body: await fs.readFile(file), contentType }); } catch { await r.fulfill({ status: 404, body: "" }); }
  };
}

async function open(t, engine, lang, view, width = 1100) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  t.after(() => browser.close());
  const page = await (await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" })).newPage();
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/*", serve(lang));
  await page.goto("http://uni-switch.test/?view=" + view);
  return { page, errors };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(words)) {
    const w = words[lang];
    test(`${engine} ${lang}: Image generation gives its MCP server's address`, async (t) => {
      for (const width of [1100, 520]) {
        const { page, errors } = await open(t, engine, lang, "settings&tab=models", width);
        // Settings is drawn again as its answers come in, each time with a
        // new row: it is read in one go, never held across a redraw
        await page.waitForFunction((a) => document.querySelector("#imagesList #imageMcpRow code")?.textContent === a, address);
        const row = await page.evaluate(() => {
          const r = document.querySelector("#imagesList #imageMcpRow");
          r.scrollIntoView({ block: "nearest" });
          return { name: r.querySelector(".name").textContent, sub: r.querySelector(".sub").textContent,
            code: r.querySelector("code").textContent, buttons: r.querySelectorAll("button").length,
            after: r.previousElementSibling?.id };
        });
        assert.equal(row.name, w.name);
        assert.match(row.sub, w.sub);
        assert.equal(row.code, address);
        assert.equal(row.buttons, 1, "a copy button");
        assert.equal(row.after, "visionMcpRow", "beside image recognition's server");
        // nothing spills out of the window at a narrow width
        assert(await page.evaluate(() => document.scrollingElement.scrollWidth <= innerWidth + 1), `no sideways scroll at ${width}px`);
        assert.deepEqual(errors, []);
      }
    });
  }
}

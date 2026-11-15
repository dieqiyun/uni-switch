// Run with Node's test runner and Playwright on the module path; see README.md.
// uni-switch's image recognition as an MCP server (JonahFu on X): Settings ›
// Models › Image recognition has an "MCP server" row with the gateway's
// address of it, /mcp/uni-switch/vision, to copy into any agent, and says a
// gateway key, and the image as a URL, go with it from another computer.
// An image an agent asked it about reads in Routing as that agent's MCP
// call, not as a description for one of uni-switch's models. In every language.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const address = "http://127.0.0.1:3999/mcp/uni-switch/vision";
const words = {
  en: { name: "Image recognition MCP server", sub: /analyze_image.*Authorization: Bearer <key>.*data URL/, story: /Claude Code called uni-switch's analyze_image MCP tool: codex\/gpt-6\.1-sol looked at the image for it/ },
  zh: { name: "识图 MCP 服务器", sub: /analyze_image.*Authorization: Bearer <key>.*data URL/, story: /Claude Code 调用了 uni-switch 的 analyze_image MCP 工具：由 codex\/gpt-6\.1-sol 代为查看图片/ },
  "zh-TW": { name: "識圖 MCP 伺服器", sub: /analyze_image.*Authorization: Bearer <key>.*data URL/, story: /Claude Code 呼叫了 uni-switch 的 analyze_image MCP 工具：由 codex\/gpt-6\.1-sol 代為查看圖片/ },
  ja: { name: "画像認識 MCP サーバー", sub: /analyze_image.*Authorization: Bearer <key>.*data URL/, story: /Claude Code が uni-switch の analyze_image MCP ツールを呼び出しました。codex\/gpt-6\.1-sol が代わりに画像を見ました/ },
  de: { name: "Bilderkennung als MCP-Server", sub: /analyze_image.*Authorization: Bearer <key>.*Data-URL/, story: /Claude Code hat das analyze_image-MCP-Tool von uni-switch aufgerufen: codex\/gpt-6\.1-sol hat sich das Bild dafür angesehen/ },
};

const now = new Date();
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const routes = [{
  id: 7, seq: 7, time: new Date(now.getTime() - 60e3).toISOString(), agent: "uni-switch", model: "codex/gpt-6.1-sol", provider: "codex",
  kind: "vision", for: { agent: "claude-code", mcp: true },
  order: [{ id: "codex", provider: "codex", name: "Codex", who: "a@b.c", kind: "account", model: "gpt-6.1-sol" }],
  tries: [{ id: "codex", model: "gpt-6.1-sol", start: new Date(now.getTime() - 60e3).toISOString(), done: true, status: 200, ms: 900 }],
  done: true, status: 200, ms: 900, tokens: 1200,
}];

function serve(lang) {
  const settings = { lang, theme: "light", gateway: "http://127.0.0.1:3999", port: 3999, vision: "", visionAuto: "codex/gpt-6.1-sol",
    visionModels: [{ id: "codex/gpt-6.1-sol", name: "GPT-6.1 Sol", provider: "codex", providerName: "Codex", icon: "openai" }] };
  const state = {
    agents: [], profiles: [], settings,
    clients: [{ id: "claude-code", name: "Claude Code", icon: "claude-color" }, { id: "uni-switch", name: "uni-switch", icon: "uni-switch" }],
  };
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
      return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname === "/api/gateway/history") {
      return json({ cut: false, days: [{ day, requests: routes.length }], routes: url.searchParams.get("day") ? routes : [] });
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
    test(`${engine} ${lang}: Image recognition gives its MCP server's address`, async (t) => {
      for (const width of [1100, 520]) {
        const { page, errors } = await open(t, engine, lang, "settings&tab=models", width);
        // Settings is drawn again as its answers come in, each time with a
        // new row: it is read in one go, never held across a redraw
        await page.waitForFunction((a) => document.querySelector("#imagesList #visionMcpRow code")?.textContent === a, address);
        const row = await page.evaluate(() => {
          const r = document.querySelector("#imagesList #visionMcpRow");
          r.scrollIntoView({ block: "nearest" });
          return { name: r.querySelector(".name").textContent, sub: r.querySelector(".sub").textContent,
            code: r.querySelector("code").textContent, buttons: r.querySelectorAll("button").length };
        });
        assert.equal(row.name, w.name);
        assert.match(row.sub, w.sub);
        assert.equal(row.code, address);
        assert.equal(row.buttons, 1, "a copy button");
        // nothing spills out of the window at a narrow width
        assert(await page.evaluate(() => document.scrollingElement.scrollWidth <= innerWidth + 1), `no sideways scroll at ${width}px`);
        assert.deepEqual(errors, []);
      }
    });

    test(`${engine} ${lang}: an agent's image through the MCP server reads as its own call`, async (t) => {
      const { page, errors } = await open(t, engine, lang, "routing");
      await page.locator(".rt-days .rt-day").nth(1).click();
      const req = page.locator(".rt-req").first();
      await req.waitFor();
      await req.click();
      const story = page.locator(".rt-steps li.kind");
      await story.waitFor();
      assert.match(await story.textContent(), w.story);
      assert.deepEqual(errors, []);
    });
  }
}

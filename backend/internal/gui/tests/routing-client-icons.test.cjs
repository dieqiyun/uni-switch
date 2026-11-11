// Run with Node's test runner and Playwright on the module path; see README.md.
// The Routing page's request list draws each request's client by its own
// icon and name, from /api/state's clients (agent.Clients): Kimi Code and
// Kimi Code Desktop showed the cube and their raw ids, kimi-code-cli and
// kimi-code-desktop (jiabubu on Discord). Kimi Code Desktop's logo is a
// PNG, which icon() draws only when app.js's pngIcons names it; one it
// doesn't is asked for as an SVG mask and falls back to the cube.
// TestEveryClientHasNameAndIcon holds the Go table to the files.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const at = (i) => new Date(now.getTime() - (i + 1) * 60e3).toISOString();
// as agent.Clients() gives them
const clients = [
  { id: "kimi", name: "Kimi Code", icon: "kimi" },
  { id: "kimi-code-desktop", name: "Kimi Code Desktop", icon: "kimi-code-desktop" },
  { id: "workbuddy", name: "WorkBuddy", icon: "workbuddy-color" },
];
const routes = clients.map((c, i) => ({
  id: 100 + i, seq: 100 + i, time: at(i), agent: c.id, model: "kimi-k3", provider: "kimi-code",
  order: [], tries: [], done: true, status: 200, ms: 900, tokens: 100,
}));

function serve(lang) {
  const state = { agents: [{ id: "kimi", name: "Kimi Code", icon: "kimi", path: "/test/config.toml", fields: [] }], clients, profiles: [], settings: { lang, theme: "light" } };
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
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
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

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a request's client is drawn by its own icon and name`, async (t) => {
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
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-client-icons.png`) });
        }
        await browser.close();
      });
      await page.goto("http://uni-switch.test/?view=routing");
      await page.locator(".rt-days .rt-day").nth(1).click();
      await page.locator(".rt-req").nth(clients.length - 1).waitFor();
      // every picture asked for has come, or failed
      await page.waitForFunction(() => [...document.querySelectorAll(".rt-req .asked .ic img")].every((i) => i.complete));

      const rows = await page.locator(".rt-req").evaluateAll((bs) => bs.map((b) => {
        const ic = b.querySelector(".asked .ic"), img = ic?.querySelector("img");
        return { title: b.title, icon: ic?.dataset.icon, generic: !!ic?.classList.contains("generic") || ic?.dataset.icon === "generic", src: img?.getAttribute("src") || "", loaded: !!img && img.naturalWidth > 0 };
      }));
      for (const c of clients) {
        const r = rows.find((x) => x.title.startsWith(c.name + " · "));
        assert(r, `${c.name}: ${JSON.stringify(rows)}`);
        assert.equal(r.icon, c.icon, JSON.stringify(r));
        assert(!r.generic, JSON.stringify(r));
      }
      // its own logo, the PNG, drawn
      const desk = rows.find((x) => x.title.startsWith("Kimi Code Desktop · "));
      assert.equal(desk.src, "icons/kimi-code-desktop.png", JSON.stringify(desk));
      assert(desk.loaded, JSON.stringify(desk));
      // no raw client id stands for a name
      assert(!rows.some((x) => /^kimi-code-(cli|desktop) · /.test(x.title)), JSON.stringify(rows));
      assert.deepEqual(errors, []);
    });
  }
}

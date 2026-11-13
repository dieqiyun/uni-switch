const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const today = new Date().toISOString().slice(0, 10);
const old = "2026-01-01T12:00:00Z";
const session = { agent: "opencode", id: "old-chat", name: "OpenCode", title: "Historical conversation", cwd: "/work", start: old, last: old, models: [], input: 0, output: 0, cost: 0, resume: "", path: "/test/opencode.db#old-chat" };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const [lang, all, width] of [["en", "All", 900], ["zh", "全部", 900], ["zh-TW", "全部", 900], ["ja", "すべて", 900], ["de", "Alle", 420]]) {
    test(`${engine} ${lang} ${width}px: empty Today offers existing history without changing the agent`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await browser.newPage({ viewport: { width, height: 760 } });
      page.setDefaultTimeout(8000);
      page.setDefaultNavigationTimeout(60000);
      await page.route("**/*", async (route) => {
        const url = new URL(route.request().url());
        const json = (data) => route.fulfill({ json: data });
        if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs={lang:"${lang}",theme:"light",web:true};` });
        if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window={};" });
        if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
        if (url.pathname === "/api/sessions") return json({ sessions: [session, { ...session, agent: "codex", id: "other", name: "Codex", title: "Other conversation" }], dirs: [] });
        if (url.pathname === "/api/sessions/stats") return json({ from: url.searchParams.get("days") === "0" ? "2026-01-01" : today, to: today, days: [], agents: { opencode: "OpenCode" } });
        if (url.pathname === "/api/sessions/overview") return json({ count: url.searchParams.get("days") === "0" ? 1 : 0, days: [], top: { tokens: [], cost: [], active: [] } });
        if (url.pathname === "/api/usage/quotas") return json([]);
        if (url.pathname.startsWith("/api/")) return json({});
        const file = url.pathname === "/app.js" && process.env.UNI_SWITCH_TEST_APP_JS ? process.env.UNI_SWITCH_TEST_APP_JS : path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
        const body = await fs.readFile(file).catch(() => null);
        const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml" }[path.extname(file)];
        return body ? route.fulfill({ body, contentType }) : route.fulfill({ status: 404, body: "" });
      });
      await page.addInitScript(() => { localStorage.setItem("uni-switch.usageTab", "sessions"); localStorage.setItem("uni-switch.sessRange", "today"); });
      await page.goto("http://uni-switch.test/?view=usage");
      // the agent filter is a menu (as the model and folder ones)
      await page.locator("#sessAgent").click();
      await page.locator('.sess-menu .pm-item[title="opencode"]').click();
      await page.locator("#sessStats .sess-show-history").click();
      await page.getByText("Historical conversation", { exact: true }).waitFor();
      assert.equal(await page.locator("#sessAgent").innerText(), "OpenCode");
      assert.match(await page.locator("#sessAgent").getAttribute("class"), /\bset\b/);
      assert.equal(await page.locator("#sessRange .opt.on").innerText(), all);
    });
  }
}

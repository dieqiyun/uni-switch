// Run with Node's test runner and Playwright on the module path; see README.md.
// Cursor Private Inference (#299) is connected as other agents are, with no
// command to run (mamba on Discord: a launch command of its own was out of
// keeping with the rest): its row has the switch, which asks uni-switch to set
// the build's CURSOR_LOCAL_AGENT_* variables for the user, and says to open
// it again; the command that starts it from a shell stays, as the square
// beside it, not the row's one control. In English and Chinese. No backend:
// the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const LAUNCH = "CURSOR_LOCAL_AGENT_BASE_URL=http://127.0.0.1:3425/v1 CURSOR_LOCAL_AGENT_API_KEY=uni-switch-cursor-local '/Applications/Cursor.app/Contents/MacOS/Cursor'";
const models = [{ value: "uni-switch/deepseek/pro", label: "uni-switch/deepseek/pro", ref: "deepseek/pro" }];
const agent = (id, name) => ({
  id, name, path: "/test/" + id, wired: true,
  fields: [{ key: "model", label: "model", value: "uni-switch/deepseek/pro", options: models }],
});
const NOTICE = "Cursor Private Inference reads uni-switch's gateway from CURSOR_LOCAL_AGENT_BASE_URL and CURSOR_LOCAL_AGENT_API_KEY, now set for your user: quit it and open it again.";
const cursorLocal = (on) => ({
  id: "cursor-local", name: "Cursor Private Inference", icon: "cursor", path: "", launch: LAUNCH, wired: on,
  fields: [{ key: "provider", label: "provider", value: on ? "uni-switch" : "", options: [{ value: "uni-switch", label: "uni-switch", icon: "uni-switch" }] }],
});
const state = {
  agents: [agent("claude", "Claude Code"), agent("codex", "Codex"), agent("pi", "Pi"),
    cursorLocal(false)],
  profiles: [],
};

function server(lang, copies, asked = []) {
  let cur = state;
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { ...cur, settings: { lang, theme: "light" } } });
    if (url.pathname.startsWith("/api/agents/connect/")) {
      asked.push(url.pathname);
      cur = { ...cur, agents: cur.agents.map((a) => (a.id === "cursor-local" ? cursorLocal(true) : a)) };
      return route.fulfill({ json: { ...cur, settings: { lang, theme: "light" }, connected: { how: "uni-switch" }, notice: NOTICE } });
    }
    if (url.pathname === "/api/copy") { copies.push(req.postDataJSON().text); return route.fulfill({ json: {} }); }
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const row = `.row.agent[data-id="cursor-local"]`;

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": Cursor Private Inference is connected by its switch", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    const W = { en: { conn: "Connect Cursor Private Inference to uni-switch", said: "Cursor Private Inference is connected to uni-switch" }, zh: { conn: "将 Cursor Private Inference 接入 uni-switch", said: "Cursor Private Inference 已接入 uni-switch" } };
    for (const lang of ["en", "zh"]) {
      await t.test(lang + ": the switch connects it, no command to run", async () => {
        const copies = [], asked = [];
        const page = await (await browser.newContext({ viewport: { width: 1100, height: 900 } })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, copies, asked));
        await page.goto("http://uni-switch.test/");
        // not connected, it is among the others, as any agent is
        await page.locator(".agent-more").click();
        const sw = page.locator(`${row} .ag-conn`);
        await sw.waitFor({ state: "visible" });
        assert.equal(await sw.getAttribute("aria-label"), W[lang].conn);
        assert.equal(await sw.getAttribute("aria-checked"), "false");
        assert.equal(await page.locator(`${row} .field.launch-solo`).count(), 0, "the command is still the row's one control");
        await sw.click();
        await page.locator(`${row} .ag-conn[aria-checked="true"]`).waitFor();
        assert.deepEqual(asked, ["/api/agents/connect/cursor-local"]);
        const said = await page.locator("#status").textContent();
        assert(said.includes(W[lang].said) && said.includes("quit it and open it again"), said);
        assert.deepEqual(copies, [], "nothing copied");
      });
    }

    assert.deepEqual(errors, []);
  });
}

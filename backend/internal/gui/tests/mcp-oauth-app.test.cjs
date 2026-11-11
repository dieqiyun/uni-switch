// Run with Node's test runner and Playwright on the module path; see README.md.
// 新v学员 on Discord: GitHub's remote MCP server signs in with GitHub's own
// OAuth, which doesn't let an app register itself, so uni-switch couldn't sign
// in to it. The server's editor takes an OAuth app of the user's own (a
// client ID, its secret, a callback port: Claude Code's --client-id,
// --client-secret, --callback-port), and the sign-in's error says so and
// opens those fields. The app is saved with the server, the sign-in button
// saying so first; Cancel keeps a server's app as saved. Every language,
// Chromium and WebKit, 1100 and 390 wide; nothing scrolls sideways. No
// backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const { I18N } = require("./strings.cjs").strings(); // i18n.js's and the plugins'
const tr = (lang, s, vars = {}) => ((lang === "en" ? s : I18N[lang][s]) || s).replace(/\{(\w+)\}/g, (_, k) => vars[k]);
const HOME = "/Users/xv";
const agent = (id, name) => ({ id, name, icon: "", skills: `${HOME}/.${id}/skills`, mcp: `${HOME}/.${id}/mcp.json` });
const GH = "https://api.githubcopilot.com/mcp/";
const ID = "Ov23liAbCdEfGhIjKlMn", SECRET = "0123456789abcdef0123456789abcdef01234567";
// mcpauth.ErrNoRegistration, as the backend says it
const NO_REG = "the server's sign-in doesn't let an app register itself (no dynamic client registration), so uni-switch signs in to it only as an OAuth app of your own: make one with the server's vendor (GitHub's under Settings › Developer settings › OAuth Apps), with http://localhost/callback as its callback URL, then give its client ID and secret under \"OAuth app\" in the server's editor";

function server(lang, calls) {
  const lib = {
    dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
    agents: [agent("claude", "Claude Code")],
    instructions: { agents: [], sets: [] }, foundServers: [], projects: [], foundSkills: [], skills: [],
    servers: [{ name: "github", transport: "http", url: GH, agents: ["claude"], signIn: { signedIn: false } }],
  };
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/library") return route.fulfill({ json: lib });
    if (url.pathname === "/api/library/mcp/check") return route.fulfill({ json: { servers: {} } });
    if (url.pathname === "/api/library/mcp-signin") {
      const s = lib.servers.find((x) => x.name === req.postDataJSON().name);
      calls.push("start " + s.name + " " + (s.oauth?.clientId || "-"));
      // GitHub's sign-in has no registration: only the user's app gets in
      if (!s.oauth) return route.fulfill({ status: 400, json: { error: NO_REG } });
      return route.fulfill({ json: { id: "s1", server: s.name, state: "waiting" } });
    }
    if (url.pathname === "/api/library/mcp-signin/s1/cancel") { calls.push("cancel"); return route.fulfill({ json: {} }); }
    if (url.pathname === "/api/library/mcp-signin/s1") return route.fulfill({ json: { id: "s1", state: "waiting" } });
    if (url.pathname === "/api/library/servers/save") {
      const { old, server: s } = req.postDataJSON();
      calls.push("save " + s.name + " " + JSON.stringify(s.oauth));
      const i = lib.servers.findIndex((x) => x.name === old);
      lib.servers[i] = { ...lib.servers[i], ...s };
      if (!s.oauth) delete lib.servers[i].oauth;
      return route.fulfill({ json: { ...lib, result: { changed: ["claude"] } } });
    }
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": an MCP server signed in to as the user's own OAuth app", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      for (const width of [1100, 390]) {
        await t.test(`${lang} ${width}`, async () => {
          const w = (s, v) => tr(lang, s, v);
          const calls = [];
          const ctx = await browser.newContext({ viewport: { width, height: 800 } });
          await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "mcp"); } catch {} });
          const page = await ctx.newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", server(lang, calls));
          await page.goto("http://uni-switch.test/");
          await page.locator('button[data-view="library"]').click();
          const row = page.locator("#view-library .lib-server", { has: page.locator('.lib-health[data-server="github"]') });
          const ed = page.locator("#modal .lib-editor");
          const box = ed.locator(".lib-signin");
          const app = ed.locator(".lib-app");
          const input = (label) => app.getByLabel(w(label), { exact: true });
          const hint = (u) => ed.locator(".hint", { hasText: u });
          const open = async () => { await row.locator(".name").click(); await ed.waitFor(); };
          const cancel = async (asked) => {
            await ed.locator(":scope > .bar button", { hasText: new RegExp("^" + w("Cancel") + "$") }).click();
            const ask = page.locator("dialog.action-confirm");
            if (asked) await ask.locator("button.primary", { hasText: w("Discard") }).click();
            await ed.waitFor({ state: "detached" });
            assert.equal(await ask.count(), 0, "asked to discard");
          };

          // the reporter's case: GitHub's sign-in has no registration; the
          // error says what to do, in the reader's language, and the app's
          // fields open
          await open();
          await app.getByRole("button", { name: w("Use your own OAuth app"), exact: true }).waitFor();
          await box.getByRole("button", { name: w("Sign in"), exact: true }).click();
          await box.locator(".lib-signin-err", { hasText: w(NO_REG) }).waitFor();
          await input("Client ID").waitFor();
          assert.equal(await page.evaluate(() => document.activeElement?.getAttribute("aria-label")), w("Client ID"));
          await hint("http://localhost/callback").waitFor();

          // typed but not saved: Cancel asks, and nothing is saved
          await input("Client ID").fill(ID);
          await box.getByRole("button", { name: w("Save and sign in"), exact: true }).waitFor();
          await cancel(true);
          assert.deepEqual(calls, ["start github -"]);

          // the app given: "Save and sign in" saves it and signs in as it
          await open();
          await app.getByRole("button", { name: w("Use your own OAuth app"), exact: true }).click();
          await input("Client ID").fill(ID);
          await input("Client secret").fill(SECRET);
          assert.equal(await input("Client secret").getAttribute("type"), "password");
          await input("Callback port").pressSequentially("31a18");
          assert.equal(await input("Callback port").inputValue(), "3118");
          await hint("http://localhost:3118/callback").waitFor();
          await box.getByRole("button", { name: w("Save and sign in"), exact: true }).click();
          await box.getByRole("button", { name: w("Cancel"), exact: true }).click();
          await box.getByRole("button", { name: w("Sign in"), exact: true }).waitFor();
          assert.deepEqual(calls.slice(1), ["save github " + JSON.stringify({ clientId: ID, clientSecret: SECRET, callbackPort: 3118 }), "start github " + ID, "cancel"]);
          await cancel(false);

          // kept: shown again; taken away, saved without it
          await open();
          assert.equal(await input("Client ID").inputValue(), ID);
          assert.equal(await input("Client secret").inputValue(), SECRET);
          await app.locator(".lib-x").click();
          await app.getByRole("button", { name: w("Use your own OAuth app"), exact: true }).waitFor();
          await ed.locator(":scope > .bar button.primary").click();
          await ed.waitFor({ state: "detached" });
          assert.equal(calls.at(-1), "save github null");
          assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, "no sideways scroll");
          await ctx.close();
        });
      }
    }
    assert.deepEqual(errors, []);
  });
}

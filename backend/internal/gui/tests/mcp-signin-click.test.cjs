// Run with Node's test runner and Playwright on the module path; see README.md.
// The owner, on the Library's MCP tab: a click on a server's "needs
// sign-in" signs in to it, as the dialog's Sign in does, not a check
// again. The status says the sign-in is under way and then lists the
// tools; no dialog opens unless there's something to do in it (the
// browser didn't open, so the link to open by hand). A server that
// refused uni-switch with no sign-in to offer (a key missing from its
// headers) is checked again, as before. Every language, Chromium and
// WebKit, 1100 and 390 wide. No backend: the API is faked here.
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

function server(lang, calls, gate) {
  const lib = {
    dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
    agents: [agent("claude", "Claude Code")],
    instructions: { agents: [], sets: [] }, foundServers: [], projects: [], foundSkills: [], skills: [],
    servers: [
      { name: "linear", transport: "http", url: "https://mcp.linear.app/mcp", agents: ["claude"], signIn: { signedIn: false } },
      // its browser doesn't open: the dialog shows the link
      { name: "notion", transport: "http", url: "https://mcp.notion.com/mcp", agents: ["claude"], signIn: { signedIn: false } },
      // refused without a sign-in to offer: a key in its headers
      { name: "keyed", transport: "http", url: "https://mcp.example.com/mcp", agents: ["claude"], signIn: { signedIn: false } },
    ],
  };
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/library") return route.fulfill({ json: lib });
    if (url.pathname === "/api/library/mcp/check") {
      const names = req.postDataJSON().names;
      calls.push("check " + names.join(","));
      const servers = {};
      for (const n of names) {
        const s = lib.servers.find((x) => x.name === n);
        if (n === "keyed") servers[n] = { state: "auth", code: 401 };
        else servers[n] = s.signIn.signedIn ? { state: "ok", tools: 7, signIn: s.signIn } : { state: "auth", code: 401, oauth: true, signIn: s.signIn };
      }
      return route.fulfill({ json: { servers } });
    }
    if (url.pathname === "/api/library/mcp-signin") {
      const name = req.postDataJSON().name;
      calls.push("start " + name);
      const st = { id: "s-" + name, server: name, state: "waiting", url: "https://example.com/authorize?n=" + name };
      if (name === "notion") st.openError = "no browser";
      return route.fulfill({ json: st });
    }
    if (url.pathname.startsWith("/api/library/mcp-signin/s-")) {
      const name = url.pathname.slice("/api/library/mcp-signin/s-".length);
      if (url.pathname.endsWith("/cancel")) { calls.push("cancel " + name.replace(/\/cancel$/, "")); return route.fulfill({ json: {} }); }
      if (!gate.done) return route.fulfill({ json: { id: "s-" + name, server: name, state: "waiting" } });
      lib.servers.find((x) => x.name === name).signIn = { signedIn: true, at: 2 };
      return route.fulfill({ json: { id: "s-" + name, server: name, state: "done" } });
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
  test(engine + ": a click on needs sign-in signs in", async (t) => {
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
          const calls = [], gate = { done: false };
          const ctx = await browser.newContext({ viewport: { width, height: 800 } });
          await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "mcp"); } catch {} });
          const page = await ctx.newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", server(lang, calls, gate));
          await page.goto("http://uni-switch.test/");
          await page.locator('button[data-view="library"]').click();
          const row = (name) => page.locator("#view-library .lib-server", { has: page.locator(`.lib-health[data-server="${name}"]`) });
          const status = (name) => page.locator(`#view-library .lib-health[data-server="${name}"]`);
          for (const n of ["linear", "notion", "keyed"]) await status(n).locator("span", { hasText: w("needs sign-in") }).waitFor();
          assert.match(await status("linear").getAttribute("title"), new RegExp(w("Click to sign in")));

          // linear: the sign-in starts from the status, with no dialog
          await status("linear").click();
          await status("linear").locator("span", { hasText: w("Finish signing in in your browser…") }).waitFor();
          assert.equal(await page.locator("#modal .lib-editor").count(), 0, "no dialog for a sign-in the browser took");
          assert.deepEqual(calls.filter((c) => c.startsWith("start")), ["start linear"]);
          gate.done = true;
          await status("linear").locator("span", { hasText: w("{n} tools", { n: 7 }) }).waitFor();
          await row("linear").locator(".name .lib-tag", { hasText: w("Signed in") }).waitFor();
          assert.equal(calls.filter((c) => c === "start linear").length, 1);

          // notion: the browser didn't open, so the dialog shows the link
          gate.done = false;
          await status("notion").click();
          const box = page.locator("#modal .lib-signin");
          await box.locator(".lib-signin-link code", { hasText: "n=notion" }).waitFor();
          await box.getByRole("button", { name: w("Cancel"), exact: true }).click();
          await box.getByRole("button", { name: w("Sign in"), exact: true }).waitFor();
          assert(calls.includes("cancel notion"), calls.join("; "));
          await page.keyboard.press("Escape");
          await page.locator("#modal .lib-editor").waitFor({ state: "detached" });
          await status("notion").locator("span", { hasText: w("needs sign-in") }).waitFor();

          // keyed: nothing to sign in to, so a click checks it again
          await status("keyed").click();
          // the check asked again, then answered as before
          for (let i = 0; i < 100 && !calls.includes("check keyed"); i++) await page.waitForTimeout(50);
          assert(calls.includes("check keyed"), calls.join("; "));
          await status("keyed").locator("span", { hasText: w("needs sign-in") }).waitFor();
          assert(!calls.includes("start keyed"), calls.join("; "));
          assert.equal(await page.locator("#modal .lib-editor").count(), 0);
          assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, "no sideways scroll");
          await ctx.close();
        });
      }
    }
    assert.deepEqual(errors, []);
  });
}

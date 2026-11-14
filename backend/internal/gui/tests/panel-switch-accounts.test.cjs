// Run with Node's test runner and Playwright on the module path; see README.md.
// The tray panel switches accounts too (shuizhuyu on X: a global shortcut
// to switch accounts). The quick panel's shortcut opens it, and a Claude
// Code or Codex not connected has an Account line in its opened row, the
// account it is signed in to, whose menu signs it in to another, as the
// window's Agents row does (Switch accounts only). The line is drawn from
// the list kept from the last read (/api/providers?last=1, no sign-in
// read): the panel asks no fresh list until the menu is opened, and then
// opens it on what that read says. Nothing kept: one fresh read. A
// connected agent has no line; Manage accounts… opens the window's
// Providers. In every language, at two panel widths, Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(process.env.UNI_SWITCH_ASSETS || path.join(__dirname, "../assets"));
const own = [{ value: "sonnet", label: "Sonnet" }];
const agents = (claudeWired) => [
  { id: "claude", name: "Claude Code", icon: "generic", path: "/fixture/claude", source: "sub", wired: claudeWired, fields: [{ key: "model", label: "model", value: "sonnet", options: own }] },
  { id: "codex", name: "Codex", icon: "generic", path: "/fixture/codex", source: "chatgpt", fields: [{ key: "model", label: "model", value: "gpt-5.5", options: [{ value: "gpt-5.5", label: "gpt-5.5" }] }] },
];

function providersOf(active, last) {
  const claude = {
    id: "claude", name: "Claude Code", icon: "claude-color", chat: "", responses: "", anthropic: "", catalog: "", routing: "smart",
    models: [], agents: [], fallback: [], headers: {}, keyList: [],
    account: {
      agent: "claude", agentName: "Claude Code", user: active, plan: "max",
      logins: ["work@example.com", "home@example.com"].map((user) => ({ user, plan: user === "work@example.com" ? "max" : "pro", active: user === active, on: true })),
    },
  };
  const codex = {
    id: "codex", name: "Codex", icon: "codex-color", chat: "", responses: "", anthropic: "", catalog: "", routing: "smart",
    models: [], agents: [], fallback: [], headers: {}, keyList: [],
    account: { agent: "codex", agentName: "Codex", user: "me@example.com", plan: "PLUS", logins: [{ user: "me@example.com", plan: "PLUS", active: true, on: true }] },
  };
  return { ...(last ? { last: true } : {}), providers: [claude, codex], presets: [], excluded: [], gateway: { running: true, window: true } };
}

// w: { kept (the active account in the kept list, or null for none kept),
// active (the account a fresh read says), wired }
function server(lang, w, log) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: agents(!!w.wired), profiles: [], settings: { lang, theme: "light", guideHidden: true } });
    if (url.pathname === "/api/agents/cli" && req.method() === "GET") return json({ agents: {}, pending: false });
    if (url.pathname === "/api/agents/install") return json([]);
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/providers" && req.method() === "GET") {
      if (url.searchParams.get("last") === "1") {
        log.push("kept");
        return json(w.kept ? providersOf(w.kept, true) : null);
      }
      log.push("fresh");
      return json(providersOf(w.active));
    }
    if (req.method() === "POST" && ["/api/login/switch", "/api/window/main"].includes(url.pathname)) {
      log.push(url.pathname + url.search + " " + (req.postData() || ""));
      if (url.pathname === "/api/login/switch") {
        w.active = w.kept = req.postDataJSON().user;
        return json(providersOf(w.active));
      }
      return route.fulfill({ status: 204 });
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const K = {
  account: "account",
  head: "Switch accounts only",
  manage: "Manage accounts…",
  only: "Only switch {agent}'s sign-in",
  switched: "Switched",
  said: "{agent} is now signed in as {user}. New {agent} sessions use this account; one already open keeps its account until it is started again.",
};
const row = (id) => `.row.agent[data-id="${id}"]`;

async function open(browser, lang, width, w) {
  const page = await (await browser.newContext({ viewport: { width, height: 640 }, reducedMotion: "reduce" })).newPage();
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [], log = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/*", server(lang, w, log));
  await page.goto("http://uni-switch.test/?mode=panel");
  await page.locator(`${row("claude")} .ag-sum`).waitFor();
  const tr = async (k, vars = {}) => {
    const s = await page.evaluate(([k, lang]) => (lang === "en" ? k : I18N[lang]?.[k]), [k, lang]);
    assert.ok(s, `${lang} has ${k.slice(0, 40)}`);
    return Object.entries(vars).reduce((s, [k, v]) => s.replaceAll(`{${k}}`, v), s);
  };
  return { page, errors, log, tr };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": the tray panel switches a Claude Code's account", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      for (const width of [440, 360]) {
        await t.test(`${lang} ${width}px`, async () => {
          // kept: work; signed in to home since (in a terminal)
          const w = { kept: "work@example.com", active: "home@example.com" };
          const { page, errors, log, tr } = await open(browser, lang, width, w);
          const cc = { agent: "Claude Code" };
          for (const k of Object.values(K)) await tr(k);
          await page.locator(`${row("claude")} .ag-sum`).click();
          const who = page.locator(`${row("claude")} .ag-open button.field.ag-who`);
          await who.waitFor({ state: "visible" });
          assert.equal(await who.locator(".k").textContent(), await tr(K.account));
          assert.equal(await who.locator(".v").textContent(), "work@example.com");
          // drawn from the kept list: no fresh read yet
          assert.ok(log.includes("kept"), "the kept list is read");
          assert.ok(!log.includes("fresh"), `a fresh list read before the menu was asked for: ${log}`);
          const box = await who.boundingBox();
          assert.ok(box && box.x >= 0 && box.x + box.width <= width, `button off the panel: ${JSON.stringify(box)}`);

          // opened, the menu is the fresh read's
          await who.click();
          const menu = page.locator(".proto-menu");
          await menu.waitFor();
          assert.deepEqual(log.filter((x) => x === "fresh"), ["fresh"]);
          assert.equal(await menu.locator(".pm-head").textContent(), await tr(K.head));
          const items = menu.locator(".pm-item .pm-name");
          assert.deepEqual(await items.allTextContents(), ["home@example.com", "work@example.com", await tr(K.only, cc), await tr(K.manage)]);
          assert.equal(await menu.locator(".pm-item.on .pm-name").allTextContents().then((x) => x.join()), "home@example.com");
          assert.equal(await page.locator(`${row("claude")} .ag-who .v`).textContent(), "home@example.com");
          const mb = await menu.boundingBox();
          assert.ok(mb.x >= 0 && mb.x + mb.width <= width, `menu off the panel: ${JSON.stringify(mb)}`);

          await menu.locator(".pm-item", { hasText: "work@example.com" }).click();
          await page.waitForFunction(() => document.querySelector('.row.agent[data-id="claude"] .ag-who .v')?.textContent === "work@example.com");
          assert.ok(log.some((x) => x.startsWith("/api/login/switch") && x.includes('"user":"work@example.com"')), `no switch posted: ${log}`);
          const now = page.locator(`${row("claude")} .ag-who`);
          assert.ok(await now.locator(".ok").isVisible(), "Switched shows on the line");
          assert.equal((await now.locator(".ok").textContent()).trim(), await tr(K.switched));
          assert.equal(await page.locator("#status").textContent(), await tr(K.said, { ...cc, user: "work@example.com" }));
          if (process.env.ARTIFACT_DIR) await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `panel-switch-${engine}-${lang}-${width}.png`) });
          // the row stays open, the line in it
          assert.equal(await page.locator(row("claude")).getAttribute("aria-expanded"), "true");

          // Manage accounts… opens the window's Providers
          await now.click();
          await menu.waitFor();
          await menu.locator(".pm-item", { hasText: await tr(K.manage) }).click();
          for (let i = 0; i < 100 && !log.some((x) => x.startsWith("/api/window/main")); i++) await page.waitForTimeout(20);
          assert.ok(log.some((x) => x.startsWith("/api/window/main?view=providers")), `Manage accounts… didn't open the window: ${log}`);
          assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1), "the panel scrolls sideways");
          assert.deepEqual(errors, []);
          await page.context().close();
        });
      }
    }
  });

  test(engine + ": nothing kept, the panel reads the list once; a connected Claude Code has no line", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const w = { kept: null, active: "work@example.com" };
    const { page, errors, log } = await open(browser, "en", 440, w);
    await page.locator(`${row("claude")} .ag-sum`).click();
    const who = page.locator(`${row("claude")} .ag-open .ag-who`);
    await who.waitFor({ state: "visible" });
    assert.equal(await who.locator(".v").textContent(), "work@example.com");
    assert.deepEqual(log.filter((x) => x === "fresh"), ["fresh"]);
    // a fresh list in hand: the menu opens on it, no second read
    await who.click();
    await page.locator(".proto-menu").waitFor();
    assert.deepEqual(log.filter((x) => x === "fresh"), ["fresh"]);
    await page.keyboard.press("Escape");
    // Codex, one account: its line says it
    await page.locator(`${row("codex")} .ag-sum`).click();
    await page.locator(`${row("codex")} .ag-open .ag-who .v`, { hasText: "me@example.com" }).waitFor({ state: "visible" });
    assert.deepEqual(errors, []);
    await page.context().close();

    const c = await open(browser, "en", 440, { kept: "work@example.com", active: "work@example.com", wired: true });
    await c.page.locator(`${row("claude")} .ag-sum`).click();
    await c.page.locator(`${row("claude")}.open`).waitFor();
    await c.page.locator(`${row("codex")} .ag-open .ag-who`).waitFor({ state: "attached" });
    assert.equal(await c.page.locator(`${row("claude")} .ag-who`).count(), 0, "a connected Claude Code has an account line");
    assert.deepEqual(c.errors, []);
    await c.page.context().close();
  });
}

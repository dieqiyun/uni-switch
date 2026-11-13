// Run with Node's test runner and Playwright on the module path; see README.md.
// Switch accounts only (the owner on X; growaiskills, Rennix7t): uni-switch as
// a Claude Code account switcher alone, with nothing connected. The Agents
// row of a Claude Code not connected says the account it is signed in to,
// always; its menu, headed Switch accounts only, signs it in to another
// (login/switch), and the button says Switched while the status line says
// new sessions use the new account. Only switch Claude Code's sign-in is
// ticked there and, ticked, the row says it switches accounts only. One
// kept signed in to an account of the user's choosing moves that choice
// (keeplogin). Codex has the same entry; with one account, no toggle.
// With no models in uni-switch to connect (the switcher's case) it is there.
// A connected Claude Code, and its WSL twin, have none. In every language,
// at a narrow width too, in Chromium and WebKit; no click moves the page.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
// with no models in uni-switch (every Claude account kept to Claude Code,
// nothing else added) the fields offer none of uni-switch's
const own = [{ value: "sonnet", label: "Sonnet" }];
const agents = (wired, options = wired ? [...own, { value: "relay/m1", label: "m1", ref: "relay/m1" }] : own) => [
  { id: "claude", name: "Claude Code", icon: "generic", path: "/fixture/claude", source: "sub", wired, fields: [{ key: "model", label: "model", value: "sonnet", options }] },
  { id: "claude@wsl:Ubuntu-24.04", name: "Claude Code", icon: "generic", path: "/fixture/wsl-claude", source: "sub", fields: [{ key: "model", label: "model", value: "sonnet", options }] },
  { id: "codex", name: "Codex", icon: "generic", path: "/fixture/codex", source: "chatgpt", fields: [{ key: "model", label: "model", value: "gpt-5.5", options }] },
];

function providersOf(w) {
  const claude = {
    id: "claude", name: "Claude Code", icon: "claude-color", chat: "", responses: "", anthropic: "", catalog: "", routing: "smart",
    models: [], agents: [], fallback: [], headers: {}, keyList: [], switchOnly: w.switchOnly || undefined,
    keepLogin: !!w.keepAs || undefined, keepLoginAs: w.keepAs || undefined,
    account: {
      agent: "claude", agentName: "Claude Code", user: w.active, plan: "max",
      logins: ["work@example.com", "home@example.com"].map((user) => ({ user, plan: user === "work@example.com" ? "max" : "pro", active: user === w.active, on: true })),
    },
  };
  const codex = {
    id: "codex", name: "Codex", icon: "codex-color", chat: "", responses: "", anthropic: "", catalog: "", routing: "smart",
    models: [], agents: [], fallback: [], headers: {}, keyList: [],
    account: { agent: "codex", agentName: "Codex", user: "me@example.com", plan: "PLUS", logins: [{ user: "me@example.com", plan: "PLUS", active: true, on: true }] },
  };
  return { providers: [claude, codex], presets: [], excluded: [], gateway: { running: true, window: true } };
}

function server(lang, w, posts) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: agents(w.wired), profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (url.pathname === "/api/providers") return json(providersOf(w));
    if (req.method() === "POST" && ["/api/login/switch", "/api/provider/switchonly", "/api/provider/keeplogin"].includes(url.pathname)) {
      const body = req.postDataJSON();
      posts.push([url.pathname, body]);
      if (url.pathname === "/api/login/switch") w.active = body.user;
      if (url.pathname === "/api/provider/switchonly") w.switchOnly = body.switchOnly;
      if (url.pathname === "/api/provider/keeplogin") w.active = w.keepAs = body.keepLoginAs;
      return json(providersOf(w));
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const K = {
  head: "Switch accounts only",
  title: "Switch accounts only: {agent} is signed in as {user}. Pick another of its accounts to sign {agent} in to it, without connecting it to uni-switch.",
  switched: "Switched",
  manage: "Manage accounts…",
  only: "Only switch {agent}'s sign-in",
  said: "{agent} is now signed in as {user}. New {agent} sessions use this account; one already open keeps its account until it is started again.",
  onlyLine: "Not connected · switching accounts only",
  sub: "Not connected · now on its Claude subscription",
};
const row = (id) => `.row.agent[data-id="${id}"]`;

async function open(engine, lang, width, w, t) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  t.after(() => browser.close());
  const page = await (await browser.newContext({ viewport: { width, height: 640 }, reducedMotion: "reduce" })).newPage();
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [], dialogs = [], posts = [];
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("dialog", (d) => { dialogs.push(d.message()); d.dismiss(); });
  await page.route("**/*", server(lang, w, posts));
  await page.goto("http://uni-switch.test/?view=agents");
  await page.locator(`${row("claude")} .ag-st`).waitFor();
  const tr = async (k, vars = {}) => {
    const s = await page.evaluate(([k, lang]) => (lang === "en" ? k : I18N[lang]?.[k]), [k, lang]);
    assert.ok(s, `${lang} has ${k.slice(0, 40)}`);
    return Object.entries(vars).reduce((s, [k, v]) => s.replaceAll(`{${k}}`, v), s);
  };
  const scrolled = () => page.evaluate(() => [document.scrollingElement.scrollTop, document.querySelector("#view-agents")?.scrollTop]);
  return { page, errors, dialogs, posts, tr, scrolled };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [980, 420]) {
      test(`${engine} ${lang} ${width}px: a Claude Code not connected says its account and switches it`, async (t) => {
        const w = { active: "work@example.com" };
        const { page, errors, dialogs, posts, tr, scrolled } = await open(engine, lang, width, w, t);
        const cc = { agent: "Claude Code" };
        for (const k of Object.values(K)) await tr(k);
        const st = page.locator(`${row("claude")} .ag-st`);
        const who = st.locator("button.ag-acc.ag-who");
        await who.waitFor();
        assert.equal(await st.locator(".ag-st-t").textContent(), await tr(K.sub));
        assert.equal(await who.locator(".v").textContent(), "work@example.com");
        assert.equal(await who.getAttribute("title"), await tr(K.title, { ...cc, user: "work@example.com" }));
        assert.equal(await who.locator(".ok").isVisible(), false);
        // the WSL twin has no list of its own here: no entry
        assert.equal(await page.locator(`${row("claude@wsl:Ubuntu-24.04")} .ag-who`).count(), 0);
        const wide = await page.evaluate(() => document.scrollingElement.scrollWidth - document.scrollingElement.clientWidth);
        assert.ok(wide <= 0, `the page scrolls sideways by ${wide}px`);
        const box = await who.boundingBox();
        assert.ok(box && box.x >= 0 && box.x + box.width <= width, `button off screen: ${JSON.stringify(box)}`);
        const top = await scrolled();

        await who.click();
        const menu = page.locator(".proto-menu");
        await menu.waitFor();
        assert.equal(await menu.locator(".pm-head").textContent(), await tr(K.head));
        assert.equal(await who.getAttribute("aria-expanded"), "true");
        const items = menu.locator(".pm-item .pm-name");
        assert.deepEqual(await items.allTextContents(), ["work@example.com", "home@example.com", await tr(K.only, cc), await tr(K.manage)]);
        assert.equal(await menu.locator(".pm-item.on .pm-name").allTextContents().then((x) => x.join()), "work@example.com");
        const mb = await menu.boundingBox();
        assert.ok(mb.x >= 0 && mb.x + mb.width <= width, `menu off screen: ${JSON.stringify(mb)}`);
        await menu.locator(".pm-item", { hasText: "home@example.com" }).click();
        await page.waitForFunction(() => document.querySelector('.row.agent[data-id="claude"] .ag-who .v')?.textContent === "home@example.com");
        assert.deepEqual(posts, [["/api/login/switch", { agent: "claude", user: "home@example.com" }]]);
        const now = st.locator("button.ag-who");
        assert.ok(await now.locator(".ok").isVisible(), "Switched shows on the button");
        assert.equal((await now.locator(".ok").textContent()).trim(), await tr(K.switched));
        assert.equal(await page.locator("#status").textContent(), await tr(K.said, { ...cc, user: "home@example.com" }));

        // Only switch Claude Code's sign-in, ticked from the same menu
        await now.click();
        await menu.waitFor();
        const only = menu.locator(".pm-item", { hasText: await tr(K.only, cc) });
        assert.equal(await only.getAttribute("role"), "menuitemcheckbox");
        assert.equal(await only.getAttribute("aria-checked"), "false");
        await only.click();
        await page.waitForFunction((s) => document.querySelector('.row.agent[data-id="claude"] .ag-st-t')?.textContent === s, await tr(K.onlyLine));
        assert.deepEqual(posts[1], ["/api/provider/switchonly", { id: "claude", switchOnly: true }]);
        await st.locator("button.ag-who").click();
        await menu.waitFor();
        assert.equal(await menu.locator(".pm-item", { hasText: await tr(K.only, cc) }).getAttribute("aria-checked"), "true");
        await page.keyboard.press("Escape");

        // Codex, one account: its entry, with no toggle to offer
        const cx = page.locator(`${row("codex")} .ag-who`);
        assert.equal(await cx.locator(".v").textContent(), "me@example.com");
        await cx.click();
        await menu.waitFor();
        assert.deepEqual(await menu.locator(".pm-item .pm-name").allTextContents(), ["me@example.com", await tr(K.manage)]);
        await menu.locator(".pm-item", { hasText: await tr(K.manage) }).click();
        await page.waitForFunction(() => new URL(location.href).searchParams.get("edit") === "codex");

        assert.equal(posts.length, 2);
        assert.deepEqual(dialogs, []);
        assert.deepEqual(errors, []);
        assert.ok(top.every((v) => v === 0 || v === undefined), `page was scrolled ${top}`);
      });
    }
  }

  test(`${engine}: one kept signed in to an account of its choosing moves that choice`, async (t) => {
    const w = { active: "work@example.com", keepAs: "work@example.com" };
    const { page, errors, posts } = await open(engine, "en", 980, w, t);
    const who = page.locator(`${row("claude")} .ag-who`);
    await who.click();
    await page.locator(".proto-menu .pm-item", { hasText: "home@example.com" }).click();
    await page.waitForFunction(() => document.querySelector('.row.agent[data-id="claude"] .ag-who .v')?.textContent === "home@example.com");
    assert.deepEqual(posts, [["/api/provider/keeplogin", { id: "claude", keepLogin: true, keepLoginAs: "home@example.com" }]]);
    assert.deepEqual(errors, []);
  });

  test(`${engine}: a connected Claude Code has no account switch on its row`, async (t) => {
    const { page, errors } = await open(engine, "en", 980, { active: "work@example.com", wired: true }, t);
    await page.locator(`${row("codex")} .ag-who`).waitFor();
    assert.equal(await page.locator(`${row("claude")} .ag-who`).count(), 0);
    assert.deepEqual(errors, []);
  });
}

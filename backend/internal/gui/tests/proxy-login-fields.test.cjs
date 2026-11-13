// Run with Node's test runner and Playwright on the module path; see README.md.
// The proxy rows outside Settings take a proxy's login in fields
// of their own (#3382), the same split Settings' proxyFields makes: a pasted
// URL keeps working — its login lands in the fields, the box keeps the
// address — and what is typed in the fields is saved joined back into the
// saved format (userinfo in the address), escaped as Settings would save it,
// so the same login typed in Settings and pasted here is one address. A
// saved login opens decoded and saves again byte for byte, non-UTF-8
// escapes included. The provider's row, an account's line, a key's line and
// the sign-in box's typed proxy. In English and Chinese.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const BARE = "socks5://127.0.0.1:1080";
const JOINED = "socks5://a%40b%3A%2F%25%20user:p%40ss%3A%2F%25%20%23pass@127.0.0.1:1080";
const LOGIN = { user: "a@b:/% user", pass: "p@ss:/% #pass" };
const codex = {
  id: "codex", name: "Codex", icon: "codex-color", chat: "", responses: "", anthropic: "", catalog: "",
  models: [{ id: "gpt-6", name: "GPT-6", on: true }], agents: [], fallback: [], headers: {}, keyList: [],
  account: {
    agent: "codex", agentName: "Codex", user: "me@example.com", plan: "PLUS",
    logins: [
      { user: "spare@example.com", plan: "PLUS", on: true },
      { user: "me@example.com", plan: "PLUS", active: true, on: true },
      { user: "Third@Example.com", plan: "PRO", on: true },
    ],
  },
  proxy: "",
  accountProxies: { "me@example.com": BARE, "spare@example.com": "direct" },
};
const relay = {
  id: "relay", name: "Relay", icon: "generic", chat: "https://relay.example.com/v1", responses: "", anthropic: "", catalog: "",
  models: [{ id: "big", name: "", on: true }], agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-…one" }, balanceToken: { takes: false, set: false },
  proxy: BARE,
  keyList: [{ id: "aaaaaaaaaa", name: "Team", masked: "sk-…one", on: true, active: true }, { id: "bbbbbbbbbb", name: "", masked: "sk-…two", on: true }, { id: "cccccccccc", name: "Spare", masked: "sk-…three", on: true }],
  accountProxies: { aaaaaaaaaa: BARE, bbbbbbbbbb: "direct" },
};
const claude = {
  id: "claude", name: "Claude", icon: "claude-color", chat: "", responses: "", anthropic: "", catalog: "",
  models: [{ id: "claude-opus-5", name: "Claude Opus 5", on: true }], agents: [], fallback: [], headers: {}, keyList: [],
  account: { agent: "claude", agentName: "Claude Code", user: "alice@example.com", plan: "MAX", logins: [{ user: "alice@example.com", plan: "MAX", active: true, on: true }, { user: "bob@example.com", plan: "PRO", lapsed: true }] },
  proxy: "", accountProxies: {},
};

function serve(lang, posts, seen) {
  // a clone each time: a save merges into this test's copy, not the
  // fixtures the next test opens with
  const [codexNow, relayNow, claudeNow] = [codex, relay, claude].map((p) => structuredClone(p));
  const providers = { providers: [codexNow, relayNow, claudeNow], presets: [], excluded: [], gateway: { running: true, window: true } };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  const flows = {};
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = ${JSON.stringify({ lang, theme: "light", web: true })};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/provider/") && req.method() === "POST") {
      const body = req.postDataJSON();
      posts.push({ action: url.pathname.slice("/api/provider/".length), body });
      const p = providers.providers.find((x) => x.id === body.id);
      if (p) { if ("proxy" in body) p.proxy = body.proxy; if ("accountProxies" in body) p.accountProxies = body.accountProxies; }
      return json(providers);
    }
    if (url.pathname === "/api/signin") {
      const b = req.postDataJSON();
      seen.starts.push(b);
      const id = "s" + seen.starts.length;
      flows[id] = { id, agent: b.agent, state: "waiting", url: "https://vendor.example/oauth?state=" + id, proxy: b.proxy ?? "" };
      return json(flows[id]);
    }
    const m = url.pathname.match(/^\/api\/signin\/(s\d+)(\/cancel)?$/);
    if (m) {
      if (m[2]) { seen.canceled.push(m[1]); flows[m[1]].state = "canceled"; }
      return json(flows[m[1]]);
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const words = {
  en: { custom: "Custom", direct: "Direct", save: "Save", user: "Username (optional)", pass: "Password (optional)", again: "Sign in again", via: "Proxy: {p}", typed: "Sign in through it" },
  zh: { custom: "自定义", direct: "直连", save: "保存", user: "用户名（可选）", pass: "密码（可选）", again: "重新登录", via: "代理：{p}", typed: "经由它登录" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    const w = words[lang];
    const browser = async (t, width, height, tag) => {
      const b = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${tag}-proxy-login.png`) });
        }
        await b.close();
      });
      const page = await (await b.newContext({ viewport: { width, height }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      return page;
    };
    const unfold = async (page) => {
      const adv = page.locator(".editor details.advanced");
      await adv.waitFor({ state: "attached" });
      if (!(await adv.evaluate((d) => d.open))) await adv.locator(":scope > summary").click();
    };
    const openEditor = async (page, name, wait) => {
      await page.locator(".row.provider", { hasText: name }).first().click();
      await unfold(page);
      await page.locator(wait).first().waitFor();
    };
    const save = async (page, posts) => {
      const n = posts.length;
      await page.locator(".editor .bar").getByRole("button", { name: w.save, exact: true }).click();
      // waits on the post itself, with half a minute's room for a busy box
      for (const end = Date.now() + 30000; posts.length === n && Date.now() < end;) await page.waitForTimeout(20);
      assert.notEqual(posts.length, n, "Save posted nothing");
      return posts.at(-1);
    };
    const creds = (scope) => ({
      user: scope.getByLabel(w.user, { exact: true }),
      pass: scope.getByLabel(w.pass, { exact: true }),
    });
    const line = (page, user) => page.locator(`.editor .acct-proxy[data-${/^[a-f0-9]{10}$/.test(user) ? "key" : "user"}="${user}"]`);

    test(`${engine} ${lang}: the provider's proxy row splits and joins a login`, async (t) => {
      const page = await browser(t, 900, 760, "provider");
      const errors = [], posts = [], seen = { starts: [], canceled: [] };
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, posts, seen));
      await page.goto("http://uni-switch.test/?view=providers");
      await openEditor(page, "Relay", ".editor .proxy-pick .proxy-url");
      const pick = page.locator(".editor .proxy-pick");
      const addr = pick.locator(".proxy-url");
      const { user, pass } = creds(pick);
      assert.equal(await addr.inputValue(), BARE, "the saved login is out of the box");
      await user.waitFor({ state: "visible" });
      assert.equal(await user.inputValue(), "");
      assert.equal(await pass.inputValue(), "");

      // typed in the fields, saved as Settings would save the same login
      await user.fill(LOGIN.user);
      await pass.fill(LOGIN.pass);
      let saved = await save(page, posts);
      assert.equal(saved.body.proxy, JOINED);

      // saved again unchanged, it is one address still
      await openEditor(page, "Relay", ".editor .proxy-pick .proxy-url");
      assert.equal(await addr.inputValue(), BARE);
      assert.equal(await user.inputValue(), LOGIN.user);
      assert.equal(await pass.inputValue(), LOGIN.pass);
      saved = await save(page, posts);
      assert.equal(saved.body.proxy, JOINED);

      // a pasted URL with a login separates, and non-UTF-8 escapes are kept
      await openEditor(page, "Relay", ".editor .proxy-pick .proxy-url");
      await addr.fill("socks5://name:p%FFss@[::1]:1080");
      assert.equal(await addr.inputValue(), "socks5://[::1]:1080");
      assert.equal(await user.inputValue(), "name");
      assert.equal(await pass.inputValue(), "p%FFss");
      saved = await save(page, posts);
      assert.equal(saved.body.proxy, "socks5://name:p%FFss@[::1]:1080");

      // an HTTP proxy's login splits the same way, as Settings splits it
      await openEditor(page, "Relay", ".editor .proxy-pick .proxy-url");
      await addr.fill("http://name:pw@proxy.example:8080");
      assert.equal(await addr.inputValue(), "http://proxy.example:8080");
      assert.equal(await user.inputValue(), "name");
      assert.equal(await pass.inputValue(), "pw");
      saved = await save(page, posts);
      assert.equal(saved.body.proxy, "http://name:pw@proxy.example:8080");
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: an account's and a key's line split and join a login`, async (t) => {
      const page = await browser(t, 900, 760, "lines");
      const errors = [], posts = [], seen = { starts: [], canceled: [] };
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, posts, seen));
      await page.goto("http://uni-switch.test/?view=providers");

      await openEditor(page, "Codex", ".editor .acct-proxy");
      const me = creds(line(page, "me@example.com"));
      await me.user.waitFor({ state: "visible" });
      await me.user.fill(LOGIN.user);
      await me.pass.fill(LOGIN.pass);
      let saved = await save(page, posts);
      assert.deepEqual(saved.body.accountProxies, { "me@example.com": JOINED, "spare@example.com": "direct" });

      await openEditor(page, "Codex", ".editor .acct-proxy");
      const meAgain = creds(line(page, "me@example.com"));
      assert.equal(await line(page, "me@example.com").locator(".proxy-url").inputValue(), BARE);
      assert.equal(await meAgain.user.inputValue(), LOGIN.user);
      assert.equal(await meAgain.pass.inputValue(), LOGIN.pass);
      saved = await save(page, posts);
      assert.deepEqual(saved.body.accountProxies, { "me@example.com": JOINED, "spare@example.com": "direct" });

      // a pasted URL with a login separates, and saves as it came
      await openEditor(page, "Codex", ".editor .acct-proxy");
      const mine = line(page, "me@example.com");
      await mine.locator(".proxy-url").fill(JOINED);
      assert.equal(await mine.locator(".proxy-url").inputValue(), BARE, "the pasted login is out of the box");
      assert.equal(await creds(mine).user.inputValue(), LOGIN.user);
      assert.equal(await creds(mine).pass.inputValue(), LOGIN.pass);
      saved = await save(page, posts);
      assert.deepEqual(saved.body.accountProxies, { "me@example.com": JOINED, "spare@example.com": "direct" });

      await openEditor(page, "Relay", ".editor .acct-proxy");
      const team = creds(line(page, "aaaaaaaaaa"));
      await team.user.waitFor({ state: "visible" });
      await team.user.fill(LOGIN.user);
      await team.pass.fill(LOGIN.pass);
      saved = await save(page, posts);
      assert.deepEqual(saved.body.accountProxies, { aaaaaaaaaa: JOINED, bbbbbbbbbb: "direct" });
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: the sign-in box's typed proxy takes a login in fields`, async (t) => {
      const page = await browser(t, 900, 800, "signin");
      const errors = [], posts = [], seen = { starts: [], canceled: [] };
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, posts, seen));
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator(".row.provider", { hasText: "Claude" }).first().click();
      const bob = page.locator(".editor .accts .acc", { hasText: "bob@example.com" });
      await bob.locator("button", { hasText: w.again }).click();
      const box = page.locator(".editor .signing");
      const via = box.locator(".acts .via");
      await via.waitFor();
      await via.scrollIntoViewIfNeeded();
      await via.click();
      const menu = page.locator(".proto-menu");
      await menu.waitFor();
      await menu.locator(".pm-item", { hasText: w.custom }).click();
      const form = box.locator("form.proxy-ask");
      await form.waitFor();
      const addr = form.locator("input").first();
      const { user, pass } = creds(form);
      await addr.fill(BARE);
      await user.waitFor({ state: "visible" });
      await user.fill(LOGIN.user);
      await pass.fill(LOGIN.pass);
      // a pasted URL with a login separates the same way
      await addr.fill(JOINED);
      assert.equal(await addr.inputValue(), BARE, "the pasted login is out of the box");
      assert.equal(await user.inputValue(), LOGIN.user);
      assert.equal(await pass.inputValue(), LOGIN.pass);
      await form.locator("button", { hasText: w.typed }).click();
      for (const end = Date.now() + 30000; seen.starts.length < 2 && Date.now() < end;) await page.waitForTimeout(20);
      assert.deepEqual(seen.starts.at(-1), { agent: "claude", user: "bob@example.com", proxy: JOINED });
      await page.waitForFunction((p) => document.querySelector(".editor .signing .acts .via")?.textContent.includes(p), BARE);
      const shown = (await via.textContent()).trim();
      assert(!shown.includes("p@ss") && !shown.includes("p%40ss"), "the link shows the login");
      assert.deepEqual(errors, []);
    });
  }
}

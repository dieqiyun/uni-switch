// Run with Node's test runner and Playwright on the module path; see README.md.
// A provider's editor has Duplicate (#268): the Add form opens on a copy
// named "{name} copy", and Add posts it as new, with copyOf, its models,
// headers and balance URL, and no key (the key is the copied provider's,
// taken by uni-switch). A signed-in account has no Duplicate. The account Codex
// is signed in to can be paused while another is on (#263): its tick posts
// login/off, and paused it is dimmed, says Paused, and its tick posts
// login/on; with no other on, its tick is fixed. In English and Chinese.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const relay = {
  id: "relay", name: "Relay", icon: "generic", chat: "https://api.relay.example/v1", responses: "", anthropic: "", catalog: "",
  models: [{ id: "m1", name: "m1", on: true }, { id: "m2", name: "m2", on: false }], agents: [], fallback: [], headers: { "X-Org": "acme" }, keyList: [],
  key: { set: true, masked: "sk-…1234" }, ready: true, balanceURL: "https://api.relay.example/q?key={key}", balancePath: "data.balance",
};
const codex = (paused, spareOn) => ({
  id: "codex", name: "Codex", icon: "codex-color", chat: "", responses: "", anthropic: "", catalog: "",
  models: [{ id: "gpt-6", name: "GPT-6", on: true }], agents: [], fallback: [], headers: {}, keyList: [],
  account: {
    agent: "codex", agentName: "Codex", user: "me@example.com", plan: "PLUS",
    logins: [
      { user: "me@example.com", plan: "PLUS", active: true, on: true, paused },
      { user: "pro@example.com", plan: "PRO", on: spareOn },
    ],
  },
});

function serve(lang, list, posts) {
  const providers = { providers: list, presets: [], excluded: [], gateway: { running: true, window: true } };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/") && route.request().method() === "POST") {
      posts.push({ action: url.pathname.slice("/api/".length), body: route.request().postDataJSON() });
      return json(providers);
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { dup: "Duplicate", add: "Add", copy: "Relay copy", head: "Copy of Relay", paused: "Paused", first: "First", inUse: "In use" },
  zh: { dup: "复制", add: "添加", copy: "Relay 副本", head: "Relay 的副本", paused: "已暂停", first: "首选", inUse: "使用中" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    const w = words[lang];
    const open = async (t, list, name, wait) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 900, height: 760 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const posts = [];
      await page.route("**/*", serve(lang, list, posts));
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator(".row.provider", { hasText: name }).first().click();
      await page.locator(wait).first().waitFor();
      return { page, errors, posts };
    };
    const posted = async (page, posts, n) => {
      for (let i = 0; i < 50 && posts.length === n; i++) await page.waitForTimeout(50);
      return posts.at(-1);
    };

    test(`${engine} ${lang}: Duplicate adds a copy of the provider`, async (t) => {
      const { page, errors, posts } = await open(t, [relay, codex(false, true)], "Relay", ".editor .bar");
      await page.locator(".editor .bar").getByRole("button", { name: w.dup, exact: true }).click();
      await page.locator(".editor", { hasText: w.head }).waitFor();
      const n = posts.length;
      await page.locator(".editor .bar").getByRole("button", { name: w.add, exact: true }).click();
      const saved = await posted(page, posts, n);
      assert.equal(saved.action, "provider/save");
      assert.equal(saved.body.new, true);
      assert.equal(saved.body.copyOf, "relay");
      assert.equal(saved.body.name, w.copy);
      assert.equal(saved.body.key, "", "the key is left to uni-switch to copy");
      assert.equal(saved.body.chat, relay.chat);
      assert.deepEqual(saved.body.models, ["m1"]);
      assert.equal(saved.body.balanceURL, relay.balanceURL);
      assert.equal(saved.body.balancePath, relay.balancePath);
      assert.equal(JSON.stringify(saved.body.headers).includes("acme"), true, JSON.stringify(saved.body.headers));
      const missing = await page.evaluate(() => [
        "Duplicate", "{name} copy", "Copy of {name}", "{masked} · {name}'s key, or paste another",
        "A new provider with {name}'s URLs, key, headers, models and balance settings, to change before adding",
        "Paused", "Pause: the gateway uses the other accounts, {agent} stays signed in to this one", "Resume: the gateway uses this account first again",
      ].filter((k) => !I18N.zh[k]));
      assert.deepEqual(missing, [], "every string has its Chinese");
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: the account Codex is signed in to pauses and resumes`, async (t) => {
      const { page, errors, posts } = await open(t, [codex(false, true)], "Codex", ".editor .acc");
      assert.equal(await page.locator(".editor .bar").getByRole("button", { name: w.dup, exact: true }).count(), 0, "a signed-in account has no Duplicate");
      const me = page.locator(".editor .acc", { hasText: "me@example.com" });
      assert.equal(await me.locator(".using").textContent(), w.first);
      let n = posts.length;
      await me.locator(".tick").click();
      let p = await posted(page, posts, n);
      assert.equal(p.action, "login/off");
      assert.deepEqual(p.body, { agent: "codex", user: "me@example.com" });
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: a paused account is dimmed and resumes`, async (t) => {
      const { page, errors, posts } = await open(t, [codex(true, true)], "Codex", ".editor .acc");
      const me = page.locator(".editor .acc", { hasText: "me@example.com" });
      assert.equal(await me.locator(".using").textContent(), w.paused);
      assert(await me.evaluate((e) => e.classList.contains("off")), "dimmed");
      assert.equal(await me.locator(".tick svg").count(), 0, "not ticked");
      const n = posts.length;
      await me.locator(".tick").click();
      const p = await posted(page, posts, n);
      assert.equal(p.action, "login/on");
      assert.deepEqual(p.body, { agent: "codex", user: "me@example.com" });
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: with no other account on, it can't be paused`, async (t) => {
      const { page, errors, posts } = await open(t, [codex(false, false)], "Codex", ".editor .acc");
      const me = page.locator(".editor .acc", { hasText: "me@example.com" });
      assert.equal(await me.locator(".using").textContent(), w.inUse);
      assert(await me.locator(".tick").evaluate((e) => e.classList.contains("fixed")));
      const n = posts.length;
      await me.locator(".tick").click();
      await page.waitForTimeout(300);
      assert.equal(posts.length, n);
      assert.deepEqual(errors, []);
    });
  }
}

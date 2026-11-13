// Run with Node's test runner and Playwright on the module path; see README.md.
// A third-party Anthropic API's Key header (Aloha on Discord: a service
// answering 401 "x-api-key 和 Authorization 不可以同时存在"): the editor has a
// Key header row, Both, x-api-key or Authorization: Bearer, opening as it
// was saved or learned; Save posts the pick. A relay with only a Chat
// address has no row until an Anthropic one is typed, and none for
// Anthropic's own or Bedrock Mantle's. Anthropic's own API (bothAuth false)
// and a signed-in account have none. In English and
// Chinese, and at a phone's width.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
// the editor folds what few need under Advanced, this field among them:
// opened as a reader would, unless something set in it opened it already
const unfold = async (page) => {
  const adv = page.locator(".editor details.advanced");
  await adv.waitFor({ state: "attached" });
  if (!(await adv.evaluate((d) => d.open))) await adv.locator(":scope > summary").click();
};
const relay = {
  id: "relay", name: "Relay", icon: "generic", chat: "", responses: "", anthropic: "https://relay.example.com", catalog: "",
  models: [{ id: "claude-haiku-4-5", name: "Claude Haiku 4.5", on: true }], agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-…one" }, keyList: [], balanceToken: { takes: false, set: false }, proxy: "", authHeader: "", bothAuth: true,
};
const learned = { ...relay, id: "learned", name: "Learned", authHeader: "x-api-key" };
const chatOnly = { ...relay, id: "chatonly", name: "Chat Only", chat: "https://chat.example.com/v1", anthropic: "", bothAuth: false };
const official = { ...relay, id: "anthropic", name: "Anthropic", preset: "anthropic", icon: "", anthropic: "https://api.anthropic.com", bothAuth: false };
const codex = {
  id: "codex", name: "Codex", icon: "codex-color", chat: "", responses: "", anthropic: "", catalog: "",
  models: [{ id: "gpt-6", name: "GPT-6", on: true }], agents: [], fallback: [], headers: {}, keyList: [],
  account: { agent: "codex", agentName: "Codex", user: "me@example.com", plan: "PLUS", logins: [{ user: "me@example.com", plan: "PLUS", active: true, on: true }] },
  proxy: "",
};
const presets = [{ id: "anthropic", name: "Anthropic", anthropic: "https://api.anthropic.com", added: true }];

function serve(lang, posts) {
  const providers = { providers: [relay, learned, chatOnly, official, codex], presets, excluded: [], gateway: { running: true, window: true } };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/provider/") && route.request().method() === "POST") {
      posts.push({ action: url.pathname.slice("/api/provider/".length), body: route.request().postDataJSON() });
      return json(providers);
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { label: "Key header", both: "Both", save: "Save", more: "More endpoints", anthropic: "Anthropic URL" },
  zh: { label: "密钥请求头", both: "两者都发", save: "保存", more: "更多端点", anthropic: "Anthropic 地址" },
};
const strings = [
  "Both", "Key header",
  "Which header the API key goes in on the Anthropic API. Most services take both; pick one for a service that refuses the two together. uni-switch also switches to one by itself when a service says so.",
];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    const w = words[lang];
    const open = async (t, name, width = 900) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${name}-${width}-auth-header.png`), fullPage: true });
        }
        await browser.close();
      });
      const page = await (await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const posts = [];
      await page.route("**/*", serve(lang, posts));
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator(".row.provider", { hasText: name }).first().click();
      await unfold(page);
      await page.locator(".editor .bar").waitFor();
      return { page, errors, posts };
    };
    const seg = (page) => page.locator(".editor .segs.auth-header");
    const picked = (page) => seg(page).locator(".opt.on").textContent();
    const save = async (page, posts) => {
      const n = posts.length;
      await page.locator(".editor .bar").getByRole("button", { name: w.save, exact: true }).click();
      for (let i = 0; i < 50 && posts.length === n; i++) await page.waitForTimeout(50);
      return posts.at(-1);
    };

    test(`${engine} ${lang}: a relay sends both, and is saved with Bearer alone`, async (t) => {
      const { page, errors, posts } = await open(t, "Relay");
      assert.equal(await seg(page).isVisible(), true);
      assert.equal(await page.locator(".editor label", { hasText: w.label }).first().isVisible(), true);
      assert.equal(await picked(page), w.both);
      await seg(page).scrollIntoViewIfNeeded();
      await seg(page).getByRole("button", { name: "Authorization: Bearer" }).click();
      const saved = await save(page, posts);
      assert.equal(saved.action, "save");
      assert.equal(saved.body.id, "relay");
      assert.equal(saved.body.authHeader, "bearer");
      for (const l of ["zh", "zh-TW", "ja", "de"]) {
        const missing = await page.evaluate(([l, ks]) => ks.filter((k) => !I18N[l][k]), [l, strings]);
        assert.deepEqual(missing, [], `every string has its ${l}`);
      }
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: one learned as x-api-key opens so, and is saved back to Both`, async (t) => {
      const { page, errors, posts } = await open(t, "Learned");
      assert.equal(await picked(page), "x-api-key");
      await seg(page).scrollIntoViewIfNeeded();
      await seg(page).getByRole("button", { name: w.both, exact: true }).click();
      const saved = await save(page, posts);
      assert.equal(saved.body.id, "learned");
      assert.equal(saved.body.authHeader, "");
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: a Chat-only relay has the row once it has an Anthropic URL`, async (t) => {
      const { page, errors, posts } = await open(t, "Chat Only");
      assert.equal(await seg(page).isVisible(), false, "no row with only a Chat address");
      await page.locator(".editor details.more summary", { hasText: w.more }).click();
      const url = page.locator(".editor details.more label", { hasText: w.anthropic }).locator("xpath=following-sibling::div[1]//input");
      await url.fill("https://relay.example.com");
      assert.equal(await seg(page).isVisible(), true, "the row comes with an Anthropic address");
      await url.fill("https://api.anthropic.com");
      assert.equal(await seg(page).isVisible(), false, "Anthropic's own takes x-api-key alone");
      await url.fill("https://relay.example.com");
      assert.equal(await seg(page).isVisible(), true);
      await url.fill("https://bedrock-mantle.us-east-1.api.aws/anthropic");
      assert.equal(await seg(page).isVisible(), false, "Bedrock Mantle's takes x-api-key alone (@marktoown on X)");
      await url.fill("");
      const saved = await save(page, posts);
      assert.equal("authHeader" in saved.body, false, "not said with no Anthropic address");
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: Anthropic's own API and a signed-in account have no row`, async (t) => {
      const { page, errors, posts } = await open(t, "Anthropic");
      assert.equal(await seg(page).isVisible(), false);
      const saved = await save(page, posts);
      assert.equal("authHeader" in saved.body, false);
      await page.locator(".editor .bar").getByRole("button", { name: lang === "zh" ? "取消" : "Cancel", exact: true }).click().catch(() => {});
      await page.locator(".row.provider", { hasText: "Codex" }).first().click();
      await unfold(page);
      await page.locator(".editor .bar").waitFor();
      assert.equal(await seg(page).count(), 0);
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: at a phone's width the row fits`, async (t) => {
      const { page, errors } = await open(t, "Relay", 390);
      await seg(page).scrollIntoViewIfNeeded();
      const fits = await page.evaluate(() => {
        const s = document.querySelector(".editor .segs.auth-header");
        const r = s.getBoundingClientRect();
        return { right: r.right, width: document.documentElement.clientWidth, scroll: document.documentElement.scrollWidth, over: [...s.querySelectorAll(".opt")].some((b) => b.scrollWidth > b.clientWidth + 1) };
      });
      assert.ok(fits.right <= fits.width + 0.5, `the control ends inside the page (${fits.right} > ${fits.width})`);
      assert.ok(fits.scroll <= fits.width, "no sideways scroll");
      assert.equal(fits.over, false, "no option's name is cut off");
      assert.deepEqual(errors, []);
    });
  }
}

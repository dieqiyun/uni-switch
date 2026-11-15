// Run with Node's test runner and Playwright on the module path; see README.md.
// mamba on Discord: Codex's only account couldn't be removed ("codex is
// signed in to … now; switch to another account first"). Its row now has
// Sign out, asked in the app's own dialog: Cancel and Escape send nothing
// and go back to the editor,
// Sign out posts login/forget and says so. With another account saved, the
// one in use has no Sign out, and a refusal says what to do in the reader's
// language. No select, no left border, the page doesn't move. In English
// and Chinese, Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const codex = (logins) => ({
  id: "codex", name: "Codex", icon: "codex-color", chat: "", responses: "", anthropic: "", catalog: "", routing: "smart",
  models: [{ id: "gpt-6", name: "GPT-6", on: true }], agents: [], fallback: [], headers: {}, keyList: [],
  account: { agent: "codex", agentName: "Codex", agentIcon: "codex-color", user: logins[0].user, plan: "PLUS", logins },
});
const claude = (logins) => ({
  id: "claude", name: "Claude", icon: "claude-color", chat: "", responses: "", anthropic: "", catalog: "", routing: "smart",
  models: [{ id: "claude-x", name: "Claude X", on: true }], agents: [], fallback: [], headers: {}, keyList: [],
  account: { agent: "claude", agentName: "Claude Code", agentIcon: "claude-color", user: logins[0].user, plan: "MAX", logins },
});
const one = [{ user: "me@example.com", plan: "PLUS", active: true, on: true }];
const two = [...one, { user: "work@example.com", plan: "PRO", on: true }];

function serve(lang, logins, posts, refuse, of = codex) {
  const providers = { providers: [of(logins)], presets: [], excluded: [], gateway: { running: true, window: true } };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/login/forget") {
      posts.push(req.postDataJSON());
      if (refuse === "only") return route.fulfill({ status: 400, json: { error: "Claude Code is signed in to me@example.com", code: "signed_in_only", agent: "claude", user: "me@example.com" } });
      if (refuse) return route.fulfill({ status: 400, json: { error: "codex is signed in to me@example.com now", code: "signed_in", agent: "codex", user: "me@example.com" } });
      return json({ ...providers, providers: [] });
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { out: "Sign out", remove: "Remove", cancel: "Cancel", ask: "Sign Codex out of me@example.com?", text: /Codex is signed out, as codex logout does/, done: "Codex signed out of me@example.com", refused: /Codex is signed in to me@example\.com now: sign it in to another of its accounts first/, only: /Claude Code is signed in to me@example\.com, the only account in uni-switch it can be signed in to/ },
  zh: { out: "退出登录", remove: "移除", cancel: "取消", ask: "让 Codex 退出 me@example.com？", text: /codex logout/, done: "Codex 已退出 me@example.com", refused: /Codex 当前登录的是 me@example\.com：请先在另一个账号上点「使用」/, only: /Claude Code 当前登录的是 me@example\.com，这是 uni-switch 里它唯一能登录的账号/ },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    const w = words[lang];
    const open = async (t, logins, refuse = false, of = codex) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 900, height: 760 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      const posts = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, logins, posts, refuse, of));
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator(".row.provider", { hasText: of === claude ? "Claude" : "Codex" }).first().click();
      await page.locator(".editor .acc").first().waitFor();
      return { page, errors, posts };
    };

    // an account's Remove and Sign out wait under its ⋯
    const more = async (row) => { await row.hover(); if (await row.locator(".acc-more").count()) await row.locator(".acc-more").click(); };

    test(`${engine} ${lang}: Codex's only account signs out, asked first`, async (t) => {
      const { page, errors, posts } = await open(t, one);
      const top = await page.evaluate(() => document.scrollingElement.scrollTop);
      const row = page.locator(".editor .acc", { hasText: "me@example.com" });
      const out = row.getByRole("button", { name: w.out, exact: true });
      await more(row);
      assert.equal(await out.count(), 1, "the only account has Sign out");
      assert.ok(await out.getAttribute("title"));

      const ask = page.locator("#modal .forget-ask");
      await out.click();
      await ask.waitFor();
      assert.equal(await ask.locator(".ehead b").textContent(), w.ask);
      assert.match(await ask.locator(".lib-confirm").textContent(), w.text);
      assert.equal(await page.locator("select").count(), 0, "no native select");
      const stripe = await ask.evaluate((n) => [n, ...n.querySelectorAll("*")].some((x) => parseFloat(getComputedStyle(x).borderLeftWidth) > 1 && getComputedStyle(x).borderLeftStyle !== "none" && getComputedStyle(x).borderLeftWidth !== getComputedStyle(x).borderRightWidth));
      assert.equal(stripe, false, "no left-border accent");
      await ask.getByRole("button", { name: w.cancel, exact: true }).click();
      await ask.waitFor({ state: "detached" });
      await row.waitFor(); // back in the editor it was asked from
      await out.click();
      await ask.waitFor();
      await page.keyboard.press("Escape");
      await ask.waitFor({ state: "detached" });
      await row.waitFor();
      assert.deepEqual(posts, [], "Cancel and Escape send nothing");

      await out.click();
      await ask.getByRole("button", { name: w.out, exact: true }).click();
      await ask.waitFor({ state: "detached" });
      assert.deepEqual(posts, [{ agent: "codex", user: "me@example.com" }]);
      await page.waitForFunction((s) => document.querySelector("#status")?.textContent.includes(s), w.done);
      assert.equal(await page.evaluate(() => document.scrollingElement.scrollTop), top, "the page doesn't move");
      const missing = await page.evaluate(() => [
        "Sign out", "Signs Codex out of this account, as codex logout does", "Sign {agent} out of {user}?",
        "{agent} is signed out, as codex logout does, and uni-switch forgets the account. Sign in again to use it; an open {agent} may need quitting and opening again. The account itself is left as it is.",
        "{agent} signed out of {user}", "Codex itself is signed out too, as codex logout does.",
        "{agent} is signed in to {user} now: sign it in to another of its accounts first (Use on that account), then remove this one",
      ].filter((k) => !I18N.zh[k] || !I18N.ja[k] || !I18N.de[k]));
      assert.deepEqual(missing, [], "every string has its zh, ja and de");
      assert.deepEqual(errors, []);
    });

    // vincentzhang1_55530 on Discord: the one in use is removed as the
    // others are, and Codex is signed in to another first (ForgetLogin)
    test(`${engine} ${lang}: with another account saved, the one in use has Remove, not Sign out`, async (t) => {
      const { page, errors, posts } = await open(t, two);
      for (const user of ["me@example.com", "work@example.com"]) {
        const row = page.locator(".editor .acc", { hasText: user });
        await more(row);
        assert.equal(await row.getByRole("button", { name: w.out, exact: true }).count(), 0, user);
      }
      const remove = page.locator(".editor .acc", { hasText: "me@example.com" }).getByRole("button", { name: w.remove, exact: true });
      assert.equal(await remove.count(), 1, "the one in use has Remove");
      assert.ok(await remove.getAttribute("title"));
      await remove.click();
      const confirm = page.locator("dialog.action-confirm[open]");
      await confirm.waitFor();
      assert.deepEqual(posts, [], "Remove waits for confirmation");
      await confirm.getByRole("button", { name: w.cancel, exact: true }).click();
      assert.deepEqual(posts, [], "Cancel keeps the signed-in account");
      assert.equal(await remove.count(), 1, "Cancel keeps the account's Remove button");
      await remove.click();
      await confirm.getByRole("button", { name: w.remove, exact: true }).click();
      await page.waitForFunction(() => document.querySelector("#status")?.textContent.includes("me@example.com"));
      assert.deepEqual(posts, [{ agent: "codex", user: "me@example.com" }]);
      const missing = await page.evaluate(() => ["{agent} is signed in to another of its accounts, and uni-switch forgets this one; the account itself is untouched"]
        .filter((k) => !I18N.zh[k] || !I18N.ja[k] || !I18N.de[k]));
      assert.deepEqual(missing, [], "every string has its zh, ja and de");
      assert.deepEqual(errors, []);
    });

    // StringKe (#4532): Claude Code's account in use is removed as Codex's
    // is, signed in to another first, while it has another it can be
    // signed in to; a setup-token one isn't that, and uni-switch never signs
    // Claude Code out, so its only one has no Remove
    test(`${engine} ${lang}: Claude Code's account in use has Remove while another can take its place`, async (t) => {
      const { page, errors, posts } = await open(t, [...one, { user: "work@example.com", plan: "MAX", on: true }], false, claude);
      const row = page.locator(".editor .acc", { hasText: "me@example.com" });
      await more(row);
      const remove = row.getByRole("button", { name: w.remove, exact: true });
      assert.equal(await remove.count(), 1, "the one in use has Remove");
      assert.match(await remove.getAttribute("title"), /Claude Code/);
      await remove.click();
      const confirm = page.locator("dialog.action-confirm[open]");
      await confirm.getByRole("button", { name: w.remove, exact: true }).click();
      await page.waitForFunction(() => document.querySelector("#status")?.textContent.includes("me@example.com"));
      assert.deepEqual(posts, [{ agent: "claude", user: "me@example.com" }]);
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: Claude Code's only account it can be signed in to has no Remove`, async (t) => {
      const { page, errors } = await open(t, [...one, { user: "ci@example.com", plan: "MAX", on: true, token: true }], false, claude);
      const row = page.locator(".editor .acc", { hasText: "me@example.com" });
      await more(row);
      assert.equal(await row.getByRole("button", { name: w.remove, exact: true }).count(), 0);
      assert.equal(await row.getByRole("button", { name: w.out, exact: true }).count(), 0, "uni-switch never signs Claude Code out");
      const missing = await page.evaluate(() => ["{agent} is signed in to {user}, the only account in uni-switch it can be signed in to, and uni-switch doesn't sign it out: add another account first, or sign out in {agent} itself, then remove this one"]
        .filter((k) => !I18N.zh[k] || !I18N["zh-TW"][k] || !I18N.ja[k] || !I18N.de[k]));
      assert.deepEqual(missing, [], "every string has its zh, zh-TW, ja and de");
      assert.deepEqual(errors, []);
    });

    // the CLI, or a GUI a step behind the accounts: the refusal of the
    // only one says so, not "sign it in to another first"
    test(`${engine} ${lang}: the only Claude account's refusal says it is the only one`, async (t) => {
      const { page, errors } = await open(t, [...one, { user: "work@example.com", plan: "MAX", on: true }], "only", claude);
      const row = page.locator(".editor .acc", { hasText: "me@example.com" });
      await more(row);
      await row.getByRole("button", { name: w.remove, exact: true }).click();
      await page.locator("dialog.action-confirm[open]").getByRole("button", { name: w.remove, exact: true }).click();
      await page.waitForFunction(() => /only account|唯一/.test([...document.querySelectorAll(".editor-error, #status")].map((n) => n.textContent).join(" ")));
      const said = await page.evaluate(() => [...document.querySelectorAll(".editor-error, #status")].map((n) => n.textContent).join(" "));
      assert.match(said, w.only);
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: a refusal says what to do, in the reader's language`, async (t) => {
      const { page, errors } = await open(t, one, true);
      await more(page.locator(".editor .acc", { hasText: "me@example.com" }));
      await page.locator(".editor .acc", { hasText: "me@example.com" }).getByRole("button", { name: w.out, exact: true }).click();
      const ask = page.locator("#modal .forget-ask");
      await ask.getByRole("button", { name: w.out, exact: true }).click();
      await page.waitForFunction(() => /me@example\.com/.test([...document.querySelectorAll(".editor-error, #status")].map((n) => n.textContent).join(" ")));
      const said = await page.evaluate(() => [...document.querySelectorAll(".editor-error, #status")].map((n) => n.textContent).join(" "));
      assert.match(said, w.refused);
      assert.deepEqual(errors, []);
    });
  }
}

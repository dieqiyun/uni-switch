// Run with Node's test runner and Playwright on the module path; see README.md.
// #2367 (Dony2050): Codex keeps its sign-in in the macOS keychain, and the
// keychain didn't give it to uni-switch (the prompt denied, or answered too
// late). The Providers page's line that says so has Try again, which posts
// provider/keychain-retry once; the line goes when the read gets it, and a
// line with no keychain to ask has no Try again. In English and Chinese,
// Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const why = "1 account is saved in uni-switch, and they are only offered beside the account Codex is signed in to. Codex keeps its sign-in in the macOS keychain (cli_auth_credentials_store = \"auto\" in /Users/me/.codex/config.toml), and macOS's keychain prompt was denied or cancelled. Allow uni-switch when macOS asks for the keychain (\"Always Allow\" lets /usr/bin/security read that item from then on, so macOS stops asking each time Codex changes it), or set cli_auth_credentials_store = \"file\" there and sign in again (codex login).";

function serve(lang, posts, retry) {
  let read = false;
  const providers = () => ({
    providers: [], presets: [], gateway: { running: true, window: true },
    excluded: read ? [] : [{ agent: "codex", agentName: "Codex", agentIcon: "codex-color", signedOut: true, why, users: ["me@example.com"], retry }],
  });
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    // the state read after the retry is slow, as on a busy machine: the
    // line goes with the list, and the status is said once the state is in
    if (url.pathname === "/api/state") {
      if (read) await new Promise((r) => setTimeout(r, 400));
      return json(state);
    }
    if (url.pathname === "/api/providers") return json(providers());
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (route.request().method() === "POST") {
      posts.push({ path: url.pathname, body: route.request().postDataJSON() });
      if (url.pathname === "/api/provider/keychain-retry") read = true;
      return json(providers());
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { line: "Codex's saved accounts aren't offered.", again: "Try again", remove: "Remove", title: "Ask the macOS keychain for Codex's sign-in again", done: "Codex's sign-in was read from the keychain" },
  zh: { line: "Codex 保存的账号没有提供出来。", again: "重试", remove: "移除", title: "再向 macOS 钥匙串读取 Codex 的登录", done: "已从钥匙串读到 Codex 的登录" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    const w = words[lang];
    const open = async (t, posts, retry) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 900, height: 760 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, posts, retry));
      await page.goto("http://uni-switch.test/?view=providers");
      const line = page.locator("#excluded .excluded", { hasText: w.line });
      await line.waitFor();
      return { page, errors, line };
    };

    test(`${engine} ${lang}: a keychain read refused has Try again, which reads once`, async (t) => {
      const posts = [];
      const { page, errors, line } = await open(t, posts, true);
      assert.match(await line.textContent(), /"Always Allow"/);
      const again = line.getByRole("button", { name: w.again, exact: true });
      assert.equal(await again.getAttribute("title"), w.title);
      // the link style its neighbours have, before Remove
      assert.equal(await again.getAttribute("class"), "link");
      const names = await line.getByRole("button").allTextContents();
      assert.deepEqual(names, [w.again, w.remove]);
      await again.click();
      await line.waitFor({ state: "detached" });
      assert.deepEqual(posts, [{ path: "/api/provider/keychain-retry", body: {} }]);
      // said after the state is read again, which comes after the line goes
      await page.waitForFunction((done) => document.querySelector("#status")?.textContent === done, w.done).catch(() => {});
      assert.equal(await page.locator("#status").textContent(), w.done);
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: a line with no keychain to ask has no Try again`, async (t) => {
      const posts = [];
      const { errors, line } = await open(t, posts, false);
      assert.equal(await line.getByRole("button", { name: w.again, exact: true }).count(), 0);
      assert.deepEqual(errors, []);
    });
  }
}

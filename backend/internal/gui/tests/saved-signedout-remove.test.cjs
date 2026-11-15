// Run with Node's test runner and Playwright on the module path; see README.md.
// Saved accounts of an agent signed out here (Discord: a banned Claude
// account, "现在没法移除了"): the Providers page's line that says they
// aren't offered has Remove, which asks first; Cancel and Escape post
// nothing, and Remove posts login/forget for each saved account, the line
// going once the store has none. In English and Chinese, Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const why = "2 accounts are saved in uni-switch, but it isn't signed in here (nothing at /home/me/.claude/.credentials.json), and they are only offered beside the account it is signed in to. Sign in (claude, then /login) with this HOME.";
const USERS = ["banned@example.com", "other@example.com"];

function serve(lang, posts) {
  const saved = new Set(USERS);
  const providers = () => ({
    providers: [], presets: [], gateway: { running: true, window: true },
    excluded: saved.size ? [{ agent: "claude", agentName: "Claude Code", agentIcon: "claudecode-color", signedOut: true, why, users: [...saved] }] : [],
  });
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers());
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (route.request().method() === "POST") {
      const body = route.request().postDataJSON();
      posts.push({ path: url.pathname, body });
      if (url.pathname === "/api/login/forget") saved.delete(body.user);
      return json(providers());
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: {
    line: "Claude Code's saved accounts aren't offered.", remove: "Remove", cancel: "Cancel",
    ask: "Remove Claude Code's 2 saved accounts?",
    says: "uni-switch forgets its copy of banned@example.com, other@example.com. Claude Code's own files and sign-in, and the account itself, are left as they are.",
    done: "banned@example.com, other@example.com removed",
  },
  zh: {
    line: "Claude Code 保存的账号没有提供出来。", remove: "移除", cancel: "取消",
    ask: "移除 Claude Code 保存的 2 个账号？",
    says: "移除 uni-switch 保存的 banned@example.com, other@example.com。Claude Code 自己的文件、登录和账号本身均不受影响。",
    done: "已移除 banned@example.com, other@example.com",
  },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    const w = words[lang];
    const open = async (t, posts) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 900, height: 760 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, posts));
      await page.goto("http://uni-switch.test/?view=providers");
      const line = page.locator("#excluded .excluded", { hasText: w.line });
      await line.waitFor();
      return { page, errors, line };
    };

    test(`${engine} ${lang}: a signed-out agent's saved accounts can be removed, asked first`, async (t) => {
      const posts = [];
      const { page, errors, line } = await open(t, posts);
      const rm = line.getByRole("button", { name: w.remove, exact: true });
      await rm.click();
      const dialog = page.locator("#modal .forget-ask");
      await dialog.waitFor();
      assert.equal((await dialog.locator(".ehead b").textContent()).trim(), w.ask);
      assert.equal((await dialog.locator(".lib-confirm").textContent()).trim(), w.says);
      // Cancel, and Escape, leave them saved
      await dialog.getByRole("button", { name: w.cancel, exact: true }).click();
      await page.locator("#modal").waitFor({ state: "hidden" });
      await rm.click();
      await dialog.waitFor();
      await page.keyboard.press("Escape");
      await page.locator("#modal").waitFor({ state: "hidden" });
      assert.deepEqual(posts, []);
      // Remove forgets each, in uni-switch's store only
      await rm.click();
      await dialog.getByRole("button", { name: w.remove, exact: true }).click();
      await page.locator("#modal").waitFor({ state: "hidden" });
      assert.deepEqual(posts, USERS.map((user) => ({ path: "/api/login/forget", body: { agent: "claude", user } })));
      await line.waitFor({ state: "detached" });
      assert.equal(await page.locator("#status").textContent(), w.done);
      assert.deepEqual(errors, []);
    });
  }
}

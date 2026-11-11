// Run with Node's test runner and Playwright on the module path; see README.md.
// Custom proxy remembered (Zane on Discord): a SOCKS address with a login,
// then Off or Auto, then Custom again shows the same address and login,
// after a restart too. The page never sends the remembered copy itself:
// settings.Save keeps it from the proxy, which the fixture does as Go does.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
function server(lang, posts) {
  let cur = {
    theme: "light", lang, tray: "panel", currency: "usd", textSize: 100,
    proxy: "", proxyNow: "", proxySource: "none",
    trayUsageEvery: 3, version: "test", dir: "/test", lanURLs: [],
    visionModels: [], imageGenModels: [], workbuddyCheckins: [],
    fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
  };
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = ${JSON.stringify({ lang, theme: "light", web: true })};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" }, fx: cur.fx });
    if (url.pathname === "/api/settings") {
      if (req.method() === "POST") {
        const body = req.postDataJSON();
        posts.push(body);
        // as the settings API: proxyCustom is the server's, set from proxy
        const own = body.proxy && body.proxy !== "direct";
        cur = { ...cur, ...body, proxyCustom: own ? body.proxy : cur.proxyCustom, proxyNow: body.proxy, proxySource: body.proxy === "direct" ? "off" : body.proxy ? "settings" : "none" };
      }
      return json(cur);
    }
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    return route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const labels = {
  en: { user: "Username (optional)", pass: "Password (optional)", custom: "Custom", off: "Off", auto: "Auto", saved: "Saved" },
  zh: { user: "用户名（可选）", pass: "密码（可选）", custom: "自定义", off: "关闭", auto: "自动", saved: "已保存" },
  "zh-TW": { user: "使用者名稱（選填）", pass: "密碼（選填）", custom: "自定義", off: "關閉", auto: "自動", saved: "已儲存" },
  ja: { user: "ユーザー名（任意）", pass: "パスワード（任意）", custom: "カスタム", off: "オフ", auto: "自動", saved: "保存しました" },
  de: { user: "Benutzername (optional)", pass: "Passwort (optional)", custom: "Benutzerdefiniert", off: "Aus", auto: "Automatisch", saved: "Gespeichert" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(labels)) {
    const l = labels[lang];
    test(`${engine} ${lang}: Custom brings back the proxy and login after Off and Auto`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const context = await browser.newContext({ viewport: { width: 1100, height: 760 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(4000);
      page.setDefaultNavigationTimeout(60000);
      const posts = [], errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(lang, posts));
      await page.goto("http://uni-switch.test/?view=settings&tab=network");
      const box = page.locator("#proxySegs");
      const address = box.locator("input.proxy");
      const user = box.getByRole("textbox", { name: l.user, exact: true });
      const pass = box.getByLabel(l.pass, { exact: true });
      const mode = (name) => box.getByRole("button", { name, exact: true });
      const saved = async (action, want) => {
        const count = posts.length;
        const response = page.waitForResponse((r) => r.url().endsWith("/api/settings") && r.request().method() === "POST");
        await action();
        await response;
        await page.waitForFunction((word) => document.querySelector("#status")?.textContent.includes(word), l.saved);
        assert.equal(posts.length, count + 1, "save once");
        assert.equal(posts.at(-1).proxy, want);
        assert(!("proxyCustom" in posts.at(-1)), "the page sends the remembered proxy");
      };
      const restored = async (why) => {
        await mode(l.custom).click();
        assert.equal(await address.inputValue(), "socks5://127.0.0.1:1080", why);
        assert.equal(await user.inputValue(), "名 a@b", why);
        assert.equal(await pass.inputValue(), "p@ss 密", why);
      };

      const login = "socks5://%E5%90%8D%20a%40b:p%40ss%20%E5%AF%86@127.0.0.1:1080";
      await mode(l.custom).click();
      await address.fill("socks5://127.0.0.1:1080");
      await user.fill("名 a@b");
      await pass.fill("p@ss 密");
      await saved(() => pass.press("Enter"), login);

      await saved(() => mode(l.off).click(), "direct");
      const offSaves = posts.length;
      await restored("after Off");
      assert.equal(posts.length, offSaves, "picking Custom saves nothing by itself");
      await pass.press("Escape");
      assert.equal(posts.length, offSaves, "Escape keeps Off");

      await page.reload(); // a restart: the copy is the server's, not the page's
      await restored("after Off and a restart");
      await pass.press("Escape");

      await saved(() => mode(l.auto).click(), "");
      await restored("after Auto");
      // leaving the restored fields puts the proxy back in force
      await saved(() => pass.press("Enter"), login);

      // a draft never saved is not what Custom brings back
      await address.fill("socks5://127.0.0.1:2080");
      await user.fill("unsaved");
      await saved(() => mode(l.off).click(), "direct");
      await restored("after an unsaved draft");
      assert.deepEqual(errors, []);
    });
  }
}

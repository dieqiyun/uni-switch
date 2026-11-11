// Run with Node's test runner and Playwright on the module path; see README.md.
// A plugin's browser sign-in that comes back to a port on uni-switch's machine
// (Devin's, Kiro's, Trae's, Zed's plugins) takes the address the browser
// ended on, for a uni-switch on a server or in Docker opened from another
// computer (Chicring): beside the plugin's own words, the same field a
// built-in's sign-in shows, posted to the sign-in, and the plugin's API key
// way still offered. Command Code's plugin, whose page posts its key from
// script and leaves no address, takes a key made on its keys page in the
// same place, as the built-in did (#2468), and offers no second key way.
// A narrow window, English and Chinese, Chromium and WebKit; no backend,
// the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const words = {
  en: { field: "Callback URL", finish: "Finish sign-in", hint: /copy its whole address and paste it here/, key: "Use an API key instead", apiKey: "API key", keys: "Open the keys page" },
  zh: { field: "回调 URL", finish: "完成登录", hint: /请复制其完整地址粘贴到这里/, key: "改用 API 密钥", apiKey: "API 密钥", keys: "打开密钥页面" },
};
const pasted = "http://localhost:51052/callback?state=s&code=good";

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    const w = words[lang];
    for (const cc of [false, true]) test(`${engine} ${lang}: a plugin's browser sign-in takes the pasted ${cc ? "key" : "address"}`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await browser.newPage({ viewport: { width: 360, height: 700 }, reducedMotion: "reduce" });
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      let posted = null;
      await page.route("**/*", async (route) => {
        const url = new URL(route.request().url());
        const json = (data) => route.fulfill({ json: data });
        if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
        if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
        if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
        if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: {} });
        if (url.pathname === "/api/groups") return json({ groups: [] });
        if (url.pathname === "/api/signin/plug-flow/callback") {
          posted = route.request().postDataJSON().url;
          return route.fulfill({ status: 204 });
        }
        if (url.pathname.startsWith("/api/")) return json({});
        const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
        const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
        try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
      });
      await page.goto("http://uni-switch.test/");
      await page.waitForFunction((l) => state.settings.lang === l, lang);
      await page.evaluate((cc) => {
        signing = cc
          ? { id: "plug-flow", agent: "commandcode-plan", method: 0, state: "waiting", pasteKey: true, keysURL: "https://commandcode.ai/settings/keys",
            url: "https://commandcode.ai/studio/auth/cli?callback=http%3A%2F%2F127.0.0.1%3A51052%2Fcallback", instructions: "Sign in to Command Code in the browser." }
          : { id: "plug-flow", agent: "devin", method: 0, state: "waiting", pasteCallback: true,
            url: "https://app.devin.ai/auth/cli/continue?redirect_uri=http%3A%2F%2F127.0.0.1%3A51052%2Fcallback&state=s",
            instructions: "Sign in to Devin in the browser." };
        const host = document.createElement("div");
        host.style.cssText = "position:fixed;inset:0 auto auto 0;width:100%;z-index:1000;";
        const name = cc ? "Command Code" : "Devin";
        host.append(renderSigning({ name, agent: signing.agent, pid: signing.agent,
          plugin: { methods: [{ type: "oauth", label: "Browser" }, { type: "api", label: "API key" }] } }));
        document.body.append(host);
      }, cc);
      await page.getByText(cc ? "Sign in to Command Code in the browser." : "Sign in to Devin in the browser.").waitFor();
      if (cc) {
        await page.getByRole("button", { name: w.keys }).waitFor();
        assert.equal(await page.getByRole("button", { name: w.key }).count(), 0, "a second key way beside the field");
      } else {
        await page.getByText(w.hint).waitFor();
        await page.getByRole("button", { name: w.key }).waitFor();
      }
      const input = page.getByRole("textbox", { name: cc ? w.apiKey : w.field });
      const submit = page.getByRole("button", { name: w.finish });
      assert(await submit.isDisabled());
      const value = cc ? "user_cc_key1" : pasted;
      await input.fill(value);
      await submit.click();
      await page.waitForFunction(() => signing.callbackSubmitted);
      assert.equal(posted, value);
      assert(await input.isDisabled());
      for (const b of [await input.boundingBox(), await submit.boundingBox()]) {
        assert(b.x >= 0 && b.x + b.width <= 360, "the paste field overflowed the narrow window");
      }
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= 360), true, "the page scrolls sideways");
      if (process.env.ARTIFACT_DIR) {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `signin-paste-plugin-${cc ? "key-" : ""}${engine}-${lang}.png`) });
      }
      assert.deepEqual(errors, []);
    });
  }
}

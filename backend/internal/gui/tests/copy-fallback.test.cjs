// Run with Node's test runner and Playwright on the module path; see README.md.
// A copy uni-switch couldn't make (#1881, BabyLy233: on KDE Plasma Wayland the
// app said "copied" and the clipboard stayed empty). uni-switch now refuses a
// copy it couldn't read back (/api/copy answers 501), and the page tries the
// clipboard API, then the copy command, and says it failed only when none
// copied, with the text to copy by hand. A copy uni-switch made is not copied
// again. Every language, Chromium and WebKit; no backend, the API is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const LINK = "https://factory.example/device?code=WDJB-MJHT&state=" + "x".repeat(40);

function server(lang, uniSwitchCopies) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [{ id: "openai", name: "OpenAI", icon: "openai", preset: "openai", models: [], agents: [], key: { set: true, masked: "sk-…ab12" } }], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    const signing = { id: "s1", agent: "factory", state: "waiting", url: LINK, code: "WDJB-MJHT" };
    if (url.pathname === "/api/signin" || url.pathname.startsWith("/api/signin/")) return json(signing);
    if (url.pathname === "/api/copy") return route.fulfill({ status: uniSwitchCopies ? 204 : 501, body: "" });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const W = {
  en: { anyway: "Sign in anyway", copied: "Sign-in link copied", failed: "Couldn't copy. Select it here and copy it yourself: " + LINK },
  zh: { anyway: "仍然登录", copied: "已复制登录链接", failed: "复制失败，请在这里选中后自行复制：" + LINK },
  "zh-TW": { anyway: "仍然登入", copied: "已複製登入連結", failed: "複製失敗，請在這裡選取後自行複製：" + LINK },
  ja: { anyway: "それでもサインイン", copied: "サインインリンクをコピーしました", failed: "コピーできませんでした。ここで選択してコピーしてください：" + LINK },
  de: { anyway: "Trotzdem anmelden", copied: "Anmeldelink kopiert", failed: "Kopieren fehlgeschlagen. Hier markieren und selbst kopieren: " + LINK },
};

// uni-switch: /api/copy copies; api: the clipboard API copies; command: the
// copy command copies; none: nothing does
const CASES = [
  { name: "uni-switch copies", uniSwitch: true, api: true, command: true, want: "copied", writes: [], commands: [] },
  { name: "uni-switch refuses, the clipboard API copies", uniSwitch: false, api: true, command: true, want: "copied", writes: [LINK], commands: [] },
  { name: "uni-switch and the clipboard API refuse, the copy command copies", uniSwitch: false, api: false, command: true, want: "copied", writes: [LINK], commands: [LINK] },
  { name: "nothing copies", uniSwitch: false, api: false, command: false, want: "failed", writes: [LINK], commands: [LINK] },
];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(W)) {
    for (const c of CASES) {
      test(`${engine} ${lang}: ${c.name}`, async (t) => {
        const w = W[lang];
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const page = await (await browser.newContext({ viewport: { width: 900, height: 800 }, reducedMotion: "reduce" })).newPage();
        t.after(() => browser.close());
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        await page.addInitScript(({ api, command }) => {
          window.__writes = [];
          window.__commands = [];
          Object.defineProperty(navigator, "clipboard", {
            configurable: true,
            value: { writeText: (s) => { window.__writes.push(s); return api ? Promise.resolve() : Promise.reject(new DOMException("refused", "NotAllowedError")); } },
          });
          document.execCommand = (cmd) => {
            if (cmd !== "copy") return false;
            const a = document.activeElement;
            window.__commands.push(a && a.tagName === "TEXTAREA" ? a.value.slice(a.selectionStart, a.selectionEnd) : String(getSelection()));
            return command;
          };
        }, { api: c.api, command: c.command });
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, c.uniSwitch));
        await page.goto("http://uni-switch.test/?view=providers");
        await page.locator("#addProvider").click();
        const sheet = page.locator("#addSheet");
        await sheet.locator('.tile[data-pick="Factory"]').click();
        const box = sheet.locator(".signing");
        await box.locator("button", { hasText: w.anyway }).click();
        const cp = box.locator(".signlink button.copy");
        await cp.waitFor();

        await cp.click();
        const want = w[c.want];
        await page.waitForFunction((s) => document.querySelector("#status").textContent === s, want);
        const st = page.locator("#status");
        if (c.want === "failed") {
          assert.ok(await st.evaluate((e) => e.classList.contains("err")), "the failure reads as an error");
          assert.equal(await st.getAttribute("role"), "alert");
          assert.ok(await st.isVisible(), "the failure is shown");
          assert.equal(await cp.evaluate((b) => b.classList.contains("done")), false, "the button doesn't say it copied");
        } else {
          assert.ok(await st.evaluate((e) => e.classList.contains("ok")));
          assert.ok(await cp.evaluate((b) => b.classList.contains("done")), "the button says it copied");
        }
        assert.deepEqual(await page.evaluate(() => window.__writes), c.writes, "clipboard API calls");
        assert.deepEqual(await page.evaluate(() => window.__commands), c.commands, "copy command calls");
        assert.equal(await page.locator("textarea[readonly]").count(), 0, "the textarea is gone");
        assert.deepEqual(errors, []);
      });
    }
  }
}

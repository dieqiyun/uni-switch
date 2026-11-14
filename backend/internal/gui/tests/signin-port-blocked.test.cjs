// Run with Node's test runner and Playwright on the module path; see README.md.
// A ChatGPT sign-in whose callback port couldn't be opened (#1782,
// nianlee-official: "port 1455 … is busy" with nothing of ChatGPT's
// running, on Windows). The sign-in goes on, and its box says why the page
// the browser ends on won't finish it: Windows reserving the port, or
// another program holding it, with the port named. Its address pasted
// finishes the sign-in. Every language, Chromium and WebKit, wide and
// narrow; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const LINK = "https://auth.openai.com/oauth/authorize?client_id=app&redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback&state=st1";
const BACK = "http://localhost:1455/auth/callback?code=ac_fake&scope=openid&state=st1";

function server(lang, blocked, posted) {
  let signing = { id: "cx1", agent: "codex", state: "waiting", url: LINK, pasteCallback: true, callbackBlocked: blocked, callbackPort: 1455 };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [{ id: "openai", name: "OpenAI", icon: "openai", preset: "openai", models: [], agents: [], key: { set: true, masked: "sk-…ab12" } }], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname === "/api/signin/cx1/callback") {
      posted.push(route.request().postDataJSON());
      signing = { ...signing, state: "done", user: "cx@example.com", using: true };
      return route.fulfill({ status: 204 });
    }
    if (url.pathname === "/api/signin" || url.pathname === "/api/signin/cx1") return json(signing);
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const W = {
  en: {
    reserved: "Windows keeps port 1455, where the sign-in comes back, for Hyper-V, WSL or Docker, so the page the browser ends on won't load. Copy its whole address and paste it here.",
    busy: "Another program has port 1455, where the sign-in comes back, so the page the browser ends on won't finish the sign-in. Copy its whole address and paste it here.",
    docker: "(uni-switch runs on a server or in Docker)", field: "Callback URL", finish: "Finish sign-in",
  },
  zh: {
    reserved: "Windows 为 Hyper-V、WSL 或 Docker 保留了登录回调的端口 1455，浏览器最终停在的页面将无法打开。请复制其完整地址粘贴到这里。",
    busy: "登录回调的端口 1455 被其他程序占用，浏览器最终停在的页面无法完成登录。请复制其完整地址粘贴到这里。",
    docker: "uni-switch 运行在服务器或 Docker 中", field: "回调 URL", finish: "完成登录",
  },
  "zh-TW": {
    reserved: "Windows 為 Hyper-V、WSL 或 Docker 保留了登入回呼的連接埠 1455，瀏覽器最終停在的頁面將無法開啟。請複製其完整位址貼上到這裡。",
    busy: "登入回呼的連接埠 1455 被其他程式佔用，瀏覽器最終停在的頁面無法完成登入。請複製其完整位址貼上到這裡。",
    docker: "uni-switch 執行在伺服器或 Docker 中",
  },
  ja: {
    reserved: "サインインの戻り先であるポート 1455 は Windows が Hyper-V、WSL または Docker 用に予約しているため、ブラウザの最後のページは読み込めません。そのアドレス全体をコピーしてここに貼り付けてください。",
    busy: "サインインの戻り先であるポート 1455 を別のプログラムが使っているため、ブラウザの最後のページではサインインが完了しません。そのアドレス全体をコピーしてここに貼り付けてください。",
    docker: "uni-switch がサーバーや Docker で動いているとき",
  },
  de: {
    reserved: "Windows hält Port 1455, auf den die Anmeldung zurückkehrt, für Hyper-V, WSL oder Docker frei, daher lädt die Seite, auf der der Browser landet, nicht. Kopieren Sie ihre vollständige Adresse und fügen Sie sie hier ein.",
    busy: "Ein anderes Programm belegt Port 1455, auf den die Anmeldung zurückkehrt, daher schließt die Seite, auf der der Browser landet, die Anmeldung nicht ab. Kopieren Sie ihre vollständige Adresse und fügen Sie sie hier ein.",
    docker: "uni-switch läuft auf einem Server oder in Docker",
  },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(W)) {
    for (const blocked of ["reserved", "busy"]) {
      for (const width of [900, 420]) {
        test(`${engine} ${lang} ${blocked} ${width}: a ChatGPT sign-in says why its port can't be had`, async (t) => {
          const w = W[lang];
          const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
          const page = await (await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" })).newPage();
          t.after(async () => {
            if (process.env.ARTIFACT_DIR) {
              await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
              await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${blocked}-${width}-signin-port-blocked.png`) });
            }
            await browser.close();
          });
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          const errors = [], posted = [];
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", server(lang, blocked, posted));
          // the vendor's sign-in page, opened in a new window, gets an empty page
          await page.context().route((url) => url.hostname !== "uni-switch.test", (route) => route.fulfill({ contentType: "text/html", body: "" }));
          await page.goto("http://uni-switch.test/?view=providers");
          await page.locator("#addProvider").click();
          const sheet = page.locator("#addSheet");
          await sheet.locator('.tile[data-pick="ChatGPT"]').click();
          const box = sheet.locator(".signing");
          await box.getByText(w[blocked]).waitFor();
          // why the port can't be had replaces the server-or-Docker guess
          assert.equal(await box.getByText(w.docker).count(), 0, "the Docker guess isn't shown too");
          assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true, "no sideways scroll");
          const fit = await box.evaluate((b) => {
            const r = b.getBoundingClientRect(), s = b.querySelector(".port-blocked").getBoundingClientRect();
            return s.left >= r.left - 0.5 && s.right <= r.right + 0.5;
          });
          assert(fit, "the note fits its box");
          if (!w.field) { assert.deepEqual(errors, []); return; }

          const field = box.getByRole("textbox", { name: w.field });
          const before = await page.evaluate(() => [window.scrollY, ...[...document.querySelectorAll("#addSheet, #addSheet *")].filter((e) => e.scrollTop).map((e) => e.scrollTop)]);
          // filled and sent with Enter: at 420 wide the field sits below the
          // sheet's fold, and WebKit's Playwright can't click what the
          // page's scroll guard keeps where it is
          await field.fill(BACK);
          assert(await box.getByRole("button", { name: w.finish }).isEnabled());
          await field.press("Enter");
          for (let i = 0; i < 40 && !posted.length; i++) await page.waitForTimeout(50);
          assert.deepEqual(posted, [{ url: BACK }]);
          await page.waitForFunction(() => document.querySelector("#status").textContent.includes("cx@example.com") && !document.querySelector("#addSheet .callback-form"));
          const after = await page.evaluate(() => [window.scrollY]);
          assert.equal(after[0], before[0], "nothing scrolled");
          assert.deepEqual(errors, []);
        });
      }
    }
  }
}

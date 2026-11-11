// Run with Node's test runner and Playwright on the module path; see README.md.
// Codex connected to uni-switch while its requests for 127.0.0.1 go through a
// proxy that doesn't pass them on (Muma102kMichael on X: every project and
// every new chat 502) is not shown connected. Its row says a proxy is in the
// way; its pill says how to fix it, in a dialog, and the dialog has
// Disconnect from uni-switch, which asks to put Codex's config back. The window
// at 560px and the tray panel at 440px; nothing runs off the side. English,
// Chinese (both), Japanese and German, Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const detail = "Codex sends its requests for uni-switch (127.0.0.1:3425/v1) through the proxy at http://127.0.0.1:7890, set in Windows' proxy settings, and that proxy doesn't pass them on to uni-switch: every turn fails, with 502 Bad Gateway, whichever model it is on. Have the proxy send 127.0.0.1 direct (a DIRECT rule for 127.0.0.0/8, or rule mode rather than global), or add 127.0.0.1 to \"Don't use the proxy server for addresses beginning with\" in Windows' proxy settings (127.* there is not enough for Codex), then quit and reopen Codex. Until then, Disconnect from uni-switch puts Codex's config back as it was.";
const codex = { id: "codex", name: "Codex", icon: "codex", path: "/c", wired: true,
  fields: [{ key: "model", label: "model", value: "fake/m1", options: [{ value: "fake/m1", label: "m1", ref: "fake/m1" }] }] };
const filler = Array.from({ length: 4 }, (_, i) => ({ id: "pi" + i, name: "Pi " + i, path: "/p", fields: [{ key: "model", label: "model", value: "", options: [] }] }));

function serve(lang, calls) {
  const drift = { kind: "proxied", field: "model", now: "fake/m1", want: "fake/m1", addr: "http://127.0.0.1:7890", detail };
  return async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [{ ...codex, drift }, ...filler], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/drift") return route.fulfill({ json: { codex: drift } });
    if (url.pathname.startsWith("/api/agents/reapply/") || url.pathname.startsWith("/api/agents/disconnect")) {
      calls.push(url.pathname);
      return route.fulfill({ json: { agents: [codex, ...filler], profiles: [], settings: { lang, theme: "light" } } });
    }
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const words = {
  en: { why: "Codex reaches uni-switch through a proxy that doesn't pass its requests on", fix: "How to fix", off: "Disconnect from uni-switch", ask: "Disconnect Codex from uni-switch?" },
  zh: { why: "Codex 经代理连 uni-switch，但这个代理没把它的请求转给 uni-switch", fix: "如何修复", off: "断开 uni-switch（还原配置）", ask: null },
  "zh-TW": { why: "Codex 經代理連 uni-switch，但這個代理沒把它的請求轉給 uni-switch", fix: "如何修復", off: "斷開 uni-switch（還原設定）", ask: null },
  ja: { why: "Codex はプロキシ経由で uni-switch につなぎますが、そのプロキシがリクエストを uni-switch に渡していません", fix: "直し方", off: "uni-switch から切断", ask: null },
  de: { why: "Codex erreicht uni-switch über einen Proxy, der seine Anfragen nicht an uni-switch weitergibt", fix: "So beheben", off: "Von uni-switch trennen", ask: null },
};
const row = `.row.agent[data-id="codex"]`;
const noSideScroll = (page) => page.evaluate(() => document.scrollingElement.scrollWidth <= innerWidth);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(words)) {
    const w = words[lang];
    test(`${engine} ${lang}: a proxy in Codex's way is told, with Disconnect at hand`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const errors = [];
      t.after(async () => {
        if (errors.length) console.log(errors);
        await browser.close();
      });
      const open = async (mode, calls) => {
        const page = await (await browser.newContext({ viewport: { width: mode ? 440 : 560, height: 640 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, calls));
        await page.goto("http://uni-switch.test/" + (mode ? "?mode=" + mode : ""));
        await page.locator(`${row}.drifted .ag-fix`).waitFor();
        return page;
      };

      for (const mode of ["", "panel"]) {
        await t.test(`${mode || "window"}: how to fix, then disconnect`, async () => {
          const calls = [];
          const page = await open(mode, calls);
          const fix = page.locator(`${row} .ag-fix`);
          assert.equal(await fix.getAttribute("aria-label"), w.fix);
          assert.match(await fix.getAttribute("title"), new RegExp(w.why));
          if (!mode) assert.equal((await fix.textContent()).trim(), w.fix);
          assert(await noSideScroll(page), "scrolls sideways");
          await fix.click();
          const dialog = page.locator("#modal:not([hidden])");
          await dialog.waitFor();
          assert.equal(await dialog.locator(".ehead b").textContent(), w.why);
          assert.match(await dialog.locator(".drift-why").textContent(), /127\.0\.0\.1 to/);
          const width = mode ? 440 : 560;
          for (const b of [await dialog.locator(".editor").boundingBox(), await dialog.locator(".drift-off").boundingBox()]) {
            assert(b.x >= 0 && b.x + b.width <= width, `runs off: ${JSON.stringify(b)}`);
          }
          assert(await noSideScroll(page), "scrolls sideways with the dialog");
          assert.deepEqual(calls, [], "explaining set something");
          const off = dialog.locator(".drift-off");
          assert.equal((await off.textContent()).trim(), w.off);
          await off.click();
          const ask = page.locator("#modal:not([hidden]) .editor.disconnect-ask");
          await ask.waitFor();
          if (w.ask) assert.equal(await ask.locator(".ehead b").textContent(), w.ask);
          assert.deepEqual(calls, [], "asking disconnected already");
        });
      }

      await t.test("window: the line under the name", async () => {
        const page = await open("", []);
        const st = page.locator(`${row} .ag-st`);
        assert.match(await st.getAttribute("class"), /\bbad\b/);
        assert.equal(await st.locator(".ag-st-t").textContent(), w.why);
        assert.match(await st.locator(".ag-st-t").getAttribute("title"), /502 Bad Gateway/);
      });

      assert.deepEqual(errors, []);
    });
  }
}

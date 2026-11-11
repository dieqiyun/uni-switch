// Run with Node's test runner and Playwright on the module path; see README.md.
// An agent connected by its config to an address where nothing answers
// (#1013: Codex pointed at Windows as WSL sees it, through a portproxy gone
// after a reboot) is no longer shown connected. Its row says it can't
// reach uni-switch; its pill says how to fix it, in a dialog (the advice is too
// long for the status line), and sets nothing. When WSL reaches Windows at
// another address now, the pill is "Use <address>" and reconnects there.
// The window at 560px and the tray panel at 440px; nothing runs off the
// side. English, Chinese, Japanese and German, Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const kept = "http://172.28.96.1:3425";
const detail = `Codex is pointed at uni-switch at ${kept}, where nothing answers from this computer, though uni-switch answers at http://127.0.0.1:3425. Under WSL's NAT networking, Windows as WSL sees it reaches uni-switch only while something listens on 172.28.96.1:3425: uni-switch itself with Settings › Network › Share on local network on, or a forward of your own (netsh interface portproxy, which may need setting up again after Windows restarts).`;
const codex = { id: "codex", name: "Codex", icon: "codex", path: "/c", wired: true,
  fields: [{ key: "model", label: "model", value: "fake/m1", options: [{ value: "fake/m1", label: "m1", ref: "fake/m1" }] }] };
const filler = Array.from({ length: 4 }, (_, i) => ({ id: "pi" + i, name: "Pi " + i, path: "/p", fields: [{ key: "model", label: "model", value: "", options: [] }] }));

function serve(lang, move, calls) {
  const drift = { kind: "unreachable", field: "model", now: "fake/m1", want: "fake/m1", addr: kept, detail };
  if (move) drift.move = move;
  return async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [{ ...codex, drift }, ...filler], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/drift") return route.fulfill({ json: calls.length ? {} : { codex: drift } });
    if (url.pathname.startsWith("/api/agents/reapply/")) {
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
  en: { why: "Codex is set up, but nothing answers at the address it reaches uni-switch by", fix: "How to fix", use: "Use 172.29.0.1:3425" },
  zh: { why: "Codex 已设好，但它连 uni-switch 用的地址没有回应", fix: "如何修复", use: "改用 172.29.0.1:3425" },
  ja: { why: "Codex は設定済みですが、uni-switch につなぐアドレスから応答がありません", fix: "直し方", use: "172.29.0.1:3425 を使う" },
  de: { why: "Codex ist eingerichtet, aber unter der Adresse, über die es uni-switch erreicht, antwortet nichts", fix: "So beheben", use: "172.29.0.1:3425 verwenden" },
};
const row = `.row.agent[data-id="codex"]`;
const noSideScroll = (page) => page.evaluate(() => document.scrollingElement.scrollWidth <= innerWidth);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "ja", "de"]) {
    const w = words[lang];
    test(`${engine} ${lang}: an address that doesn't answer is told, not connected`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const errors = [];
      t.after(async () => {
        if (errors.length) console.log(errors);
        await browser.close();
      });
      const open = async (mode, move, calls) => {
        const page = await (await browser.newContext({ viewport: { width: mode ? 440 : 560, height: 640 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, move, calls));
        await page.goto("http://uni-switch.test/" + (mode ? "?mode=" + mode : ""));
        await page.locator(`${row}.drifted .ag-fix`).waitFor();
        return page;
      };

      for (const mode of ["", "panel"]) {
        await t.test(`${mode || "window"}: how to fix`, async () => {
          const calls = [];
          const page = await open(mode, "", calls);
          const fix = page.locator(`${row} .ag-fix`);
          assert.equal(await fix.getAttribute("aria-label"), w.fix);
          assert.match(await fix.getAttribute("title"), new RegExp(w.why));
          if (!mode) assert.equal((await fix.textContent()).trim(), w.fix);
          assert(await noSideScroll(page), "scrolls sideways");
          await fix.click();
          const dialog = page.locator("#modal:not([hidden])");
          await dialog.waitFor();
          assert.equal(await dialog.locator(".ehead b").textContent(), w.why);
          assert.match(await dialog.locator(".drift-why").textContent(), /portproxy/);
          const box = await dialog.locator(".editor").boundingBox();
          const width = mode ? 440 : 560;
          assert(box.x >= 0 && box.x + box.width <= width, `the dialog runs off: ${JSON.stringify(box)}`);
          assert(await noSideScroll(page), "scrolls sideways with the dialog");
          await dialog.locator(".bar button.primary").click();
          await page.waitForFunction(() => document.querySelector("#modal").hidden);
          assert.deepEqual(calls, [], "explaining set something");
        });
      }

      await t.test("window: the line under the name", async () => {
        const page = await open("", "", []);
        // what is wrong, in red, never "Connected"; the advice its tooltip
        const st = page.locator(`${row} .ag-st`);
        assert.match(await st.getAttribute("class"), /\bbad\b/);
        assert.equal(await st.locator(".ag-st-t").textContent(), w.why);
        assert.match(await st.locator(".ag-st-t").getAttribute("title"), /Share on local network/);
      });

      await t.test("window: use the address WSL has now", async () => {
        const calls = [];
        const page = await open("", "http://172.29.0.1:3425", calls);
        const fix = page.locator(`${row} .ag-fix`);
        assert.equal((await fix.textContent()).trim(), w.use);
        assert(await noSideScroll(page), "scrolls sideways");
        await fix.click();
        await page.waitForFunction(() => !document.querySelector('.row.agent[data-id="codex"] .ag-fix'));
        assert.deepEqual(calls, ["/api/agents/reapply/codex"]);
      });

      assert.deepEqual(errors, []);
    });
  }
}

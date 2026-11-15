// Run with Node's test runner and Playwright on the module path; see README.md.
// Jason on Discord: a Codex turn uni-switch held 2m0s before routing it was
// stopped with a 504 that said to report it "with uni-switch's log", and the
// desktop app's log goes only to a stderr its user never sees. uni-switch now
// saves a report of where it was held, and the request's story on the
// Routing page copies it (Copy Report) and opens its folder (Open Folder,
// on this computer only, not in uni-switch web). A request with no report
// has no such line. In every language, both engines, wide and narrow.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const ago = (minutes) => new Date(now.getTime() - minutes * 60e3).toISOString();
const name = "held-20261029-045944-499212000.txt";
const report = "uni-switch held a request before routing it, and stopped it with 504\n\nknown waits: in body room 1m40s (under way)\n";

const route = (held) => ({ id: 1, seq: 1, time: ago(1), agent: "AirbuildEnvironment", model: "gpt-6-astra", provider: "uni-switch", done: true, status: 504, ms: 120000,
  error: "uni-switch held this request for 2m0s without routing it to any provider, so it was stopped rather than left waiting.", order: [], tries: [], ...(held ? { held: name } : {}) });

function serve(lang, { web, held, seen }) {
  return async (request) => {
    const url = new URL(request.request().url());
    const json = (data) => request.fulfill({ json: data });
    if (url.pathname === "/boot.js") return request.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:${web}};` });
    if (url.pathname === "/wails/runtime.js") return request.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) return new Promise(() => {}); // nothing more comes
      return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 0 }, routes: [route(held)], ids: [1] });
    }
    if (url.pathname === "/api/gateway/held/" + name) { seen.read++; return json({ text: report }); }
    if (url.pathname === "/api/gateway/held/reveal") { seen.reveal++; return request.fulfill({ status: 204 }); }
    if (url.pathname === "/api/copy") { seen.copied.push(JSON.parse(request.request().postData()).text); return json({}); }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ models: [], groups: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await request.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const said = {
  en: ["uni-switch saved a report of where it held this request. If it keeps happening, send the report with yours.", "Copy Report", "Open Folder"],
  zh: ["uni-switch 保存了一份报告，记下这个请求卡在了哪里。如果反复出现，请把这份报告附在你的反馈里。", "复制报告", "打开文件夹"],
  "zh-TW": ["uni-switch 儲存了一份報告，記下這個請求卡在了哪裡。如果反覆出現，請把這份報告附在你的回報裡。", "複製報告", "打開資料夾"],
  ja: ["uni-switch はこのリクエストがどこで止まっていたかをレポートに保存しました。繰り返し起きる場合は、報告にこのレポートを添えてください。", "レポートをコピー", "フォルダを開く"],
  de: ["uni-switch hat in einem Bericht festgehalten, wo diese Anfrage hängen blieb. Passiert es wieder, senden Sie den Bericht mit Ihrer Meldung mit.", "Bericht kopieren", "Ordner öffnen"],
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [1100, 420]) {
      for (const web of [false, true]) {
        test(`${engine} ${lang} ${width}px ${web ? "web" : "app"}: a held request's story copies its report`, async (t) => {
          const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
          t.after(() => browser.close());
          const page = await browser.newPage({ viewport: { width, height: 700 }, reducedMotion: "reduce" });
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          const errors = [];
          page.on("pageerror", (error) => errors.push(error.message));
          const seen = { read: 0, reveal: 0, copied: [] };
          await page.route("**/*", serve(lang, { web, held: true, seen }));
          await page.goto("http://uni-switch.test/?view=routing");
          const [line, copyWord, openWord] = said[lang];
          const li = page.locator(".rt-steps li.held");
          await li.waitFor();
          assert.ok((await li.textContent()).startsWith(line), await li.textContent());
          const buttons = li.locator("button.link");
          assert.deepEqual((await buttons.allTextContents()).map((s) => s.trim()), web ? [copyWord] : [copyWord, openWord]);
          // the buttons are on the page, not cut off at its edge, and the
          // reader scrolls the page to them: uni-switch keeps the page where its
          // reader left it (no unasked scroll), so a click's own scroll is
          // undone and the page is scrolled by the wheel, as a reader does
          const view = page.locator("#view-routing");
          const vb = await view.boundingBox();
          await page.mouse.move(vb.x + vb.width / 2, vb.y + vb.height / 2);
          for (let i = 0; i < 20; i++) {
            const box = await buttons.last().boundingBox();
            if (box && box.y + box.height <= vb.y + vb.height) break;
            await page.mouse.wheel(0, 120);
            await page.waitForTimeout(30);
          }
          for (const b of await buttons.all()) {
            const box = await b.boundingBox();
            assert.ok(box && box.x >= 0 && box.x + box.width <= width && box.y >= vb.y && box.y + box.height <= vb.y + vb.height, `a button is out of sight: ${JSON.stringify(box)} in ${JSON.stringify(vb)}`);
          }
          await buttons.first().click();
          for (let i = 0; i < 250 && !seen.copied.length; i++) await page.waitForTimeout(20);
          assert.equal(seen.read, 1, "Copy Report read the report");
          assert.deepEqual(seen.copied, [report], "Copy Report copied the report itself");
          if (!web) {
            await buttons.nth(1).click();
            for (let i = 0; i < 250 && !seen.reveal; i++) await page.waitForTimeout(20);
            assert.equal(seen.reveal, 1, "Open Folder asked for the reports' folder");
          }
          assert.deepEqual(errors, []);
        });
      }
    }
    test(`${engine} ${lang}: a held request with no report has no report line`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await browser.newPage({ viewport: { width: 1100, height: 700 }, reducedMotion: "reduce" });
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const seen = { read: 0, reveal: 0, copied: [] };
      await page.route("**/*", serve(lang, { web: false, held: false, seen }));
      await page.goto("http://uni-switch.test/?view=routing");
      await page.locator(".rt-steps li.bad").first().waitFor();
      assert.equal(await page.locator(".rt-steps li.held").count(), 0);
    });
  }
}

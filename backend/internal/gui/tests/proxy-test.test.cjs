// Run with Node's test runner and Playwright on the module path; see README.md.
// #2076 (c4ex): Settings › Network › Proxy couldn't say whether the proxy set
// works. Test asks Cloudflare's trace page through it and direct
// (POST /api/proxy/check) and the line under the row's text says the
// address each left from: working (two addresses), the same address both
// ways with the proxy connected to (a TUN's one exit, #2369, not a
// failure) and without (the proxy passed by), the proxy failing, no proxy in
// force, and the check itself failing. Testing… shows while it runs, the
// result outlives a redraw of Settings and goes when the proxy changes.
// Every language, Chromium and WebKit, at 1100 and 420 wide; no backend,
// the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { strings } = require("./strings.cjs");
const { watchSettingsReads, settingsDrawn } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const { I18N } = strings();
const LANGS = ["en", "zh", "zh-TW", "ja", "de"];
const tr = (lang, s, args = {}) => Object.entries(args).reduce((a, [k, v]) => a.replaceAll(`{${k}}`, v), lang === "en" ? s : I18N[lang]?.[s] ?? s);
const PROXY = "http://me:s3cret@127.0.0.1:7890";

function server(lang, st) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    const cur = () => ({
      theme: "light", lang, tray: "panel", currency: "usd", textSize: 100,
      proxy: st.proxy, proxyNow: st.proxy === "direct" ? "" : st.proxy ? "http://127.0.0.1:7890" : "", proxySource: st.proxy === "direct" ? "off" : st.proxy ? "settings" : "none",
      trayUsageEvery: 3, version: "test", dir: "/test", lanURLs: [], visionModels: [], imageGenModels: [], workbuddyCheckins: [],
      fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
    });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = ${JSON.stringify({ lang, theme: "light" })};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: cur(), fx: cur().fx });
    if (url.pathname === "/api/settings") {
      if (req.method() === "POST") st.proxy = req.postDataJSON().proxy ?? st.proxy;
      return json(cur());
    }
    if (url.pathname === "/api/proxy/check") {
      assert.equal(req.method(), "POST");
      st.checks++;
      await st.gate;
      if (st.answer === "fail") return route.fulfill({ status: 500, contentType: "application/json", body: JSON.stringify({ error: "boom" }) });
      return json(st.answer);
    }
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    const body = await fs.readFile(file).catch(() => null);
    return body ? route.fulfill({ body, contentType }) : route.fulfill({ status: 404, body: "" });
  };
}

const US = { ip: "203.0.113.9", loc: "US", ms: 312 }, CN = { ip: "198.51.100.7", loc: "CN", ms: 41 };
// [name, proxy, answer, the line, a failure?]
const CASES = (w) => [
  ["works", PROXY, { proxy: "http://127.0.0.1:7890", source: "settings", through: { ...US, proxied: true }, direct: CN },
    w("Works: through the proxy requests leave from {where}, {ms} ms; direct ones from {direct}", { where: "203.0.113.9 (US)", ms: 312, direct: "198.51.100.7 (CN)" }), false],
  // #2369 (Emt-lin): a TUN gives both one exit, while the request through
  // the proxy connected to it: not a failure
  ["one-exit", PROXY, { proxy: "http://127.0.0.1:7890", source: "settings", through: { ...CN, proxied: true }, direct: CN },
    w("The proxy took the requests, but they leave from {where} as direct ones do: a TUN or system VPN may send every request out one exit, so the address can't tell the two paths apart", { where: "198.51.100.7 (CN)" }), false],
  ["passed-by", PROXY, { proxy: "http://127.0.0.1:7890", source: "settings", through: CN, direct: CN },
    w("The request through the proxy didn't connect to it and leaves from {where}, the same address as direct ones: the proxy is passed by", { where: "198.51.100.7 (CN)" }), true],
  ["down", PROXY, { proxy: "http://127.0.0.1:7890", source: "settings", through: { error: "connect: connection refused" }, direct: CN },
    w("The proxy failed: {error}. Direct requests leave from {direct}", { error: "connect: connection refused", direct: "198.51.100.7 (CN)" }), true],
  ["none", "", { source: "none", direct: CN },
    w("No proxy in force: requests leave from {where}, {ms} ms", { where: "198.51.100.7 (CN)", ms: 41 }), false],
  ["offline", "", { source: "none", direct: { error: "dial tcp: i/o timeout" } },
    w("No proxy in force, and a direct request failed: {error}", { error: "dial tcp: i/o timeout" }), true],
];

const wide = (page) => page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": Settings › Proxy's Test says whether requests leave through the proxy", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const shots = [];
    t.after(async () => {
      if (process.env.ARTIFACT_DIR) {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        for (const [name, buf] of shots) await fs.writeFile(path.join(process.env.ARTIFACT_DIR, `${engine}-proxy-test-${name}.png`), buf);
      }
      await browser.close();
    });
    for (const lang of LANGS) {
      const w = (s, a) => tr(lang, s, a);
      for (const width of [1100, 420]) {
        await t.test(`${lang} at ${width}px`, async () => {
          for (const [name, proxy, answer, want, bad] of CASES(w)) {
            let open;
            const st = { proxy, answer, checks: 0, gate: new Promise((r) => { open = r; }) };
            const context = await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" });
            const page = await context.newPage();
            page.setDefaultTimeout(5000);
            page.setDefaultNavigationTimeout(60000);
            const errors = [];
            page.on("pageerror", (e) => errors.push(e.message));
            await page.route("**/*", server(lang, st));
            await watchSettingsReads(page);
            await page.goto("http://uni-switch.test/?view=settings&tab=network");
            // the network tab is drawn again by load()'s read of the settings:
            // the row is pressed and held once that is in (#4469)
            await settingsDrawn(page);
            const button = page.locator("#proxySegs button.proxy-test");
            const line = page.locator("#proxyCheck");
            assert.equal((await button.textContent()).trim(), w("Test"));
            assert.equal(await line.isVisible().catch(() => false), false, "nothing said before a test");
            await button.click();
            await page.waitForFunction((s) => document.querySelector("#proxySegs button.proxy-test")?.textContent.trim() === s, w("Testing…"));
            assert.equal(await page.locator("#proxySegs button.proxy-test").isDisabled(), true, `${name}: one test at a time`);
            open();
            await line.waitFor({ state: "visible" });
            assert.equal(await line.textContent(), want, name);
            assert.equal(await line.evaluate((e) => e.classList.contains("err")), bad, `${name}: shown as ${bad ? "a failure" : "fine"}`);
            assert.doesNotMatch(await page.locator("#setPage-network").textContent(), /s3cret/);
            assert.equal(st.checks, 1);
            assert.equal((await button.textContent()).trim(), w("Test"));
            assert.equal(await wide(page), 0, `${name}: nothing runs off to the side`);
            const row = page.locator("#setPage-network .row.pref").first();
            const lb = await line.boundingBox(), rb = await row.boundingBox();
            assert.ok(lb.x >= rb.x && lb.x + lb.width <= rb.x + rb.width + 1, `${name}: the line stays in its row`);
            shots.push([`${lang}-${width}-${name}`, await row.screenshot()]);
            if (name === "works") {
              // a change of proxy takes the old answer away
              await page.locator("#proxySegs .segs button", { hasText: w("Off") }).click();
              await page.waitForFunction(() => document.querySelector("#proxyCheck")?.hidden);
            }
            assert.deepEqual(errors, []);
            await context.close();
          }
          // the check itself failing
          const st = { proxy: PROXY, answer: "fail", checks: 0, gate: Promise.resolve() };
          const context = await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" });
          const page = await context.newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          await page.route("**/*", server(lang, st));
          await watchSettingsReads(page);
          await page.goto("http://uni-switch.test/?view=settings&tab=network");
          await settingsDrawn(page);
          await page.locator("#proxySegs button.proxy-test").click();
          await page.locator("#proxyCheck").waitFor({ state: "visible" });
          assert.equal(await page.locator("#proxyCheck").textContent(), w("The test couldn't run: {error}", { error: w("boom") }));
          await context.close();
        });
      }
    }
  });
}

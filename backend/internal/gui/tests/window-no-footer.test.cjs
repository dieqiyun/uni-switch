// Run with Node's test runner and Playwright on the module path; see README.md.
// The main window has no footer bar (yogurt36_51912 on Discord): its footer
// held only the status line, so with nothing said it was an empty 28px strip
// along the window's foot, and every page stopped above it, its last rows
// cut off over a blank bar beside the sidebar. Now a page runs to the
// window's foot, and a line said there floats as a pill over it, inside the
// window, and goes. A press under a passing note reaches the page
// (plugin-namesake's Install at the window's foot). The tray panel's footer, with Quit, is as it was.
// Both engines, every language, with the sidebar and the header's tabs,
// wide and narrow.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const providers = Array.from({ length: 40 }, (_, i) => ({
  id: "p" + i, name: "Provider " + i, icon: "generic", chat: `https://p${i}.example/v1`, responses: "", anthropic: "", catalog: "",
  agents: [], fallback: [], headers: {}, keyList: [], proxy: "", key: { set: true, masked: "sk-…" + i }, models: [{ id: "m", name: "", on: true }],
}));

function serve(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers, presets: [], excluded: [], gateway: { url: "http://127.0.0.1:3425", running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    if (!/^\/[\w./-]*$/.test(url.pathname) || url.host !== "uni-switch.test") return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

// where the open page ends, how much room the footer takes, and the status
const look = (page) => page.evaluate(() => {
  const v = document.querySelector(".view:not([hidden])").getBoundingClientRect();
  const f = document.querySelector("footer.foot").getBoundingClientRect();
  const s = document.querySelector("#status"), sr = s.getBoundingClientRect(), cs = getComputedStyle(s);
  return {
    side: document.body.classList.contains("side-nav"), vh: innerHeight, vw: innerWidth, view: v.bottom, foot: f.height,
    status: { text: s.textContent, lift: s.classList.contains("lift"), h: sr.height, top: sr.top, bottom: sr.bottom, left: sr.left, right: sr.right, bg: cs.backgroundColor, cut: s.scrollWidth > s.clientWidth + 1 },
  };
});

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: the main window has no footer bar; the panel keeps its own`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      for (const width of [1200, 700, 390]) for (const view of ["providers", "settings"]) await t.test(`${lang} ${width} ${view}`, async () => {
        const context = await browser.newContext({ viewport: { width, height: 600 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(8000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        await page.goto("http://uni-switch.test/?view=" + view);
        await page.locator(`#view-${view}:not([hidden])`).waitFor();
        if (width === 1200) await page.waitForFunction(() => document.body.classList.contains("side-nav"));
        let got = await look(page);
        if (width === 1200) assert(got.side, "the sidebar at 1200");
        // nothing said: no bar, the page runs to the window's foot
        assert.equal(got.foot, 0, `the footer takes no room: ${JSON.stringify(got)}`);
        assert(Math.abs(got.view - got.vh) < 1, `the page reaches the window's foot: ${JSON.stringify(got)}`);
        // something said: a pill over the page's foot, in the window, whole
        await page.evaluate(() => status(t("Saved"), "ok"));
        got = await look(page);
        assert(got.status.text, "the line is said");
        assert(!got.status.lift && got.status.h <= 24, `a short line is one line, as it was in the footer: ${JSON.stringify(got)}`);
        assert(got.status.bottom <= got.vh - 8 && got.status.top > got.vh - 80, `the pill is at the window's foot: ${JSON.stringify(got)}`);
        assert(got.status.left >= 0 && got.status.right <= got.vw && !got.status.cut, `it fits: ${JSON.stringify(got)}`);
        assert.notEqual(got.status.bg, "rgba(0, 0, 0, 0)", "it has its own ground over the page");
        assert(Math.abs(got.view - got.vh) < 1, `saying it doesn't make the page shorter: ${JSON.stringify(got)}`);
        // a press where the note is lands on the page under it; an error
        // stays on top, to be selected and copied
        const hit = () => page.evaluate(() => {
          const s = document.querySelector("#status").getBoundingClientRect();
          const e = document.elementFromPoint(s.left + s.width / 2, s.top + s.height / 2);
          return !!e && !!e.closest("#status");
        });
        assert(!(await hit()), "a press under a passing note reaches the page");
        await page.evaluate(() => status("x".repeat(300), "ok"));
        assert(!(await hit()), "under a long one too");
        await page.evaluate(() => status("failed", "err"));
        assert(await hit(), "an error can be selected");
        // a plain line too
        await page.evaluate(() => status("…"));
        got = await look(page);
        assert.notEqual(got.status.bg, "rgba(0, 0, 0, 0)", "a plain line has its ground too");
        assert.deepEqual(errors, []);
        await context.close();
      });
      await t.test(`${lang} panel`, async () => {
        const context = await browser.newContext({ viewport: { width: 380, height: 520 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(8000);
        page.setDefaultNavigationTimeout(60000);
        await page.route("**/*", serve(lang));
        await page.goto("http://uni-switch.test/?mode=panel");
        await page.locator(".foot #quit").waitFor();
        const f = await page.evaluate(() => { const r = document.querySelector("footer.foot").getBoundingClientRect(), q = document.querySelector("#quit").getBoundingClientRect(); return { h: r.height, bottom: r.bottom, vh: innerHeight, pos: getComputedStyle(document.querySelector("footer.foot")).position, quit: q.width > 0 && q.bottom <= innerHeight }; });
        assert(f.h >= 28 && f.pos === "static" && Math.abs(f.bottom - f.vh) < 1 && f.quit, `the panel's footer and Quit as they were: ${JSON.stringify(f)}`);
        await context.close();
      });
    }
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// A trackpad's scroll goes on after the fingers lift (issenn on Discord,
// macOS app: 含滚动条的页面无法上下滚动……强行通过触摸板滚动页面会高频振动并复位).
// WebKit on the Mac carries that coast itself, frame by frame, and the page is
// told of the scrolls, not of a wheel for each: a wheel made the reader's
// moment only 250ms long, so the rest of the coast was put back as no one's,
// every frame, the page shaking where the wheel had left it and then back.
// Here the browser's coast is played by the page itself (a scroll each frame
// after the last trusted wheel, as a scroll by code is, since the page can't
// tell them apart either). It must stand to its end; a scroll by code once the
// page is at rest is still put back; and a click in the coast ends it, so what
// was clicked stays where it was (a click never scrolls the page).
// English and Chinese, Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = process.env.UNI_SWITCH_COAST_ASSETS || path.resolve(__dirname, "../assets");
const AGENTS = Array.from({ length: 14 }, (_, i) => ({
  id: "a" + i, name: "Agent " + i, path: "/t/a" + i + ".json", icon: "generic", wired: true,
  fields: [{ key: "model", label: "model", value: "m", options: [{ value: "m", label: "M", ref: "a/m" }] }],
}));

function server(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: AGENTS, profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// the page at rest: its scrollTop the same across a few frames' looks
async function rested(p, view, ms = 3000) {
  let last = NaN, same = 0;
  const until = Date.now() + ms;
  while (Date.now() < until) {
    const t = await view.evaluate((v) => v.scrollTop);
    same = t === last ? same + 1 : 0;
    if (same >= 3) return t;
    last = t;
    await p.waitForTimeout(50);
  }
  return last;
}

// the browser's coast after the lift: a scroll each frame, 6px slowing to 1px,
// for `ms` after the last wheel, with no wheel of its own. It counts what it
// asked for, and stops at the page's foot or when told to.
const coast = (v, ms) => new Promise((done) => {
  const end = performance.now() + ms;
  let d = 6, asked = 0;
  window.__coastStop = false;
  const step = () => {
    if (window.__coastStop || performance.now() > end || v.scrollTop + v.clientHeight >= v.scrollHeight - 1) return done(asked);
    v.scrollTop += Math.max(1, Math.round(d));
    asked += Math.max(1, Math.round(d));
    d *= 0.97;
    requestAnimationFrame(step);
  };
  requestAnimationFrame(step);
});

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a trackpad's coast after the lift is the reader's scroll", { timeout: 120000 }, async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit : chromium).launch();
    t.after(() => browser.close());
    for (const lang of ["en", "zh"]) {
      await t.test(lang, async () => {
        const ctx = await browser.newContext({ viewport: { width: 440, height: 620 }, reducedMotion: "reduce" });
        const p = await ctx.newPage();
        p.setDefaultTimeout(8000);
        p.setDefaultNavigationTimeout(60000);
        const errors = [];
        p.on("pageerror", (e) => { if (!/^ResizeObserver loop/.test(e.message)) errors.push(e.message); });
        await p.route("**/*", server(lang));
        const view = p.locator("#view-agents");
        await p.goto("http://uni-switch.test/?view=agents");
        await p.locator("#view-agents .ag-lead").first().waitFor();
        await rested(p, view);
        const over = await view.evaluate((v) => v.scrollHeight - v.clientHeight);
        assert(over > 600, `the page must be tall enough to coast: ${over}`);
        const r = await view.boundingBox();
        await p.mouse.move(r.x + r.width / 2, r.y + r.height / 2);

        // fingers on the pad: a few small wheels, then the coast, a second long
        const fingers = async () => { for (let i = 0; i < 5; i++) { await p.mouse.wheel(0, 8); await p.waitForTimeout(16); } };
        await fingers();
        const lifted = await view.evaluate((v) => v.scrollTop);
        const asked = await view.evaluate(coast, 1000);
        const end = await rested(p, view);
        assert(asked > 150, `the coast must ask for more than the 250ms after a wheel covers: ${asked}`);
        assert(end >= lifted + asked - 2, `the coast stands to its end: lifted at ${lifted}, coast asked ${asked}, left at ${end}`);

        // at rest, a scroll by code is put back as before
        await p.waitForTimeout(400);
        await view.evaluate((v) => { v.scrollTop -= 120; });
        await p.waitForTimeout(300);
        assert.equal(await rested(p, view), end, "a scroll by code at rest is put back");

        // a click in the coast ends it: what follows the click is put back,
        // and the row clicked stays where it was clicked
        await view.evaluate((v) => { v.scrollTop = 0; });
        await p.mouse.wheel(0, -2000);
        await rested(p, view);
        await p.waitForTimeout(400);
        await fingers();
        const going = view.evaluate(coast, 1500);
        await p.waitForTimeout(200);
        // pressed by coordinate, on the edge of the agent's row in the middle
        // of the window, as the reader's pointer is
        const [x, y] = await p.evaluate(([x, y]) => {
          const row = window.__clicked = document.elementFromPoint(x, y)?.closest(".row.agent");
          const b = row?.getBoundingClientRect();
          return b ? [b.left + 3, b.top + b.height / 2] : [0, 0];
        }, [r.x + r.width / 2, r.y + r.height / 2]);
        assert(x > 0, "an agent's row is in the middle of the window");
        const rowTop = () => p.evaluate(() => window.__clicked.getBoundingClientRect().top);
        await p.mouse.click(x, y);
        const at = await rowTop();
        await going;
        await rested(p, view);
        const after = await rowTop();
        assert(Math.abs(after - at) <= 12, `the click ends the coast: the row was at ${at} after the click, ${after} once at rest`);
        assert.deepEqual(errors, []);
        await ctx.close();
      });
    }
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// A page scrolled by the middle button's autoscroll (多喝热水 on Discord,
// Windows): pressing the wheel and moving the mouse scrolls the view with no
// wheel, touch or drag of the reader's for the page to see. Chromium and
// WebView2 send a pointerdown of button 1 and then only pointer moves with no
// button held (the press is let go at once and the autoscroll runs on), or
// none at all while the mouse is held still. The "where the reader is" rule
// put each of those scrolls back as it came, and the page twitched between
// where the autoscroll took it and where it was. A scroll the reader never
// asked for (code setting scrollTop) is still put back once the autoscroll
// has ended, by the next press of a button, as Chromium ends it. The middle
// press focuses the button under it, and the autoscroll takes it out of
// sight: the next scroll put back brought it back into view, and the page
// jumped to it. So did a button a press focused and the wheel scrolled away.
// Chromium only scrolls: WebKit has no middle-button autoscroll, and there the
// press must leave the rule as it was. English and Chinese, Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const AGENTS = Array.from({ length: 14 }, (_, i) => ({
  id: "a" + i, name: "Agent " + i, path: "/t/a" + i + ".json", icon: "generic", wired: true,
  fields: [{ key: "model", label: "model", value: "m", options: [{ value: "m", label: "M", ref: "a/m" }] }],
}));
const VIEWPORT = { width: 440, height: 620 };

function server(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: AGENTS, profiles: [], settings: { lang, theme: "light" }, fx: { rate: 7.2, at: new Date().toISOString() } });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// the page's height once it has stopped changing
async function settled(p, view) {
  let last = -1;
  for (let i = 0; i < 40; i++) {
    const h = await view.evaluate((v) => v.scrollHeight);
    if (h === last) break;
    last = h;
    await p.waitForTimeout(120);
  }
  return view.evaluate((v) => v.scrollHeight - v.clientHeight);
}

// Every scroll position the view passes through, read in the page each frame:
// a scroll put back shows as a step up while the autoscroll goes down.
async function record(view) {
  await view.evaluate((v) => {
    window.__tops = [];
    const read = () => { window.__tops.push(v.scrollTop); window.__rec = requestAnimationFrame(read); };
    v.addEventListener("scroll", () => window.__tops.push(v.scrollTop), { passive: true });
    read();
  });
}
const recorded = (view) => view.evaluate(() => { cancelAnimationFrame(window.__rec); return window.__tops.splice(0); });
const top = (view) => view.evaluate((v) => v.scrollTop);
const backs = (tops) => tops.slice(1).filter((y, i) => y < tops[i] - 0.5).length;

// a scroll by code is put back, as it was before any middle press
async function codePutBack(p, view, over, what) {
  await p.waitForTimeout(400); // the reader's last scroll lets the next 250ms stand
  const cur = await top(view);
  const want = cur > over / 2 ? 0 : over;
  await view.evaluate((v, y) => { v.scrollTop = y; }, want);
  await p.waitForTimeout(250);
  const now = await top(view);
  assert.ok(Math.abs(now - cur) < 1, `${what}: a scroll by code is put back (was ${cur}, asked ${want}, at ${now})`);
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": the middle button's autoscroll scrolls a page", { timeout: 120000 }, async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    // Chromium has the autoscroll only on Windows by default; WebView2 is that
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium", args: ["--enable-blink-features=MiddleClickAutoscroll"] }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh"]) {
      await t.test(lang, async () => {
        const errors = [];
        const ctx = await browser.newContext({ viewport: VIEWPORT, reducedMotion: "reduce" });
        const p = await ctx.newPage();
        p.setDefaultTimeout(8000);
        p.setDefaultNavigationTimeout(60000);
        p.on("pageerror", (e) => errors.push(e.message));
        await p.route("**/*", server(lang));
        const view = p.locator("#view-agents");
        await p.goto("http://uni-switch.test/?view=agents");
        await p.locator("#view-agents .ag-lead").first().waitFor();
        const over = await settled(p, view);
        assert.ok(over > 150, `the page must overflow: ${over}`);
        const r = await view.boundingBox();
        // pressed on a button near the page's top, which the press focuses
        const b = await view.locator("button.ag-start").first().boundingBox();
        const x = b.x + b.width / 2, y = b.y + b.height / 2;

        // pressed and let go, then the mouse moved below where it was pressed
        await record(view);
        await p.mouse.move(x, y);
        await p.mouse.down({ button: "middle" });
        await p.mouse.up({ button: "middle" });
        for (let i = 1; i <= 6; i++) { await p.mouse.move(x, y + i * 12); await p.waitForTimeout(40); }
        // held still: the autoscroll runs on with no event of the reader's
        await p.waitForTimeout(600);
        let tops = await recorded(view);
        const ran = await top(view);
        if (engine === "chromium") {
          assert.ok(ran > 60, `the autoscroll scrolled the page: at ${ran}, passed ${tops.join(",")}`);
          assert.equal(backs(tops), 0, `no scroll was put back under the autoscroll: ${tops.join(",")}`);
        }
        // the next press ends it, and with it the reader's scroll: it was a
        // left press in the view's own padding, away from its scrollbar, which does nothing
        await p.mouse.click(r.x + 3, r.y + r.height - 4);
        await p.waitForTimeout(150);
        const ended = await top(view);
        await p.waitForTimeout(300);
        assert.ok(Math.abs((await top(view)) - ended) < 1, "the autoscroll ended on the next press");
        await codePutBack(p, view, over, "after the autoscroll");

        // a button a press focused, then scrolled away by the wheel: a scroll
        // put back leaves the page where the wheel took it
        await p.mouse.wheel(0, -2000);
        await p.waitForTimeout(400);
        await p.mouse.move(x, y);
        await p.mouse.down();
        await p.mouse.move(r.x + 3, r.y + r.height - 4, { steps: 4 });
        await p.mouse.up();
        await p.mouse.move(r.x + r.width / 2, r.y + r.height / 2);
        await p.mouse.wheel(0, 500);
        let wheeled = NaN;
        for (let i = 0; i < 40 && wheeled !== (wheeled = await top(view)); i++) await p.waitForTimeout(80);
        assert.ok(wheeled > 100, `the wheel scrolled the page: ${wheeled}`);
        await codePutBack(p, view, over, "after the wheel took a focused button away");

        // pressed and held while the mouse moves, let go once it has gone on
        if (engine === "chromium") {
          // from where the wheel left the page, pressed low in the view
          const before = await top(view);
          await p.mouse.move(r.x + r.width / 2, r.y + r.height - 60);
          await record(view);
          await p.mouse.down({ button: "middle" });
          for (let i = 1; i <= 6; i++) { await p.mouse.move(r.x + r.width / 2, r.y + r.height - 60 - i * 12); await p.waitForTimeout(40); }
          await p.waitForTimeout(500);
          tops = await recorded(view);
          const now = await top(view);
          assert.ok(now < before - 40, `the held autoscroll scrolled the page up: ${before} to ${now}`);
          assert.equal(tops.slice(1).filter((v, i) => v > tops[i] + 0.5).length, 0, `no scroll was put back under the held autoscroll: ${tops.join(",")}`);
          // the lift after a move ends a held one
          await p.mouse.up({ button: "middle" });
          await p.waitForTimeout(300);
          await codePutBack(p, view, over, "after the held autoscroll");
        }
        assert.deepEqual(errors, []);
        await ctx.close();
      });
    }
  });
}

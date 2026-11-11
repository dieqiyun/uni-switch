// Run with Node's test runner and Playwright on the module path; see README.md.
// On macOS 13.7.8, whose WebKit is Safari 16.6's (18615), no page of the window
// scrolled, by trackpad or mouse wheel; a trackpad's scroll shook and snapped
// back, and only dragging the bar moved it (issenn on Discord). The view guard
// puts back every scroll outside a window the reader's own events open, and
// that WebKit scrolls a view under a wheel the page is never told of, so each
// scroll was the guard's to put back. Here the page is kept from hearing the
// wheel (a capture listener before app.js's stops it), as there: the wheels
// must still scroll the view all the way they ask, a scroll by code at rest
// is still put back, and so is one a click holds against.
// English and Chinese, Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = process.env.UNI_SWITCH_UNHEARD_ASSETS || path.resolve(__dirname, "../assets");
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


const DEAF = `addEventListener("wheel", (e) => e.stopImmediatePropagation(), { capture: true, passive: true });`;

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a wheel the page never hears still scrolls the view", { timeout: 120000 }, async (t) => {
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
        await p.addInitScript(DEAF);
        await p.route("**/*", server(lang));
        const view = p.locator("#view-agents");
        await p.goto("http://uni-switch.test/?view=agents");
        await p.locator("#view-agents .ag-lead").first().waitFor();
        await rested(p, view);
        const over = await view.evaluate((v) => v.scrollHeight - v.clientHeight);
        assert(over > 400, `the page must be tall enough to scroll: ${over}`);
        const r = await view.boundingBox();
        await p.mouse.move(r.x + r.width / 2, r.y + r.height / 2);
        assert.equal(await p.evaluate(() => { let heard = 0; addEventListener("wheel", () => heard++, { capture: true }); window.__heard = () => heard; return 0; }), 0);

        // a run of wheels down, as fingers on the pad or a wheel turned
        for (let i = 0; i < 10; i++) { await p.mouse.wheel(0, 20); await p.waitForTimeout(16); }
        // a wheel turned slowly, a notch every 400ms (a run broken on a loaded
        // Mac looks the same): every notch still lands
        const before = await rested(p, view);
        for (let i = 0; i < 5; i++) { await p.mouse.wheel(0, 20); await p.waitForTimeout(400); }
        assert(await rested(p, view) >= before + 95, "five slow notches of 20px land");
        for (let i = 0; i < 5; i++) { await p.mouse.wheel(0, -20); await p.waitForTimeout(400); }
        const down = await rested(p, view);
        assert.equal(await p.evaluate(() => window.__heard()), 0, "the page heard none of the wheels");
        assert(down >= 190, `ten wheels of 20px scroll the view the page never heard them in: left at ${down}`);
        // and back up
        for (let i = 0; i < 6; i++) { await p.mouse.wheel(0, -20); await p.waitForTimeout(16); }
        const up = await rested(p, view);
        assert(up <= down - 110, `six wheels up scroll it back: from ${down} to ${up}`);

        // at rest, past the second a run of the reader's goes on for, a scroll
        // by code is put back as before, twice in a row too
        await p.waitForTimeout(1100);
        await view.evaluate((v) => { v.scrollTop -= 60; });
        await p.waitForTimeout(100);
        await view.evaluate((v) => { v.scrollTop -= 60; });
        await p.waitForTimeout(300);
        assert.equal(await rested(p, view), up, "a scroll by code at rest is put back");

        // a click holds what it clicked: a scroll by code after it is no
        // reader's, and is put back. (A run of scrolls with nothing redrawn
        // is the reader's, a click before it or not: view-wheel-after-click.)
        const [x, y] = await p.evaluate(([x, y]) => {
          const row = window.__clicked = document.elementFromPoint(x, y)?.closest(".row.agent");
          const b = row?.getBoundingClientRect();
          return b ? [b.left + 3, b.top + b.height / 2] : [0, 0];
        }, [r.x + r.width / 2, r.y + r.height / 2]);
        assert(x > 0, "an agent's row is in the middle of the window");
        const rowTop = () => p.evaluate(() => window.__clicked.getBoundingClientRect().top);
        await p.mouse.click(x, y);
        const at = await rowTop();
        await view.evaluate((v) => { v.scrollTop += 120; });
        await rested(p, view);
        const after = await rowTop();
        assert(Math.abs(after - at) <= 12, `the row clicked stays: at ${at} after the click, ${after} once at rest`);
        assert.deepEqual(errors, []);
        await ctx.close();
      });
    }
  });
}

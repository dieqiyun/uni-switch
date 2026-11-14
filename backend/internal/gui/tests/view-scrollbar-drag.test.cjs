// Run with Node's test runner and Playwright on the module path; see README.md.
// A page's own scrollbar is dragged by the mouse (emo172, #1089): Chromium and
// WebView2 give a scrollbar drag no pointermove of the reader's, so the "where
// the reader is" rule put each of its scrolls back as it came and the bar would
// not move — the wheel worked, only the drag did not. The same holds after a
// click in the page: what was clicked is held in place for a few seconds, and a
// hold that is not let go of fights the drag frame by frame. A scroll the reader
// never asked for (code setting scrollTop) is still put back: only the bar's own
// drag counts as the reader's, not every scroll.
// The app's own scrollbar CSS is what the pages draw with, so nothing here
// injects one: a scrollbar-width set on the view makes Chromium drop every
// ::-webkit-scrollbar rule (app.css) and draw Windows' classic bar instead.
// English and Chinese, Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const at = (i) => new Date(now.getTime() - (i + 1) * 60e3).toISOString();
const who = { id: "fixture-key", provider: "fixture", name: "Fixture", who: "key-1", kind: "key", model: "model-a", routing: "", used: 0 };
const routes = Array.from({ length: 12 }, (_, i) => ({
  id: 100 - i, seq: 100 - i, time: at(i), agent: "fixture", model: "model-a", provider: "fixture",
  order: [who], tries: [{ id: who.id, model: "model-a", start: at(i), done: true, status: 200, ms: 1200 }],
  done: true, status: 200, ms: 1200, tokens: 3000,
}));
// rows enough to make a page taller than a short window
const AGENTS = Array.from({ length: 14 }, (_, i) => ({
  id: "a" + i, name: "Agent " + i, path: "/t/a" + i + ".json", icon: "generic", wired: true,
  fields: [{ key: "model", label: "model", value: "m", options: [{ value: "m", label: "M", ref: "a/m" }] }],
}));
// Two shapes of page: a list of rows, and the Routing page, which redraws itself
// around a live trace and keeps its own place across a redraw. Narrow, because
// the Routing page at a wide window lays its stage beside the list and has only
// ~35px left to scroll — not enough for a drag to be told from a nudge.
// The drag's last scroll arriving after the lift is checked on the list page
// only. It was left off Routing while #1249 was open — WebKit clamped Routing's
// last stretch, so a scroll sent there after the lift measured that bug rather
// than the window held open past it. #1249 is fixed in main now, so this could
// be widened to Routing; it is not, because that clamp was only ever checked on
// Windows headless WebKit here, not on the macOS WKWebView the maintainer runs,
// and widening a check is beyond what review asked for.
// `click` waits for the page to be drawn; `hold` is what the click in it lands
// on, and must leave the page as it is — the Routing page's own header opens
// the group editor in English, growing the page and scrolling it on its own.
const PAGES = [
  { view: "agents", click: "#view-agents .ag-lead", hold: "#view-agents .ag-lead", after: true },
  { view: "routing", click: "#view-routing .row-head", hold: "#view-routing .rt-mode", after: false },
];
const VIEWPORT = { width: 440, height: 620 };

function server(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: AGENTS, profiles: [], settings: { lang, theme: "light" }, fx: { rate: 7.2, at: now.toISOString() } });
    if (url.pathname === "/api/gateway/trace") return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    if (url.pathname === "/api/gateway/history") {
      const d = url.searchParams.get("day");
      return json({ cut: false, days: [{ day, requests: routes.length }], routes: d ? routes : [] });
    }
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// The page's height is measured when it is asked for, not once at load: the
// Routing page draws its live stage a moment after the rest, and a check that
// measured at load would look for room the page no longer has.
async function settled(p, view) {
  let last = -1;
  for (let i = 0; i < 40; i++) {
    const h = await view.evaluate((v) => v.scrollHeight);
    if (h === last) return view.evaluate((v) => ({ over: v.scrollHeight - v.clientHeight, bar: v.offsetWidth - v.clientWidth, h: v.clientHeight }));
    last = h;
    await p.waitForTimeout(120);
  }
  return view.evaluate((v) => ({ over: v.scrollHeight - v.clientHeight, bar: v.offsetWidth - v.clientWidth, h: v.clientHeight }));
}

// where the bar's thumb is now, so a press lands on it and not on the track or
// its arrow buttons, which scroll a page or a line and would leave the page put
async function thumbAt(p, view, r, shape) {
  const s = await view.evaluate((v) => ({ top: v.scrollTop, h: v.clientHeight }));
  const thumb = (s.h * s.h) / (s.h + shape.over); // the thumb's height in the track
  return {
    x: r.x + r.width - shape.bar / 2, // the middle of the bar's band
    y: r.y + (s.top / shape.over) * (s.h - thumb) + thumb / 2,
  };
}

// press on the thumb, move it down, lift. The steps are spaced as a hand's are:
// WebKit drops the gesture when the pointer jumps the whole way at once. What
// onLift asks for is put in the page before the lift and runs on the pointerup
// itself, the way the browser sends the drag's last scrolls: a round trip
// between the lift and that scroll would, under parallel load, land outside the
// window the app holds open for it.
async function drag(p, view, r, shape, onLift) {
  const { x, y } = await thumbAt(p, view, r, shape);
  await p.mouse.move(x, y);
  await p.mouse.down();
  if (onLift) await view.evaluate(onLift);
  for (let i = 1; i <= 8; i++) {
    await p.mouse.move(x, y + i * 16, { steps: 2 });
    await p.waitForTimeout(30);
  }
  await p.mouse.up();
  await p.waitForTimeout(250);
}

// the wheel's scroll is still running when the event returns, and under
// parallel load it takes longer than any fixed wait covers: wait for the page
// to come to rest instead
async function rested(p, view, ms = 2500) {
  let last = NaN;
  const until = Date.now() + ms;
  while (Date.now() < until) {
    const t = await view.evaluate((v) => v.scrollTop);
    if (t === last) return t;
    last = t;
    await p.waitForTimeout(60);
  }
  return last;
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a page's scrollbar is dragged by the mouse", { timeout: 120000 }, async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    // headless Chromium hides scrollbars unless asked not to
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium", ignoreDefaultArgs: ["--hide-scrollbars"] }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh"]) {
      for (const page of PAGES) {
        await t.test(`${lang} ${page.view}`, async () => {
          const errors = [];
          // WebKit reports a ResizeObserver round left to the next frame as a
          // page error; Chromium doesn't. The Routing page redraws itself
          // around a live trace and keeps its own place with observers, and a
          // scrollbar dragged over it narrows the view in the same frame, so
          // under load that lands past the loop's depth. Nothing is lost or
          // painted wrong: the round is delivered on the next frame.
          const ctx = await browser.newContext({ viewport: VIEWPORT, reducedMotion: "reduce" });
          const p = await ctx.newPage();
          p.setDefaultTimeout(8000);
          p.setDefaultNavigationTimeout(60000);
          p.on("pageerror", (e) => {
            if (!/^ResizeObserver loop completed with undelivered notifications/.test(e.message)) errors.push(e.message);
          });
          await p.route("**/*", server(lang));
          const view = p.locator("#view-" + page.view);
          const top = () => view.evaluate((v) => v.scrollTop);

          await p.goto(`http://uni-switch.test/?view=${page.view}`);
          await p.locator(page.click).first().waitFor();
          let shape = await settled(p, view);
          assert(shape.over > 40, `${page.view} must overflow by more than a drag: ${JSON.stringify(shape)}`);
          assert(shape.bar > 0, `${page.view} must draw a scrollbar: ${JSON.stringify(shape)}`);
          let r = await view.boundingBox();

          // the drag, straight away
          await drag(p, view, r, shape);
          const plain = await top();
          assert(plain >= 40, `the drag scrolled the page: ${plain}`);

          // what the reader never asked for is still put back: only the bar's own
          // drag counts as the reader's, not every scroll. The number is one the
          // page can reach — past its end the browser would only clamp it — and
          // one the page is not already at, or the write would be a no-op and the
          // check would pass for the wrong reason. It runs before any click: a
          // click holds what was clicked on the screen for a few seconds, and
          // that hold moves the page on its own, which would answer for the app
          await p.waitForTimeout(400); // the drag's own window closes on the lift
          const cur = await top();
          const far = Math.min(240, shape.over - 1);
          const code = cur === far ? 0 : far;
          await view.evaluate((v, y) => { v.scrollTop = y; }, code);
          await p.waitForTimeout(250);
          const back = await top();
          assert.notEqual(back, code, `a scroll code asked for is put back: at ${cur}, asked for ${code}, left at ${back}`);

          // and after a click in the page, which holds what was clicked in place
          // for a few seconds: the bar's drag lets go of that. Pressed by
          // coordinate, as a reader's mouse is: a locator click would scroll the
          // target into view and re-check it under the pointer, which the app's
          // own rule answers. The target is one that leaves the page as it is:
          // the Routing page's own header opens the group editor in English, and
          // a page that grows under the click is scrolled by the app itself
          await p.mouse.move(r.x + r.width / 2, r.y + r.height / 2);
          await p.mouse.wheel(0, -600);
          await p.waitForTimeout(300);
          const name = await p.locator(page.hold).first().boundingBox();
          await p.mouse.click(name.x + name.width / 2, name.y + name.height / 2);
          await p.waitForTimeout(20);
          // the page is measured again where it is now, and the drag is judged by
          // how far it moves the page rather than by where it ends up: were the
          // page to scroll itself, an absolute position would pass without the
          // drag having done anything
          shape = await settled(p, view);
          r = await view.boundingBox();
          const pre = await top();
          await drag(p, view, r, shape);
          const held = await top();
          assert(held - pre >= 40, `the drag scrolled the page with a click holding it: ${held} - ${pre}`);

          // and the wheel still scrolls it, with the room the page has now. It
          // is asked in whichever direction has the more room, so the wheel has
          // a stretch to move over: near a page's foot, asking downwards could
          // land on the end and move nothing, which would say nothing about
          // the wheel whether or not the drag reached that end just before
          shape = await settled(p, view);
          const at0 = await top();
          const room = Math.max(at0, shape.over - at0);
          if (room > 40) {
            const down = at0 < shape.over - at0;
            await p.mouse.move(r.x + r.width / 2, r.y + r.height / 2);
            await p.mouse.wheel(0, down ? 240 : -240);
            const after = await rested(p, view);
            assert.ok(down ? after > at0 : after < at0, "the wheel still scrolls the page");
          }

          // the drag's last scrolls come after the lift in WebKit, once the
          // gesture is over, so the reader's window is held open past it: one
          // arriving now is the reader's still, and not put back. It is sent from
          // the pointerup itself, the way the browser sends it, so that no round
          // trip between the lift and the scroll can push it outside the window
          if (page.after) {
            await view.evaluate((v) => {
              addEventListener("pointerup", () => {
                const want = Math.min(v.scrollHeight - v.clientHeight, v.scrollTop + 120);
                v.scrollTop = want;
                window.__trailing = want;
              }, { once: true });
            });
            await drag(p, view, r, shape);
            const trailing = await view.evaluate(() => window.__trailing);
            assert.equal(await top(), trailing, "a scroll arriving after the lift is still the reader's");
          }
          assert.deepEqual(errors, []);
          await ctx.close();
        });
      }
    }
  });
}

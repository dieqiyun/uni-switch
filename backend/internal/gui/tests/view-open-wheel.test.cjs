// Run with Node's test runner and Playwright on the module path; see README.md.
// issennknight on Discord (after f92db4cdd): the Routing and Usage pages,
// scrolled as soon as they were opened, shook and snapped back. Each page
// puts the view back where the reader left it once what it loads comes in
// (show()'s back, and the providers' reload drawing a page they show): a
// redraw may have pulled it up. That ran while the reader was scrolling it
// too, with the place last heard of, and the page hears of a scroll late
// (WebKit scrolls on its own thread; a loaded Mac tells the page frames
// later), so each load that landed under the wheel took the reader's last
// steps back and stopped their coast. And on macOS 13 (WebKit 16.6, where
// issennknight is), the page never hears the wheel at all: every gesture's
// first scrolls were put back until three made a run, a shake as the page
// was opened and wheeled. Here the first data waits until the wheel is
// going, the page hears of each scroll 80ms late, or never (learned on the
// page before, as a real window learns it); the view never goes back up
// under a wheel going down, in the frames painted nor by a write the frames
// can't show. Both pages, opened and opened again, every language, narrow
// and wide, Chromium and WebKit. The browser's scroll anchoring (the view
// up as data comes in above, what's on the screen still) is not a put-back.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = process.env.UNI_SWITCH_OPEN_WHEEL_ASSETS || path.resolve(__dirname, "../assets");
const now = new Date();
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const at = (i) => new Date(now.getTime() - (i + 1) * 60e3).toISOString();
const who = { id: "fixture-key", provider: "fixture", name: "Fixture", who: "key-1", kind: "key", model: "model-a", routing: "", used: 0 };
const routes = Array.from({ length: 12 }, (_, i) => ({ id: 100 - i, seq: 100 - i, time: at(i), agent: "fixture", model: "model-a", provider: "fixture", order: [who],
  tries: [{ id: who.id, model: "model-a", start: at(i), done: true, status: 200, ms: 1200 }], done: true, status: 200, ms: 1200, tokens: 3000 }));
const AGENTS = Array.from({ length: 14 }, (_, i) => ({ id: "a" + i, name: "Agent " + i, path: "/t/a" + i + ".json", icon: "generic", wired: true,
  fields: [{ key: "model", label: "model", value: "m", options: [{ value: "m", label: "M", ref: "a/m" }] }] }));
const U = (id) => ({ id, name: id, calls: 10, errors: 0, input: 1000, output: 500, cache_read: 0, cache_write: 0, cost: 0.1 });
const USAGE = { calls: 140, errors: 0, input: 14000, output: 7000, reasoning: 0, unpriced: 0, cost: 1.4, bucket: "day",
  series: Array.from({ length: 7 }, (_, i) => ({ label: "d" + i, time: at(i * 1440), calls: 20, input: 2000, output: 1000, cost: 0.2 })),
  agents: Array.from({ length: 10 }, (_, i) => U("agent" + i)), models: Array.from({ length: 12 }, (_, i) => U("model" + i)), path: "" };
const QUOTAS = Array.from({ length: 4 }, (_, i) => ({ provider: "p" + i, name: "Vendor " + i, user: "u" + i + "@x.test", plan: "Pro",
  windows: [{ name: "5h", used: 20 + i, resets: new Date(now.getTime() + 3600e3).toISOString() }, { name: "Week", used: 40, resets: new Date(now.getTime() + 86400e3).toISOString() }] }));

// The first reads a page makes when it opens wait for open(): the reader
// wheels before they land
function server(lang, gate) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = async (data, slow) => { if (slow && gate.on) await gate.wait; return route.fulfill({ json: data }); };
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: AGENTS, profiles: [], settings: { lang, theme: "light", guideHidden: true } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } }, true);
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) {
        await new Promise((ok) => setTimeout(ok, 700));
        const k = gate.traceSeq = (gate.traceSeq || 1) + 1;
        if (k > 8) return new Promise(() => {});
        const r = { ...routes[0], id: 100 + k, seq: 100 + k, time: new Date().toISOString() };
        return route.fulfill({ json: { mine: true, now: new Date().toISOString(), seq: k, totals: { requests: routes.length + k, rerouted: 0, errors: 0 }, routes: [r] } });
      }
      return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: routes.length, rerouted: 0, errors: 0 }, routes }, true);
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [{ day, requests: routes.length }], routes: [] }, true);
    if (url.pathname === "/api/usage/quotas") {
      if (gate.on) await gate.wait;
      const n = gate.quotaReads = (gate.quotaReads || 0) + 1;
      const q = QUOTAS.map((x, i) => ({ ...x, windows: x.windows.map((w) => ({ ...w, used: w.used + n })) }));
      return route.fulfill({ json: q, headers: { "X-Uni-Switch-Reading": n < 6 ? "1" : "0", "Access-Control-Expose-Headers": "X-Uni-Switch-Reading" } });
    }
    if (url.pathname === "/api/usage") return json(USAGE, true);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] }, true);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (url.pathname === "/api/window/fit") return route.fulfill({ status: 204 });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file).catch(() => ""), contentType });
  };
}

// the page told of a view's scroll late, as WebKit tells it under load: the
// view is already where the wheel took it, the page still thinks it where
// it last heard
const LATE = `addEventListener("scroll", (e) => {
  const v = e.target;
  if (!e.isTrusted || !(v instanceof Element) || !v.classList.contains("view")) return;
  e.stopImmediatePropagation();
  setTimeout(() => v.dispatchEvent(new Event("scroll")), 80);
}, { capture: true });`;
// macOS 13's WebKit (16.6) scrolls a view under a wheel it never tells the
// page of: a capture listener before app.js's stops each one, as
// view-wheel-unheard does
const DEAF = `addEventListener("wheel", (e) => e.stopImmediatePropagation(), { capture: true, passive: true });`;
// every time code moves a view up, by how much: the put-backs the frames
// can't show (one put back before the frame is painted is a step lost)
const UPS = `{
  const d = Object.getOwnPropertyDescriptor(Element.prototype, "scrollTop");
  window.ups = [];
  Object.defineProperty(Element.prototype, "scrollTop", { get: d.get, configurable: true, set(y) {
    if (this.classList?.contains("view") && !this.hidden) window.ups.push(d.get.call(this) - y);
    d.set.call(this, y);
  } });
}`;
// UNI_SWITCH_OPEN_WHEEL_HEARD=late or unheard runs only that half
const HEARD = process.env.UNI_SWITCH_OPEN_WHEEL_HEARD ? [process.env.UNI_SWITCH_OPEN_WHEEL_HEARD] : ["late", "unheard"];
const LANGS = ["en", "zh", "zh-TW", "ja", "de"];

for (const engine of ["chromium", "webkit"]) {
  test(engine + ": a page scrolled as it opens keeps the reader's place when its data lands", { timeout: 900000 }, async (t) => {
    const browser = await (engine === "webkit" ? webkit : chromium).launch();
    t.after(() => browser.close());
    for (const heard of HEARD) for (const lang of LANGS) for (const width of [420, 1000]) for (const name of ["usage", "routing"]) for (const visit of ["opened", "opened again"]) {
      await t.test(`${name}, ${visit}, wheels ${heard}, ${lang}, ${width}px`, async () => {
        const ctx = await browser.newContext({ viewport: { width, height: 600 }, reducedMotion: "reduce" });
        t.after(() => ctx.close());
        const p = await ctx.newPage();
        p.setDefaultTimeout(8000);
        p.setDefaultNavigationTimeout(60000);
        const errors = [];
        p.on("pageerror", (e) => { if (!/^ResizeObserver loop/.test(e.message)) errors.push(e.message); });
        await p.addInitScript(heard === "late" ? LATE : DEAF);
        await p.addInitScript(UPS);
        const gate = { on: false };
        await p.route("**/*", server(lang, gate));
        await p.goto("http://uni-switch.test/?view=agents");
        await p.locator("#view-agents .ag-lead").first().waitFor();
        await p.waitForTimeout(500);
        // unheard: the window learns it on the page before, as the reader's
        // first scroll there teaches it (and it is kept for the next start)
        if (heard === "unheard") {
          const a = await p.locator("#view-agents").boundingBox();
          await p.mouse.move(a.x + a.width / 2, a.y + a.height / 2);
          for (let i = 0; i < 8; i++) { await p.mouse.wheel(0, 30); await p.waitForTimeout(16); }
          await p.waitForTimeout(400);
          assert.equal(await p.evaluate(() => localStorage.getItem("uni-switch.unheardWheels")), "1", "wheels the page never heard are learned");
        }
        const view = p.locator("#view-" + name);
        // opened again: what it drew the first time is there while it reads again
        if (visit === "opened again") {
          await p.locator(`#nav button[data-view="${name}"]`).click();
          await p.waitForTimeout(1000);
          await p.locator('#nav button[data-view="agents"]').click();
          await p.waitForTimeout(300);
        }
        let release;
        gate.wait = new Promise((ok) => { release = ok; });
        gate.on = true;
        await p.locator(`#nav button[data-view="${name}"]`).click();
        await view.waitFor();
        await p.waitForTimeout(150);
        // every frame's place, and how far what was under the pointer the
        // frame before has moved on the screen since
        const r = await view.boundingBox();
        const x = r.x + r.width / 2, y = r.y + r.height * 0.6;
        await view.evaluate((v, [x, y]) => {
          window.tops = []; window.downs = [];
          let was = null;
          const tick = () => {
            window.tops.push(v.scrollTop);
            const now = was?.[0].isConnected ? was[0].getBoundingClientRect().top : null;
            window.downs.push(now == null ? 0 : now - was[1]);
            const n = document.elementFromPoint(x, y);
            was = n && v.contains(n) ? [n, n.getBoundingClientRect().top] : null;
            requestAnimationFrame(tick);
          };
          requestAnimationFrame(tick);
        }, [x, y]);
        await p.mouse.move(x, y);
        await p.evaluate(() => { window.ups = []; });
        for (let i = 0; i < 30; i++) {
          await p.mouse.wheel(0, 30);
          await p.waitForTimeout(16);
          if (i === 6) { gate.on = false; release(); } // the data lands under the wheel
        }
        await p.waitForTimeout(1500);
        const [tops, downs, ups] = await p.evaluate(() => [window.tops, window.downs, window.ups]);
        // back is the view going up with what was under the pointer going
        // down with it: not the browser's anchoring (the view up, what's on
        // screen still), nor a part above growing (what's on screen down,
        // the view where it was)
        const back = tops.findIndex((y, i) => i && y < tops[i - 1] - 1 && downs[i] > 1);
        assert.equal(back, -1, `the view went back up under a wheel going down, ${tops[back - 1]} to ${tops[back]} (frame ${back}): ${tops.map(Math.round).join(" ")}`);
        // a step of the wheel put back (a redraw's few pixels kept in
        // place by the browser's anchoring are not one)
        assert.deepEqual(ups.filter((by) => by > 20).map(Math.round), [], "nothing put the view back up under the wheel: " + tops.map(Math.round).join(" "));
        assert.ok(tops[tops.length - 1] > 100, "the wheel scrolled the page: " + tops.map(Math.round).join(" "));
        assert.deepEqual(errors, []);
      });
    }
  });
}

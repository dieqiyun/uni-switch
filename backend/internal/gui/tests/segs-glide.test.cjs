// Run with Node's test runner and Playwright on the module path; see README.md.
// A segmented control's thumb glides once, from where it is, to the option
// picked, and no other thumb on the page moves. Every save's answer draws
// Settings again, a new thumb for every control, and a new thumb took its
// place from what was remembered: a redraw mid-glide started the glide over
// (Lightweight mode's thumb went most of the way to On, jumped back to Off
// and slid again), and Settings drawn while hidden (opened on another tab,
// or at start) was remembered 0 wide, so the first save grew every thumb on
// the page in from the left. Checked frame by frame: General shown from
// another tab has every thumb under its option; a click whose save comes
// back mid-glide, one whose save comes back at once, and one redrawn twice
// on the way all move the thumb one way only and end under the option, with
// every other thumb still; a refused save glides it back. Chromium and
// WebKit, en, zh, zh-TW, ja and de, 900 and 440px, the API faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");

function serve(lang, st, ctl) {
  const settings = () => ({ lang, theme: "light", searchVendors: [], searchAPIs: [], ...st });
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data, status = 200) => r.fulfill({ status, json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: settings() });
    if (url.pathname === "/api/settings" && r.request().method() === "POST") {
      await new Promise((ok) => setTimeout(ok, ctl.hold));
      if (ctl.refuse) return json({ error: "refused" }, 500);
      Object.assign(st, JSON.parse(r.request().postData()));
      return json(settings());
    }
    if (url.pathname.startsWith("/api/settings")) return json(settings());
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname === "/api/groups") return json({ models: [], groups: [], pools: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await r.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// every thumb on General: where it is drawn, and where its option is. A
// control is named by the nearest id above it: two off|on controls whose
// parent has none (Full window's and Codex async's, in .compact-at) were
// one name, and one's thumb was read as the other's (#2683)
const THUMBS = `[...document.querySelectorAll("#setPage-general .segs")].filter((s) => s.offsetParent).map((s) => {
  const th = s.querySelector(":scope > .thumb"), on = s.querySelector(":scope > .on"), b = s.getBoundingClientRect();
  const r = th?.getBoundingClientRect(), o = on?.getBoundingClientRect();
  return { id: s.parentElement.id || s.parentElement.closest("[id]")?.id + " " + s.dataset.kind, x: r ? r.left - b.left : -1, w: r ? r.width : -1, ox: o ? o.left - b.left : -1, ow: o ? o.width : -1 };
})`;

// records every thumb on General at each frame, and between them
async function record(page) {
  await page.evaluate((src) => {
    const read = new Function("return " + src);
    window.frames_ = [];
    window.recording = true;
    const tick = () => { window.frames_.push(read()); if (window.recording) requestAnimationFrame(tick); };
    requestAnimationFrame(tick);
    window.between = setInterval(() => window.frames_.push(read()), 4);
  }, THUMBS);
}
async function stop(page) {
  return page.evaluate(() => { window.recording = false; clearInterval(window.between); return window.frames_; });
}
// one control's thumb moved one way only, from its start to its end, and
// every other control's thumb didn't move at all
function oneWay(frames, id, label) {
  const xs = frames.map((f) => f.find((c) => c.id === id)).filter(Boolean);
  assert.ok(xs.length > 3, label + ": frames recorded");
  const ids = frames[0].map((c) => c.id);
  assert.deepEqual(ids, [...new Set(ids)], label + ": two controls under one name");
  const up = xs[xs.length - 1].x >= xs[0].x;
  for (let i = 1; i < xs.length; i++) {
    const d = xs[i].x - xs[i - 1].x;
    assert.ok(up ? d > -0.75 : d < 0.75, `${label}: ${id}'s thumb went back from ${xs[i - 1].x.toFixed(1)} to ${xs[i].x.toFixed(1)} (${xs.map((p) => p.x.toFixed(0)).join(" ")})`);
    assert.ok(xs[i].w > 4, `${label}: ${id}'s thumb ${xs[i].w.toFixed(1)} wide`);
  }
  for (const c of frames[0]) {
    if (c.id === id) continue;
    for (const f of frames) {
      const n = f.find((x) => x.id === c.id);
      if (!n) continue;
      assert.ok(Math.abs(n.x - c.x) < 0.75 && Math.abs(n.w - c.w) < 0.75, `${label}: ${c.id}'s thumb moved (${c.x.toFixed(1)}/${c.w.toFixed(1)} → ${n.x.toFixed(1)}/${n.w.toFixed(1)}) when ${id} was clicked`);
    }
  }
}
// every shown thumb under its option once it has come to rest
async function underPicks(page, label) {
  await page.waitForTimeout(400);
  for (const c of await page.evaluate((src) => new Function("return " + src)(), THUMBS)) {
    assert.ok(Math.abs(c.x - c.ox) < 1 && Math.abs(c.w - c.ow) < 1, `${label}: ${c.id}'s thumb at ${c.x.toFixed(1)}/${c.w.toFixed(1)}, its option at ${c.ox.toFixed(1)}/${c.ow.toFixed(1)}`);
  }
}

// brought out from under the header and the foot by the reader's wheel, as
// code's scrolls are put back, till the pointer at its middle is on it
async function reveal(page, loc) {
  for (let i = 0; i < 40; i++) {
    const b = await loc.boundingBox();
    const h = page.viewportSize().height;
    // all of it: one cut by the view's foot is scrolled in by Playwright's
    // click itself, and the page seemed to move on a click (#2683)
    if (b && await loc.evaluate((o, [x, y]) => {
      const v = o.closest("#view-settings").getBoundingClientRect(), r = o.getBoundingClientRect();
      return o.contains(document.elementFromPoint(x, y)) && r.top >= v.top && r.bottom <= v.bottom;
    }, [b.x + b.width / 2, b.y + b.height / 2])) return;
    await page.mouse.move(page.viewportSize().width / 2, h / 2);
    await page.mouse.wheel(0, b && b.y < h / 2 ? -150 : 150);
    await page.waitForTimeout(60);
  }
  throw new Error("could not bring the option into view");
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [900, 440]) {
      test(`${engine} ${lang} ${width}px: a thumb glides once, one way, and no other thumb moves`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await (await browser.newContext({ viewport: { width, height: 1000 } })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        if (process.env.THROTTLE && engine === "chromium") await (await page.context().newCDPSession(page)).send("Emulation.setCPUThrottlingRate", { rate: +process.env.THROTTLE });
        const errors = [];
        // WebKit's notice of a ResizeObserver loop is no error of the page's
        page.on("pageerror", (e) => { if (!/ResizeObserver loop/.test(e.message)) errors.push(e.message); });
        // turned off by the user, so On is a glide away
        const st = { lightweight: false }, ctl = { hold: 0, refuse: false };
        await page.route("**/*", serve(lang, st, ctl));
        // opened on another tab: General is drawn while hidden
        await watchSettingsReads(page);
        await page.goto("http://uni-switch.test/?view=settings&tab=about");
        await settingsDrawn(page); // rows found are the ones that stay (#1659)
        await page.locator("#setTab-general").waitFor();
        await page.waitForTimeout(300);
        await page.locator("#setTab-general").click();
        await page.locator("#lightweightSegs .opt").first().waitFor();
        await underPicks(page, "General shown from another tab");

        const opt = (i) => page.locator("#lightweightSegs .opt").nth(i);
        const scroll = () => page.evaluate(() => document.querySelector("#view-settings").scrollTop);
        await reveal(page, opt(1));
        await page.waitForTimeout(300);
        const top = await scroll();

        // the save's answer comes back mid-glide, and draws the page again
        ctl.hold = 120;
        await record(page);
        await opt(1).click();
        await page.waitForTimeout(700);
        oneWay(await stop(page), "lightweightSegs", "answer mid-glide");
        assert.equal(st.lightweight, true);
        await underPicks(page, "after On");

        // the answer at once, then the page drawn twice more on the way
        ctl.hold = 0;
        await record(page);
        await opt(0).click();
        await page.waitForTimeout(60);
        await page.evaluate(() => renderSettings());
        await page.waitForTimeout(60);
        await page.evaluate(() => renderSettings());
        await page.waitForTimeout(600);
        oneWay(await stop(page), "lightweightSegs", "drawn again on the way");
        assert.equal(st.lightweight, false);
        await underPicks(page, "after Off");

        // a refused save puts Off back: the thumb glides back to it
        ctl.refuse = true;
        ctl.hold = 300;
        await record(page);
        await opt(1).click();
        await page.waitForTimeout(1000);
        const frames = await stop(page);
        const lw = frames.map((f) => f.find((c) => c.id === "lightweightSegs")).filter(Boolean);
        assert.ok(lw.some((p) => p.x > lw[0].x + 8), "it went towards On first");
        await underPicks(page, "a refused save");
        assert.equal(await opt(0).evaluate((b) => b.classList.contains("on")), true, "Off is back");

        assert.equal(await scroll(), top, "a click never moves the page");
        assert.deepEqual(errors, []);
      });
    }
  }
}

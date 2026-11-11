// Run with Node's test runner and Playwright on the module path; see README.md.
// A provider's editor grows out of the row that was clicked and folds back
// into it (the owner, 10-20: it came from the top right or a lower corner,
// not from the provider, unlike iOS), and as an add sheet's tile does (10-21:
// a full-size strip widening over the row was "太生硬"). Held at the start of
// the opening and at the end of the closing, what shows of the dialog is the
// dialog shrunk over the row's icon and name: the row's height, from its
// left to where its words end. At rest
// nothing is clipped, the shadow included. Halfway back it still shows,
// only the backdrop fading. Reduced motion only fades. Wide and narrow,
// Chromium and WebKit; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const provider = (id, name) => ({
  id, name, icon: "generic", chat: `https://api.${id}.example/v1`, url: `https://api.${id}.example/v1`, host: `api.${id}.example`, responses: "", anthropic: "", catalog: "",
  models: [{ id: "m1", name: "", on: true }, { id: "m2", name: "", on: true }],
  agents: [], fallback: [], headers: {}, keyList: [], proxy: "", key: { set: true, masked: "sk-…1234" }, ready: true,
});

function serve() {
  const settings = { theme: "light", lang: "en", tray: "panel", currency: "usd" };
  const providers = { providers: ["alpha", "beta", "gamma", "delta", "epsilon", "zeta"].map((id) => provider(id, id[0].toUpperCase() + id.slice(1))), presets: [], excluded: [], gateway: { running: true, window: true } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"en",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    try {
      await route.fulfill({ body: await fs.readFile(file), contentType: { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)] });
    } catch {
      await route.fulfill({ status: 404, body: "" });
    }
  };
}

// what shows of the dialog, on screen: its transformed box less its clip,
// with every animation on it and the backdrop held at time `at` (a fraction
// of each one)
const shown = (page, at) => page.evaluate((at) => {
  const d = document.querySelector("#modal > .dialog");
  for (const a of [...d.parentElement.getAnimations(), ...d.getAnimations()]) { a.pause(); a.currentTime = at * a.effect.getTiming().duration; }
  const box = d.getBoundingClientRect(), s = box.width / d.offsetWidth;
  const clip = getComputedStyle(d).clipPath;
  const m = clip.match(/^inset\(([-\d.]+)px(?: ([-\d.]+)px)?(?: ([-\d.]+)px)?(?: ([-\d.]+)px)?/);
  if (!m) return { clip, top: box.top, bottom: box.bottom, left: box.left, right: box.right };
  const [t, r = t, b = t, l = r] = m.slice(1).map((v) => v === undefined ? undefined : +v);
  return { clip, top: box.top + Math.max(0, t) * s, bottom: box.bottom - Math.max(0, b) * s, left: box.left + Math.max(0, l) * s, right: box.right - Math.max(0, r) * s };
}, at);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const width of [1240, 420]) {
    test(`${engine} ${width}: a provider's editor grows out of its row and folds back into it`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await browser.newPage({ viewport: { width, height: 760 }, reducedMotion: "no-preference" });
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve());
      // the modal's animations are held from the moment they start, so a
      // slow machine can't let one finish before it is measured
      await page.addInitScript(() => {
        const animate = Element.prototype.animate;
        Element.prototype.animate = function () {
          const a = animate.apply(this, arguments);
          if (window.__hold && this.matches("#modal, #modal > .dialog")) a.pause();
          return a;
        };
      });
      await page.goto("http://uni-switch.test/?view=providers");
      const row = page.locator('.row.provider[data-id="delta"]');
      await row.waitFor();
      // the list still: at 420px its rows come in, a pixel or more each
      // frame, and a row measured on the way read 1 to 5px off where the
      // dialog then grew from (10-23)
      await page.waitForFunction(() => {
        const y = document.querySelector('.row.provider[data-id="delta"]').getBoundingClientRect().y;
        const was = window.__rowY;
        window.__rowY = y;
        return was === y;
      }, null, { polling: 100 });
      await page.evaluate(() => { window.__hold = true; });
      const r = await row.boundingBox();

      // held at the start: a strip of the dialog over the row, pressed on
      // its logo, the row's handle (it rose from below, 10-24)
      await row.locator(".pv-handle").click();
      await page.locator("#modal:not([hidden]) > .dialog").waitFor();
      const start = await shown(page, 0);
      assert.match(start.clip, /^inset\(/, `${engine} ${width}: the opening clips the dialog: ${start.clip}`);
      const near = (a, b, what) => assert(Math.abs(a - b) <= 3, `${engine} ${width}: ${what}: ${a} vs ${b}`);
      near(start.top, r.y, "the strip's top on the row's");
      near(start.bottom, r.y + r.height, "the strip's bottom on the row's");
      assert(start.left >= r.x - 1 && start.right <= r.x + r.width + 1, `${engine} ${width}: the strip stays inside the row: ${JSON.stringify(start)} ${JSON.stringify(r)}`);
      // out of the provider's icon and name, as a tile in the add sheet:
      // the dialog shrunk to them, not a full-size strip widening (the
      // owner, 10-21: "太生硬")
      const words = await row.evaluate((row) => {
        const text = document.createRange();
        let end = 0;
        for (const n of row.querySelectorAll(".who .name, .who .sub")) { text.selectNodeContents(n.firstChild); end = Math.max(end, text.getBoundingClientRect().right); }
        return end;
      });
      near(start.left, r.x, "the start's left on the row's");
      assert(start.right <= words + 16, `${engine} ${width}: it starts over the icon and name, not the row: ${start.right} vs ${words}`);
      const scale = await page.evaluate(() => { const d = document.querySelector("#modal > .dialog"); return d.getBoundingClientRect().width / d.offsetWidth; });
      assert(scale < .8, `${engine} ${width}: it starts shrunk, as a tile's: ${scale}`);
      if (process.env.MORPH_SHOT) await page.screenshot({ path: `${process.env.MORPH_SHOT}-${engine}-${width}-open0.png` });
      for (const at of [.15, .35]) {
        await shown(page, at);
        if (process.env.MORPH_SHOT) await page.screenshot({ path: `${process.env.MORPH_SHOT}-${engine}-${width}-open${at}.png` });
      }

      // at rest: nothing clipped, its shadow whole
      await page.evaluate(() => { for (const a of document.querySelector("#modal").getAnimations({ subtree: true })) a.finish(); });
      const rest = await page.locator("#modal > .dialog").evaluate((d) => [getComputedStyle(d).clipPath, getComputedStyle(d).transform]);
      assert.deepEqual(rest, ["none", "none"], `${engine} ${width}: at rest`);
      if (process.env.MORPH_SHOT) await page.screenshot({ path: `${process.env.MORPH_SHOT}-${engine}-${width}-rest.png` });

      // closed: held at its end, back in the row's strip
      await page.keyboard.press("Escape");
      await page.locator("#modal.out").waitFor({ state: "attached" });
      for (const at of [.5]) {
        await shown(page, at);
        if (process.env.MORPH_SHOT) await page.screenshot({ path: `${process.env.MORPH_SHOT}-${engine}-${width}-close${at}.png` });
      }
      // halfway back it is still there to see: the backdrop fades by
      // itself, not the modal holding the dialog
      const seen = await page.evaluate(() => {
        const m = document.querySelector("#modal");
        return +getComputedStyle(m).opacity * +getComputedStyle(m.firstElementChild).opacity;
      });
      assert(seen > .8, `${engine} ${width}: halfway back the dialog shows: ${seen}`);
      const end = await shown(page, 1);
      near(end.top, r.y, "closed: the strip's top on the row's");
      near(end.bottom, r.y + r.height, "closed: the strip's bottom on the row's");
      assert(end.left >= r.x - 1 && end.right <= words + 16, `${engine} ${width}: closed into the icon and name: ${JSON.stringify(end)}`);
      await page.evaluate(() => { window.__hold = false; for (const a of [...document.querySelector("#modal").getAnimations({ subtree: true })]) a.play(); });
      await page.locator("#modal").waitFor({ state: "hidden" });
      assert.equal(await page.locator("#modal > .dialog").evaluate((d) => d.style.clipPath), "", "no clip left behind");

      // opened again from past its name: it grows from where it was
      // pressed (the owner, 10-24: 应该从点击处放大), and folds back there
      await page.evaluate(() => { window.__hold = true; });
      const px = r.width * .62;
      await row.click({ position: { x: px, y: r.height / 2 } });
      await page.locator("#modal:not([hidden]) > .dialog").waitFor();
      const again = await shown(page, 0);
      near(again.top, r.y, "again: the strip's top on the row's");
      near(again.bottom, r.y + r.height, "again: the strip's bottom on the row's");
      near((again.left + again.right) / 2, r.x + px, "again: the strip centred on the press");
      near(again.right - again.left, start.right - start.left, "again: as wide as the icon and name were");
      assert(again.left >= r.x - 1 && again.right <= r.x + r.width + 1, `${engine} ${width}: again inside the row: ${JSON.stringify(again)}`);
      await page.evaluate(() => { for (const a of document.querySelector("#modal").getAnimations({ subtree: true })) a.finish(); });
      await page.keyboard.press("Escape");
      await page.locator("#modal.out").waitFor({ state: "attached" });
      const back = await shown(page, 1);
      near((back.left + back.right) / 2, r.x + px, "closed again: back where it was pressed");
      assert.deepEqual(errors, []);
    });
  }

  test(`${engine}: reduced motion only fades`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const page = await browser.newPage({ viewport: { width: 1240, height: 760 }, reducedMotion: "reduce" });
    page.setDefaultTimeout(5000);
    page.setDefaultNavigationTimeout(60000);
    await page.route("**/*", serve());
    await page.goto("http://uni-switch.test/?view=providers");
    const row = page.locator('.row.provider[data-id="delta"]');
    await row.locator(".name, .who").first().click();
    await page.locator("#modal:not([hidden]) > .dialog").waitFor();
    const moving = await page.locator("#modal > .dialog").evaluate((d) => d.getAnimations().flatMap((a) => a.effect.getKeyframes().flatMap((k) => Object.keys(k))).filter((k) => k === "transform" || k === "clipPath"));
    assert.deepEqual(moving, [], "nothing moves or clips under reduced motion");
  });
}

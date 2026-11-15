// A failed state's dot is an x, not a red dot alone (coolmantou on Discord,
// who is red-green colour-weak): green and red dots read as one colour to
// such a reader, so the shape says which is which, in greyscale too. Every
// ok/bad pair of status dots is checked with the real stylesheets: the good
// one stays a round dot, the bad one is drawn through the x mask, with ink on
// its arms and none above its centre, and the box it takes in the line is
// the good one's, so nothing moves. Chromium and WebKit, light and dark,
// wide and narrow; no strings, no backend.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const assets = path.resolve(__dirname, "../assets");

// each pair: its name, the good one's markup and the bad one's, and the dot in it
const PAIRS = [
  ["test models", '<span class="mchip">gpt-5 <span class="tdot ok"></span></span>', '<span class="mchip">gpt-5 <span class="tdot bad"></span></span>', ".tdot"],
  ["agent", '<div class="ag-st on"><span class="ag-dot"></span><span class="ag-st-t">Connected</span></div>', '<div class="ag-st bad"><span class="ag-dot"></span><span class="ag-st-t">Not wired</span></div>', ".ag-dot"],
  ["check-in", '<div class="wb-checkin" data-state="ok"><span class="ci-say"><i class="ci-dot ok"></i><span>Checked in</span></span></div>', '<div class="wb-checkin"><span class="ci-say"><i class="ci-dot bad"></i><span>Failed</span></span></div>', ".ci-dot"],
  ["request ledger", '<table class="led"><tr class="led-row"><td><span class="st"><i class="dot"></i>200</span></td></tr></table>', '<table class="led"><tr class="led-row bad"><td><span class="st"><i class="dot"></i>429</span></td></tr></table>', ".st .dot"],
  ["routing try", '<div class="rt-req"><span class="to"><i></i>200</span></div>', '<div class="rt-req bad"><span class="to"><i></i>429</span></div>', ".to i"],
  ["routing probe", '<div class="rt-probe"><span class="to"><i></i>ok</span></div>', '<div class="rt-probe bad"><span class="to"><i></i>failed</span></div>', ".to i"],
  ["routing preview", '<span class="ps-node ps-dst"><i></i><span class="ps-name">a</span></span>', '<span class="ps-node ps-dst bad"><i></i><span class="ps-name">a</span></span>', ".ps-dst > i"],
  ["routing step", '<ul class="rt-steps"><li class="ok">answered</li></ul>', '<ul class="rt-steps"><li class="bad">refused</li></ul>', ".rt-steps li", "::before"],
  ["library server", '<span class="lib-health ok"><span>Running</span></span>', '<span class="lib-health err"><span>Failed</span></span>', ".lib-health", "::before"],
];

const page = (theme) => `<!doctype html><html data-theme="${theme}"><head><meta charset="utf-8">
<link rel="stylesheet" href="/app.css"><link rel="stylesheet" href="/plugins/routing/routing.css"><link rel="stylesheet" href="/plugins/library/library.css">
<style>body { margin: 0; padding: 24px; background: var(--card); } .pair { display: flex; gap: 40px; padding: 10px 0; } .pair > * { flex: none; } .rt-steps { margin: 0; padding: 0; list-style: none; }</style>
</head><body>${PAIRS.map(([, ok, bad]) => `<div class="pair"><div class="good">${ok}</div><div class="bad-one">${bad}</div></div>`).join("")}</body></html>`;

for (const engine of process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"]) {
  test(engine + ": a failed status dot is an x, a good one a dot", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const theme of ["light", "dark"]) {
      for (const width of [980, 420]) {
        await t.test(`${theme} at ${width}`, async () => {
          const context = await browser.newContext({ viewport: { width, height: 900 }, deviceScaleFactor: 4, reducedMotion: "reduce", colorScheme: theme });
          t.after(() => context.close());
          const p = await context.newPage();
          await p.route("**/*", async (route) => {
            const url = new URL(route.request().url());
            if (url.pathname === "/") return route.fulfill({ contentType: "text/html", body: page(theme) });
            if (url.pathname.endsWith(".css")) return route.fulfill({ contentType: "text/css", body: await fs.readFile(path.join(assets, url.pathname)) });
            return route.fulfill({ status: 404, body: "" });
          });
          await p.goto("http://uni-switch.test/");
          await p.evaluate(() => document.fonts.ready);

          for (let i = 0; i < PAIRS.length; i++) {
            const [name, , , sel, pseudo = null] = PAIRS[i];
            const read = (side) => p.locator(`.pair >> nth=${i}`).locator(`${side} ${sel}`).evaluate((e, pseudo) => {
              const s = getComputedStyle(e, pseudo);
              return { mask: s.maskImage && s.maskImage !== "none" ? s.maskImage : s.webkitMaskImage || "none", w: s.width, h: s.height, bg: s.backgroundColor };
            }, pseudo);
            const good = await read(".good"), bad = await read(".bad-one");
            assert.equal(good.mask, "none", `${name}: the good dot is a plain dot`);
            assert.match(bad.mask, /data:image\/svg\+xml/, `${name}: the failed dot is drawn through the x`);
            assert.deepEqual([bad.w, bad.h], [good.w, good.h], `${name}: the failed dot takes the good one's box`);

            // what is drawn: ink on the x's arms and at its centre, none
            // straight above the centre, where a round dot is solid
            const box = await p.locator(`.pair >> nth=${i}`).locator(`.bad-one ${sel}`).evaluate((e, pseudo) => {
              if (!pseudo) { const r = e.getBoundingClientRect(); return { x: r.x, y: r.y, w: r.width, h: r.height }; }
              // a pseudo-element has no rect of its own: read its place
              // from its computed box, scaled about its centre as drawn
              const s = getComputedStyle(e, pseudo), r = e.getBoundingClientRect();
              const w = parseFloat(s.width), h = parseFloat(s.height);
              const m = new DOMMatrix(s.transform === "none" ? undefined : s.transform);
              let x, y;
              if (s.position === "absolute") { x = r.x + parseFloat(s.left); y = r.y + parseFloat(s.top); }
              else { x = r.x + parseFloat(getComputedStyle(e).paddingLeft); y = r.y + (r.height - h) / 2; }
              return { x: x + w / 2 - (w * m.a) / 2, y: y + h / 2 - (h * m.d) / 2, w: w * m.a, h: h * m.d };
            }, pseudo);
            // a little room round it: where an engine places a scaled
            // box differs by a pixel or two, so the mark is found by its
            // colour inside the crop, then read within its own bounds
            const pad = 2.5;
            const png = await p.screenshot({ clip: { x: box.x - pad, y: box.y - pad, width: box.w + 2 * pad, height: box.h + 2 * pad } });
            const ink = await p.evaluate(async ([b64, colour]) => {
              const img = new Image();
              img.src = "data:image/png;base64," + b64;
              await img.decode();
              const c = document.createElement("canvas");
              c.width = img.width; c.height = img.height;
              const g = c.getContext("2d");
              g.drawImage(img, 0, 0);
              const px = g.getImageData(0, 0, img.width, img.height).data;
              const want = colour.match(/[\d.]+/g).map(Number);
              const on = (x, y) => { const k = (y * img.width + x) * 4; return Math.abs(px[k] - want[0]) + Math.abs(px[k + 1] - want[1]) + Math.abs(px[k + 2] - want[2]) < 90; };
              let x0 = img.width, y0 = img.height, x1 = -1, y1 = -1;
              for (let y = 0; y < img.height; y++) for (let x = 0; x < img.width; x++) if (on(x, y)) { x0 = Math.min(x0, x); y0 = Math.min(y0, y); x1 = Math.max(x1, x); y1 = Math.max(y1, y); }
              if (x1 < 0) return { none: true };
              const at = (fx, fy) => on(Math.round(x0 + fx * (x1 - x0)), Math.round(y0 + fy * (y1 - y0)));
              return { size: [x1 - x0 + 1, y1 - y0 + 1], centre: at(0.5, 0.5), arm: at(0.15, 0.15) && at(0.85, 0.85) && at(0.85, 0.15) && at(0.15, 0.85), top: at(0.5, 0.1) };
            }, [png.toString("base64"), bad.bg]);
            assert(ink.centre && ink.arm, `${name}: the x is drawn (${JSON.stringify(ink)})`);
            assert(!ink.top, `${name}: the failed dot isn't a round dot (${JSON.stringify(ink)})`);
          }
        });
      }
    }
  });
}

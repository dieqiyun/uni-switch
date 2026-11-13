// Run with Node's test runner and Playwright on the module path; see README.md.
// The menu bar panel's usage rings are round on a 1x screen too (#1735,
// beansiayo: on an external 1080p display the ring looked slightly oval).
// A ring is two circles, .pq-dial and the .pq-dial b over it. With 34px and
// 27px the band was 3.5px a side, so at DPR 1 each edge rounded its own way:
// 4, 3, 3 and 4 device pixels (parkavenue9639's measurement). The test paints
// the band and the inner circle in flat colours, screenshots each ring and
// counts the band's pixels left, right, above and below the inner circle, at
// device scale 1 and 2, in Chromium and WebKit, English and Chinese.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const at = (h) => new Date(Date.now() + h * 36e5).toISOString();
const quotas = [
  { provider: "claude", name: "Claude Code", icon: "claude-color", plan: "Max 5x",
    windows: [{ name: "5 hours", used: 30, resetsAt: at(2) }, { name: "7 days", used: 95, resetsAt: at(90) }] },
];

function serve(lang) {
  const settings = { theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd" };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") return json(quotas);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

// the band's run of pixels on each side of the inner circle, through its
// centre row and column, in device pixels
async function bands(page, png) {
  return page.evaluate(async (src) => {
    const img = new Image();
    img.src = src;
    await img.decode();
    const c = document.createElement("canvas");
    c.width = img.width;
    c.height = img.height;
    const g = c.getContext("2d");
    g.drawImage(img, 0, 0);
    const d = g.getImageData(0, 0, c.width, c.height).data;
    // red is the band, blue the inner circle, anything else the backdrop
    const kind = (x, y) => {
      const i = (y * c.width + x) * 4, r = d[i], b = d[i + 2], gr = d[i + 1];
      return r > 128 && r > b && r > gr ? "r" : b > 128 && b > r && b > gr ? "b" : "";
    };
    let x0 = c.width, x1 = -1, y0 = c.height, y1 = -1;
    for (let y = 0; y < c.height; y++) for (let x = 0; x < c.width; x++) if (kind(x, y) === "b") {
      x0 = Math.min(x0, x); x1 = Math.max(x1, x); y0 = Math.min(y0, y); y1 = Math.max(y1, y);
    }
    const cx = Math.floor((x0 + x1) / 2), cy = Math.floor((y0 + y1) / 2);
    const run = (x, y, dx, dy) => {
      while (kind(x, y) === "b") { x += dx; y += dy; }
      let n = 0;
      while (x >= 0 && y >= 0 && x < c.width && y < c.height && kind(x, y) === "r") { n++; x += dx; y += dy; }
      return n;
    };
    return { left: run(cx, cy, -1, 0), right: run(cx, cy, 1, 0), top: run(cx, cy, 0, -1), bottom: run(cx, cy, 0, 1) };
  }, "data:image/png;base64," + png.toString("base64"));
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: the usage rings are round at 1x and 2x`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      for (const scale of [1, 2]) {
        const page = await (await browser.newContext({ viewport: { width: 440, height: 640 }, deviceScaleFactor: scale, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        await page.goto("http://uni-switch.test/?mode=panel");
        await page.locator('#ptabs [data-ptab="usage"]').click();
        const dials = page.locator(".pq-dial");
        await dials.first().waitFor();
        assert.equal(await dials.count(), 2);
        // flat colours, the sizes left as they are
        await page.addStyleTag({ content: ".pq-ring, .pq-ring.full { background: #0f0 !important; } .pq-dial, .pq-ring.full .pq-dial { background: #f00 !important; } .pq-dial b { background: #00f !important; color: transparent !important; }" });
        await page.waitForTimeout(100);
        for (let i = 0; i < 2; i++) {
          const box = await dials.nth(i).boundingBox();
          const png = await page.screenshot({ clip: { x: box.x - 2, y: box.y - 2, width: box.width + 4, height: box.height + 4 }, animations: "disabled" });
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await fs.writeFile(path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-quota-dial-${scale}x-${i}.png`), png);
          }
          const b = await bands(page, png);
          assert.equal(b.left, b.right, `${scale}x ring ${i}: the band as wide left and right ${JSON.stringify(b)}`);
          assert.equal(b.top, b.bottom, `${scale}x ring ${i}: the band as wide above and below ${JSON.stringify(b)}`);
          assert(b.left > 0 && b.top > 0, `${scale}x ring ${i}: the band shows ${JSON.stringify(b)}`);
        }
        assert.deepEqual(errors, []);
        await page.context().close();
      }
    });
  }
}

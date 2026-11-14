// Run with Node's test runner and Playwright on the module path; see README.md.
// A logo that is a solid tile or disc to its edges (Codex, WorkBuddy,
// ZCode) read a size bigger than a line logo (Pi, OpenCode, Hermes) in the
// row beside it: both filled the same box, the tile with ~96% of it inked
// and the line logo with ~40%. A tile is now drawn at 82% of the box, the
// size Alma's and Cindy's tiles have of their own. Every logo in icons/ is
// measured (the app's own icons, manifest.json's, aren't logos), so a new
// tile logo left out of app.css's list, or a line logo put in it, fails
// here. In the Agents list the tile is drawn at 82% and the line logo at
// full size, in Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

// inked to within 7% of every edge, and most of the box inked
const isTile = (r) => Math.min(r.w, r.h) >= 0.93 && r.ink >= 0.7;

async function tileList() {
  const src = await fs.readFile(path.join(assets, "app.css"), "utf8");
  const m = src.match(/\.ic:is\(([^)]*)\) > \* \{ width: 82%; height: 82%; \}/);
  assert.ok(m, "app.css has the tile logos' rule");
  return new Set([...m[1].matchAll(/\[data-icon="([^"]+)"\]/g)].map((x) => x[1]));
}

// the app's own icons, which manifest.json lists for a phone's home screen
// (apple-touch-icon is index.html's too), are never a logo drawn in a .ic
// (#3858)
async function appIcons() {
  const manifest = JSON.parse(await fs.readFile(path.join(assets, "manifest.json"), "utf8"));
  return new Set(manifest.icons.map((i) => i.src.replace(/^icons\//, "")));
}

test("the tile logos' rule names every logo solid to its edges, and no other", async () => {
  const listed = await tileList(), own = await appIcons();
  const files = (await fs.readdir(path.join(assets, "icons"))).filter((f) => /\.(svg|png)$/.test(f) && !own.has(f));
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage();
    await page.setContent("<html></html>");
    const found = new Set(), all = new Set();
    for (const f of files) {
      const name = f.replace(/\.(svg|png)$/, "");
      all.add(name);
      const mime = f.endsWith(".png") ? "image/png" : "image/svg+xml";
      const url = `data:${mime};base64,` + (await fs.readFile(path.join(assets, "icons", f))).toString("base64");
      const r = await page.evaluate(async (url) => {
        const img = new Image();
        img.src = url;
        await img.decode();
        const S = 256, c = document.createElement("canvas");
        c.width = c.height = S;
        const x = c.getContext("2d");
        const nw = img.naturalWidth || S, nh = img.naturalHeight || S, k = Math.min(S / nw, S / nh);
        x.drawImage(img, (S - nw * k) / 2, (S - nh * k) / 2, nw * k, nh * k);
        const d = x.getImageData(0, 0, S, S).data;
        let l = S, t = S, rt = -1, b = -1, ink = 0;
        for (let y = 0; y < S; y++) for (let i = 0; i < S; i++) {
          if (d[(y * S + i) * 4 + 3] <= 24) continue;
          ink++; l = Math.min(l, i); rt = Math.max(rt, i); t = Math.min(t, y); b = Math.max(b, y);
        }
        return { w: (rt - l + 1) / S, h: (b - t + 1) / S, ink: ink / S / S };
      }, url);
      if (isTile(r)) found.add(name);
    }
    for (const n of found) assert.ok(listed.has(n), `${n} is solid to its edges but not in app.css's tile rule`);
    for (const n of listed) {
      assert.ok(all.has(n), `the tile rule names ${n}, which icons/ doesn't have`);
      assert.ok(found.has(n), `the tile rule names ${n}, which is not solid to its edges`);
    }
  } finally {
    await browser.close();
  }
});

function serve() {
  const agents = [
    { id: "pi", name: "Pi", icon: "pi", path: "/test/pi", fields: [] },
    { id: "codex", name: "Codex", icon: "codex-color", path: "/test/codex", fields: [] },
    { id: "zcode", name: "ZCode", icon: "zcode", path: "/test/zcode", fields: [] },
  ];
  const state = { agents, profiles: [], settings: { lang: "en", theme: "dark" } };
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"en",theme:"dark",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) return new Promise(() => {});
      return json({ mine: true, now: new Date().toISOString(), seq: 0, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    let body;
    try { body = await fs.readFile(file); } catch { return r.fulfill({ status: 404, body: "" }); }
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await r.fulfill({ body, contentType, headers: { "Cache-Control": "no-cache" } });
  };
}

for (const [engine, type] of [["chromium", chromium], ["webkit", webkit]]) {
  test(`a tile logo is drawn at 82% of its box, a line logo whole (${engine})`, async () => {
    const browser = await type.launch();
    try {
      const page = await browser.newPage({ viewport: { width: 900, height: 700 } });
      await page.route("http://uni-switch.test/**", serve());
      await page.goto("http://uni-switch.test/");
      const size = (icon) => page.evaluate((icon) => {
        const e = document.querySelector(`.ic[data-icon="${icon}"]`);
        if (!e || !e.firstElementChild) return null;
        const b = e.getBoundingClientRect(), p = e.firstElementChild.getBoundingClientRect();
        return { box: b.width, pic: p.width, dx: p.left - b.left - (b.right - p.right), dy: p.top - b.top - (b.bottom - p.bottom) };
      }, icon);
      await page.waitForFunction(() => ["pi", "codex-color", "zcode"].every((i) => document.querySelector(`.ic[data-icon="${i}"] > *`)));
      // a row's logo opens from nothing as it comes in: measured once it holds still
      let last = "";
      for (let i = 0; i < 40; i++) {
        const now = JSON.stringify(await Promise.all(["pi", "codex-color", "zcode"].map(size)));
        if (now === last) break;
        last = now;
        await page.waitForTimeout(100);
      }
      for (const icon of ["codex-color", "zcode"]) {
        const s = await size(icon);
        assert.ok(s.box > 0, `${icon} has a box`);
        assert.ok(Math.abs(s.pic / s.box - 0.82) < 0.02, `${icon} is drawn at ${s.pic}/${s.box}`);
        assert.ok(Math.abs(s.dx) < 0.6 && Math.abs(s.dy) < 0.6, `${icon} is centred in its box`);
      }
      const pi = await size("pi");
      assert.equal(pi.pic, pi.box, "a line logo fills its box");
    } finally {
      await browser.close();
    }
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// #2079 (maidangdang1): the tray card's line of a window's cycle was 16px
// tall across the card's width, so a month read as one straight line on its
// dashes, and whether it was above them (lasts) or below (runs out first)
// couldn't be told. The line is taller now, and the gap between what was
// left and the even burn is shaded: green where it is ahead, amber where it
// is behind, nothing on the other side. The pixels are read back from a
// screenshot of the spark itself. The spark's tooltip also says the
// window's Pace. Every language, Chromium and WebKit, at 1100 and 420 wide;
// no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { strings } = require("./strings.cjs");

const assets = path.resolve(__dirname, "../assets");
const { I18N } = strings();
const LANGS = ["en", "zh", "zh-TW", "ja", "de"];
const tr = (lang, s) => (lang === "en" ? s : I18N[lang]?.[s] ?? s);
const H = 3600e3;
const iso = (ms) => new Date(ms).toISOString();

// two weekly windows half through: one 20% used (ahead of an even burn),
// one 70% used (behind it), read every 6 hours along a straight line
function fixtures(now) {
  const start = now - 84 * H, reset = now + 84 * H;
  // two vendors: the tray shows one card per vendor's account in use
  const acct = (provider, user, used) => ({ provider, name: provider === "kimi" ? "Kimi" : "Codex", icon: provider === "kimi" ? "kimi-color" : "openai", user,
    windows: [{ name: "Weekly", used, startsAt: iso(start), resetsAt: iso(reset) }] });
  const line = (used) => {
    const points = [];
    for (let h = 0; h <= 84; h += 6) points.push({ at: iso(start + h * H), left: 100 - (used * h) / 84, start: iso(start), resetsAt: iso(reset) });
    return [{ name: "Weekly", points }];
  };
  return {
    quotas: [acct("codex", "ahead@x.com", 20), acct("kimi", "behind@x.com", 70)],
    history: [{ provider: "codex", user: "ahead@x.com", lines: line(20) }, { provider: "kimi", user: "behind@x.com", lines: line(70) }],
  };
}

function serve(lang, data) {
  const settings = { theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd" };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (body) => route.fulfill({ json: body });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") return json(data.quotas);
    if (url.pathname === "/api/usage/quotas/history") return json(data.history);
    if (url.pathname === "/api/usage") return json({ calls: 0, input: 0, output: 0, cost: 0, agents: [], accounts: [], models: [], series: [], days: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

// the colour at (fx, fy), fractions of the spark's box, from its screenshot
async function pixels(page, png, spots) {
  return page.evaluate(async ({ b64, spots }) => {
    const img = new Image();
    img.src = "data:image/png;base64," + b64;
    await img.decode();
    const c = document.createElement("canvas");
    c.width = img.width; c.height = img.height;
    const x = c.getContext("2d");
    x.drawImage(img, 0, 0);
    return spots.map(([fx, fy]) => [...x.getImageData(Math.round(fx * img.width), Math.round(fy * img.height), 1, 1).data].slice(0, 3));
  }, { b64: png.toString("base64"), spots });
}
const greenish = ([r, g, b]) => g - r > 8 && g - b > 3;
const amberish = ([r, g, b]) => r - b > 15 && r - g > 8;
const plain = ([r, g, b]) => Math.abs(r - g) <= 6 && Math.abs(g - b) <= 6;

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": the tray's spark is tall enough, and shades which side of the even burn it is on", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const shots = [];
    t.after(async () => {
      if (process.env.ARTIFACT_DIR) {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        for (const [name, buf] of shots) await fs.writeFile(path.join(process.env.ARTIFACT_DIR, `${engine}-tray-spark-gap-${name}.png`), buf);
      }
      await browser.close();
    });
    for (const lang of LANGS) {
      for (const width of [1100, 420]) {
        await t.test(`${lang} at ${width}px`, async () => {
          const context = await browser.newContext({ viewport: { width, height: 700 }, reducedMotion: "reduce" });
          const page = await context.newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          const errors = [];
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", serve(lang, fixtures(Date.now())));
          await page.goto("http://uni-switch.test/?mode=panel");
          await page.locator('#ptabs [data-ptab="usage"]').click();
          for (const [user, pace, ahead] of [["ahead@x.com", "−30%", true], ["behind@x.com", "+20%", false]]) {
            const box = page.locator(".pq-card", { hasText: user }).locator(".pq-spark-box");
            await box.locator(".pq-gap").waitFor();
            // the card is drawn again when the reads on opening come back:
            // wait for it to sit still before it is measured and shot
            await page.waitForTimeout(150);
            const r = await box.boundingBox();
            assert.ok(r.height >= 28 && r.height <= 32, `taller than the 16px line: ${r.height}`);
            const card = await page.locator(".pq-card", { hasText: user }).boundingBox();
            assert.ok(r.width > card.width * 0.7, "across the card");
            const title = await box.getAttribute("title");
            assert.ok(title.includes(tr(lang, "Pace {n}").replace("{n}", pace)), `the tooltip says its pace: ${title}`);
            const png = await box.screenshot();
            shots.push([`${lang}-${width}-${user.split("@")[0]}`, png]);
            // 40% into the cycle: the even burn is at 40% down; ahead is
            // 84% left (16% down), behind 44% left (56% down). Between the
            // two lines the gap; on the far side of the dashes, nothing.
            const [gap, other] = await pixels(page, png, ahead ? [[0.4, 0.28], [0.4, 0.75]] : [[0.4, 0.48], [0.4, 0.15]]);
            assert.ok(ahead ? greenish(gap) : amberish(gap), `${user}: the gap is shaded ${ahead ? "green" : "amber"}: ${gap}`);
            assert.ok(plain(other), `${user}: nothing on the other side: ${other}`);
          }
          assert.equal(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth), 0);
          assert.deepEqual(errors, []);
          await context.close();
        });
      }
    }
  });
}

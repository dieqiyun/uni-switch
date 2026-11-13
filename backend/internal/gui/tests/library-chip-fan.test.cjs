// Run with Node's test runner and Playwright on the module path; see README.md.
// A row's agent chips sit stacked like a hand of cards and fan out under
// the pointer. The fanning was the margins between them growing, every
// chip at once, the row laid out again on every frame, and the ring that
// cuts each chip out of the one under it gone in one step (the owner: 一点
// 都不丝滑优雅精致美观，应该像翻起扑克牌一样). The chips are now laid out
// fanned for good and slid onto each other: fanning moves translate alone,
// the cards dealt out one after another, the one with furthest to go
// first, so none passes over another; nothing is laid out again and All
// and the strip stay where they are. Reduced motion fans at once. English,
// Chinese, Traditional Chinese, Japanese and German; the API is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/me";
const agent = (id, name, icon) => ({ id, name, icon, skills: `${HOME}/.${id}/skills`, mcp: `${HOME}/.${id}/mcp.json` });
const AGENTS = Array.from({ length: 6 }, (_, i) => agent(`a${i}`, `Agent ${i}`, i % 2 ? "openai" : "claudecode-color"));
const ALL = AGENTS.map((a) => a.id);

function server(lang, lib) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/library") return json(lib);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname.startsWith("/api/")) return json({});
    if (url.host !== "uni-switch.test") return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const lib = () => ({
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: structuredClone(AGENTS),
  instructions: { agents: [], sets: [] }, foundServers: [], projects: [], foundSkills: [], problems: [], skills: [],
  servers: [{ name: "context7", transport: "stdio", command: "npx", args: ["-y", "@upstash/context7-mcp"], agents: ALL.slice(0, 3) }],
});

// where each card is drawn, where it is laid out, and what moves it
const look = (box) => {
  const s = box.getBoundingClientRect();
  const all = box.querySelector(".lib-ag.all");
  const chips = [...box.querySelectorAll(".lib-ag[data-agent]")];
  return {
    strip: [Math.round(s.left), Math.round(s.width)],
    all: all ? Math.round(all.getBoundingClientRect().left) : null,
    x: chips.map((c) => Math.round(c.getBoundingClientRect().left * 10) / 10),
    laid: chips.map((c) => c.offsetLeft),
    margins: chips.map((c) => getComputedStyle(c).marginLeft),
  };
};
const pitch = (x) => x.slice(1).map((v, i) => Math.round(v - x[i]));

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a row's agent chips fan out like a hand of cards", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) for (const motion of ["no-preference", "reduce"]) {
      await t.test(`${lang} ${motion}`, async () => {
        const ctx = await browser.newContext({ viewport: { width: 1000, height: 700 }, reducedMotion: motion });
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "mcp"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, lib()));
        try {
          await page.goto("http://uni-switch.test/");
          await page.locator('button[data-view="library"]').click();
          const strip = page.locator("#view-library .lib-row > .lib-agents").first();
          await strip.locator(".lib-ag[data-agent]").first().waitFor();
          await page.mouse.move(5, 5);
          await page.waitForTimeout(500);
          const rest = await strip.evaluate(look);
          // stacked: each card 7px under the one before
          assert.deepEqual(pitch(rest.x), [17, 17, 17, 17, 17], JSON.stringify(rest));

          // sample every frame from the moment the pointer comes
          await strip.evaluate((box) => {
            window.__frames = [];
            const chips = [...box.querySelectorAll(".lib-ag[data-agent]")];
            const t0 = performance.now();
            const tick = () => {
              window.__frames.push({ t: performance.now() - t0, x: chips.map((c) => c.getBoundingClientRect().left), laid: chips.map((c) => c.offsetLeft) });
              if (performance.now() - t0 < 700) requestAnimationFrame(tick);
            };
            requestAnimationFrame(tick);
          });
          const b = await strip.boundingBox();
          await page.mouse.move(b.x + b.width - 4, b.y + b.height / 2);
          await page.waitForTimeout(motion === "reduce" ? 60 : 800);
          const fan = await strip.evaluate(look);
          // fanned: 3px apart, the top card where it was, All and the strip unmoved
          assert.deepEqual(pitch(fan.x), [27, 27, 27, 27, 27], JSON.stringify(fan));
          assert.equal(fan.x.at(-1), rest.x.at(-1), JSON.stringify([rest, fan]));
          assert.deepEqual([fan.strip, fan.all], [rest.strip, rest.all], JSON.stringify([rest, fan]));
          // nothing laid out again: the same margins and places throughout
          assert.deepEqual([fan.margins, fan.laid], [rest.margins, rest.laid], JSON.stringify([rest, fan]));
          if (motion === "reduce") return;
          await page.waitForFunction(() => window.__frames.at(-1).t >= 700);
          const frames = await page.evaluate(() => window.__frames);
          for (const f of frames) {
            assert.deepEqual(f.laid, rest.laid, JSON.stringify(f));
            // no card passes over the one above it
            for (let i = 1; i < f.x.length; i++) assert(f.x[i] > f.x[i - 1], JSON.stringify(f));
          }
          // it moves over several frames, and each card only ever leftward
          const between = frames.filter((f) => f.x[0] < rest.x[0] - 1 && f.x[0] > fan.x[0] + 1);
          assert(between.length >= 3, JSON.stringify(frames.map((f) => [Math.round(f.t), Math.round(f.x[0])])));
          for (let j = 1; j < frames.length; j++) for (let i = 0; i < 6; i++) assert(frames[j].x[i] <= frames[j - 1].x[i] + 0.01, JSON.stringify([frames[j - 1], frames[j]]));
          // dealt one after another: the bottom card, with furthest to go, is first off
          const moved = (i) => frames.findIndex((f) => f.x[i] < rest.x[i] - 0.5);
          assert(moved(0) >= 0 && moved(0) <= moved(4), JSON.stringify([moved(0), moved(4)]));
          assert.deepEqual(errors, []);
        } finally {
          await ctx.close();
        }
      });
    }
  });
}

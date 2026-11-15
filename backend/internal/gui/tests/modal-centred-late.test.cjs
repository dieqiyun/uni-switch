// Run with Node's test runner and Playwright on the module path; see README.md.
// A dialog whose content arrives after it opens stays centred: a plugin's
// page opens with a short skeleton, and its README comes later. Until the
// reader presses in it, the dialog keeps the middle as it grows (it sat low
// before, its top held where the skeleton had put it). After a press, it
// holds its place, so what was under the pointer doesn't move away.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const listings = [{ package: "opencode-copilot-auth", name: "GitHub Copilot", icon: "githubcopilot", providers: ["github-copilot"],
  summary: { en: "Your GitHub Copilot plan." }, npm: { version: "0.0.9", weekly: 48213, publisher: "thdxr" } }];
const readme = (n) => ["# opencode-copilot-auth", ...Array.from({ length: n }, (_, i) => "\nParagraph " + i + ".")].join("\n");

function server(reply) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"en",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang: "en", theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    const state = { bun: true, bunVersion: "1.3.0", plugins: [] };
    if (url.pathname === "/api/plugins/market") return json({ listings, state });
    if (url.pathname === "/api/plugins") return json(state);
    if (url.pathname === "/api/plugins/listings") return json({ listings });
    if (url.pathname === "/api/plugins/search") return json({ hits: [{ package: "opencode-copilot-auth", version: "0.0.9" }] });
    if (url.pathname === "/api/plugins/page") return json(await reply());
    if (url.pathname.startsWith("/api/")) return json({});
    if (!/^\/[\w./-]*$/.test(url.pathname) || url.host !== "uni-switch.test") return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

// the dialog's gaps above and below it, inside the modal's padding (which
// differs top and foot in the window)
const gaps = (page) => page.evaluate(() => {
  const el = document.querySelector("#modal"), c = getComputedStyle(el), m = el.getBoundingClientRect();
  const d = document.querySelector("#modal .dialog").getBoundingClientRect();
  return { top: d.top - m.top - parseFloat(c.paddingTop), bottom: m.bottom - parseFloat(c.paddingBottom) - d.bottom, height: d.height };
});

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a dialog whose content comes late stays centred until it is pressed", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const height of [820, 560]) {
      await t.test(height + "px", async () => {
        const page = await (await browser.newContext({ viewport: { width: 980, height } })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        let release;
        const gate = () => new Promise((r) => { release = r; });
        let next = gate();
        await page.route("**/*", server(() => next));

        await page.goto("http://uni-switch.test/?view=plugins");
        await page.locator("#view-plugins .pm-find input").fill("copilot");
        const card = page.locator('.pm-card[data-pkg="opencode-copilot-auth"]');
        // the search's hits and npm's facts draw the card again: pressed
        // before they are in, the press is lost
        await card.locator(".pm-dl", { hasText: "48k/week" }).waitFor();
        await page.waitForLoadState("networkidle");
        await card.locator(".pm-sum").click();
        await page.locator("#modal .pm-detail").waitFor();
        const before = await gaps(page);
        // the README is short enough to fit at 820, and taller than the window at 560
        release({ readme: readme(height > 600 ? 12 : 60), updated: "2026-09-01T00:00:00Z" });
        await page.locator("#modal .pm-md p", { hasText: "Paragraph 11." }).waitFor();
        await page.waitForFunction((h) => document.querySelector("#modal .dialog").getBoundingClientRect().height > h + 40, before.height);
        // measured when the open's morph is over
        await page.waitForFunction(() => document.querySelector("#modal .dialog").getAnimations().length === 0);
        const after = await gaps(page);
        assert(Math.abs(after.top - after.bottom) <= 1, `centred after the README came: ${JSON.stringify(after)}`);

        // pressed in, it holds its top while the content changes under it
        await page.locator("#modal .pm-detail .pm-md h4, #modal .pm-detail .pm-md h1, #modal .pm-detail .pm-md p").first().dispatchEvent("pointerdown");
        await page.evaluate(() => { const p = document.querySelector("#modal .pm-md p:last-of-type"); for (let i = 0; i < 3; i++) p.after(p.cloneNode(true)); });
        await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
        const held = await gaps(page);
        assert(Math.abs(held.top - after.top) <= 1, `held its top after a press: ${JSON.stringify({ after, held })}`);
        assert.deepEqual(errors, []);
        await page.context().close();
      });
    }
  });

  test(engine + ": a dialog grows to its README over frames, centred all the way, not in one jump", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const height of [820, 560]) {
      await t.test(height + "px", async () => {
        const page = await (await browser.newContext({ viewport: { width: 980, height } })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        let release;
        const next = new Promise((r) => { release = r; });
        await page.route("**/*", server(() => next));
        await page.goto("http://uni-switch.test/?view=plugins");
        await page.locator("#view-plugins .pm-find input").fill("copilot");
        const card = page.locator('.pm-card[data-pkg="opencode-copilot-auth"]');
        await card.locator(".pm-dl", { hasText: "48k/week" }).waitFor();
        await page.waitForLoadState("networkidle");
        await card.locator(".pm-sum").click();
        await page.locator("#modal .pm-detail").waitFor();
        // the open's morph over, so each frame's box is the dialog's own
        await page.waitForFunction(() => document.querySelector("#modal .dialog").getAnimations().length === 0);
        const before = await gaps(page);
        // each frame's gaps, from the README's arrival until the dialog settles
        const frames = page.evaluate(() => new Promise((done) => {
          const m = document.querySelector("#modal"), c = getComputedStyle(m), d = m.querySelector(".dialog"), out = [];
          const step = () => {
            if (d.querySelector(".pm-md")) {
              const a = m.getBoundingClientRect(), b = d.getBoundingClientRect();
              out.push({ top: b.top - a.top - parseFloat(c.paddingTop), bottom: a.bottom - parseFloat(c.paddingBottom) - b.bottom, height: b.height });
              if (out.length > 3 && !d.getAnimations().length) return done(out);
            }
            requestAnimationFrame(step);
          };
          step();
        }));
        release({ readme: readme(height > 600 ? 12 : 60), updated: "2026-09-01T00:00:00Z" });
        const seen = await frames;
        const end = seen.at(-1);
        assert(end.height > before.height + 40, `grew: ${JSON.stringify({ before, end })}`);
        // drawn at heights between the two, not the skeleton's then the README's
        const between = seen.filter((f) => f.height > before.height + 4 && f.height < end.height - 4);
        assert(between.length >= 3, `grew over frames: ${JSON.stringify(seen.map((f) => Math.round(f.height)))}`);
        for (const f of seen) assert(Math.abs(f.top - f.bottom) <= 2, `centred in every frame: ${JSON.stringify(f)}`);
        assert.deepEqual(errors, []);
        await page.context().close();
      });
    }
  });
}

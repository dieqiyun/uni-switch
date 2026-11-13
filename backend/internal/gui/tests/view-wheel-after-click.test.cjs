// Run with Node's test runner and Playwright on the module path; see README.md.
// issennknight on Discord (macOS, trackpad): every page scrolled again, but a
// saved profile opened on the Agents page and then scrolled through often
// stayed where it was. macOS 13's WebKit scrolls a view under a wheel it never
// tells the page of, and the view guard takes a run of such scrolls as the
// reader's only with no click holding: a click holds what it clicked for up
// to 4s, and every scroll in that time was put back. Here the wheel goes
// over the profile just opened, right after the click, heard and unheard:
// the page must follow it down, to its foot and back up, with details longer
// than the window and shorter, and nothing but the view scrolls.
// Chromium and WebKit, a narrow and a wide window, every language.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = process.env.UNI_SWITCH_AFTER_CLICK_ASSETS || path.resolve(__dirname, "../assets");
const LANGS = ["en", "zh", "zh-TW", "ja", "de"];
const AGENTS = Array.from({ length: 10 }, (_, i) => ({
  id: "a" + i, name: "Agent " + i, path: "/t/a" + i + ".json", icon: "generic", wired: true,
  fields: [{ key: "model", label: "model", value: "m", options: [{ value: "m", label: "M", ref: "a/m" }] }],
}));
const group = (i) => ({ id: "g" + i, name: "Agent " + i, fields: [{ key: "model", label: "model", value: "uni-switch/x/m" + i }, { key: "effort", label: "effort", value: "high" }] });
// "long" holds more than a window shows, "short" a few lines
const PROFILES = [
  ...Array.from({ length: 4 }, (_, i) => ({ name: "p" + i, summary: "", agents: [] })),
  { name: "long", summary: "", agents: Array.from({ length: 16 }, (_, i) => group(i)) },
  { name: "short", summary: "", agents: [group(0)] },
];

function server(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: AGENTS, profiles: PROFILES, settings: { lang, theme: "light", guideHidden: true } });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/window/fit") return route.fulfill({ status: 204 });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// the view at rest: its scrollTop the same across a few looks
async function rested(p, view, ms = 3000) {
  let last = NaN, same = 0;
  const until = Date.now() + ms;
  while (Date.now() < until) {
    const t = await view.evaluate((v) => v.scrollTop);
    same = t === last ? same + 1 : 0;
    if (same >= 3) return t;
    last = t;
    await p.waitForTimeout(50);
  }
  return last;
}

const wheels = async (p, n, dy) => { for (let i = 0; i < n; i++) { await p.mouse.wheel(0, dy); await p.waitForTimeout(16); } };
// what scrolled besides the view: the document, and any part of the page
const others = (p) => p.evaluate(() => [document.scrollingElement, ...document.querySelectorAll("#view-agents *")]
  .filter((n) => n.scrollTop > 0).map((n) => n.id || n.className));
// the chip's name: ↻ and × are at its other end
const clickChip = async (p, name) => {
  const b = await p.locator("#profiles .chip", { hasText: new RegExp("^" + name) }).boundingBox();
  await p.mouse.click(b.x + 12, b.y + b.height / 2);
};

const DEAF = `addEventListener("wheel", (e) => e.stopImmediatePropagation(), { capture: true, passive: true });`;
const RUNS = [];
for (const lang of LANGS) for (const width of [420, 1000]) RUNS.push({ lang, width, deaf: true });
RUNS.push({ lang: "en", width: 420, deaf: false }, { lang: "zh", width: 1000, deaf: false });

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a wheel right after a click scrolls the view", { timeout: 600000 }, async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit : chromium).launch();
    t.after(() => browser.close());
    for (const { lang, width, deaf } of RUNS) {
      await t.test(`${lang}, ${width}px, ${deaf ? "a wheel the page never hears" : "a wheel heard"}`, async () => {
        const ctx = await browser.newContext({ viewport: { width, height: 600 }, reducedMotion: "reduce" });
        const p = await ctx.newPage();
        p.setDefaultTimeout(8000);
        p.setDefaultNavigationTimeout(60000);
        const errors = [];
        p.on("pageerror", (e) => { if (!/^ResizeObserver loop/.test(e.message)) errors.push(e.message); });
        if (deaf) await p.addInitScript(DEAF);
        await p.route("**/*", server(lang));
        const view = p.locator("#view-agents");
        await p.goto("http://uni-switch.test/?view=agents");
        await p.locator("#view-agents .ag-lead").first().waitFor();
        await p.locator("#profiles .chip").first().waitFor();
        await rested(p, view);
        const r = await view.boundingBox();
        const max = () => view.evaluate((v) => v.scrollHeight - v.clientHeight);

        // down to the profiles at the page's foot, as the reader would
        await p.mouse.move(r.x + r.width / 2, r.y + r.height / 2);
        await wheels(p, 40, 40);
        const foot = await rested(p, view);
        assert(foot >= (await max()) - 1 && foot > 100, `the wheel takes the page to its foot: ${foot} of ${await max()}`);

        // the long profile opened, and wheeled through at once: the page
        // follows, the pointer over the chip and then the details
        await clickChip(p, "long");
        await p.locator(".prof-detail").waitFor();
        const tall = await max();
        assert(tall > foot + 300, `the long profile's details run past the window: ${foot} → ${tall}`);
        await wheels(p, 10, 30);
        const down = await rested(p, view);
        assert(down >= foot + 250, `ten wheels of 30px right after the click scroll the page: from ${foot} to ${down}`);
        // and on to its foot, where the wheel has nothing further to take
        await wheels(p, 40, 40);
        const end = await rested(p, view);
        assert(end >= tall - 1, `the wheel reaches the details' end: ${end} of ${tall}`);
        await wheels(p, 5, 40);
        assert.equal(await rested(p, view), end, "past its end the page stays at its foot");
        await wheels(p, 10, -30);
        const up = await rested(p, view);
        assert(up <= end - 250, `and back up: from ${end} to ${up}`);
        assert.deepEqual(await others(p), [], "nothing but the view scrolls");

        // the short one, opened at the foot once the long one is closed: the
        // details fit in the window, and the page still goes up and down
        // under the wheel
        await p.keyboard.press("Escape");
        await p.locator(".prof-detail").waitFor({ state: "detached" });
        await p.waitForTimeout(1100);
        await wheels(p, 20, 40);
        await rested(p, view);
        const b = await p.locator("#profiles .chip", { hasText: /^short/ }).boundingBox();
        assert(b && b.y > r.y && b.y + b.height < r.y + r.height, "the short profile's chip is in sight");
        await clickChip(p, "short");
        await p.locator(".prof-detail").waitFor();
        const at = await rested(p, view);
        const fits = await p.locator(".prof-detail").evaluate((d) => d.offsetHeight);
        assert(fits < r.height / 2, `the short profile's details fit in the window: ${fits}`);
        await wheels(p, 10, -30);
        const higher = await rested(p, view);
        assert(at > 100 && higher <= Math.max(0, at - 250), `ten wheels up right after the click scroll the page: from ${at} to ${higher}`);
        await wheels(p, 10, 30);
        assert(await rested(p, view) >= Math.min(higher + 250, (await max()) - 1), "and down again");
        assert.deepEqual(await others(p), [], "nothing but the view scrolls");
        assert.deepEqual(errors, []);
        await ctx.close();
      });
    }
  });
}

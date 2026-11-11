// Run with Node's test runner and Playwright on the module path; see README.md.
// #2396 (gif156359098): with the tabs across the top (the sidebar off), the
// Settings tabs and the find-a-setting box scrolled away with a long part
// (About), so another part was a scroll back up away. They stay at the top
// of the page now while the part scrolls under them, on a strip the rows
// don't show through, and a tab there still opens its part. At the page's
// top nothing moved. The Library's and the Plugins' tabs already stayed
// (.lib-head); the Library's is checked the same way. With the sidebar the
// tabs are in the title bar as before. Chromium and WebKit, a wide and a
// narrow window, every language; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/aimer";
const agent = (id, name) => ({ id, name, icon: "", skills: `${HOME}/.${id}/skills`, mcp: `${HOME}/.${id}/mcp.json` });
const lib = {
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: [agent("codex", "Codex"), agent("pi", "Pi")],
  instructions: { agents: [], sets: [] }, foundServers: [], projects: [], foundSkills: [], skills: [],
  servers: Array.from({ length: 30 }, (_, i) => ({ name: `server-${i + 1}`, transport: "stdio", command: "npx", args: ["fs-mcp"], agents: ["codex"] })),
};
const settings = (lang) => ({
  theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd", textSize: 100,
  dock: false, dockWindow: false, proxy: "", redact: false, redactPersonal: false, redactWords: [],
  codexWarmup: "", claudeWarmup: "", codexWarmAt: "", claudeWarmAt: "", workbuddyCheckin: false, noStats: false,
  trayUsage: "", trayUsageEvery: 3, vision: "", imageGen: "",
  version: "0.1.400", dir: "~/.config/uni-switch", gateway: "http://127.0.0.1:3425",
  proxyNow: "none", proxySource: "none", login: false,
  visionModels: [], imageGenModels: [], workbuddyCheckins: [], lanURLs: [],
  fx: { rate: 7.2, at: new Date().toISOString(), stale: false },
});

function server(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/settings") return json(settings(lang));
    if (url.pathname === "/api/library") return json(lib);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/window/fit") return route.fulfill({ status: 204 });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const LANGS = ["en", "zh", "zh-TW", "ja", "de"];
const scrollTop = (page, v) => page.locator(v).evaluate((e) => e.scrollTop);
// down to the view's foot by the wheel, as the reader goes there
async function toFoot(page, v) {
  const box = await page.locator(v).boundingBox();
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  for (let i = 0; i < 60; i++) {
    await page.mouse.wheel(0, 300);
    await page.waitForTimeout(20);
    if (await page.locator(v).evaluate((e) => e.scrollTop + e.clientHeight >= e.scrollHeight - 1)) break;
  }
  await page.waitForTimeout(300);
  const at = await scrollTop(page, v);
  assert.ok(at > 100, `${v} scrolled down: ${at}`);
  return at;
}
// where the head is against its view's visible top, and whether each of its
// controls is the thing under its own middle (nothing drawn over it, not
// scrolled off)
const headAt = (page, v, head) => page.evaluate(([v, head]) => {
  const view = document.querySelector(v), h = view.querySelector(head);
  const vr = view.getBoundingClientRect(), hr = h.getBoundingClientRect();
  const hit = [...h.querySelectorAll("button:not([hidden]), input")].filter((c) => c.getClientRects().length).map((c) => {
    const r = c.getBoundingClientRect(), e = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
    return !!e && (e === c || c.contains(e));
  });
  return { gap: hr.top - (vr.top + view.clientTop), top: hr.top, controls: hit.length, under: hit.every(Boolean) };
}, [v, head]);
// the strip's paint: a row scrolled beneath must not show through it
// and where it reaches: from the view's visible top to past the head's margin
const strip = (page, sel) => page.locator(sel).evaluate((h) => {
  const b = getComputedStyle(h, "::before"), hs = getComputedStyle(h), v = h.closest(".view");
  const hr = h.getBoundingClientRect(), vtop = v.getBoundingClientRect().top + v.clientTop;
  return {
    content: b.content, bg: b.backgroundColor, position: hs.position,
    over: b.content !== "none" && hr.top + parseFloat(b.top) <= vtop + 0.5 && hr.bottom - parseFloat(b.bottom) >= hr.bottom + parseFloat(hs.marginBottom) - 0.5,
  };
});
const opaque = (bg) => {
  const m = bg.match(/[\d.]+/g)?.map(Number) || [];
  if (bg.startsWith("color(")) return (m.length > 4 ? m[m.length - 1] : 1) >= 0.9;
  return m.length === 3 || (m.length === 4 && m[3] >= 0.9);
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: Settings' tabs stay at the top of a long part (#2396)`, async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());

    const open = async (lang, width, layout) => {
      const context = await browser.newContext({ viewport: { width, height: 400 }, reducedMotion: "reduce" });
      await context.addInitScript((layout) => {
        try { localStorage.setItem("uni-switch.settingsTab", "about"); localStorage.setItem("uni-switch.libTab", "mcp"); localStorage.setItem("uni-switch.navLayout", layout); } catch {}
      }, layout);
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      page.errors = [];
      page.on("pageerror", (e) => page.errors.push(e.message));
      await page.route("**/*", server(lang));
      await page.goto("http://uni-switch.test/");
      await page.waitForSelector("#nav button.on");
      return page;
    };

    for (const width of [1100, 420]) for (const lang of LANGS) await t.test(`tabs across the top, ${width}px, ${lang}`, async () => {
      const page = await open(lang, width, "top");
      assert.ok(!(await page.evaluate(() => document.body.classList.contains("side-nav"))), "no sidebar");
      const set = "#view-settings";
      await page.locator("#prefs").click();
      await page.locator("#about .row").first().waitFor();
      assert.equal(await page.locator("#setTab-about").getAttribute("aria-selected"), "true");

      // at the page's top the head is where it always was: under the view's
      // padding and its own margin, the part under it past its margin
      const rest = await page.evaluate(() => {
        const v = document.querySelector("#view-settings"), h = v.querySelector(".set-head"), p = document.querySelector("#setPage-about");
        const cs = getComputedStyle(v), hs = getComputedStyle(h);
        return {
          gap: h.getBoundingClientRect().top - v.getBoundingClientRect().top - v.clientTop - parseFloat(cs.paddingTop) - parseFloat(hs.marginTop),
          below: p.getBoundingClientRect().top - h.getBoundingClientRect().bottom - parseFloat(hs.marginBottom),
          stuck: h.classList.contains("stuck"),
        };
      });
      assert.ok(Math.abs(rest.gap) <= 0.5, `at the top the head is in its own place: ${rest.gap}`);
      assert.ok(Math.abs(rest.below) <= 0.5, `and the part right under it: ${rest.below}`);
      assert.equal(rest.stuck, false, "no hairline at the top");

      // About at its foot: the tabs and the search are at the view's top,
      // each one under nothing, on a strip the rows don't show through
      await toFoot(page, set);
      const at = await headAt(page, set, ".set-head");
      assert.ok(at.gap >= -0.5 && at.gap <= 1, `the tabs at the top of the view: ${at.gap}`);
      assert.ok(at.controls >= 9, `every tab and the search box seen: ${at.controls}`);
      assert.ok(at.under, "nothing over the tabs or the search box");
      const s = await strip(page, `${set} .set-head`);
      assert.equal(s.position, "sticky");
      assert.notEqual(s.content, "none", "a strip under the tabs");
      assert.ok(opaque(s.bg), `the strip hides what scrolls under it: ${s.bg}`);
      assert.ok(s.over, "the strip covers from the view's top to the part under the head");
      assert.ok(await page.locator(`${set} .set-head`).evaluate((h) => h.classList.contains("stuck")), "a hairline once something is beneath");
      assert.ok(await page.locator("#setQ").isVisible(), "the search box is there");

      // a tab there opens its part
      await page.locator("#setTab-general").click();
      await page.waitForFunction(() => document.querySelector("#setTab-general").getAttribute("aria-selected") === "true");
      assert.ok(await page.locator("#setPage-general").isVisible(), "General shown");
      assert.equal(await page.locator("#setPage-about").isVisible(), false, "About hidden");
      const after = await headAt(page, set, ".set-head");
      assert.ok(after.gap >= -0.5 && after.gap <= 1, `the tabs still at the top: ${after.gap}`);

      // the Library's tabs, the same way (they stayed already: .lib-head)
      const libv = "#view-library";
      await page.locator('#nav button[data-view="library"]').click();
      await page.locator(`${libv} .lib-row, ${libv} .row`).first().waitFor();
      await toFoot(page, libv);
      const la = await headAt(page, libv, ".lib-head");
      assert.ok(la.gap >= -2.5 && la.gap <= 1, `the Library's tabs at the top of the view: ${la.gap}`);
      assert.ok(la.under, "nothing over the Library's tabs");
      await page.locator(`${libv} .lib-tabs > button`).nth(0).click();
      await page.waitForFunction(() => document.querySelectorAll("#view-library .lib-tabs > button")[0]?.classList.contains("on"));
      // and the Plugins' head is the same strip
      await page.locator('#nav button[data-view="plugins"]').click();
      await page.locator("#view-plugins .pm-head").waitFor();
      assert.equal((await strip(page, "#view-plugins .pm-head")).position, "sticky", "the Plugins' tabs stay too");

      assert.deepEqual(page.errors, []);
      await page.context().close();
    });

    // with the sidebar the head is in the title bar, as before: fixed there,
    // no strip of its own, at the window's top at the foot of About too
    for (const lang of LANGS) await t.test(`sidebar, ${lang}`, async () => {
      const page = await open(lang, 1100, "side");
      assert.ok(await page.evaluate(() => document.body.classList.contains("side-nav")), "the sidebar is shown");
      await page.locator("#prefs").click();
      await page.locator("#about .row").first().waitFor();
      const s = await strip(page, "#view-settings .set-head");
      assert.equal(s.position, "fixed");
      assert.equal(s.content, "none", "no strip of its own in the title bar");
      const y0 = await page.locator("#view-settings .set-head").evaluate((h) => h.getBoundingClientRect().top);
      assert.equal(y0, 0, "in the title bar");
      await toFoot(page, "#view-settings");
      assert.equal(await page.locator("#view-settings .set-head").evaluate((h) => h.getBoundingClientRect().top), 0, "still in the title bar");
      assert.ok(await page.locator("#setQ").isVisible(), "the search box in the title bar");
      assert.deepEqual(page.errors, []);
      await page.context().close();
    });
  });
}

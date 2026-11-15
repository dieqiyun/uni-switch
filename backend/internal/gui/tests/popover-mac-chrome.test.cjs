// Run with Node's test runner and Playwright on the module path; see README.md.
// lyston11 (#1573): in a short Mac window, a popover opened upward from near
// the window's foot was held only to 8px from the top, so it covered the
// traffic lights and the header they are drawn over. Every placer that opens
// upward stops under the window's chrome now: an agent's model list (Pick),
// the field picker (#pop) and the app menu (.proto-menu). On a Mac window
// each one's top edge sits under header.top's bottom; on Windows they are
// where they were, 8px from the top. Each is opened from an anchor at the
// foot of a short, narrow window; no click moves the page. English and
// Chinese, Chromium and WebKit; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const models = () => [
  ...["gpt-5.5", "gpt-5.5-mini", "gpt-5.4", "gpt-5.3-codex", "o4", "o4-mini"].map((m, i) =>
    ({ id: "openai/" + m, name: m.toUpperCase(), group: "OpenAI", icon: "openai", context: 400e3, inUse: i === 0 })),
  ...Array.from({ length: 30 }, (_, i) => ({ id: "openrouter/m" + i, name: "Router Model " + i, group: "OpenRouter", icon: "openrouter", context: 128e3 })),
];

function server(lang) {
  const list = models();
  const options = list.map((m) => ({ value: "uni-switch/" + m.id, label: m.name, ref: m.id }));
  const state = () => ({
    agents: [{
      id: "codex", name: "Codex", path: "/test/config.toml", icon: "codex-color", wired: true,
      fields: [{ key: "model", label: "model", value: options[0].value, options }],
      models: { shown: list.length, listed: list.length, by: [{ name: "OpenAI", icon: "openai", n: 6 }, { name: "OpenRouter", icon: "openrouter", n: 30 }] },
    }],
    profiles: [], settings: { lang, theme: "light" },
  });
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state());
    if (url.pathname === "/api/agent-models/codex") return json({ models: list });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const settled = (page) => page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const platform of ["MacIntel", "Win32"]) for (const lang of ["en", "zh"]) {
    test(`${engine} ${platform} ${lang}: a popover opened upward stays under the window's chrome`, async (t) => {
      const mac = platform === "MacIntel";
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const ctx = await browser.newContext({ viewport: { width: 480, height: 560 } });
      await ctx.addInitScript((p) => { Object.defineProperty(Navigator.prototype, "platform", { get: () => p }); }, platform);
      const page = await ctx.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(lang));
      await page.goto("http://uni-switch.test/?mode=window");
      assert.equal(await page.evaluate(() => document.body.classList.contains("mac")), mac);
      const head = await page.evaluate(() => document.querySelector("header.top").getBoundingClientRect().bottom);
      if (mac) assert(head >= 49, `a Mac window's header reaches under the traffic lights: ${head}`);
      // the highest a popover may start: under the chrome on a Mac, 8px elsewhere
      const floor = mac ? head + 8 : 8;

      const row = page.locator('.row.agent[data-id="codex"]');
      await row.locator(".ag-link").click();
      const entry = row.locator(".ag-exp .ag-chips .ag-quiet");
      await entry.waitFor();
      await page.waitForFunction(() => document.getAnimations().length === 0);
      // the window made short enough that the anchor is at its foot, with
      // more room over it than under it; clicked where it is, as a person
      // does (Playwright's own click scrolls it into view first, and a
      // scroll closes a popover)
      const atFoot = async (loc, room) => {
        const b = await loc.boundingBox();
        await page.setViewportSize({ width: 480, height: Math.round(b.y + b.height + 24) });
        await settled(page);
        const r = await loc.boundingBox();
        assert(r.y > room, `the anchor has room over it: ${r.y}`);
        return () => page.mouse.click(r.x + Math.min(r.width / 2, 30), r.y + r.height / 2);
      };
      const tops = () => page.evaluate(() => [scrollY, $("#view-agents").scrollTop]);

      // 1. the agent's model list (Pick), the reporter's case
      let click = await atFoot(entry, 200);
      let was = await tops();
      await click();
      const am = page.locator(".am-pop:not(.leaving)");
      await am.waitFor();
      await page.waitForFunction(() => document.querySelector(".am-pop:not(.leaving)")?.getAnimations().length === 0);
      assert(await am.evaluate((e) => e.classList.contains("up")), "it opens upward");
      let b = await am.boundingBox();
      assert(b.y >= floor - 0.5, `the model list's top ${b.y} is under ${floor}`);
      assert(b.y < floor + 40, `the model list still uses the room it has: ${b.y}`);
      assert.deepEqual(await tops(), was, "opening it moves nothing");
      await page.keyboard.press("Escape");
      await page.waitForFunction(() => !document.querySelector(".am-pop:not(.leaving)"));

      // 2. the field picker (#pop) from the model field at the row's foot:
      // too tall to fit under or over it, it is held to the window's top
      await page.setViewportSize({ width: 480, height: 560 });
      const field = row.locator('.field[data-key="model"]').first();
      click = await atFoot(field, 120);
      was = await tops();
      await click();
      const pop = page.locator("#pop:not([hidden])");
      await pop.locator("#list li").first().waitFor();
      await page.waitForFunction(() => document.querySelector("#pop").getAnimations().length === 0);
      b = await pop.boundingBox();
      assert(b.y >= floor - 0.5, `the picker's top ${b.y} is under ${floor}`);
      assert(b.y + b.height <= (await page.evaluate(() => innerHeight)) + 0.5, "the picker is on the screen whole");
      assert.deepEqual(await tops(), was, "opening it moves nothing");
      await page.keyboard.press("Escape");
      await page.waitForFunction(() => document.querySelector("#pop").hidden);

      // 3. an app menu (.proto-menu) taller than the room under its anchor
      was = await tops();
      await page.evaluate(() => {
        const a = document.createElement("button");
        a.id = "pm-anchor";
        a.textContent = "menu";
        a.style.cssText = `position:fixed;left:20px;top:${innerHeight - 40}px;height:24px`;
        document.body.append(a);
        openProtoMenu(a, Array.from({ length: 40 }, (_, i) => ({ v: "v" + i, name: "Choice " + i, note: "", literalName: true })), "v0", () => {});
      });
      const menu = page.locator(".proto-menu");
      await menu.waitFor();
      await page.waitForFunction(() => document.querySelector(".proto-menu").getAnimations().length === 0);
      b = await menu.boundingBox();
      assert(await menu.evaluate((e) => e.classList.contains("up")), "the menu opens upward");
      assert(b.y >= floor - 0.5, `the menu's top ${b.y} is under ${floor}`);
      assert(b.y + b.height <= (await page.evaluate(() => innerHeight)) - 30, "the menu ends over its anchor");
      assert.deepEqual(await tops(), was, "opening it moves nothing");

      if (process.env.ARTIFACT_DIR) {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${platform}-${lang}-popover.png`) });
      }
      assert.deepEqual(errors, []);
    });
  }
}

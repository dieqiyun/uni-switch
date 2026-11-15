// Run with Node's test runner and Playwright on the module path; see README.md.
// #499: a provider added later couldn't be put first on the Providers tab.
// Now a provider that is on has its logo for a handle, as an agent row has:
// the grip in the row's margin is drawn only with the pointer on the row,
// dragging it moves the row and saves the order (the one providers are
// tried in too), Alt+↑/↓ moves it from the keyboard and keeps the keyboard
// on it, and a click opens the row as before, without moving the page. One
// switched off has no handle and keeps its place. Chromium and WebKit,
// English and Chinese; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const prov = (id, extra = {}) => ({
  id, name: id.toUpperCase(), icon: "generic", host: id + ".example.com", chat: "https://" + id + ".example.com/v1", responses: "", anthropic: "", catalog: "",
  models: [{ id: "m", name: "M", on: true }], agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-…" + id }, keyList: [], balanceToken: { takes: false, set: false }, proxy: "", ...extra,
});

function fixture(lang) {
  const providers = { providers: [prov("a"), prov("b"), prov("c"), prov("d", { off: true })], presets: [], excluded: [], gateway: { running: true, window: true } };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  const posted = [];
  const route = async (route) => {
    const request = route.request(), url = new URL(request.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers/arrange") {
      const { order } = request.postDataJSON();
      posted.push(order);
      providers.providers = order.map((id) => providers.providers.find((p) => p.id === id));
      return json(providers);
    }
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
  return { posted, route };
}

const order = (page) => page.locator("#providers > .row.provider").evaluateAll((rs) => rs.map((r) => r.dataset.id));
const grips = (page) => page.locator("#providers > .row.provider").evaluateAll((rs) => rs.map((r) => {
  const h = r.querySelector(".pv-handle");
  if (!h) return { id: r.dataset.id };
  const s = getComputedStyle(h, "::before"), hb = h.getBoundingClientRect(), rb = r.getBoundingClientRect();
  const left = hb.left + parseFloat(s.left), w = parseFloat(s.width);
  return { id: r.dataset.id, handle: true, opacity: Number(s.opacity), image: s.backgroundImage, inRow: left >= rb.left && left + w <= hb.left + 0.5, x: hb.left + hb.width / 2, y: hb.top + hb.height / 2 };
}));
const scrolled = (page) => page.evaluate(() => [scrollY, document.querySelector("#view-providers").scrollTop]);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a provider can be moved up the list", { timeout: 120000 }, async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      await browser.close();
      assert.deepEqual(errors, []);
    });
    for (const lang of ["en", "zh"]) {
      await t.test(lang, async () => {
        const ctx = await browser.newContext({ viewport: { width: 1000, height: 640 }, reducedMotion: "reduce" });
        const page = await ctx.newPage(), f = fixture(lang);
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", f.route);
        await page.goto("http://uni-switch.test/?view=providers");
        await page.locator("#providers > .row.provider").nth(2).waitFor();
        await page.mouse.move(1, 1);
        assert.deepEqual(await order(page), ["a", "b", "c"]);

        // the ones on have a handle with a grip in their margin, drawn only
        // with the pointer on the row; the one off has none
        await page.waitForFunction(() => [...document.querySelectorAll("#providers .pv-handle")].every((h) => getComputedStyle(h, "::before").opacity === "0"));
        let g = await grips(page);
        for (const x of g) {
          assert(x.handle, `${x.id} has no handle`);
          assert.match(x.image, /gradient/, `${x.id}'s grip draws nothing`);
          assert(x.inRow, `${x.id}'s grip is off its row or over its logo`);
        }
        assert.equal(await page.locator("#offProviders .pv-handle").count(), 0, "a provider switched off has a handle");
        const tip = await page.evaluate(() => t("Drag to reorder — a model several providers serve goes to the higher one first · Alt+↑/↓ to move") + " · " + t("right-click to move it to the top or the bottom"));
        if (lang === "zh") assert.match(tip, /拖动/);
        assert.equal(await page.locator("#providers .pv-handle").first().getAttribute("title"), tip);
        await page.locator("#providers > .row.provider").nth(1).locator(".name").hover();
        await page.waitForFunction(() => {
          const o = [...document.querySelectorAll("#providers .pv-handle")].map((h) => getComputedStyle(h, "::before").opacity);
          return o[1] === "1" && o[0] === "0" && o[2] === "0";
        });

        // dragging c's handle to the top puts it first, saves the order with
        // the one off kept, and opens nothing
        const y0 = await scrolled(page);
        await page.mouse.move(g[2].x, g[2].y);
        await page.mouse.down();
        await page.mouse.move(g[2].x, g[0].y - 14, { steps: 12 });
        await page.locator("#providers > .row.dragging").waitFor();
        await page.mouse.up();
        await page.waitForFunction(() => document.querySelector("#providers > .row.provider").dataset.id === "c");
        await page.locator("#status").filter({ hasText: await page.evaluate(() => t("Provider order saved")) }).waitFor();
        assert.deepEqual(f.posted.at(-1), ["c", "a", "b", "d"]);
        assert.deepEqual(await order(page), ["c", "a", "b"]);
        assert.equal(await page.locator("#providers > .row.provider.selected").count(), 0, "the drag opened the row");
        assert.deepEqual(await scrolled(page), y0);

        // from the keyboard: Alt+↓ moves it down one, and it keeps the keyboard
        await page.locator('#providers > .row.provider[data-id="c"] .pv-handle').focus();
        await page.keyboard.press("Alt+ArrowDown");
        await page.waitForFunction(() => [...document.querySelectorAll("#providers > .row.provider")].map((r) => r.dataset.id).join() === "a,c,b");
        await page.waitForFunction(() => document.activeElement?.closest(".row.provider")?.dataset.id === "c");
        await new Promise((r) => setTimeout(r, 200));
        assert.deepEqual(f.posted.at(-1), ["a", "c", "b", "d"]);
        assert.equal(await page.evaluate(() => document.activeElement?.closest(".row.provider")?.dataset.id), "c");
        // the first can't go up
        const n = f.posted.length;
        await page.locator('#providers > .row.provider[data-id="a"] .pv-handle').focus();
        await page.keyboard.press("Alt+ArrowUp");
        await new Promise((r) => setTimeout(r, 200));
        assert.equal(f.posted.length, n);

        // a click on the handle opens the row, as a click on the row does,
        // and the page stays where it is
        const y1 = await scrolled(page);
        await page.locator('#providers > .row.provider[data-id="b"] .pv-handle').click();
        await page.locator('#providers > .row.provider.selected[data-id="b"]').waitFor();
        assert.deepEqual(await scrolled(page), y1);
        await ctx.close();
      });
    }
  });
}

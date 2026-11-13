// Run with Node's test runner and Playwright on the module path; see README.md.
// #1519 (fatkun): the Providers page showed skeletons for as long as
// /api/providers took, seconds after a start. Like the Agents page, it now
// draws the list kept from the last read (/api/providers?last=1) at once,
// and the fresh one replaces it when it is in; a row opens only on the
// fresh list, since the kept one leaves out headers and proxies. Started on
// another page, the app reads the list in the background once the state is
// in, and the Providers page opens on it without a skeleton. With nothing
// kept, the page shows its skeletons until the read, as before.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const SLOW = 1500; // each fresh read of the list
const LANGS = ["en", "zh", "zh-TW", "ja", "de"];

const provider = (name, extra) => ({
  id: "relay", name, icon: "generic", chat: "https://relay.example.com/v1", responses: "", anthropic: "", catalog: "",
  models: [{ id: "model-a", name: "Model A", on: true }], agents: [], fallback: [], keyList: [],
  key: { set: true, masked: "sk-…one" }, balanceToken: { takes: false, set: false }, ...extra,
});
const list = (p) => ({ providers: [p], presets: [], excluded: [], gateway: { running: true, window: true } });
// without the server's "last": true, as most pages' fixtures answer
// ?last=1 with the full list: the page knows it kept by where it came from
const keptList = list(provider("Kept Relay"));
const freshList = list(provider("Fresh Relay", { headers: { "X-Team": "t1" }, proxy: "" }));

function serve(lang, kept, reads) {
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(url.searchParams.get("last") === "1" ? null : state);
    if (url.pathname === "/api/providers") {
      if (url.searchParams.get("last") === "1") return json(kept ? keptList : null);
      reads.asked++;
      await new Promise((r) => setTimeout(r, SLOW));
      reads.done++;
      return json(freshList);
    }
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

async function open(engine, t, lang, kept, view, width = 900) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  t.after(() => browser.close());
  const page = await (await browser.newContext({ viewport: { width, height: 700 }, reducedMotion: "reduce" })).newPage();
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const reads = { asked: 0, done: 0 };
  await page.route("**/*", serve(lang, kept, reads));
  await page.goto("http://uni-switch.test/?view=" + view);
  return { page, errors, reads };
}

const row = (page) => page.locator("#providers .row.provider:not(.pv-sk-row)");
const skeletons = (page) => page.locator("#providers .pv-sk-row");
const name = (page) => row(page).first().locator(".name").evaluate((e) => e.firstChild.textContent);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of LANGS) {
    test(`${engine} ${lang}: the Providers page draws the kept list at once, then the fresh one`, async (t) => {
      const { page, errors, reads } = await open(engine, t, lang, true, "providers", 420);
      await row(page).first().waitFor();
      assert.equal(reads.done, 0, "the rows came only with the fresh read");
      assert.equal(await name(page), "Kept Relay");
      assert.equal(await page.evaluate(() => document.querySelector("#view-providers").classList.contains("loading")), false);
      await page.waitForFunction(() => document.querySelector("#providers .row.provider .name")?.firstChild.textContent === "Fresh Relay", null, { timeout: SLOW + 4000 });
      assert.equal(reads.asked, 1, "one fresh read");
      assert.equal(await row(page).count(), 1);
      assert.equal(await page.evaluate(() => document.scrollingElement.scrollWidth <= innerWidth), true, "no sideways scroll at narrow width");
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: a row clicked on the kept list opens on the fresh one`, async (t) => {
      const { page, errors, reads } = await open(engine, t, lang, true, "providers");
      await row(page).first().waitFor();
      const top = await page.evaluate(() => document.querySelector("#view-providers").scrollTop);
      await row(page).first().click();
      await page.locator(".editor").waitFor({ timeout: SLOW + 4000 });
      assert(reads.done >= 1, "the editor opened before the fresh list was in");
      assert.equal(await name(page), "Fresh Relay");
      assert.equal(await page.evaluate(() => document.querySelector("#view-providers").scrollTop), top, "the click moved the page");
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: started on Agents, the Providers page is read behind it`, async (t) => {
      const { page, errors, reads } = await open(engine, t, lang, false, "agents");
      await page.waitForFunction(() => document.querySelector("#view-agents") && !document.querySelector("#view-agents").classList.contains("loading"));
      for (let i = 0; i < 100 && reads.done < 1; i++) await page.waitForTimeout(50);
      assert.equal(reads.done, 1, "the list was not read in the background");
      await page.locator('#nav button[data-view="providers"]').click();
      await row(page).first().waitFor({ timeout: 1000 });
      assert.equal(await skeletons(page).count(), 0, "skeletons drawn over a list read in the background");
      assert.equal(await name(page), "Fresh Relay");
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: with nothing kept, skeletons until the read`, async (t) => {
      const { page, errors, reads } = await open(engine, t, lang, false, "providers");
      await skeletons(page).first().waitFor();
      assert.equal(await row(page).count(), 0);
      await row(page).first().waitFor({ timeout: SLOW + 4000 });
      assert.equal(reads.asked, 1);
      assert.equal(await name(page), "Fresh Relay");
      assert.deepEqual(errors, []);
    });
  }
}

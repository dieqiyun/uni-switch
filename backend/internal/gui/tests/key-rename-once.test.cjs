// Run with Node's test runner and Playwright on the module path; see README.md.
// A key's rename in the provider editor ends once (#3944). Escape keeps the
// name, typed over or not; Enter saves it with one keys/rename, and Enter
// again before the save answers sends nothing more. Chromium blurs the
// focused input as the editor's redraw takes it out, and the blur ended the
// rename a second time: after Escape it saved the typed name, after Enter it
// posted it again, and with the name unchanged the second redraw threw
// inside the first. Chromium and WebKit, English and Chinese. The page's own
// requests are counted as it makes them; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function fixture(lang) {
  const keyList = [
    { id: "key-1", name: "Key 1", masked: "test-key-1", active: true, on: true },
    { id: "key-2", name: "Key 2", masked: "test-key-2", active: false, on: true },
  ];
  const providers = { providers: [{
    id: "relay", name: "Relay", icon: "generic", chat: "https://example.invalid/v1", models: [], agents: [], fallback: [],
    headers: {}, key: { set: true, masked: "fixture" }, routing: "", keyList,
  }], presets: [], excluded: [], gateway: { running: false, window: true } };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  // renamed: each keys/rename the fake answered; hold: a promise its answer waits on
  const f = { renamed: [], hold: null };
  f.route = async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs={lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window={};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/keys/rename") {
      const body = req.postDataJSON();
      if (f.hold) await f.hold;
      f.renamed.push(body);
      keyList.find((k) => k.id === body.ref).name = body.name;
      return json(providers);
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    return route.fulfill({ body: await fs.readFile(file), contentType });
  };
  return f;
}

const name = (page) => page.locator('.editor .accts .acc[data-account-id="key-1"] .rename');
const renames = (page) => page.evaluate(() => window.renames);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    // a page with Relay's editor open, its requests to keys/rename counted
    // as the page sends them
    const open = async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const context = await browser.newContext({ viewport: { width: 1000, height: 850 }, reducedMotion: "reduce" });
      await context.addInitScript(() => {
        window.renames = 0;
        const send = window.fetch;
        window.fetch = function (url) {
          if (String(url).endsWith("/api/keys/rename")) window.renames++;
          return send.apply(this, arguments);
        };
      });
      const page = await context.newPage(), f = fixture(lang), errors = [];
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", f.route);
      await page.goto("http://uni-switch.test/?view=providers&edit=relay");
      await name(page).waitFor();
      return { page, f, errors };
    };
    const rename = async (page) => {
      await name(page).click();
      const i = page.locator(".editor .accts .rename-in");
      await i.waitFor();
      assert.equal(await i.evaluate((e) => e === document.activeElement), true, "the rename's input has the focus");
      return i;
    };

    test(`${engine} ${lang}: Escape keeps a key's name, and draws the editor once`, async (t) => {
      const { page, errors } = await open(t);
      const i = await rename(page);
      await page.keyboard.press("Escape");
      await i.waitFor({ state: "detached" });
      assert.deepEqual(errors, [], "Escape drew the editor again inside its own redraw");
      assert.equal(await renames(page), 0);
      assert.equal(await name(page).textContent(), "Key 1");
    });

    test(`${engine} ${lang}: Escape after typing keeps the old name and saves nothing`, async (t) => {
      const { page, f, errors } = await open(t);
      const i = await rename(page);
      await i.fill("Renamed");
      await page.keyboard.press("Escape");
      await i.waitFor({ state: "detached" });
      assert.equal(await renames(page), 0, `Escape saved the typed name: keys/rename sent ${await renames(page)} time(s)`);
      assert.equal(await name(page).textContent(), "Key 1");
      assert.deepEqual(f.renamed, []);
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: Enter saves the new name with one keys/rename`, async (t) => {
      const { page, f, errors } = await open(t);
      const i = await rename(page);
      await i.fill("Renamed");
      await page.keyboard.press("Enter");
      // the editor drawn on the answer: a second send would be under way by now
      await page.waitForFunction(() => document.querySelector('.editor .accts .acc[data-account-id="key-1"] .rename')?.textContent === "Renamed");
      assert.equal(await renames(page), 1, `Enter sent keys/rename ${await renames(page)} times`);
      assert.deepEqual(f.renamed, [{ id: "relay", ref: "key-1", name: "Renamed" }]);
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: Enter again before the save answers sends nothing more`, async (t) => {
      const { page, f, errors } = await open(t);
      let answer;
      f.hold = new Promise((r) => { answer = r; });
      const i = await rename(page);
      await i.fill("Renamed");
      await page.keyboard.press("Enter");
      await page.keyboard.press("Enter");
      assert.equal(await renames(page), 1, `a second Enter sent keys/rename again (${await renames(page)} sent)`);
      f.hold = null;
      answer();
      await page.waitForFunction(() => document.querySelector('.editor .accts .acc[data-account-id="key-1"] .rename')?.textContent === "Renamed");
      assert.equal(await renames(page), 1, `keys/rename sent ${await renames(page)} times`);
      assert.deepEqual(errors, []);
    });
  }
}

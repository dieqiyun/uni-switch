// Run with Node's test runner and Playwright on the module path; see README.md.
// The Codex app reads the sign-in only when it opens. After uni-switch switches
// Codex to another account it stays on the one before, shows that account's
// limits and, once they are spent, sends in no thread (CavillZhang on X).
// Codex's accounts say so, with the account the app is on and what moves it
// (quit and open it again), until the app reopens or "Got it" is clicked.
// Its Restart (nb5p_hQ on X) asks first in uni-switch's own dialog, then has
// uni-switch restart the app's app-server; one that fails says why in the
// reader's language. In every language, Chromium and WebKit, wide and narrow.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const codex = {
  id: "codex", name: "Codex", icon: "codex-color", chat: "", responses: "", anthropic: "", catalog: "", routing: "smart",
  models: [{ id: "gpt-6", name: "GPT-6", on: true }], agents: [], fallback: [], headers: {}, keyList: [],
  account: {
    agent: "codex", agentName: "Codex", user: "spare@example.com", plan: "PLUS",
    logins: [
      { user: "spare@example.com", plan: "PLUS", active: true, on: true },
      { user: "spent@example.com", plan: "PLUS", on: true },
    ],
  },
};

const notBack = "The Codex app's app-server ended, but the app didn't start another. Quit the app and open it again.";

function serve(lang, posted, fail) {
  let app = "spent@example.com";
  const providers = () => ({ providers: [codex], presets: [], excluded: [], gateway: { running: true, window: true }, ...(app ? { codexApp: app } : {}) });
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers());
    if (url.pathname === "/api/codex/daemon/dismiss-app") { posted.push(url.pathname); app = ""; return json(providers()); }
    if (url.pathname === "/api/codex/daemon/restart-app") {
      posted.push(url.pathname);
      if (fail) return route.fulfill({ status: 500, json: { error: notBack } });
      app = "";
      return json(providers());
    }
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const keys = [
  "The Codex app is still signed in as {user}",
  "It shows that account's usage limits until it is quit and opened again. Quit it and open it again to use the new account.",
  "Got it",
  "Restart",
  "Restart {agent}?",
  "The Codex app",
  "Its app-server restarts and signs in to the new account; its window stays. A running turn stops.",
  "Codex app-server restarted",
  "The {agent} app's app-server ended, but the app didn't start another. Quit the app and open it again.",
];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [900, 480]) {
      test(`${engine} ${lang} ${width}px: Codex's accounts say the Codex app is still on the account before`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await (await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], posted = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, posted));
        await page.goto("http://uni-switch.test/?view=providers");
        await page.locator(".row.provider", { hasText: "Codex" }).first().click();
        const box = page.locator(".editor .signing.daemon");
        await box.waitFor();
        const text = await box.textContent();
        assert.match(text, /spent@example\.com/);
        if (lang === "en") assert.match(text, /Quit it and open it again/);
        else {
          const missing = await page.evaluate(([l, ks]) => ks.filter((k) => !I18N[l][k]), [lang, keys]);
          assert.deepEqual(missing, [], `every string has its ${lang}`);
          assert.doesNotMatch(text, /Quit it and open it again/);
        }
        const fits = await box.evaluate((b) => [...b.querySelectorAll("*")].every((e) => e.getBoundingClientRect().right <= b.getBoundingClientRect().right + 0.5));
        assert.ok(fits, "nothing in the notice runs past it");
        assert.equal(await page.evaluate(() => document.scrollingElement.scrollWidth <= innerWidth), true, "no sideways scroll");
        for (const b of await box.locator("button").all()) {
          const r = await b.boundingBox();
          assert.ok(r.x >= 0 && r.x + r.width <= width, "a button past the window: " + JSON.stringify(r));
        }
        await box.locator("button:not(.primary)").click();
        await box.waitFor({ state: "detached" });
        assert.deepEqual(posted, ["/api/codex/daemon/dismiss-app"]);
        assert.deepEqual(errors, []);
      });

      test(`${engine} ${lang} ${width}px: Restart asks first, then restarts the Codex app's app-server`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await (await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], posted = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, posted));
        await page.goto("http://uni-switch.test/?view=providers");
        await page.locator(".row.provider", { hasText: "Codex" }).first().click();
        const box = page.locator(".editor .signing.daemon");
        await box.waitFor();
        const go = box.locator("button.primary");
        assert.equal((await go.textContent()).trim(), await page.evaluate((l) => (l === "en" ? "Restart" : I18N[l].Restart), lang));
        const ask = page.locator("dialog.action-confirm[open]");
        await go.click();
        await ask.waitFor();
        assert.doesNotMatch(await ask.textContent(), /\{\w+\}/);
        const dlg = await ask.boundingBox();
        assert.ok(dlg.x >= 0 && dlg.x + dlg.width <= width, "the dialog past the window");
        await page.keyboard.press("Escape");
        await ask.waitFor({ state: "detached" });
        assert.deepEqual(posted, [], "Escape restarts nothing");
        await go.click();
        await ask.locator("button.primary").click();
        await box.waitFor({ state: "detached" });
        assert.deepEqual(posted, ["/api/codex/daemon/restart-app"]);
        assert.deepEqual(errors, []);
      });
    }

    test(`${engine} ${lang}: a Restart that fails says why, in the reader's language`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 900, height: 760 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [], posted = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, posted, true));
      await page.goto("http://uni-switch.test/?view=providers");
      await page.locator(".row.provider", { hasText: "Codex" }).first().click();
      const box = page.locator(".editor .signing.daemon");
      await box.waitFor();
      await box.locator("button.primary").click();
      await page.locator("dialog.action-confirm[open] button.primary").click();
      const said = page.locator(".editor .editor-error");
      await said.waitFor();
      const text = (await said.textContent()).trim();
      assert.ok(text.includes("Codex"), text);
      if (lang === "en") assert.ok(text.includes(notBack), text);
      else assert.ok(!text.includes(notBack), "not translated: " + text);
      assert.equal(await box.count(), 1, "the notice stays for another try");
      assert.equal(await box.locator("button.primary.busy").count(), 0, "not left spinning");
      assert.deepEqual(posted, ["/api/codex/daemon/restart-app"]);
      assert.deepEqual(errors, []);
    });
  }
}

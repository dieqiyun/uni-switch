// Run with Node's test runner and Playwright on the module path; see README.md.
// Which of a Codex account's resets is used is the user's to pick
// (01huadalang on Discord: there was only auto-use, and by hand the one
// that runs out first). The confirmation offers each reset the account
// holds in the app's menu, the soonest first and picked unless another is;
// once another is picked it names the account and that reset, says the
// sooner one is kept, and the use names that reset's credit. Picking none
// uses the one that runs out first as before, and an account with one
// reset, or whose resets weren't listed one by one, is offered no pick.
// Escape closes the open menu, not the confirmation. The Usage page at
// 900px and 390px and the menu bar panel at 440px; English, Chinese (both),
// Japanese and German; Chromium and WebKit. No backend, the API is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(process.env.ASSET_DIR || path.resolve(__dirname, "../assets"));
const day = 864e5, base = Date.now();
const soon = new Date(base + 2 * day).toISOString(), late = new Date(base + 20 * day).toISOString();
const windows = [{ name: "5 hours", used: 40, resetsAt: new Date(base + 3600e3).toISOString() }, { name: "7 days", used: 100, resetsAt: new Date(base + 3 * day).toISOString() }];
const quotas = [
  { provider: "codex", name: "Codex", icon: "codex-color", user: "me@example.com", plan: "Plus", windows,
    resets: { count: 3, until: soon, each: [{ until: soon, id: "cr-soon" }, { until: late, id: "cr-late" }, { id: "cr-never" }] } },
  { provider: "codex", name: "Codex", icon: "codex-color", user: "one@example.com", plan: "Plus", windows,
    resets: { count: 1, until: soon, each: [{ until: soon, id: "cr-only" }] } },
  // resets counted, not listed with their ids (an older uni-switch's reading)
  { provider: "codex", name: "Codex", icon: "codex-color", user: "old@example.com", plan: "Plus", windows,
    resets: { count: 2, until: soon, each: [{ until: soon }, { until: late }] } },
];

// serve answers with the one account asked for: several of one vendor's
// fold to a line each until opened, which isn't what this is about
function serve(lang, posted, user) {
  const settings = { theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd", trayUsages: ["codex"], trayUsageEvery: 3 };
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/usage/quotas") return json(quotas.filter((q) => q.user === user));
    if (url.pathname === "/api/usage/codex-reset") { posted.push(JSON.parse(req.postData() || "{}")); return json({ code: "reset", windows: 2 }); }
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file).catch(() => ""), contentType });
  };
}

const KEYS = ["Reset to use", "Used unless you pick another", "The one used is {who}'s reset that runs out {when}.",
  "The one used is {who}'s reset that never runs out.", "The one that runs out first, {when}, is kept, and is lost if it isn't used by then.",
  "Reset {n} runs out {when}", "Reset {n} never runs out", "The one used is the one that runs out first, {when}."];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${engine} ${lang}: the confirmation picks which of the account's resets is used`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const errors = [];
      const open = async (url, viewport, posted, user) => {
        const page = await (await browser.newContext({ viewport, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, posted, user));
        await page.goto(url);
        return page;
      };
      // the words as the page says them, each one translated for lang
      const say = (page, key, vars) => page.evaluate(([lang, key, vars]) => {
        if (lang !== "en" && !I18N[lang]?.[key]) return "untranslated: " + key;
        for (const k of ["when"]) if (vars?.[k]) vars[k] = new Date(vars[k]).toLocaleString(intlLang());
        return t(key, vars);
      }, [lang, key, vars]);

      const run = async (url, viewport, useSel) => {
        let posted = [];
        const load = async (user) => {
          posted = [];
          const page = await open(url, viewport, posted, user);
          if (url.includes("panel")) await page.locator('#ptabs [data-ptab="usage"]').click();
          return page;
        };
        let page = await load("me@example.com");
        for (const k of KEYS) assert.notEqual(await say(page, k, { who: "x", when: soon, n: 1 }), "untranslated: " + k);
        const card = () => page.locator(useSel);
        let ask = page.locator(".reset-ask");
        let picker = ask.locator(".reset-pick .reset-card");

        // picked: the later one, named with its account, the sooner one kept
        await card().first().click();
        await ask.waitFor();
        assert.equal(await picker.count(), 1, "an account with three resets offers the pick");
        assert.equal((await picker.textContent()).trim(), await page.evaluate(([d]) => t("Reset {n} runs out {when}", { n: 1, when: resetClock(new Date(d)) }), [soon]));
        assert.equal((await ask.locator(".reset-which").textContent()).trim(), await say(page, "The one used is the one that runs out first, {when}.", { when: soon }));
        assert.equal(await ask.locator(".reset-sooner").isVisible(), false);
        // it fits the confirmation, however narrow
        const fits = await page.evaluate(() => {
          const ed = document.querySelector(".reset-ask").getBoundingClientRect(), p = document.querySelector(".reset-ask .reset-card").getBoundingClientRect();
          return p.right <= ed.right + 0.5 && p.left >= ed.left - 0.5 && document.documentElement.scrollWidth <= innerWidth;
        });
        assert(fits, "the pick fits the confirmation");
        await picker.click();
        const items = page.locator(".proto-menu .pm-item");
        assert.equal(await items.count(), 3, "each reset the account holds");
        const names = await page.locator(".proto-menu .pm-item .pm-name").allTextContents();
        assert.deepEqual(names, [
          await page.evaluate(([d]) => t("Reset {n} runs out {when}", { n: 1, when: resetClock(new Date(d)) }), [soon]),
          await page.evaluate(([d]) => t("Reset {n} runs out {when}", { n: 2, when: resetClock(new Date(d)) }), [late]),
          await page.evaluate(() => t("Reset {n} never runs out", { n: 3 })),
        ], "soonest first, the one that never runs out last");
        assert.equal(await page.locator(".proto-menu .pm-item.on").count(), 1);
        assert.match(await page.locator(".proto-menu .pm-item.on").textContent(), new RegExp(await say(page, "Used unless you pick another")));
        // Escape closes the menu and leaves the confirmation open
        await page.keyboard.press("Escape");
        await page.locator(".proto-menu").waitFor({ state: "detached" });
        assert.equal(await ask.isVisible(), true, "Escape on the menu closed the confirmation");
        await picker.click();
        await items.nth(1).click();
        assert.equal((await picker.locator("span").textContent()).trim(), await page.evaluate(([d]) => t("Reset {n} runs out {when}", { n: 2, when: resetClock(new Date(d)) }), [late]));
        assert.equal((await ask.locator(".reset-which").textContent()).trim(), await say(page, "The one used is {who}'s reset that runs out {when}.", { who: "me@example.com", when: late }));
        assert.equal((await ask.locator(".reset-sooner").textContent()).trim(), await say(page, "The one that runs out first, {when}, is kept, and is lost if it isn't used by then.", { when: soon }));
        assert.equal(await ask.locator(".reset-sooner").isVisible(), true);
        assert.equal(posted.length, 0, "nothing spent before the user says so");
        await ask.locator("button.primary").click();
        await ask.waitFor({ state: "detached" });
        assert.deepEqual(posted, [{ user: "me@example.com", credit: "cr-late" }], "the picked reset is the one spent");

        // the one that never runs out
        await card().first().click();
        await picker.click();
        await items.nth(2).click();
        assert.equal((await ask.locator(".reset-which").textContent()).trim(), await say(page, "The one used is {who}'s reset that never runs out.", { who: "me@example.com" }));
        await ask.locator("button.primary").click();
        await ask.waitFor({ state: "detached" });
        assert.deepEqual(posted[1], { user: "me@example.com", credit: "cr-never" });

        // none picked: the one that runs out first, as before
        await card().first().click();
        await ask.locator("button.primary").click();
        await ask.waitFor({ state: "detached" });
        assert.deepEqual(posted[2], { user: "me@example.com" }, "no pick names no credit");

        // one reset, or resets not listed by id: nothing to pick
        assert.equal(posted.length, 3);
        await page.context().close();
        for (const user of ["one@example.com", "old@example.com"]) {
          page = await load(user);
          ask = page.locator(".reset-ask");
          picker = ask.locator(".reset-pick .reset-card");
          await card().first().click();
          await ask.waitFor();
          assert.equal(await picker.count(), 0, user + " offers no pick");
          assert.equal((await ask.locator(".reset-which").textContent()).trim(), await say(page, "The one used is the one that runs out first, {when}.", { when: soon }));
          await page.keyboard.press("Escape");
          await ask.waitFor({ state: "detached" });
          assert.equal(posted.length, 0);
          await page.context().close();
        }
      };

      for (const width of [900, 390]) await run("http://uni-switch.test/?view=usage", { width, height: 800 }, ".quota-resets button");
      await run("http://uni-switch.test/?mode=panel", { width: 440, height: 700 }, ".pq-use");
      assert.deepEqual(errors, []);
    });
  }
}

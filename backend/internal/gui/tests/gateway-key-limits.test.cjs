// A gateway key with several limits at once (#2227): the badge says the
// one holding it most and how many more there are, each limit has its
// line under the row, and the editor is of one limit at a time, picked
// from a tab, with Add a limit and Remove this limit. In every language,
// both engines, wide and narrow.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { fixture } = require("./fixtures/caller-keys.cjs");

const src = fs.readFileSync(path.resolve(__dirname, "../assets/i18n.js"), "utf8");
const I18N = new Function(src.slice(0, src.indexOf("\n};") + 3) + "; return I18N;")();

const hour = 36e5, day = 864e5;
const limits = () => ({
  laptop: {
    limit: { period: "hours", hours: 5, cost: 10, since: new Date(Date.now() - 2 * hour).toISOString() },
    moreLimits: [{ period: "week", cost: 50 }],
    used: { period: "hours", hours: 5, start: new Date(Date.now() - 2 * hour).toISOString(), reset: new Date(Date.now() + 3 * hour).toISOString(), calls: 8,
      tokens: 300000, cost: 4, costLimit: 10, tokensLeft: 0, costLeft: 6, spent: false },
    usedAll: [
      { period: "hours", hours: 5, start: new Date(Date.now() - 2 * hour).toISOString(), reset: new Date(Date.now() + 3 * hour).toISOString(), calls: 8,
        tokens: 300000, cost: 4, costLimit: 10, tokensLeft: 0, costLeft: 6, spent: false },
      { period: "week", start: new Date(Date.now() - 4 * day).toISOString(), reset: new Date(Date.now() + 3 * day).toISOString(), calls: 120,
        tokens: 9000000, cost: 50.5, costLimit: 50, tokensLeft: 0, costLeft: 0, spent: true },
    ],
  },
});

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [1100, 420]) {
      test(`${engine} ${lang} ${width}: a key's several limits`, async (t) => {
        const tr = (k, vars = {}) => {
          const s = lang === "en" ? k : I18N[lang][k];
          assert.equal(typeof s, "string", `${lang} has ${k}`);
          return s.replace(/\{(\w+)\}/g, (_, v) => String(vars[v]));
        };
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await (await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(6000);
        page.setDefaultNavigationTimeout(60000);
        const events = [], errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", fixture(lang, "light", events, { lan: true, limits: limits() }));
        await page.goto("http://uni-switch.test/?view=gateway");
        await page.locator("#gatewayKeys .acc[data-key]").last().waitFor();
        const row = (id) => page.locator(`#gatewayKeys .acc[data-key="${id}"]`);
        const sent = (action) => events.filter((e) => e.action === action);
        const fits = async (what) => assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth), false, what + " scrolls sideways");
        const fiveHours = tr("every {n} hours", { n: 5 }), week = tr("per week");
        // tap clicks what may be below the fold on a narrow window, after
        // scrolling to it with the wheel as a person would, above the footer
        const tap = async (loc) => {
          await page.mouse.move(width / 2, 400);
          for (let i = 0; i < 20 && !(await loc.evaluate((e) => { const r = e.getBoundingClientRect(); return r.top >= 0 && r.bottom <= (document.querySelector("footer.foot")?.getBoundingClientRect().top ?? innerHeight); })); i++) {
            await page.mouse.wheel(0, 120);
            await page.waitForTimeout(50);
          }
          await loc.click();
        };

        // the badge: the spent week holds the key longest, and one more
        const badge = row("laptop").locator(".amodels.limit");
        const text = await badge.textContent();
        assert(text.includes(week) && text.includes(tr("Spent")) && text.includes(tr("+1 limit")), "badge says " + text);
        assert.equal(await row("laptop").locator(".amodels.limit.spent").count(), 1);
        // a line for each, its window first
        const lines = row("laptop").locator(".key-limit");
        assert.equal(await lines.count(), 2);
        assert((await lines.nth(0).textContent()).startsWith(fiveHours));
        assert((await lines.nth(1).textContent()).startsWith(week));
        assert.equal(await lines.nth(1).evaluate((e) => e.classList.contains("spent")), true);
        assert.equal(await row("server").locator(".key-limit").count(), 0, "a key without a limit says nothing");
        await fits("the rows");

        // the editor opens on the limit holding it, each limit a tab
        await badge.click();
        const ed = row("laptop").locator(".key-limit-ed");
        await ed.waitFor();
        const tabs = ed.getByRole("tablist", { name: tr("This key's limits"), exact: true }).getByRole("tab");
        assert.deepEqual((await tabs.allTextContents()).map((s) => s.trim()), [fiveHours, week]);
        assert.equal(await tabs.nth(1).getAttribute("aria-selected"), "true");
        const win = ed.getByRole("button", { name: tr("Limit window"), exact: true });
        const costIn = ed.getByRole("textbox", { name: tr("Estimated cost limit, US$"), exact: true });
        assert.equal(await win.getAttribute("data-value"), "week");
        assert.equal(await costIn.inputValue(), "50");
        for (const sel of [".klf-which", ".klf-tab"]) {
          assert.equal(await ed.locator(sel).first().evaluate((e) => getComputedStyle(e).borderLeftStyle), "none");
        }
        await fits("the editor");
        // Reset usage of the week alone
        await tap(ed.getByRole("button", { name: tr("Reset usage"), exact: true }));
        const modal = page.locator("#modal");
        await modal.getByRole("button", { name: tr("Reset usage"), exact: true }).click();
        await modal.waitFor({ state: "hidden" });
        assert.deepEqual(sent("reset-limit-key").map((e) => e.body), [{ key: "laptop", index: 1 }]);
        assert.equal(await row("laptop").locator(".amodels.limit.spent").count(), 0, "still spent after the week's Reset");

        // the 5 hours' tab: its own caps, changed alone
        await ed.waitFor();
        await ed.getByRole("tab", { name: fiveHours, exact: true }).click();
        assert.equal(await ed.getByRole("tab", { name: fiveHours, exact: true }).getAttribute("aria-selected"), "true");
        assert.equal(await win.getAttribute("data-value"), "hours");
        assert.equal(await costIn.inputValue(), "10");
        await costIn.fill("12");
        // Enter saves, as Save does: on a narrow window Save is below the fold
        await costIn.press("Enter");
        await ed.waitFor({ state: "detached" });
        assert.deepEqual(sent("limit-key").map((e) => e.body), [{ key: "laptop", limit: { period: "hours", hours: 5, tokens: 0, cost: 12, cacheReads: false }, index: 0 }]);

        // Add a limit: a window it has none in, nothing to save until a cap
        await badge.click();
        await ed.waitFor();
        await tap(ed.getByRole("button", { name: tr("Add a limit"), exact: true }));
        assert.equal(await ed.getByRole("tab", { name: tr("New limit"), exact: true }).getAttribute("aria-selected"), "true");
        assert.equal(await win.getAttribute("data-value"), "day");
        const save = ed.getByRole("button", { name: tr("Save"), exact: true });
        assert.equal(await save.isDisabled(), true, "a limit with no cap can be saved");
        await ed.getByRole("textbox", { name: tr("Token limit"), exact: true }).fill("1000000");
        await tap(save);
        await ed.waitFor({ state: "detached" });
        assert.deepEqual(sent("limit-key").at(-1).body, { key: "laptop", limit: { period: "day", tokens: 1000000, cost: 0, cacheReads: false }, index: 2 });
        assert.equal(await lines.count(), 3);
        assert((await badge.textContent()).includes(tr("+{n} limits", { n: 2 })));
        await fits("three limits");

        // Remove this limit takes off that one only
        await badge.click();
        await ed.waitFor();
        await ed.getByRole("tab", { name: tr("per day"), exact: true }).click();
        await tap(ed.getByRole("button", { name: tr("Remove this limit"), exact: true }));
        await tap(ed.getByRole("button", { name: tr("Save"), exact: true }));
        await ed.waitFor({ state: "detached" });
        assert.deepEqual(sent("limit-key").at(-1).body, { key: "laptop", limit: null, index: 2 });
        assert.equal(await lines.count(), 2);
        assert.deepEqual(errors, []);
      });
    }
  }
}

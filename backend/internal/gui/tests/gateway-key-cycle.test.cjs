// A gateway key's limit every N days or every N hours (#1509, #2102) and its Reset:
// the window menu offers "Every N days" with its days beside it, and Reset
// usage, after a confirm, counts the key from 0 again, tokens and cost
// together. In every language, both engines, and at a narrow width.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { fixture } = require("./fixtures/caller-keys.cjs");

const src = fs.readFileSync(path.resolve(__dirname, "../assets/i18n.js"), "utf8");
const I18N = new Function(src.slice(0, src.indexOf("\n};") + 3) + "; return I18N;")();

const day = 864e5;
const limits = () => ({
  laptop: {
    limit: { period: "days", days: 10, cost: 800, since: new Date(Date.now() - 4 * day).toISOString() },
    used: { period: "days", days: 10, start: new Date(Date.now() - 4 * day).toISOString(), reset: new Date(Date.now() + 6 * day).toISOString(), calls: 90,
      tokens: 4000000, cost: 812.5, costLimit: 800, tokensLeft: 0, costLeft: 0, spent: true },
  },
  server: {
    limit: { period: "month", tokens: 1000000 },
    used: { period: "month", start: new Date(Date.now() - 3 * day).toISOString(), reset: new Date(Date.now() + 3 * day).toISOString(), calls: 40,
      tokens: 400000, cost: 2, tokenLimit: 1000000, tokensLeft: 600000, costLeft: 0, spent: false },
  },
});

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${engine} ${lang}: a key's limit every N days, and Reset usage`, async (t) => {
      const tr = (k, vars = {}) => {
        const s = lang === "en" ? k : I18N[lang][k];
        assert.equal(typeof s, "string", `${lang} has ${k}`);
        return s.replace(/\{(\w+)\}/g, (_, v) => String(vars[v]));
      };
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 1000, height: 700 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(6000);
      page.setDefaultNavigationTimeout(60000);
      const events = [], errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", fixture(lang, "light", events, { lan: true, limits: limits() }));
      await page.goto("http://uni-switch.test/?view=gateway");
      await page.locator("#gatewayKeys .acc[data-key]").last().waitFor();
      const row = (id) => page.locator(`#gatewayKeys .acc[data-key="${id}"]`);
      const sent = (action) => events.filter((e) => e.action === action);
      const badge = row("laptop").locator(".amodels.limit");
      assert.match(await badge.textContent(), new RegExp(tr("every {n} days", { n: 10 })));
      assert.equal(await row("laptop").locator(".amodels.limit.spent").count(), 1);
      const view = page.locator("#view-gateway");
      const scrolled = async () => [await view.evaluate((v) => v.scrollTop), await page.evaluate(() => window.scrollY)];
      const at = await scrolled();

      // its editor: the window is "Every N days", its days 10
      await row("laptop").getByRole("button", { name: tr("Change this key's limit"), exact: true }).click();
      const ed = row("laptop").locator(".key-limit-ed");
      await ed.waitFor();
      const win = ed.getByRole("button", { name: tr("Limit window"), exact: true });
      assert.equal(await win.getAttribute("data-value"), "days");
      assert.equal((await win.textContent()).trim(), tr("Every N days"));
      const daysIn = ed.getByRole("textbox", { name: tr("Days in a cycle"), exact: true });
      assert.equal(await daysIn.inputValue(), "10");
      assert.equal(await ed.locator(".munsaved").isVisible(), false, "nothing changed yet");
      for (const sel of [".klf-reset", ".klf-used"]) {
        assert.equal(await ed.locator(sel).evaluate((e) => getComputedStyle(e).borderLeftStyle), "none");
      }

      // Reset usage: asked first; Cancel sends nothing
      const resetBtn = ed.getByRole("button", { name: tr("Reset usage"), exact: true });
      await resetBtn.click();
      const modal = page.locator("#modal");
      await modal.getByText(tr("Reset {name}'s usage?", { name: "Laptop" })).waitFor();
      assert.match(await modal.textContent(), new RegExp(tr("The tokens and cost {name} has used go back to 0 together, and a new {n}-day cycle starts now.", { name: "Laptop", n: 10 }).replace(/[.?()]/g, "\\$&")));
      await modal.getByRole("button", { name: tr("Cancel"), exact: true }).click();
      await modal.waitFor({ state: "hidden" });
      assert.equal(sent("reset-limit-key").length, 0, "Cancel reset the key");
      await resetBtn.click();
      await modal.getByRole("button", { name: tr("Reset usage"), exact: true }).click();
      await modal.waitFor({ state: "hidden" });
      assert.deepEqual(sent("reset-limit-key").map((e) => e.body), [{ key: "laptop" }]);
      assert.equal(sent("limit-key").length, 0, "Reset changed the limit");
      assert.equal(await row("laptop").locator(".amodels.limit.spent").count(), 0, "still spent after Reset");
      assert.match(await row("laptop").locator(".key-limit").first().textContent(), /\$0\.00/);
      assert.deepEqual(await scrolled(), at, "Reset moved the page");
      await ed.getByRole("button", { name: tr("Cancel"), exact: true }).click();
      await ed.waitFor({ state: "detached" });

      // a calendar limit's Reset keeps its window's end
      await row("server").getByRole("button", { name: tr("Change this key's limit"), exact: true }).click();
      const sed = row("server").locator(".key-limit-ed");
      await sed.waitFor();
      assert.equal(await sed.getByRole("textbox", { name: tr("Days in a cycle"), exact: true }).isVisible(), false, "a month has no days field");
      await sed.getByRole("button", { name: tr("Reset usage"), exact: true }).click();
      const calendar = tr("The tokens and cost {name} has used go back to 0 together. Calls so far in this window stop counting; it still resets {when}.", { name: "Server", when: "\u0000" }).split("\u0000")[0];
      assert(((await modal.textContent()) || "").includes(calendar), "the calendar Reset says the window keeps its end");
      await modal.getByRole("button", { name: tr("Reset usage"), exact: true }).click();
      await modal.waitFor({ state: "hidden" });
      assert.deepEqual(sent("reset-limit-key").at(-1).body, { key: "server" });
      await sed.getByRole("button", { name: tr("Cancel"), exact: true }).click();
      await sed.waitFor({ state: "detached" });

      // a new limit every 3 days: the days are checked, then sent with it
      await row("work").getByRole("button", { name: tr("Set a limit for this key"), exact: true }).click();
      const wed = row("work").locator(".key-limit-ed");
      await wed.waitFor();
      assert.equal(await wed.locator("select").count(), 0, "no native select");
      await wed.getByRole("textbox", { name: tr("Token limit"), exact: true }).fill("2M");
      await wed.getByRole("button", { name: tr("Limit window"), exact: true }).click();
      await page.locator(".proto-menu .pm-item", { hasText: tr("Every N days") }).click();
      const wdays = wed.getByRole("textbox", { name: tr("Days in a cycle"), exact: true });
      assert.equal(await wdays.isVisible(), true);
      assert.equal(await wed.getByRole("button", { name: tr("Save"), exact: true }).isDisabled(), true, "no days yet");
      assert.equal(await wed.locator(".klf-err").textContent(), tr("Days is a whole number from 1 to 3650"));
      for (const bad of ["0", "3651", "2.5", "x"]) {
        await wdays.fill(bad);
        assert.equal(await wed.getByRole("button", { name: tr("Save"), exact: true }).isDisabled(), true, bad);
      }
      await wdays.fill("3");
      assert.equal(await wed.locator(".klf-err").isVisible(), false);
      // narrow, under the editor's one-column width: it and its days fit
      await page.setViewportSize({ width: 500, height: 740 });
      await page.waitForFunction(() => getComputedStyle(document.querySelector("#gatewayKeys .klf")).gridTemplateColumns.split(" ").length === 1);
      assert.equal(await wed.evaluate((e) => e.scrollWidth > e.clientWidth || e.getBoundingClientRect().right > window.innerWidth), false, "the editor overflows at 500px");
      const box = await wdays.boundingBox();
      assert(box && box.width > 100 && box.x + box.width <= 500, "the days field is squeezed");
      // the app's own narrow width (the nav's labels need more below it)
      await page.setViewportSize({ width: 560, height: 740 });
      await page.waitForTimeout(100);
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth), false, "the page overflows at 560px");
      await page.setViewportSize({ width: 1000, height: 700 });
      assert.equal(sent("limit-key").length, 0, "sent before Save");
      await wed.getByRole("button", { name: tr("Save"), exact: true }).click();
      await wed.waitFor({ state: "detached" });
      assert.deepEqual(sent("limit-key").map((e) => e.body), [{ key: "work", limit: { period: "days", days: 3, tokens: 2000000, cost: 0, cacheReads: false } }]);
      assert.match(await row("work").locator(".amodels.limit").textContent(), new RegExp(tr("every {n} days", { n: 3 })));

      // every 5 hours (#2102): its hours field, checked, then sent; days are not
      await row("work").getByRole("button", { name: tr("Change this key's limit"), exact: true }).click();
      const hed = row("work").locator(".key-limit-ed");
      await hed.waitFor();
      await hed.getByRole("button", { name: tr("Limit window"), exact: true }).click();
      await page.locator(".proto-menu .pm-item", { hasText: tr("Every N hours") }).click();
      const hrs = hed.getByRole("textbox", { name: tr("Hours in a cycle"), exact: true });
      assert.equal(await hrs.isVisible(), true);
      assert.equal(await hed.getByRole("textbox", { name: tr("Days in a cycle"), exact: true }).isVisible(), false, "hours show the days field");
      assert.equal(await hed.getByRole("button", { name: tr("Save"), exact: true }).isDisabled(), true, "no hours yet");
      assert.equal(await hed.locator(".klf-err").textContent(), tr("Hours is a whole number from 1 to 8760"));
      for (const bad of ["0", "8761", "1.5", "x"]) {
        await hrs.fill(bad);
        assert.equal(await hed.getByRole("button", { name: tr("Save"), exact: true }).isDisabled(), true, bad);
      }
      await hrs.fill("5");
      assert.equal(await hed.locator(".klf-err").isVisible(), false);
      await page.setViewportSize({ width: 500, height: 740 });
      await page.waitForFunction(() => getComputedStyle(document.querySelector("#gatewayKeys .klf")).gridTemplateColumns.split(" ").length === 1);
      assert.equal(await hed.evaluate((e) => e.scrollWidth > e.clientWidth || e.getBoundingClientRect().right > window.innerWidth), false, "the editor overflows at 500px");
      const hbox = await hrs.boundingBox();
      assert(hbox && hbox.width > 100 && hbox.x + hbox.width <= 500, "the hours field is squeezed");
      await page.setViewportSize({ width: 1000, height: 700 });
      await hed.getByRole("button", { name: tr("Save"), exact: true }).click();
      await hed.waitFor({ state: "detached" });
      assert.deepEqual(sent("limit-key").at(-1).body, { key: "work", limit: { period: "hours", hours: 5, tokens: 2000000, cost: 0, cacheReads: false } });
      assert.match(await row("work").locator(".amodels.limit").textContent(), new RegExp(tr("every {n} hours", { n: 5 })));
      // its Reset says a new 5-hour cycle starts
      await row("work").getByRole("button", { name: tr("Change this key's limit"), exact: true }).click();
      const red = row("work").locator(".key-limit-ed");
      await red.waitFor();
      assert.equal(await red.getByRole("textbox", { name: tr("Hours in a cycle"), exact: true }).inputValue(), "5");
      await red.getByRole("button", { name: tr("Reset usage"), exact: true }).click();
      assert.match(await modal.textContent(), new RegExp(tr("The tokens and cost {name} has used go back to 0 together, and a new {n}-hour cycle starts now.", { name: "Work", n: 5 }).replace(/[.?()]/g, "\\$&")));
      await modal.getByRole("button", { name: tr("Cancel"), exact: true }).click();
      await modal.waitFor({ state: "hidden" });
      await red.getByRole("button", { name: tr("Cancel"), exact: true }).click();
      assert.deepEqual(errors, []);
    });
  }
}

// A gateway key's limit lines say what it has used while the Gateway
// page stays open: Young (Discord, v0.1.18xx) reset his keys' usage and
// their lines read $0.00 from then on, while Usage showed the same keys
// spending, because the keys were read only on a visit to the page. Now
// the window coming back, and a timer while the page is in sight, read
// them again. Both engines, en and zh.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { fixture } = require("./fixtures/caller-keys.cjs");

const src = fs.readFileSync(path.resolve(__dirname, "../assets/i18n.js"), "utf8");
const I18N = new Function(src.slice(0, src.indexOf("\n};") + 3) + "; return I18N;")();

const hour = 36e5, day = 864e5;
// Young's shape: a key held to some models, $ caps over 5 hours and 7 days
// and a token cap over 30, just reset
const used = (cost, tokens) => {
  const since = new Date(Date.now() - 60e3).toISOString();
  const u = (period, extra) => ({ period, start: since, calls: cost ? 12 : 0, tokens, cost, tokensLeft: 0, costLeft: 0, spent: false, ...extra });
  return [
    u("hours", { hours: 5, reset: new Date(Date.now() + 5 * hour).toISOString(), costLimit: 30, costLeft: 30 - cost }),
    u("days", { days: 7, reset: new Date(Date.now() + 7 * day).toISOString(), costLimit: 153, costLeft: 153 - cost }),
    u("days", { days: 30, reset: new Date(Date.now() + 30 * day).toISOString(), tokenLimit: 1e13, tokensLeft: 1e13 - tokens }),
  ];
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a key's used limits are read again while the page is open`, async (t) => {
      const tr = (k, vars = {}) => {
        const s = lang === "en" ? k : I18N[lang][k];
        assert.equal(typeof s, "string", `${lang} has ${k}`);
        return s.replace(/\{(\w+)\}/g, (_, v) => String(vars[v]));
      };
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 1100, height: 760 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(6000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const key = {
        id: "pool", name: "拼车-自用", masked: "sk-uni-switch-key-…6fcf81", models: ["a/m", "group/mine"],
        limit: { period: "hours", hours: 5, cost: 30 },
        moreLimits: [{ period: "days", days: 7, cost: 153 }, { period: "days", days: 30, tokens: 1e13 }],
      };
      const keys = [key, { id: "other", name: "其他模型", masked: "sk-uni-switch-key-…222222" }];
      const spend = (cost, tokens) => { key.usedAll = used(cost, tokens); key.used = key.usedAll[0]; };
      spend(0, 0);
      await page.clock.install();
      await page.route("**/*", fixture(lang, "light", [], { lan: true, keys }));
      await page.goto("http://uni-switch.test/?view=gateway");
      const lines = page.locator('#gatewayKeys .acc[data-key="pool"] .key-limit');
      await lines.first().waitFor();
      const zero = tr("{used} of {limit} estimated · {left} left", { used: "$0.00", limit: "$30.00", left: "$30.00" });
      assert((await lines.nth(0).textContent()).includes(zero), await lines.nth(0).textContent());

      // calls made through the key: the window coming back shows them
      spend(9.93, 63e6);
      await page.evaluate(() => window.dispatchEvent(new Event("focus")));
      const spent = tr("{used} of {limit} estimated · {left} left", { used: "$9.93", limit: "$30.00", left: "$20.07" });
      await page.waitForFunction((s) => document.querySelector('#gatewayKeys .acc[data-key="pool"] .key-limit')?.textContent.includes(s), spent);
      assert((await lines.nth(1).textContent()).includes("$9.93"), await lines.nth(1).textContent());

      // and, left alone in sight, the timer does
      spend(12.5, 80e6);
      await page.clock.runFor(16e3);
      const later = tr("{used} of {limit} estimated · {left} left", { used: "$12.50", limit: "$30.00", left: "$17.50" });
      await page.waitForFunction((s) => document.querySelector('#gatewayKeys .acc[data-key="pool"] .key-limit')?.textContent.includes(s), later);

      // an open limit editor is someone typing: never drawn over
      await page.locator('#gatewayKeys .acc[data-key="pool"] .amodels.limit').click();
      const ed = page.locator('#gatewayKeys .acc[data-key="pool"] .key-limit-ed');
      await ed.waitFor();
      const costIn = ed.getByRole("textbox", { name: tr("Estimated cost limit, US$"), exact: true });
      await costIn.fill("31");
      spend(14, 90e6);
      await page.clock.runFor(16e3);
      await page.evaluate(() => window.dispatchEvent(new Event("focus")));
      await page.clock.runFor(2e3);
      assert.equal(await costIn.inputValue(), "31", "the editor kept what was typed");
      assert.equal(await costIn.evaluate((e) => e === document.activeElement), true, "and the field being typed in");
      assert.deepEqual(errors, []);
    });
  }
}

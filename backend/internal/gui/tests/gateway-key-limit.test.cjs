// A gateway key's own limit (#585): what each key has used of it, and
// its editor under the key's row, staged until Save.
const assert = require("node:assert/strict");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { fixture } = require("./fixtures/caller-keys.cjs");

const day = 864e5;
const limits = () => ({
  laptop: {
    limit: { period: "day", tokens: 1000000 },
    used: { period: "day", start: new Date(Date.now() - day / 2).toISOString(), reset: new Date(Date.now() + day / 2).toISOString(), calls: 12,
      tokens: 250000, cost: 0.8, tokenLimit: 1000000, tokensLeft: 750000, costLeft: 0, inFlight: 2, spent: false },
  },
  server: {
    limit: { period: "month", cost: 5 },
    used: { period: "month", start: new Date(Date.now() - 3 * day).toISOString(), reset: new Date(Date.now() + 3 * day).toISOString(), calls: 40,
      tokens: 900000, cost: 5.2, costLimit: 5, tokensLeft: 0, costLeft: 0, unpriced: 3, spent: true },
  },
});

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a gateway key's limit is shown and set with Save`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 1000, height: 560 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(6000);
      page.setDefaultNavigationTimeout(60000);
      const events = [], errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", fixture(lang, "light", events, { lan: true, limits: limits() }));
      const zh = lang === "zh";
      const w = zh
        ? { limit: "为此密钥设置限额", change: "修改此密钥的限额", left: /剩余/, refused: /前拒绝请求/, tokens: "Token 限额", cost: "估算费用限额（美元）", window: "限额周期", cache: "缓存读取也计入", save: "保存", cancel: "取消", none: "不限制", unsaved: "未保存", week: "每周", bad: /Token 应为数量/ }
        : { limit: "Set a limit for this key", change: "Change this key's limit", left: /tokens · .* left/, refused: /refused until/, tokens: "Token limit", cost: "Estimated cost limit, US$", window: "Limit window", cache: "Count cache reads too", save: "Save", cancel: "Cancel", none: "No limit", unsaved: "unsaved", week: "per week", bad: /Tokens is a count/ };
      await page.goto("http://uni-switch.test/?view=gateway");
      await page.locator("#gatewayKeys .acc[data-key]").last().waitFor();
      const row = (id) => page.locator(`#gatewayKeys .acc[data-key="${id}"]`);
      // what each limited key has used, has left and when it resets
      assert.match(await row("laptop").locator(".key-limit").textContent(), w.left);
      assert.match(await row("laptop").locator(".key-limit").textContent(), zh ? /2 个请求进行中/ : /2 in flight/);
      assert.match(await row("laptop").locator(".amodels.limit").textContent(), zh ? /每日/ : /per day/);
      assert.match(await row("server").locator(".key-limit").textContent(), w.refused);
      assert.match(await row("server").locator(".key-limit").textContent(), /\$5\.20/);
      assert.equal(await row("server").locator(".amodels.limit.spent").count(), 1);
      assert.match(await row("server").locator(".key-limit").getAttribute("title"), zh ? /3 次调用无价格/ : /3 calls without a known price/);
      assert.equal(await row("work").locator(".key-limit").count(), 0, "a key without a limit says nothing under its row");
      // nothing about it is drawn with a coloured left border
      for (const sel of [".key-limit", ".amodels.limit"]) {
        const styles = await page.locator("#gatewayKeys " + sel).evaluateAll((els) => els.map((e) => getComputedStyle(e).borderLeftStyle));
        assert(styles.every((s) => s === "none"), sel + " has a left border");
      }
      const view = page.locator("#view-gateway");
      const scrolled = async () => [await view.evaluate((v) => v.scrollTop), await page.evaluate(() => window.scrollY)];
      await page.mouse.move(500, 300);
      await page.mouse.wheel(0, 120);
      await page.waitForTimeout(100);
      const at = await scrolled();
      // open Work's editor: staged, nothing sent
      await row("work").getByRole("button", { name: w.limit, exact: true }).click();
      const ed = row("work").locator(".key-limit-ed");
      await ed.waitFor();
      assert.deepEqual(await scrolled(), at, "opening the editor moved the page");
      assert.equal(await ed.evaluate((e) => getComputedStyle(e).borderLeftStyle), "none");
      await ed.getByRole("textbox", { name: w.tokens, exact: true }).fill("2M");
      // the window is the app's own menu, not a native select
      assert.equal(await ed.locator("select").count(), 0, "no native select");
      const win = ed.getByRole("button", { name: w.window, exact: true });
      await win.click();
      assert.equal((await page.locator(".proto-menu .pm-head").textContent()).trim(), w.window);
      await page.locator(".proto-menu .pm-item", { hasText: w.week }).click();
      assert.equal(await page.locator(".proto-menu").count(), 0, "a pick closes it");
      assert.equal(await win.getAttribute("data-value"), "week");
      assert.equal((await win.textContent()).trim(), w.week);
      assert.equal(await ed.isVisible(), true, "a pick leaves the editor open");
      await ed.getByLabel(w.cache).check();
      assert.equal(await ed.locator(".munsaved").isVisible(), true);
      assert.equal(await ed.locator(".munsaved").textContent(), w.unsaved);
      await ed.getByRole("button", { name: w.cancel, exact: true }).click();
      await ed.waitFor({ state: "detached" });
      assert.equal(events.filter((e) => e.action === "limit-key").length, 0, "Cancel sent the limit");
      assert.deepEqual(await scrolled(), at, "Cancel moved the page");
      // opened again, the draft is gone
      await row("work").getByRole("button", { name: w.limit, exact: true }).click();
      await ed.waitFor();
      assert.equal(await ed.getByRole("textbox", { name: w.tokens, exact: true }).inputValue(), "");
      assert.equal(await ed.locator(".munsaved").isVisible(), false);
      await ed.getByRole("textbox", { name: w.tokens, exact: true }).fill("lots");
      assert.match(await ed.locator(".klf-err").textContent(), w.bad);
      assert.equal(await ed.getByRole("button", { name: w.save, exact: true }).isDisabled(), true);
      await ed.getByRole("textbox", { name: w.tokens, exact: true }).fill(zh ? "200万" : "2M");
      await ed.getByRole("textbox", { name: w.cost, exact: true }).fill("5");
      await ed.getByRole("button", { name: w.window, exact: true }).click();
      await page.locator(".proto-menu .pm-item", { hasText: w.week }).click();
      await ed.getByLabel(w.cache).check();
      assert.equal(events.filter((e) => e.action === "limit-key").length, 0, "a pick was sent before Save");
      await ed.getByRole("button", { name: w.save, exact: true }).click();
      await ed.waitFor({ state: "detached" });
      const sent = events.filter((e) => e.action === "limit-key");
      assert.deepEqual(sent.map((e) => e.body), [{ key: "work", limit: { period: "week", tokens: 2000000, cost: 5, cacheReads: true } }]);
      assert.match(await row("work").locator(".amodels.limit").textContent(), new RegExp(w.week));
      assert.match(await row("work").locator(".key-limit").textContent(), w.left);
      assert.deepEqual(await scrolled(), at, "Save moved the page");
      // the limit taken off
      await row("laptop").getByRole("button", { name: w.change, exact: true }).click();
      const led = row("laptop").locator(".key-limit-ed");
      await led.waitFor();
      assert.equal(await led.getByRole("textbox", { name: w.tokens, exact: true }).inputValue(), "1000000");
      await led.getByRole("button", { name: w.none, exact: true }).click();
      assert.equal(events.filter((e) => e.action === "limit-key").length, 1);
      await led.getByRole("button", { name: w.save, exact: true }).click();
      await led.waitFor({ state: "detached" });
      assert.deepEqual(events.filter((e) => e.action === "limit-key").at(-1).body, { key: "laptop", limit: null });
      assert.equal(await row("laptop").locator(".key-limit").count(), 0);
      assert.deepEqual(await scrolled(), at, "no click moved the page");
      // narrow: the editor fits
      await row("server").getByRole("button", { name: w.change, exact: true }).click();
      await row("server").locator(".key-limit-ed").waitFor();
      await page.setViewportSize({ width: 560, height: 740 });
      // the header's tabs are fitted by a ResizeObserver a moment after the
      // resize, so wait for the state rather than reading it at once
      await page.waitForFunction(() => document.documentElement.scrollWidth <= window.innerWidth, null, { timeout: 3000 }).catch(async () => {
        assert.fail(`sideways scroll at 560px: ${await page.evaluate(() => document.documentElement.scrollWidth)}`);
      });
      assert.deepEqual(errors, []);
    });
  }
}

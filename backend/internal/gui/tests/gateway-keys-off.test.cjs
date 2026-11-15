const assert = require("node:assert/strict");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { fixture } = require("./fixtures/caller-keys.cjs");
const engines = process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"];

// Key management remains available locally, including when the list is empty.
for (const engine of engines) for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
  test(`${engine} ${lang}: local keys need no sharing switch`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const context = await browser.newContext({ viewport: { width: 1000, height: 760 }, reducedMotion: "reduce" });
    await context.addInitScript(() => localStorage.setItem("uni-switch.gwConnectFolded", "0"));
    for (const keys of [[], undefined]) {
      const page = await context.newPage(), events = [], errors = [];
      page.on("pageerror", e => errors.push(e.message));
      await page.route("**/*", fixture(lang, "light", events, { keys }));
      await page.goto("http://uni-switch.test/?view=gateway");
      await page.locator("#gatewayKeysBlock").waitFor();
      await page.locator("#connect .snip").waitFor();
      assert.equal(await page.locator("#addGatewayKey").isVisible(), true);
      assert.equal(await page.locator("#gatewayKeysOff,#gatewayLan,#connectAddress").count(), 0);
      assert.equal(await page.locator("#gatewayKeys .acc[data-key]").count(), keys ? 0 : 3);
      assert.equal(await page.locator("#connectKey").count(), keys ? 0 : 1);
      assert.match(await page.locator("#connect .snip").textContent(), /127\.0\.0\.1:3999/);
      assert.deepEqual(events.filter(e => e.action === "lan"), []);
      assert.deepEqual(errors, []);
      await page.close();
    }
  });
}

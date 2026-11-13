const assert = require("node:assert/strict");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { fixture } = require("./fixtures/caller-keys.cjs");
const engines = process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"];

// Legacy address picks are ignored; the selected local named key survives reloads.
for (const engine of engines) for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
  test(`${engine} ${lang}: Connect keeps its named key and only the local URL`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const context = await browser.newContext({ viewport: { width: 1000, height: 780 }, reducedMotion: "reduce" });
    await context.addInitScript(() => {
      localStorage.setItem("uni-switch.gwConnectFolded", "0");
      localStorage.setItem("uni-switch.connectAddress", "http://192.168.1.10:3999");
    });
    const page = await context.newPage(), errors = [];
    page.on("pageerror", e => errors.push(e.message));
    await page.route("**/*", fixture(lang, "light", []));
    await page.goto("http://uni-switch.test/?view=gateway");
    await page.locator("#connectKey").click();
    await page.locator(".proto-menu .pm-item", { hasText: "Server" }).click();
    await page.waitForFunction(() => document.querySelector("#connect .snip")?.textContent.includes("fixture-server"));
    await page.reload();
    await page.waitForFunction(() => document.querySelector("#connect .snip")?.textContent.includes("fixture-server"));
    assert.match(await page.locator("#connectKey").innerText(), /Server/);
    assert.match(await page.locator("#connect .snip").innerText(), /127\.0\.0\.1:3999/);
    assert.equal(await page.locator("#connectAddress,#gatewayLan").count(), 0);
    await page.setViewportSize({ width: 760, height: 600 });
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    assert.deepEqual(errors, []);
  });
}

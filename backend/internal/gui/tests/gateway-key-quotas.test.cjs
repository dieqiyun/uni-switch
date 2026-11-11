// A gateway key held to a budget, models or accounts is told no quotas
// (#1981) unless its owner lets it be told those of what it may use
// (#2216, Kenoia): a "Quotas" badge on the key's row, shown on hover while
// off and always while on, sends "quotas-key".
const assert = require("node:assert/strict");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { fixture } = require("./fixtures/caller-keys.cjs");

const words = {
  en: { label: "Show this key its quotas", off: "Quotas", on: "Quotas shown" },
  zh: { label: "让这个密钥查看额度", off: "额度", on: "可看额度" },
  "zh-TW": { label: "讓這個金鑰查看額度", off: "額度", on: "可看額度" },
  ja: { label: "このキーに残量を見せる", off: "残量", on: "残量を表示" },
  de: { label: "Diesem Schlüssel seine Kontingente zeigen", off: "Kontingente", on: "Kontingente sichtbar" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const width of [1100, 420]) {
    for (const [lang, w] of Object.entries(words)) {
      test(`${engine} ${width} ${lang}: a held key is let see its quotas`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await (await browser.newContext({ viewport: { width, height: 700 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(6000);
        page.setDefaultNavigationTimeout(60000);
        const events = [], errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", fixture(lang, "light", events, { lan: true, keyHolds: { server: { models: ["relay/*"], quotas: true }, work: { models: ["relay/*"] } } }));
        await page.goto("http://uni-switch.test/?view=gateway");
        await page.locator("#gatewayKeys .acc[data-key]").last().waitFor();
        const row = (id) => page.locator(`#gatewayKeys .acc[data-key="${id}"]`);
        const badge = (id) => row(id).getByRole("button", { name: w.label, exact: true });
        // on: said always, not only on hover
        await page.mouse.move(0, 0);
        assert.equal((await badge("server").textContent()).trim(), w.on);
        assert.equal(await badge("server").getAttribute("aria-pressed"), "true");
        assert.equal(await badge("server").evaluate((b) => getComputedStyle(b).opacity), "1");
        assert.ok((await badge("server").boundingBox()).width > 0, "an on badge is hidden");
        // off: on hover only
        assert.equal(await badge("work").getAttribute("aria-pressed"), "false");
        assert.equal(await badge("work").evaluate((b) => getComputedStyle(b).opacity), "0");
        const view = page.locator("#view-gateway");
        const scrolled = async () => [await view.evaluate((v) => v.scrollTop), await page.evaluate(() => window.scrollY)];
        const before = await scrolled();
        await row("work").hover();
        assert.equal((await badge("work").textContent()).trim(), w.off);
        await badge("work").click();
        await page.waitForFunction(() => document.querySelector('#gatewayKeys .acc[data-key="work"] .key-quotas')?.getAttribute("aria-pressed") === "true");
        assert.deepEqual(events.filter((e) => e.action === "quotas-key").map((e) => e.body), [{ key: "work", quotas: true }]);
        assert.equal((await badge("work").textContent()).trim(), w.on);
        await row("server").hover();
        await badge("server").click();
        await page.waitForFunction(() => document.querySelector('#gatewayKeys .acc[data-key="server"] .key-quotas')?.getAttribute("aria-pressed") === "false");
        assert.deepEqual(events.filter((e) => e.action === "quotas-key").at(-1).body, { key: "server", quotas: false });
        assert.deepEqual(await scrolled(), before, "a click moved the page");
        // the row still fits: nothing runs past the window's edge
        const over = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
        assert.ok(over <= 0, `the page scrolls sideways by ${over}px`);
        const rowBox = await row("server").boundingBox(), b = await badge("server").boundingBox();
        await row("server").hover();
        assert.ok(b.x >= rowBox.x && b.x + b.width <= rowBox.x + rowBox.width + 1, "the badge runs out of its row");
        assert.equal(await page.locator("select").count(), 0, "no native select");
        const lefts = await page.locator("#gatewayKeys .key-quotas").evaluateAll((els) => els.map((e) => getComputedStyle(e).borderLeftStyle));
        assert(lefts.every((s) => s === "none"), "a coloured left border");
        assert.deepEqual(errors, []);
      });
    }
  }
}

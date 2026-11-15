const assert = require("node:assert/strict");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { fixture } = require("./fixtures/caller-keys.cjs");

// No gateway key yet (HarriesChen on Discord, 10-25): uni-switch web on a phone
// drew "No gateway keys yet" in the body's text, 14px and dark, flush
// against its card's edges. It is said in its card the way the same page's
// Recent calls says it has none: muted, small, inset. So is a list that
// can't be read, and the page's next redraw keeps saying so.
const NONE = { en: "No gateway keys yet", zh: "尚无网关密钥", "zh-TW": "尚無閘道金鑰", ja: "ゲートウェイキーはまだありません", de: "Noch keine Zugangsschlüssel" };

const look = (page) => page.evaluate(() => {
  const none = document.querySelector("#gatewayKeys .none");
  const calls = document.querySelector("#activity .none");
  const card = none.parentElement.getBoundingClientRect();
  const r = document.createRange();
  r.selectNodeContents(none);
  const text = r.getBoundingClientRect();
  const cs = getComputedStyle(none), ref = getComputedStyle(calls);
  return {
    inCard: none.parentElement.matches("#gatewayKeys > .accts"),
    text: none.textContent,
    color: cs.color, refColor: ref.color,
    size: cs.fontSize, refSize: ref.fontSize,
    weight: cs.fontWeight,
    inset: Math.min(text.left - card.left, card.right - text.right, text.top - card.top, card.bottom - text.bottom),
    overflow: document.documentElement.scrollWidth - innerWidth,
  };
});

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const theme of ["light", "dark"]) {
      test(`${engine} ${lang} ${theme}: no gateway key is said like the page's other empty lists`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await (await browser.newContext({ viewport: { width: 390, height: 844 }, reducedMotion: "reduce", colorScheme: theme })).newPage();
        page.setDefaultTimeout(6000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", fixture(lang, theme, [], { lan: true, web: true, keys: [] }));
        await page.goto("http://uni-switch.test/?view=gateway");
        await page.locator("#gatewayKeys .none").waitFor();
        await page.locator("#activity .none").waitFor();
        const s = await look(page);
        assert.equal(s.inCard, true, "said inside the list's card");
        assert.equal(s.text, NONE[lang]);
        assert.equal(s.color, s.refColor, "muted, as Recent calls' empty line");
        assert.equal(s.size, s.refSize, "the size of Recent calls' empty line");
        assert.equal(s.weight, "400");
        assert(s.inset >= 10, `inset from the card's edges, got ${s.inset}px`);
        assert(s.overflow <= 0, "no sideways scroll at 390px");
        assert.deepEqual(errors, []);
      });
    }
  }

  test(`${engine}: a gateway key list that can't be read says so in its card, past a redraw`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const page = await (await browser.newContext({ viewport: { width: 390, height: 844 }, reducedMotion: "reduce" })).newPage();
    page.setDefaultTimeout(6000);
    page.setDefaultNavigationTimeout(60000);
    await page.route("**/*", fixture("en", "light", [], { lan: true, web: true, keysError: "gateway keys unreadable" }));
    await page.goto("http://uni-switch.test/?view=gateway");
    await page.locator("#gatewayKeys .none").waitFor();
    await page.locator("#activity .none").waitFor();
    // the page drawn again, as the providers' poll does
    await page.evaluate(() => renderGatewayView());
    const s = await look(page);
    assert.equal(s.inCard, true, "said where the list goes, in its card");
    assert.equal(s.text, "gateway keys unreadable");
    assert.equal(s.color, s.refColor);
    assert.equal(s.size, s.refSize);
    assert(s.inset >= 10, `inset from the card's edges, got ${s.inset}px`);
  });

  test(`${engine}: a gateway key list read before stays when a later read fails`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    const page = await (await browser.newContext({ viewport: { width: 390, height: 844 }, reducedMotion: "reduce" })).newPage();
    page.setDefaultTimeout(6000);
    page.setDefaultNavigationTimeout(60000);
    await page.route("**/*", fixture("en", "light", [], { lan: true, web: true }));
    await page.goto("http://uni-switch.test/?view=gateway");
    await page.locator("#gatewayKeys .acc[data-key]").nth(2).waitFor();
    await page.route("**/api/caller-keys", (route) => route.fulfill({ status: 500, json: { error: "gateway keys unreadable" } }));
    await page.evaluate(() => loadGatewayKeys());
    await page.waitForFunction(() => document.body.textContent.includes("gateway keys unreadable"));
    assert.equal(await page.locator("#gatewayKeys .acc[data-key]").count(), 3, "the keys read before stay");
    assert.equal(await page.locator("#gatewayKeys .none").count(), 0);
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// Gateway keys in the order the user puts them in (alextjy on Discord): a
// key is dragged by its row, anywhere off its buttons, to another place, the
// grip in its margin shown only on the row under the pointer; the order is
// sent once as move-key and Saved is said; Alt+↑/↓ on a focused row moves it
// a place, the keyboard staying on it; an order that can't be saved is put
// back as it is kept; and the page opened again lists the keys in the order
// saved. In every language, Chromium and WebKit, 1100px and 420px wide, with
// the API faked.
const assert = require("node:assert/strict");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { fixture } = require("./fixtures/caller-keys.cjs");

const saved = { en: "Gateway key order saved", zh: "网关密钥的顺序已保存", "zh-TW": "閘道金鑰的順序已儲存", ja: "ゲートウェイキーの順番を保存しました", de: "Reihenfolge der Gateway-Schlüssel gespeichert" };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) for (const width of [1100, 420]) {
    test(`${engine} ${lang} ${width}px: gateway keys are dragged into order`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width, height: 900 } })).newPage();
      page.setDefaultTimeout(6000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [], events = [], options = { lan: true };
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", fixture(lang, "light", events, options));
      await page.goto("http://uni-switch.test/?view=gateway");
      const rows = page.locator("#gatewayKeys .acc[data-key]");
      await rows.last().waitFor();
      const order = () => rows.evaluateAll((rs) => rs.map((r) => r.dataset.key).join(","));
      const waitOrder = (want) => page.waitForFunction((want) => [...document.querySelectorAll("#gatewayKeys .acc[data-key]")].map((r) => r.dataset.key).join(",") === want, want);
      const moves = () => events.filter((e) => e.action !== "ledger");
      assert.equal(await order(), "laptop,server,work");
      assert(await page.evaluate(() => document.scrollingElement.scrollWidth <= innerWidth), "nothing wider than the window");

      // the grip shows only on the row under the pointer
      const grip = (i) => rows.nth(i).locator(".grip").evaluate((g) => getComputedStyle(g).opacity);
      await page.mouse.move(2, 2);
      await page.waitForFunction(() => getComputedStyle(document.querySelector("#gatewayKeys .acc[data-key] .grip")).opacity === "0");
      await rows.nth(1).hover({ position: { x: 40, y: 10 } });
      await page.waitForFunction(() => getComputedStyle(document.querySelectorAll("#gatewayKeys .acc[data-key] .grip")[1]).opacity === "1");
      assert.equal(await grip(0), "0", "only the row under the pointer has its grip");
      const box = await rows.nth(1).locator(".grip").boundingBox(), row = await rows.nth(1).boundingBox();
      assert(box.x >= row.x && box.x + box.width <= (await rows.nth(1).locator(".dot").boundingBox()).x, "the grip is in the row's margin, before its tick");

      const clear = () => page.evaluate(() => { const s = document.querySelector("#status"); s.className = "status"; s.textContent = ""; });
      const said = () => page.waitForFunction(() => document.querySelector("#status")?.classList.contains("ok")).then(() => page.locator("#status").textContent());
      const y0 = await page.evaluate(() => scrollY);

      // drag Work, by its masked key, above Laptop
      await clear();
      const from = await rows.nth(2).locator(".plan").boundingBox(), to = await rows.nth(0).boundingBox();
      const fx = from.x + from.width / 2, fy = from.y + from.height / 2;
      await page.mouse.move(fx, fy);
      await page.mouse.down();
      for (let i = 1; i <= 12; i++) await page.mouse.move(fx, fy + (to.y + 4 - fy) * i / 12);
      await page.mouse.up();
      await waitOrder("work,laptop,server");
      assert.equal((await said()).trim(), saved[lang]);
      assert.deepEqual(moves(), [{ action: "move-key", body: { key: "work", index: 0 } }], "letting go sent the order once, and clicked nothing");

      // Alt+↓ on Work moves it a place, the keyboard staying on it
      await clear();
      await rows.nth(0).focus();
      await page.keyboard.press("Alt+ArrowDown");
      await waitOrder("laptop,work,server");
      await said();
      assert.deepEqual(moves().at(-1), { action: "move-key", body: { key: "work", index: 1 } });
      await page.waitForFunction(() => document.activeElement?.matches?.('#gatewayKeys .acc[data-key="work"]') && document.activeElement.isConnected);

      // an order that can't be saved is put back as it is kept
      options.refuseMove = true;
      await rows.nth(2).focus();
      await page.keyboard.press("Alt+ArrowUp");
      await page.waitForFunction(() => document.querySelector("#status")?.classList.contains("err"));
      await waitOrder("laptop,work,server");
      options.refuseMove = false;
      assert.equal(moves().length, 3);

      // a click on a row's buttons still does what it did, and moves nothing
      await rows.nth(0).locator(".tick").click();
      await page.waitForFunction(() => document.querySelector('#gatewayKeys .acc[data-key="laptop"]')?.classList.contains("off"));
      assert.equal(moves().at(-1).action, "off-key");
      assert.equal(await order(), "laptop,work,server");
      assert.equal(await page.evaluate(() => scrollY), y0, "arranging moved the page");

      // opened again, the keys are in the order saved
      await page.reload();
      await rows.last().waitFor();
      assert.equal(await order(), "laptop,work,server");
      assert(await page.evaluate(() => document.scrollingElement.scrollWidth <= innerWidth), "nothing wider than the window");
      assert.deepEqual(errors, []);
    });
  }
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// Local Connect key menus stay open after an incidental scroll and close
// when the reader scrolls, in both browser engines and at narrow widths.
const assert = require("node:assert/strict");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { fixture } = require("./fixtures/caller-keys.cjs");

const LANGS = ["en", "zh", "zh-TW", "ja", "de"];

async function setup(t, engine, lang, width) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  t.after(() => browser.close());
  const page = await (await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" })).newPage();
  await page.addInitScript(() => { try { localStorage.setItem("uni-switch.gwConnectFolded", "0"); } catch {} }); // Connect opened, as a user who reads it
  page.setDefaultTimeout(6000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/*", fixture(lang, "light", [], { web: true }));
  await page.goto("http://uni-switch.test/?view=gateway");
  await page.locator("#connectKey").waitFor();
  return { page, errors };
}

const view = (page) => page.locator("#view-gateway");
const top = async (page, sel) => (await page.locator(sel).boundingBox()).y;
const menuOpen = (page) => page.locator(".proto-menu").count();

// the reader's own scroll: the wheel, over the view, till sel is in sight
async function readerBrings(page, sel) {
  const vb = await view(page).boundingBox();
  await page.mouse.move(vb.x + vb.width / 2, vb.y + vb.height / 2);
  for (let i = 0; i < 30; i++) {
    const r = await page.locator(sel).boundingBox();
    if (r.y > vb.y + 40 && r.y + r.height < vb.y + vb.height - 200) return;
    await page.mouse.wheel(0, r.y > vb.y + vb.height / 2 ? 120 : -120);
    await page.waitForTimeout(80);
  }
  throw new Error(sel + " never came into sight");
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of LANGS) {
    for (const width of [420, 1100]) {
        test(`${engine} ${lang} ${width}: Connect's menus stay open till the reader scrolls`, async (t) => {
          const { page, errors } = await setup(t, engine, lang, width);
          for (const sel of ["#connectKey"]) {
            await readerBrings(page, sel);
            await page.locator(sel).click();
            await page.locator(".proto-menu").waitFor();
            const at = await top(page, sel);
            // a scroll of the view the reader didn't make, in the frame after
            // the click: put back, and the menu stays beside its anchor
            await view(page).evaluate((v) => requestAnimationFrame(() => { v.scrollTop += 40; }));
            await page.waitForTimeout(300);
            assert.equal(await menuOpen(page), 1, `${sel}: the menu stays open after a scroll that wasn't the reader's`);
            assert.ok(Math.abs((await top(page, sel)) - at) < 1, `${sel}: its anchor stays where it was`);
            const [m, a] = [await page.locator(".proto-menu").boundingBox(), await page.locator(sel).boundingBox()];
            assert.ok(Math.abs(m.y - (a.y + a.height + 5)) < 2 || Math.abs(m.y + m.height + 5 - a.y) < 2, `${sel}: the menu is beside its anchor`);
            assert.ok(m.x >= 0 && m.x + m.width <= width, `${sel}: the menu fits the window`);
            // the reader's wheel closes it
            await page.mouse.wheel(0, 60);
            await page.locator(".proto-menu").waitFor({ state: "detached" });
          }
          assert.deepEqual(errors, []);
        });
    }
  }
}

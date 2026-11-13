const assert = require("node:assert/strict");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { fixture } = require("./fixtures/caller-keys.cjs");
const { openPart } = require("./reader.cjs");

// Jevik_AI on X deleted uni-switch's folders by hand, and Codex kept pointing
// at a gateway that was gone. Settings › About › Uninstall uni-switch asks
// first, disconnects every agent and removes the service (POST
// /api/uninstall), and only then has uni-switch quit and its files deleted
// (POST /api/uninstall/finish). A failure is said and nothing more is
// removed; the Codex app still on the old settings is said before uni-switch
// quits. uni-switch web never offers it.
const LANGS = ["en", "zh", "zh-TW", "ja", "de"];
const words = {
  en: { row: "Uninstall uni-switch", title: "Uninstall uni-switch?", go: "Uninstall", cancel: "Cancel", again: "Try again", quit: "Quit uni-switch" },
  zh: { row: "卸载 uni-switch", title: "卸载 uni-switch？", go: "卸载", cancel: "取消" },
  "zh-TW": { row: "解除安裝 uni-switch", title: "解除安裝 uni-switch？", go: "解除安裝", cancel: "取消" },
  ja: { row: "uni-switch をアンインストール", title: "uni-switch をアンインストールしますか？", go: "アンインストール", cancel: "キャンセル" },
  de: { row: "uni-switch deinstallieren", title: "uni-switch deinstallieren?", go: "Deinstallieren", cancel: "Abbrechen" },
};

async function setup(t, engine, lang, width, { web = false, reply = { failed: false, says: [] } } = {}) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  t.after(() => browser.close());
  const page = await (await browser.newContext({ viewport: { width, height: 700 }, reducedMotion: "reduce" })).newPage();
  page.setDefaultTimeout(6000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [], posts = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/*", fixture(lang, "light", [], { web }));
  await page.route("**/api/uninstall**", (route) => {
    const p = new URL(route.request().url()).pathname;
    posts.push(p);
    if (p === "/api/uninstall/finish") return route.fulfill({ status: 204 });
    return route.fulfill({ json: { agents: [], system: [], data: [], ...reply } });
  });
  await page.goto("http://uni-switch.test/?view=gateway");
  await page.locator("#prefs").click();
  await openPart(page, "about");
  await page.locator("#about .row").first().waitFor();
  return { page, errors, posts };
}

const dialog = (page) => page.locator("dialog.action-confirm");
const scrolled = (page) => page.evaluate(() => document.querySelector("#view-settings").scrollTop);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of LANGS) {
    for (const width of [1100, 420]) {
      test(`${engine} ${lang} ${width}: Uninstall uni-switch asks, then disconnects and quits`, async (t) => {
        const { page, errors, posts } = await setup(t, engine, lang, width);
        const w = words[lang];
        const row = page.locator("#uninstallRow");
        await showRow(page, row);
        assert.match(await row.locator(".name").textContent(), new RegExp(w.row.replace(/[?？]/g, "")));
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, "wider than the window");
        const before = await scrolled(page);
        const go = row.locator("button");
        await go.click();
        await dialog(page).waitFor();
        assert.equal(await scrolled(page), before, "the click moved the page");
        assert.equal(await dialog(page).locator("h2").textContent(), w.title);
        // Cancel does nothing at all
        await dialog(page).getByRole("button", { name: w.cancel, exact: true }).click();
        await dialog(page).waitFor({ state: "detached" });
        assert.deepEqual(posts, []);
        // Uninstall: the agents and the service, then the files on quitting
        await go.click();
        await dialog(page).getByRole("button", { name: w.go, exact: true }).click();
        await waitFor(() => posts.length === 2);
        assert.deepEqual(posts, ["/api/uninstall", "/api/uninstall/finish"]);
        assert.deepEqual(errors, []);
      });
    }
  }

  test(`${engine}: what couldn't be undone is said, and nothing more is removed`, async (t) => {
    const { page, errors, posts } = await setup(t, engine, "en", 1100, {
      reply: { failed: true, says: ["✓ removed background service", "! disconnected Codex: config.toml: permission denied"] },
    });
    await page.locator("#uninstallRow button").click();
    await dialog(page).getByRole("button", { name: "Uninstall", exact: true }).click();
    await dialog(page).getByRole("button", { name: "Try again", exact: true }).waitFor();
    assert.match(await dialog(page).locator("p").textContent(), /disconnected Codex: config\.toml: permission denied/);
    assert.doesNotMatch(await dialog(page).locator("p").textContent(), /background service/);
    await dialog(page).getByRole("button", { name: "Cancel", exact: true }).click();
    await dialog(page).waitFor({ state: "detached" });
    assert.deepEqual(posts, ["/api/uninstall"]);
    assert.equal(await page.locator("#uninstallRow button").isEnabled(), true);
    assert.deepEqual(errors, []);
  });

  test(`${engine}: the Codex app on its old settings is said before uni-switch quits`, async (t) => {
    const { page, errors, posts } = await setup(t, engine, "en", 1100, { reply: { failed: false, codexApp: true, says: [] } });
    await page.locator("#uninstallRow button").click();
    await dialog(page).getByRole("button", { name: "Uninstall", exact: true }).click();
    await dialog(page).getByRole("button", { name: "Quit uni-switch", exact: true }).waitFor();
    assert.deepEqual(posts, ["/api/uninstall"]);
    await dialog(page).getByRole("button", { name: "Quit uni-switch", exact: true }).click();
    await waitFor(() => posts.length === 2);
    assert.deepEqual(posts, ["/api/uninstall", "/api/uninstall/finish"]);
    assert.deepEqual(errors, []);
  });

  test(`${engine}: uni-switch web never offers it`, async (t) => {
    const { page, errors } = await setup(t, engine, "en", 1100, { web: true });
    assert.equal(await page.locator("#uninstallRow").count(), 0);
    assert.doesNotMatch(await page.locator("#about").textContent(), /Uninstall/);
    assert.deepEqual(errors, []);
  });
}

// scrolled to as a reader does, with the wheel: a scroll the page makes
// itself is put back by the hold a click keeps on what was clicked
async function showRow(page, row) {
  await page.locator("#view-settings").hover();
  for (let i = 0; i < 40; i++) {
    const box = await row.boundingBox();
    const v = await page.locator("#view-settings").boundingBox();
    if (box && v && box.y >= v.y + 4 && box.y + box.height <= v.y + v.height - 4) break;
    await page.mouse.wheel(0, 180);
    await page.waitForTimeout(16);
  }
  await page.waitForTimeout(250);
}

async function waitFor(ok) {
  for (const until = Date.now() + 5000; Date.now() < until; await new Promise((r) => setTimeout(r, 20))) if (ok()) return;
  assert.fail("waited for state that never came");
}

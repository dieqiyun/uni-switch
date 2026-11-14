// Run with Node's test runner and Playwright on the module path; see README.md.
// A key's value replaced in place (#1869, wuwei0727 on #1598): in a
// provider's list of several keys, the pencil beside a key's copy button
// ("Change key"), or a double-click on the row, puts a password field
// where its masked value was; Enter posts keys/replace with the key's id
// and the new value, and the row keeps its name and place. Escape leaves
// it as it was, posting nothing. Before, a rotated key had to be removed
// and added again, which lost its name and place. In every language, at a
// wide and a narrow window; nothing scrolls.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const until = new Date(Date.now() + 40 * 60e3).toISOString();
const relay = (replaced) => ({
  id: "relay", name: "Relay", icon: "generic", chat: "https://relay.example.com/v1", responses: "", anthropic: "", catalog: "",
  models: [{ id: "big", name: "", on: true }], agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-…one" }, balanceToken: { takes: false, set: false }, proxy: "",
  keyList: [
    { id: "aaaaaaaaaa", name: "", masked: "sk-…one", on: true, active: true },
    replaced
      ? { id: "cccccccccc", name: "Team", masked: "sk-…new", on: true }
      : { id: "bbbbbbbbbb", name: "Team", masked: "sk-…two", on: true, rest: { why: "auth", status: 401, until, key: "relay#bbbbbbbbbb" } },
    { id: "dddddddddd", name: "Spare", masked: "sk-…six", on: true },
  ],
});

function serve(lang, posts) {
  const state = { agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  let replaced = false;
  const providers = () => ({ providers: [relay(replaced)], presets: [], excluded: [], gateway: { running: true, window: true } });
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/keys/replace") {
      posts.push(JSON.parse(route.request().postData() || "{}"));
      replaced = true;
      return json(providers());
    }
    if (url.pathname === "/api/providers") return json(providers());
    if (url.pathname.startsWith("/api/keys/")) {
      posts.push({ other: url.pathname });
      return json(providers());
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const want = {
  en: { edit: "Change key", hint: "Paste the key's new value", note: "Team now uses its new value" },
  zh: { edit: "更换密钥", hint: "粘贴密钥的新值", note: "Team 已改用新的值" },
  "zh-TW": { edit: "更換金鑰", hint: "貼上金鑰的新值", note: "Team 已改用新的值" },
  ja: { edit: "キーを変更", hint: "キーの新しい値を貼り付け", note: "Team は新しい値を使います" },
  de: { edit: "Schlüssel ändern", hint: "Neuen Wert des Schlüssels einfügen", note: "Team nutzt jetzt den neuen Wert" },
};

const scrolled = (page) => page.evaluate(() => [window.scrollY, ...[...document.querySelectorAll("*")].filter((e) => e.scrollTop > 0).map((e) => `${e.className}:${e.scrollTop}`)].join(" "));
const sideways = (page) => page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1);
// a reader wheels the row into sight and points at it
async function reach(page, row) {
  await page.mouse.move(200, 300);
  for (let i = 0; i < 30 && !(await row.evaluate((e) => e.getBoundingClientRect().bottom < innerHeight - 80)); i++) await page.mouse.wheel(0, 200);
  await page.waitForTimeout(200);
  await row.hover();
}
const at = (loc) => loc.evaluate((e) => { const r = e.getBoundingClientRect(); return e.contains(document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2)); });

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(want)) {
    for (const width of [1100, 420]) {
      const open = async (t, name) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const context = await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], posts = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, posts));
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-${name}.png`), fullPage: true });
          }
          await browser.close();
        });
        await page.goto("http://uni-switch.test/?view=providers");
        await page.locator(".row.provider", { hasText: "Relay" }).first().click();
        const row = page.locator('.acc[data-account-id="bbbbbbbbbb"]');
        await row.waitFor();
        return { page, errors, posts, row };
      };

      test(`${engine} ${lang} ${width}px: Change key puts the new value in place of the key's`, async (t) => {
        const { page, errors, posts, row } = await open(t, "key-replace");
        assert(!(await sideways(page)), "the page scrolls sideways");
        await reach(page, row);
        const edit = row.getByRole("button", { name: want[lang].edit, exact: true });
        assert(await at(edit), "Change key can't be reached");
        const sc = await scrolled(page);
        await edit.click();
        const field = row.locator("input.key-value-in");
        await field.waitFor();
        assert.equal(await field.getAttribute("type"), "password");
        assert.equal(await field.getAttribute("placeholder"), want[lang].hint);
        assert.equal(await field.evaluate((e) => e === document.activeElement), true, "the field isn't focused");
        // in place of the masked value; the name stays
        assert.equal(await row.locator(".plan.mono").count(), 0);
        assert.equal((await row.locator(".n").textContent()).trim(), "Team");
        assert(!(await sideways(page)), "the field scrolls the page sideways");
        await field.fill("sk-new-value");
        await field.press("Enter");
        for (let i = 0; i < 60 && !posts.length; i++) await page.waitForTimeout(50);
        assert.deepEqual(posts, [{ id: "relay", ref: "bbbbbbbbbb", key: "sk-new-value" }]);
        // the row the answer gives, second as before, its rest gone
        const now = page.locator('.acc[data-account-id="cccccccccc"]');
        await now.waitFor();
        assert.equal(await page.locator(".accts.keys .acc[data-account-id]").evaluateAll((rs) => rs.map((r) => r.dataset.accountId).join(" ")), "aaaaaaaaaa cccccccccc dddddddddd");
        assert.equal((await now.locator(".plan.mono").textContent()).trim(), "sk-…new");
        assert.equal(await now.locator(".key-rest").count(), 0);
        await page.waitForFunction((w) => document.querySelector("#status")?.textContent.trim() === w, want[lang].note);
        assert.equal(await scrolled(page), sc, "changing the key scrolled");
        assert.deepEqual(errors, []);
      });

      test(`${engine} ${lang} ${width}px: a double-click on a key's row opens it to change; Escape leaves it`, async (t) => {
        const { page, errors, posts, row } = await open(t, "key-replace-dbl");
        await reach(page, row);
        const sc = await scrolled(page);
        await row.locator(".plan.mono").dblclick();
        const field = row.locator("input.key-value-in");
        await field.waitFor();
        await field.fill("sk-half");
        await field.press("Escape");
        await field.waitFor({ state: "detached" });
        assert.equal((await page.locator('.acc[data-account-id="bbbbbbbbbb"] .plan.mono').textContent()).trim(), "sk-…two");
        await page.waitForTimeout(150);
        assert.equal(await scrolled(page), sc, "the double-click or Escape scrolled");
        // a double-click on a control doesn't. (Its scroll isn't checked: under
        // load the editor's body went back to the top after the copy
        // button's double-click, once in ~300 runs, with no script scrolling
        // it, which is #1504.)
        await page.locator('.acc[data-account-id="bbbbbbbbbb"] .copy').dblclick();
        assert.equal(await page.locator("input.key-value-in").count(), 0);
        await page.waitForTimeout(150);
        assert.deepEqual(posts, []);
        assert.deepEqual(errors, []);
      });
    }
  }
}

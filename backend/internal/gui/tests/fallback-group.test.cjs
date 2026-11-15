// Run with Node's test runner and Playwright on the module path; see README.md.
// A provider's Fallback can be a routing group, not only a model (01huadalang
// on Discord: "备用模型增加路由组的选择，现在只能选模型"). The picker lists
// the groups beside the models, each with the Routing fan and the words
// "routing group"; a group picked is a row with the same tell, and Save sends
// "group/<id>". A group that is gone says it is skipped. In English and
// Chinese; every new string has its zh, zh-TW, ja and de.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const base = { chat: "", responses: "", anthropic: "", catalog: "", agents: [], fallback: [], headers: {}, keyList: [], key: { set: true }, ready: true };
const acme = { ...base, id: "acme", name: "Acme", icon: "generic", models: [{ id: "a1", name: "A1", on: true }], fallback: ["group/gone"] };
const wb = { ...base, id: "wb", name: "WorkBuddy", icon: "generic", models: [{ id: "m1", name: "WB Model 1", on: true }] };
const backup = { id: "group/backup", name: "Backup Lane", icons: ["generic"], providers: ["WorkBuddy"], members: ["wb/m1"], efforts: [] };

function serve(lang, posts) {
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  const providers = { providers: [acme, wb], presets: [], excluded: [], gateway: { running: true, window: true, groups: [backup] } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (route.request().method() === "POST" && url.pathname.startsWith("/api/provider")) {
      posts.push({ path: url.pathname, body: route.request().postDataJSON() });
      return json(providers);
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { tell: "routing group", gone: "No routing group group/gone now; it is skipped", none: "No model or routing group matches", save: "Save" },
  zh: { tell: "路由组", gone: "现在没有路由组 group/gone，会跳过它", none: "没有匹配的模型或路由组", save: "保存" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a routing group can be picked as a fallback`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 900, height: 760 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [], posts = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, posts));
      await page.goto("http://uni-switch.test/?view=providers");
      const W = words[lang];
      const missing = await page.evaluate(() => {
        const keys = ["add a model or routing group: filter, or type provider/model…", "No routing group {id} now; it is skipped", "No model or routing group matches", "routing group"];
        return ["zh", "zh-TW", "ja", "de"].flatMap((l) => keys.filter((k) => !I18N[l]?.[k]).map((k) => l + ": " + k));
      });
      assert.deepEqual(missing, [], "every new string in every language");
      await page.locator(".row.provider", { hasText: "Acme" }).first().click();
      { const adv = page.locator(".editor details.advanced"); await adv.waitFor({ state: "attached" }); if (!(await adv.evaluate((d) => d.open))) await adv.locator(":scope > summary").click(); }
      const box = page.locator(".editor .fallback");
      // a group that is gone: its fan, its id, the tell, and why it is skipped
      const gone = box.locator(".fbrow").first();
      assert.equal(await gone.getAttribute("title"), W.gone);
      assert.equal((await gone.locator(".mtell").textContent()).trim(), W.tell);
      assert.equal(await gone.locator(":scope > svg").count(), 1, "the group's fan, not a provider icon");
      // the picker lists the group beside the models, with its fan and tell
      await box.locator("input").focus();
      const chip = box.locator(".mchips .mchip", { hasText: "Backup Lane" });
      await chip.waitFor();
      assert.equal(await chip.getAttribute("title"), "group/backup");
      assert.equal((await chip.locator(".mtell").textContent()).trim(), W.tell);
      assert.equal(await chip.locator("svg").count(), 1);
      assert.equal(await box.locator(".mchips .mchip", { hasText: "WB Model 1" }).count(), 1, "the models are still there");
      assert.equal(await box.locator("select").count(), 0, "never a native select");
      // a filter finds a group by its member providers too, and says when nothing matches
      await box.locator("input").fill("workbuddy");
      assert.equal(await chip.count(), 1);
      await box.locator("input").fill("zzz");
      assert.equal((await box.locator(".mchips .hint").textContent()).trim(), W.none);
      await box.locator("input").fill("");
      await box.locator("input").focus();
      await chip.click();
      const row = box.locator(".fbrow").nth(1);
      assert.equal((await row.locator(".n").textContent()).trim(), "Backup Lane");
      assert.equal((await row.locator(".mtell").textContent()).trim(), W.tell);
      assert.equal(await row.getAttribute("title"), null);
      // no coloured left-border accent on a group row
      assert.equal(await row.evaluate((e) => getComputedStyle(e).borderLeftWidth), "0px");
      const sent = page.waitForRequest((r) => r.method() === "POST" && new URL(r.url()).pathname.startsWith("/api/provider"));
      await page.getByRole("button", { name: W.save, exact: true }).click();
      await sent;
      assert.deepEqual(posts.at(-1).body.fallback, ["group/gone", "group/backup"]);
      assert.deepEqual(errors, []);
    });
  }
}

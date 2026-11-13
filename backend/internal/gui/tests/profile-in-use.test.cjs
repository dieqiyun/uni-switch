// Run with Node's test runner and Playwright on the module path; see README.md.
// Which profile is in use (superhtoooth on X, macOS: 保存方案设置了方案名字，
// 但是应用方案没有用这个名字。menu 里面变成了方案 1，方案 2。而且在应用里面
// 没有显示当前使用的方案). The tray panel's Profiles button read
// "方案 1", "方案 2": the number of profiles, read as the name the profile
// had been saved under. With a profile in use (state's inUse: applying it
// would change nothing) the button names it, as a pop-up button names what
// is picked; its chip is ticked, in the panel and in the window, and its
// details say it is in use. With none in use the button counts them, as
// before. The reporter's own steps: save a profile under a name typed, then
// apply another from the button's list. Every language, both engines, the
// panel at its narrowest. No backend: the API is faked here, and which
// profile is in use is uni-switch's to say (profile.InUse, tested in Go).
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { reach } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const models = ["model-a", "model-b"].map((m) => ({ value: m, label: m }));
const agent = (id, model) => ({
  id, name: id, path: "/test/" + id, fields: [{ key: "model", label: "model", value: model, options: models }],
});

// server: profiles saved by name, each holding codex's model; the one whose
// model codex has now is in use, as uni-switch decides it
function server(lang) {
  let model = "model-b";
  const profiles = [{ name: "工作", holds: "model-a" }, { name: "游戏", holds: "model-b-x" }];
  const state = () => ({
    agents: [agent("codex", model)],
    profiles: profiles.map((p) => ({
      name: p.name, summary: "codex " + p.holds, inUse: p.holds === model,
      agents: [{ id: "codex", name: "Codex", fields: [{ key: "model", label: "model", value: p.holds }] }],
    })),
    settings: { lang, theme: "light" },
  });
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: state() });
    if (url.pathname === "/api/profile/save") {
      const { name } = req.postDataJSON();
      profiles.push({ name, holds: model });
      profiles.sort((a, b) => (a.name < b.name ? -1 : 1));
      return route.fulfill({ json: state() });
    }
    if (url.pathname === "/api/profile/use") {
      const { name } = req.postDataJSON();
      model = profiles.find((p) => p.name === name).holds;
      return route.fulfill({ json: { ...state(), changed: 1 } });
    }
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname === "/api/window/fit") return route.fulfill({ status: 204 });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const inUse = { en: "In use", zh: "使用中", "zh-TW": "使用中", ja: "使用中", de: "In Verwendung" };
const label = { en: "Profiles", zh: "方案", "zh-TW": "方案", ja: "プロファイル", de: "Profile" };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": the profile in use", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(() => browser.close());
    const open = async (lang, mode, width) => {
      const page = await (await browser.newContext({ viewport: { width, height: 460 } })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(lang));
      await page.goto("http://uni-switch.test/" + (mode === "panel" ? "?mode=panel" : ""));
      await page.locator("#profiles .chip").first().waitFor({ state: "attached" });
      return page;
    };
    // a chip by its name, clicked on the name (its ↻ and × are its own)
    const chip = (page, name) => page.locator("#profiles .chip > span", { hasText: new RegExp("^" + name + "$") });
    const ticked = (page) => page.locator("#profiles .chip.cur").evaluateAll((cs) => cs.map((c) => c.querySelector(".cur-tick + span").textContent));

    for (const lang of Object.keys(inUse)) {
      await t.test(lang + ": panel, the reporter's steps", async () => {
        const page = await open(lang, "panel", 360);
        const btn = page.locator("#profBtn");
        // none in use: the button counts them, and no chip is ticked
        assert.equal((await btn.textContent()).replace(/\s+/g, " ").trim(), label[lang] + "2");
        assert.equal(await page.locator("#profBtn em.cur").count(), 0);
        await btn.click();
        await page.locator(".profiles.open").waitFor();
        assert.deepEqual(await ticked(page), []);

        // saved under a name typed: it is the setup now, and the button names it
        await page.locator("#save").click();
        const field = page.locator(".profiles > .chip-input");
        await field.pressSequentially("周末 setup");
        await field.press("Enter");
        await page.waitForFunction(() => document.querySelector("#profBtn em")?.textContent === "周末 setup");
        assert.deepEqual(await ticked(page), ["周末 setup"]);
        assert.equal(await page.locator("#profiles .chip.cur").getAttribute("aria-current"), "true");
        assert.equal(await page.locator("#profBtn em").getAttribute("title"), inUse[lang]);

        // another applied from the list: the button and the tick move to it
        await chip(page, "工作").click();
        assert.equal(await page.locator(".prof-detail .pd-cur").count(), 0, "工作 isn't in use yet");
        await page.locator(".prof-detail .pd-apply").click();
        await page.waitForFunction(() => document.querySelector("#profBtn em")?.textContent === "工作");
        await btn.click();
        await page.locator(".profiles.open").waitFor();
        assert.deepEqual(await ticked(page), ["工作"]);
        await chip(page, "工作").click();
        assert.equal((await page.locator(".prof-detail .pd-cur").textContent()).trim(), inUse[lang]);

        // at the panel's narrowest the name stays on the foot's one line
        const fit = await page.evaluate(() => {
          const b = document.querySelector("#profBtn").getBoundingClientRect(), f = document.querySelector(".foot").getBoundingClientRect();
          return { inside: b.right <= f.right && b.top >= f.top - 1 && b.bottom <= f.bottom + 1, wide: document.documentElement.scrollWidth <= innerWidth };
        });
        assert(fit.inside && fit.wide, JSON.stringify(fit));
      });

      await t.test(lang + ": window", async () => {
        const page = await open(lang, "window", 1100);
        // under the Get started card (3e88fc09d) the chips are past the
        // window's foot: the reader scrolls down to them
        await reach(page, chip(page, "游戏"));
        await chip(page, "游戏").click();
        await reach(page, page.locator(".prof-detail .pd-apply"));
        await page.locator(".prof-detail .pd-apply").click();
        await page.waitForFunction(() => document.querySelector("#profiles .chip.cur"));
        assert.deepEqual(await ticked(page), ["游戏"]);
        const tip = await page.locator("#profiles .chip.cur").getAttribute("title");
        assert.equal(tip.split("\n")[0], inUse[lang]);
      });
    }
    assert.deepEqual(errors, []);
  });
}

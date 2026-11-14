// Run with Node's test runner and Playwright on the module path; see README.md.
// A provider editor serving Picked only shows the models picked, not every
// model with the picks ticked (yyddbds on Discord: 这个模型分类选择好像没有用，
// 选择了仅所选分类，下边的模型项并没有变化 — OpenCode Zen's 8 free models, 3
// ticked, all 8 still shown under 仅所选). The others wait behind Show the
// other n; a click on a model while picking leaves them all in sight, and
// Picked only clicked again folds them away. Vendor's list and Usage only
// show every model, and Picked only after them gives the picks back (to
// the list and back, every model came back picked). In every language,
// Chromium and WebKit, at 1100 and 420.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const fetched = new Date(Date.now() - 3600e3).toISOString();
// the reporter's list, as the screenshot has it
const names = [["big-pickle", "Big Pickle"], ["beam-free", "Beam Free"], ["flint-40b-free", "Flint 40B Free"], ["mimo-v2.6-flash-free", "MiMo-V2.6-Flash Free"], ["nemotron-3.5-super-vl-free", "Nemotron 3.5 Super VL Free"], ["nemotron-3.5-lightning-free", "Nemotron 3.5 Lightning Free"], ["deepseek-v4.1-flash-free", "DeepSeek V4.1 Flash Free"], ["glm-5.3-flash-free", "GLM-5.3-Flash Free"]];
const picked = ["mimo-v2.6-flash-free", "deepseek-v4.1-flash-free", "glm-5.3-flash-free"];
const zen = {
  id: "opencode-zen", name: "OpenCode Zen", icon: "generic", host: "opencode.ai", chat: "https://opencode.ai/zen/v1", responses: "", anthropic: "", catalog: "",
  models: names.map(([id, name]) => ({ id, name, free: true, on: picked.includes(id) })), chosen: picked, fetched, agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-…zen" }, keyList: [], balanceToken: { takes: false, set: false }, proxy: "",
};

function serve(lang) {
  const providers = { providers: [zen], presets: [], excluded: [], gateway: { running: true, window: true } };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { rest: "Show the other 5" },
  zh: { rest: "显示其余 5 个" },
  "zh-TW": { rest: "顯示其餘 5 個" },
  ja: { rest: "残り 5 個を表示" },
  de: { rest: "Die übrigen 5 anzeigen" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(words)) {
    const w = words[lang];
    for (const width of [1100, 420]) {
      test(`${engine} ${lang} ${width}: Picked only shows the models picked`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const page = await (await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" })).newPage();
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-provider-picked-only.png`) });
          }
          await browser.close();
        });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        await page.goto("http://uni-switch.test/?view=providers");
        await page.locator(".row.provider", { hasText: "OpenCode Zen" }).click();
        const editor = page.locator("#modal:not([hidden]) .editor");
        const chips = editor.locator(".mchips .mchip");
        const shown = () => chips.evaluateAll((cs) => cs.map((c) => c.firstChild.textContent));
        const nameOf = (id) => names.find(([x]) => x === id)[1];
        const rest = editor.locator(".mchips .mchips-rest");
        const seg = (v) => editor.locator(`.mserves .opt[data-serves=${v}]`);
        await chips.first().waitFor();
        assert.equal(await seg("picked").getAttribute("aria-pressed"), "true", "opened on Picked only");

        // opened on Picked only: the 3 picks, ticked, and the other 5 behind a button
        assert.deepEqual(await shown(), picked.map(nameOf));
        assert.equal(await editor.locator(".mchips .mchip:not(.on)").count(), 0, "every chip shown is picked");
        assert.equal((await rest.innerText()).trim(), w.rest);
        const box = await rest.boundingBox(), ed = await editor.boundingBox();
        assert.ok(box && box.x >= ed.x - 1 && box.x + box.width <= ed.x + ed.width + 1, "the button fits the editor");

        // to the vendor's list and back (yyddbds' second report: back on
        // Picked only, every model came back picked), and by Usage only: the
        // same 3 picks, the others behind the button
        for (const away of ["list", "none"]) {
          await seg(away).click();
          assert.deepEqual(await shown(), names.map(([, n]) => n), away + ": every model");
          await seg("picked").click();
          assert.deepEqual(await shown(), picked.map(nameOf), "back from " + away + ": the picks, not the list");
          assert.equal(await editor.locator(".mchips .mchip:not(.on)").count(), 0);
          assert.equal((await rest.innerText()).trim(), w.rest);
        }

        // shown, all 8 are there to pick from, and a pick keeps them in sight
        await rest.click();
        assert.deepEqual(await shown(), names.map(([, n]) => n));
        assert.equal(await rest.count(), 0);
        await chips.filter({ hasText: "Big Pickle" }).click();
        await chips.filter({ hasText: "MiMo-V2.6-Flash Free" }).click();
        assert.deepEqual(await shown(), names.map(([, n]) => n), "nothing vanishes while picking");
        assert.equal(await seg("picked").getAttribute("aria-pressed"), "true");

        // Picked only clicked folds the unpicked away again
        await seg("picked").click();
        assert.deepEqual(await shown(), ["Big Pickle", "DeepSeek V4.1 Flash Free", "GLM-5.3-Flash Free"]);
        assert.equal((await rest.innerText()).trim(), w.rest);

        // Vendor's list: every model, the first 24 drawn as served
        await seg("list").click();
        assert.deepEqual(await shown(), names.map(([, n]) => n));
        assert.equal(await editor.locator(".mchips .mchip.auto").count(), 8);
        assert.equal(await rest.count(), 0);
        // Usage only: every model, none picked, to pick from
        await seg("none").click();
        assert.deepEqual(await shown(), names.map(([, n]) => n));
        assert.equal(await editor.locator(".mchips .mchip.on, .mchips .mchip.auto").count(), 0);
        assert.equal(await rest.count(), 0);
        // and a model clicked from there leaves the rest in sight
        await chips.filter({ hasText: "Beam Free" }).click();
        assert.deepEqual(await shown(), names.map(([, n]) => n));
        // Picked only from there: that one pick, the other 7 behind the button
        await seg("picked").click();
        assert.deepEqual(await shown(), ["Beam Free"]);
        assert.equal((await rest.innerText()).trim(), w.rest.replace("5", "7"));
        assert.deepEqual(errors, []);
      });
    }
  }
}

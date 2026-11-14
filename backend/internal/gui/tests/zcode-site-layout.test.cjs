// Run with Node's test runner and Playwright on the module path; see README.md.
// ZCode's site step in the Add sheet keeps its question readable (361): the
// two sites and the plugin's other ways (the ZCode app's sign-in, the API
// key) once stood in the question's row, which left the question a strip a
// character or two wide and the box very tall. Now the question keeps the
// row, the sites are a group under it and the other ways links under them,
// wrapping. At the tray panel's width (440), 700 and 960px, en and zh: the
// question is wide, no button runs out of the box, the sites share a line
// and each way still signs in. The API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

const plugins = [
  { id: "zcode", pid: "zcode", name: "ZCode", icon: "generic", spec: "the retired opencode-zcode-auth", signedIn: false, models: 3,
    methods: [{ type: "oauth", label: "ZCode: Z.ai GLM Coding Plan" }, { type: "oauth", label: "ZCode: BigModel (智谱) GLM Coding Plan" }, { type: "oauth", label: "ZCode app's sign-in" }, { type: "api", label: "GLM Coding Plan API key" }] },
];

function server(lang, mode, asked) {
  const payload = () => ({ providers: [{ id: "deepseek", name: "DeepSeek", icon: "deepseek", chat: "https://api.deepseek.com/v1", models: [], agents: [], key: {} }], presets: [], excluded: [], gateway: { running: true, window: mode === "window" }, plugins, onPlugins: ["zcode"] });
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    const body = () => route.request().postDataJSON();
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json(payload());
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname === "/api/plugin-signin/prompt") return json({ prompt: null, inputs: {} });
    if (url.pathname === "/api/plugin-signin") { const b = body(); asked.push(b); return json({ id: "s" + asked.length, agent: b.provider, state: "waiting", url: "https://fake.test/device", code: "" }); }
    if (/^\/api\/signin\/s\d+\/cancel$/.test(url.pathname)) return json({});
    if (/^\/api\/signin\/s\d+$/.test(url.pathname)) return new Promise(() => {}); // stays waiting
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const sizes = [["window", 440, 640], ["window", 700, 760], ["window", 960, 760]];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": ZCode's site step keeps its question readable", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh"]) for (const [mode, width, height] of sizes) {
      await t.test(`${lang} ${mode} ${width}`, async () => {
        const page = await (await browser.newContext({ viewport: { width, height }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], asked = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, mode, asked));
        await page.goto(`http://uni-switch.test/?view=providers&mode=${mode}`);
        await page.locator("#addProvider").click();
        const sheet = page.locator("#addSheet");
        const box = sheet.locator(".signing");
        await sheet.locator('.tile[data-pick="ZCode"]').click();
        await box.locator('button[data-site="bigmodel"]').waitFor();
        // in a short window the step is under the tiles: the reader scrolls to it
        // (with the wheel: uni-switch puts back a scroll the reader didn't make)
        await page.mouse.move(width / 2, height / 2);
        for (let i = 0; i < 20 && await box.evaluate((b) => b.getBoundingClientRect().bottom > innerHeight - 8); i++) {
          await page.mouse.wheel(0, 120);
          await page.waitForTimeout(60);
        }

        const m = await box.evaluate((b) => {
          const r = (e) => e.getBoundingClientRect();
          const tt = b.querySelector(".tt"), q = b.querySelector(".tt .s");
          return {
            box: r(b).width, tt: r(tt).width, height: r(b).height,
            lineHeight: parseFloat(getComputedStyle(q).lineHeight) || 16, q: r(q).height,
            sites: [...b.querySelectorAll("button[data-site]")].map((e) => r(e).top),
            out: [...b.querySelectorAll("button")].filter((e) => r(e).left < r(b).left - 0.5 || r(e).right > r(b).right + 0.5 || e.scrollWidth > e.clientWidth + 1).map((e) => e.textContent),
          };
        });
        if (process.env.ARTIFACT_DIR) await box.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `zcode-site-${engine}-${lang}-${width}.png`) });
        // the question takes most of the row, not a strip
        assert.ok(m.box > 300, `box ${m.box}px wide`);
        assert.ok(m.tt >= m.box * 0.55,`question ${m.tt}px of a ${m.box}px box`);
        assert.ok(m.q <= m.lineHeight * 4.5, `question ${m.q}px tall (line ${m.lineHeight}px)`);
        assert.ok(m.height < 260, `box ${m.height}px tall`);
        assert.deepEqual(m.out, [], "a button runs out of the box");
        // the two sites are one group on one line
        assert.equal(m.sites.length, 2);
        assert.ok(Math.abs(m.sites[0] - m.sites[1]) < 2, "the sites share a line");
        // the other ways are still there and still sign in
        assert.equal(await box.locator("button[data-method]").count(), 2);
        const y = await page.evaluate(() => [scrollY, ...[...document.querySelectorAll("*")].filter((e) => e.scrollTop).map((e) => e.scrollTop)].join());
        // clicked where it is, as the reader does: Playwright's own scroll into
        // view moved a 440px window's page by 3px before its click
        const b = await box.locator('button[data-method="2"]').boundingBox();
        await page.mouse.click(b.x + b.width / 2, b.y + b.height / 2);
        await page.waitForTimeout(150);
        assert.deepEqual(asked, [{ provider: "zcode", method: 2, inputs: {} }]);
        assert.equal(await page.evaluate(() => [scrollY, ...[...document.querySelectorAll("*")].filter((e) => e.scrollTop).map((e) => e.scrollTop)].join()), y, "the click scrolled");
        assert.deepEqual(errors, []);
      });
    }
  });
}

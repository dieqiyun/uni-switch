// Run with Node's test runner and Playwright on the module path; see README.md.
// A weighted key's row fits (#841, dengtongcai): with By weight on, each
// key's name, its masked key and its weight are shown side by side, and
// its name is never squeezed to nothing by them; "First" stays one pill
// on one line and "Make first" stays inside the card. In a wide window
// and a narrow one, in English and Chinese.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const relay = () => ({
  id: "relay", name: "Relay", icon: "generic", chat: "https://relay.example.com/v1", responses: "", anthropic: "", catalog: "",
  models: [{ id: "big", name: "", on: true }], agents: [], fallback: [], headers: {}, routing: "weight",
  key: { set: true, masked: "2190…9335" }, balanceToken: { takes: false, set: false }, proxy: "",
  keyList: [
    { id: "aaaaaaaaaa", name: "个人335", masked: "2190…9335", on: true, active: true, weight: 1 },
    { id: "bbbbbbbbbb", name: "团队857", masked: "1675…4857", on: true, weight: 1 },
  ],
});

function serve(lang) {
  const p = relay();
  const providers = { providers: [p], presets: [], excluded: [], gateway: { running: true, window: true } };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    for (const width of [900, 640, 520, 440]) {
      test(`${engine} ${lang} ${width}px: a weighted key's name, key and weight fit its row`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const page = await (await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" })).newPage();
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.locator(".editor .accts").screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-key-weight-fit.png`) }).catch(() => {});
          }
          await browser.close();
        });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        await page.goto("http://uni-switch.test/?view=providers");
        await page.locator(".row.provider", { hasText: "Relay" }).first().click();
        await page.locator(".editor .accts.weighted .acc .key-weight").first().waitFor();

        for (const id of ["aaaaaaaaaa", "bbbbbbbbbb"]) {
          const m = await page.evaluate((id) => {
            const row = document.querySelector(`.editor .acc[data-account-id="${id}"]`);
            const box = row.closest(".accts").getBoundingClientRect();
            // lines: how many lines its text is set on
            const lines = (e) => { const g = document.createRange(); g.selectNodeContents(e); return new Set([...g.getClientRects()].filter((q) => q.width > 0).map((q) => Math.round(q.top))).size; };
            const r = (s) => { const e = row.querySelector(s); if (!e) return null; const b = e.getBoundingClientRect(); const g = document.createRange(); g.selectNodeContents(e); const st = getComputedStyle(e); return { left: b.left, right: b.right, sw: e.scrollWidth, cw: e.clientWidth, lines: lines(e), text: g.getBoundingClientRect().width, room: e.clientWidth - parseFloat(st.paddingLeft) - parseFloat(st.paddingRight) }; };
            return { box: { left: box.left, right: box.right }, name: r(".n"), plan: r(".plan"), weight: r(".key-weight"), using: r(".using"), first: r("button.text:not(.quiet)") };
          }, id);
          // the name is shown whole: neither clipped nor cut to "…"
          assert.ok(m.name.cw >= m.name.sw - 1, `${id}: its name is cut (${m.name.cw} of ${m.name.sw}px)`);
          assert.ok(m.name.cw > 0, `${id}: its name is squeezed to nothing`);
          // with room for it, the masked key isn't cut to "…" either
          if (width >= 640) assert.ok(m.plan.text <= m.plan.room + 0.5, `${id}: its masked key is cut (${m.plan.text} in ${m.plan.room}px)`);
          for (const k of ["name", "plan", "weight", "using", "first"]) {
            if (!m[k]) continue;
            assert.ok(m[k].right <= m.box.right + 0.5 && m[k].left >= m.box.left - 0.5, `${id}: ${k} runs out of the card (${m[k].left}–${m[k].right} in ${m.box.left}–${m.box.right})`);
          }
          // the badge is one pill on one line
          if (m.using) assert.equal(m.using.lines, 1, `${id}: "First" is broken over ${m.using.lines} lines`);
          if (m.first) assert.equal(m.first.lines, 1, `${id}: "Make first" is broken over ${m.first.lines} lines`);
          if (m.first) assert.ok(m.first.cw >= m.first.sw - 1, `${id}: "Make first" is cut`);
        }
        // hovered, Remove shows whole inside the card, and the name is still there
        await page.locator('.editor .acc[data-account-id="bbbbbbbbbb"] .key-weight').hover();
        await page.waitForTimeout(250);
        const hov = await page.evaluate(() => {
          const row = document.querySelector('.editor .acc[data-account-id="bbbbbbbbbb"]');
          const box = row.closest(".accts").getBoundingClientRect();
          const rm = row.querySelector(".text.quiet:not(.key-switch)"), n = row.querySelector(".n");
          const b = rm.getBoundingClientRect();
          return { opacity: getComputedStyle(rm).opacity, cw: rm.clientWidth, sw: rm.scrollWidth, right: b.right, boxRight: box.right, name: n.clientWidth };
        });
        assert.equal(hov.opacity, "1", "Remove isn't shown on hover");
        assert.ok(hov.cw > 0 && hov.cw >= hov.sw - 1, `Remove is cut on hover (${hov.cw} of ${hov.sw}px)`);
        assert.ok(hov.right <= hov.boxRight + 0.5, "Remove runs out of the card on hover");
        assert.ok(hov.name > 0, "hovering squeezes the name to nothing");
        await page.mouse.move(0, 0);

        // nothing in the card scrolls sideways
        const over = await page.evaluate(() => [...document.querySelectorAll(".editor .accts .acc")].filter((e) => e.scrollWidth > e.clientWidth + 1).map((e) => `${e.dataset.accountId}:${e.scrollWidth}/${e.clientWidth}`));
        assert.deepEqual(over, [], "a row overflows its card");
        // nothing has a coloured left border
        const borders = await page.evaluate(() => [...document.querySelectorAll(".editor .accts *")].filter((e) => { const s = getComputedStyle(e); return parseFloat(s.borderLeftWidth) > 1 && s.borderLeftStyle !== "none"; }).map((e) => e.className));
        assert.deepEqual(borders, []);
        assert.deepEqual(errors, []);
      });
    }
  }
}

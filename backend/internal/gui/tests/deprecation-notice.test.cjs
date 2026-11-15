// Run with Node's test runner and Playwright on the module path; see README.md.
// The deprecation notice over the Providers list is as calm as the list's
// rows (the owner: 这个太丑了 — it was an amber-tinted box with a puzzle tile, a
// Deprecated badge, three lines and a purple button): the card's own
// background and border, an amber dot, the line naming what is deprecated
// with why under it in grey, and Not now and the way on side by side on the
// right, each on one line; on a phone they go under the text, at its end,
// with nothing wider than the page. In English and Chinese, light and dark,
// Chromium and WebKit; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const subs = [["devin", "Devin"], ["workbuddy-ai", "WorkBuddy AI"]];
const pkg = (id) => `@uni-switch-community/opencode-${id}-auth`;

function serve(lang, theme) {
  const providers = subs.map(([id, name]) => ({
    id, name, icon: id, chat: "", responses: "", anthropic: "", catalog: "", models: [{ id: "m", name: "M", on: true }],
    agents: [], fallback: [], headers: {}, keyList: [], key: {},
    account: { agent: id, agentName: name, user: "ada", logins: [{ user: "ada", active: true, on: true, own: true }] },
    move: { package: pkg(id), state: "" },
  }));
  const market = {
    listings: subs.map(([id, name]) => ({ package: pkg(id), name, icon: id, providers: [id], community: true, replaces: id, summary: { en: name, zh: name }, npm: { version: "0.1.0" } })),
    state: { bun: true, bunVersion: "1.3.0", plugins: [], movable: subs.map(([id, name]) => ({ id, name, package: pkg(id), accounts: 1 })) },
  };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"${theme}",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme } });
    if (url.pathname === "/api/providers") return json({ providers, presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [], onPlugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json(market.state);
    if (url.pathname === "/api/plugins/listings") return json({ listings: market.listings });
    if (url.pathname === "/api/plugins/market") return json(market);
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const L = { en: { head: "These built-in subscriptions are deprecated: Devin, WorkBuddy AI", look: "Review in Plugins", later: "Not now" },
  zh: { head: "以下内置订阅已弃用：Devin、WorkBuddy AI", look: "前往插件页迁移", later: "暂不" } };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    for (const theme of ["light", "dark"]) {
      test(`${engine} ${lang} ${theme}: the deprecation notice is a quiet line like the list's rows`, async (t) => {
        const w = L[lang];
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await (await browser.newContext({ viewport: { width: 1140, height: 700 }, reducedMotion: "reduce", colorScheme: theme })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, theme));
        await page.goto("http://uni-switch.test/?view=providers");
        const notice = page.locator("#movable .deprecation");
        await notice.waitFor();
        await page.locator("#providers .row.provider").first().waitFor();
        const shot = async (name) => {
          if (!process.env.ARTIFACT_DIR) return;
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${theme}-${name}.png`), clip: await page.locator("#view-providers").boundingBox() });
        };
        await page.waitForTimeout(300); // its fade-in
        await shot("wide");

        const look = () => notice.evaluate((n) => {
          const list = getComputedStyle(document.querySelector("#providers")), cs = getComputedStyle(n), box = n.getBoundingClientRect();
          const head = n.querySelector(".dep-head").getBoundingClientRect();
          const btns = [...n.querySelectorAll("button")].map((b) => { const r = b.getBoundingClientRect(); return { text: b.textContent, top: r.top, bottom: r.bottom, right: r.right, h: r.height, lh: parseFloat(getComputedStyle(b).lineHeight) || 16 }; });
          const dot = n.querySelector(".dep-dot");
          return {
            bg: cs.backgroundColor, listBg: list.backgroundColor, border: cs.borderTopColor, listBorder: list.borderTopColor,
            leftBorder: cs.borderLeftWidth === cs.borderTopWidth && cs.borderLeftColor === cs.borderTopColor,
            badges: n.querySelectorAll(".badge").length, tiles: n.querySelectorAll(".dep-ic, svg").length, primary: n.querySelectorAll(".primary").length,
            dot: dot ? getComputedStyle(dot).backgroundColor : null, amber: getComputedStyle(document.body).getPropertyValue("--amber").trim(),
            head: { top: head.top, bottom: head.bottom, right: head.right }, box: { left: box.left, right: box.right, top: box.top, bottom: box.bottom }, btns,
            wide: document.documentElement.scrollWidth > innerWidth, text: n.innerText,
          };
        });
        const a = await look();
        assert.equal(a.bg, a.listBg, "the notice isn't on the list's own background");
        assert.equal(a.border, a.listBorder, "the notice's border isn't the list's");
        assert(a.leftBorder, "a left edge of its own");
        assert.equal(a.badges, 0, "a badge in the notice");
        assert.equal(a.tiles, 0, "an icon tile in the notice");
        assert.equal(a.primary, 0, "a filled primary button in the notice");
        assert(a.dot, "no dot");
        assert(a.text.includes(w.head), a.text);
        if (lang === "zh") assert(!/。 /.test(a.text), "a space after a Chinese full stop");
        assert.deepEqual(a.btns.map((b) => b.text), [w.later, w.look]);
        for (const b of a.btns) assert(b.h <= 30 && b.bottom <= a.box.bottom && b.right <= a.box.right, `${b.text} wraps or spills: ${JSON.stringify(b)}`);
        // side by side on the right, beside the text
        assert(a.btns[0].right < a.btns[1].right && Math.abs(a.btns[0].top + a.btns[0].h / 2 - (a.btns[1].top + a.btns[1].h / 2)) <= 1, JSON.stringify(a.btns));
        assert(a.btns[0].right > a.head.right, "the buttons aren't right of the text");

        // a phone: under the text, at its end, nothing wider than the page
        await page.setViewportSize({ width: 390, height: 760 });
        await page.waitForTimeout(150);
        await shot("phone");
        const p = await look();
        assert(!p.wide, "the page scrolls sideways");
        // phone pages make touch targets 44px tall (phone-web.test.cjs)
        for (const b of p.btns) assert(b.h <= 46 && b.right <= p.box.right && b.top >= p.head.bottom, `${b.text} on a phone: ${JSON.stringify(b)}`);
        assert(p.box.right - p.btns[1].right <= 14, "the way on isn't at the end");
        assert.deepEqual(errors, []);
      });
    }
  }
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// The repositories tagged uni-switch-plugin on GitHub are many and of every
// kind (the owner: 这里应该有分类以及搜索框方便用户筛选), so their section
// has its kinds to pick from, each with how many there are, and a field of
// its own that narrows them by name, author or description as it is typed.
// Both narrow what the page's search already found, the field keeps its
// focus and its node while the page is drawn again under it (npm's answer
// comes while typing), nothing is left over at 420px, and a filter that
// leaves nothing says so with a Show all. What's installed is the other
// question the section is read with (the owner: 加一个未安装), so a second
// strip beside the kinds picks it out, its counts following the kinds and
// the field as they are picked, and the two narrow the grid together; a
// list with nothing installed, or nothing else, has no such strip. The
// repositories are the dev instance's real /api/plugins/github answer of
// 2026-10-13 (31: 27 providers, 3 middleware, an agent); the API is faked
// and no request leaves the page. English, Chinese, Traditional Chinese, Japanese and
// German, 1100px, 980px and 420px.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const GH = require("./fixtures/plugins-github-31.json");
// two of them already here, a provider and a middleware, installed by the
// spec the page would send for each: the strip says both sides of it, and
// the kinds narrow it either way
const HERE = ["mu-zi-lee/uni-switch-our-free-model", "ezdemo/uni-switch-haiku55-thinking"];
const INSTALLED = HERE
  .map((repo) => GH.repos.find((r) => r.repo === repo))
  .map((r) => ({ spec: r.spec, package: r.package, providers: [], version: "1.0.0", moved: [] }));

function server(lang, fetched, installed = INSTALLED) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname === "/api/plugins") return json({ bun: true, bunVersion: "1.4.2", plugins: installed, picker: false, movable: [] });
    if (url.pathname === "/api/plugins/listings") return json({ listings: [{ package: "the retired opencode-zed-auth", name: "Zed", community: true }] });
    // npm answers late, while the field is being typed in: the page is drawn again under it
    if (url.pathname === "/api/plugins/npm") { await new Promise((r) => setTimeout(r, 1500)); return json({ npm: {} }); }
    if (url.pathname === "/api/plugins/github") return json(GH);
    if (url.pathname === "/api/plugins/search") return json({ hits: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    if (url.host !== "uni-switch.test" || !/^\/[\w./-]*$/.test(url.pathname)) { fetched.push(url.href); return route.fulfill({ status: 404, body: "" }); }
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const kindOf = (r) => r.kind === "middleware" ? "Middleware" : r.kind === "agent" ? "Agent" : "Provider";
const names = (rs) => rs.map((r) => r.repo.split("/")[1]).sort();
const has = (r, s) => [r.repo, r.owner, r.package || "", r.description || ""].join(" ").toLowerCase().includes(s.toLowerCase());
// the two the fixture installs; that the page tells them apart the same
// way is checked against its own cards
const here = (r) => HERE.includes(r.repo);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": the plugins on GitHub are narrowed by kind and by a field of their own", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    t.after(() => browser.close());
    // nothing from GitHub installed: Not installed would be All again, so
    // there is no strip to pick it from, only the kinds and the field
    await t.test("nothing installed", async () => {
      const ctx = await browser.newContext({ viewport: { width: 980, height: 860 } });
      const page = await ctx.newPage();
      page.setDefaultTimeout(6000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [], fetched = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server("en", fetched, []));
      try {
        await page.goto("http://uni-switch.test/?view=plugins");
        const sec = page.locator("#view-plugins .pm-ghsec");
        await sec.locator(".pm-card").first().waitFor();
        assert.equal(await sec.locator(".pm-ghkinds").count(), 1);
        assert.equal(await sec.locator(".pm-ghhave").count(), 0);
        assert.equal(await sec.locator(".pm-card").count(), GH.repos.length);
        assert.deepEqual(errors, []);
      } finally {
        await ctx.close();
      }
    });
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) for (const width of [1100, 980, 420]) {
      await t.test(`${lang} ${width}px`, async () => {
        const ctx = await browser.newContext({ viewport: { width, height: 860 } });
        const page = await ctx.newPage();
        page.setDefaultTimeout(6000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], fetched = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, fetched));
        try {
          await page.goto("http://uni-switch.test/?view=plugins");
          const sec = page.locator("#view-plugins .pm-ghsec");
          const cards = sec.locator(".pm-card");
          await cards.first().waitFor();
          assert.equal(await cards.count(), 31);
          const tr = (s) => page.evaluate((s) => t(s), s);
          if (lang !== "en") for (const s of ["Kind of plugin", "Installed or not", "Not installed", "Filter by name, author or description", "Filter the plugins on GitHub", "No plugin on GitHub matches"]) assert.notEqual(await tr(s), s, `${s} in ${lang}`);
          const want = async (rs) => [await tr("All") + " · " + rs.length, ...(await Promise.all(["Provider", "Middleware", "Agent"].map(async (k) => (await tr(k)) + " · " + rs.filter((r) => kindOf(r) === k).length)))];
          const kinds = sec.locator(".pm-ghkinds .opt");
          assert.deepEqual(await kinds.allTextContents(), await want(GH.repos));
          // and beside them what's installed, over the very repositories:
          // two of them, a provider and a middleware, are already here, as
          // the page's own cards say
          const have = sec.locator(".pm-ghhave .opt");
          const wantHave = async (rs) => [await tr("All"), await tr("Installed") + " · " + rs.filter(here).length, await tr("Not installed") + " · " + rs.filter((r) => !here(r)).length];
          assert.deepEqual(await have.allTextContents(), await wantHave(GH.repos));
          assert.equal(await sec.locator(".pm-ghhave .opt.on").textContent(), (await wantHave(GH.repos))[0]);
          assert.deepEqual((await sec.locator(".pm-card.have .pm-name b").allInnerTexts()).sort(), names(GH.repos.filter(here)));
          // both strips and the field, nothing left over at this width: the
          // kinds in Japanese are wider than the bar at 420px
          const overflow = () => page.evaluate(() => {
            const bar = document.querySelector("#view-plugins .pm-ghbar");
            const v = document.querySelector("#view-plugins");
            return [bar.scrollWidth - bar.clientWidth, v.scrollWidth - v.clientWidth, document.documentElement.scrollWidth - innerWidth];
          });
          assert.deepEqual((await overflow()).map((x) => Math.max(0, x)), [0, 0, 0], JSON.stringify(await overflow()));
          // a reader scrolls to what's under the footer with the wheel; a
          // scroll by code, as a click's own scrolling into view, is put
          // back (no unasked scroll), and the footer took WebKit's click
          const press = async (b) => {
            await page.mouse.move(width / 2, 400);
            for (let i = 0; i < 12; i++) {
              const [at, foot] = [await b.boundingBox(), await page.locator(".foot").boundingBox()];
              if (at && (!foot || at.y + at.height <= foot.y)) break;
              await page.mouse.wheel(0, 200);
              await page.waitForTimeout(80);
            }
            await b.click();
          };
          const field = sec.locator(".pm-ghfind input");
          assert.equal(await field.getAttribute("placeholder"), await tr("Filter by name, author or description"));

          // not installed: the 29 nobody has, both strips counting the
          // other as it is picked
          await have.nth(2).click();
          assert.equal(await cards.count(), GH.repos.filter((r) => !here(r)).length);
          assert.equal(await sec.locator(".pm-card.have").count(), 0);
          assert.deepEqual(await kinds.allTextContents(), await want(GH.repos.filter((r) => !here(r))));
          // a kind on top of it: the middleware nobody has, its own kind
          // strip counting what's left of it
          await kinds.nth(2).click();
          assert.equal(await cards.count(), GH.repos.filter((r) => kindOf(r) === "Middleware" && !here(r)).length);
          assert.deepEqual(await have.allTextContents(), await wantHave(GH.repos.filter((r) => kindOf(r) === "Middleware")));
          // installed: the two here, the middleware among them
          await have.nth(1).click();
          assert.deepEqual(await cards.locator(".pm-name b").allInnerTexts(), names(GH.repos.filter((r) => kindOf(r) === "Middleware" && here(r))));
          // the field narrows both strips' counts, as it does the kinds'
          await field.fill("doubao");
          const doubao0 = GH.repos.filter((r) => has(r, "doubao"));
          assert.deepEqual(await have.allTextContents(), await wantHave(doubao0.filter((r) => kindOf(r) === "Middleware")));
          assert.deepEqual(await kinds.allTextContents(), await want(doubao0.filter(here)));
          assert.equal(await cards.count(), 0);
          const none0 = sec.locator(".pm-ghnone");
          assert.equal((await none0.locator("span").textContent()).trim(), await tr("No plugin on GitHub matches"));
          // Show all puts back both strips and the field
          await press(none0.locator("button"));
          assert.equal(await field.inputValue(), "");
          assert.equal(await cards.count(), GH.repos.length);
          assert.equal(await sec.locator(".pm-ghhave .opt.on").textContent(), (await wantHave(GH.repos))[0]);
          assert.equal(await sec.locator(".pm-ghkinds .opt.on").textContent(), (await want(GH.repos))[0]);

          // a kind: its cards alone, each saying so
          await kinds.nth(2).click();
          assert.equal(await cards.count(), 3);
          assert.deepEqual([...new Set(await cards.locator(".pm-chip.kind").allTextContents())], [await tr("Middleware")]);

          // the field, typed in while npm's answer redraws the page: the same
          // node keeps its focus and every key; the kinds count what it leaves
          await field.evaluate((x) => { window.__field = x; });
          await field.click();
          await page.keyboard.type("doubao", { delay: 120 });
          await page.waitForTimeout(800);
          assert(await field.evaluate((x) => x === window.__field && document.activeElement === x && x.value === "doubao"), "the field kept its node, focus and text");
          const doubao = GH.repos.filter((r) => has(r, "doubao"));
          assert.deepEqual(await kinds.allTextContents(), await want(doubao));
          // no middleware is about doubao: said so, with Show all
          assert.equal(await cards.count(), 0);
          const none = sec.locator(".pm-ghnone");
          assert.equal((await none.locator("span").textContent()).trim(), await tr("No plugin on GitHub matches"));
          await kinds.nth(0).click();
          assert.deepEqual((await cards.locator(".pm-who").allTextContents()).length, doubao.length);

          // an author, then words of a description
          for (const s of ["mu-zi-lee", "Cloudflare AI Gateway"]) {
            await field.fill(s);
            const rs = GH.repos.filter((r) => has(r, s));
            assert(rs.length > 0);
            assert.equal(await cards.count(), rs.length, s);
          }

          // Show all puts back every kind and every card
          await kinds.nth(3).click();
          await field.fill("zzz-nothing");
          await press(none.locator("button"));
          assert.equal(await field.inputValue(), "");
          assert.equal(await cards.count(), 31);
          assert.equal(await sec.locator(".pm-ghkinds .opt.on").textContent(), (await want(GH.repos))[0]);

          // the page's search narrows first, the section's within it; what
          // it leaves has nothing installed, so the strip goes and so does
          // its pick, rather than narrowing the grid out of sight
          await have.nth(1).click();
          assert.equal(await cards.count(), GH.repos.filter(here).length);
          const q = page.locator("#view-plugins .pm-head .pm-find input");
          await q.fill("translate");
          const tl = GH.repos.filter((r) => has(r, "translate"));
          await page.waitForFunction((n) => document.querySelectorAll("#view-plugins .pm-ghsec .pm-card").length === n, tl.length);
          assert.deepEqual(await kinds.count(), 0, "one kind alone needs no picking");
          assert.equal(tl.some(here), false);
          assert.equal(await have.count(), 0, "nothing installed is left to pick out");
          await field.fill("seed");
          assert.equal(await cards.count(), tl.filter((r) => has(r, "seed")).length);

          // nothing left over at any width
          const over = await overflow();
          assert.deepEqual(over.map((x) => Math.max(0, x)), [0, 0, 0], JSON.stringify(over));
          assert.deepEqual(errors, []);
        } finally {
          await ctx.close();
        }
      });
    }
  });
}

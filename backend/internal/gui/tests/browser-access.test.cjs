// Run with Node's test runner and Playwright on the module path; see README.md.
// Browser access supports a keyed management page on this computer only.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { reach, watchSettingsReads, settingsDrawn } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const KEY = "0123456789abcdef0123456789abcdef";

function server(lang, web, posts, gets) {
  let wp = { mode: "", port: 3430, serving: false };
  const say = () => {
    if (!wp.mode) return { mode: "", port: wp.port, serving: false };
    const s = { ...wp, link: `http://127.0.0.1:${wp.port}/?k=${wp.key}` };
    if (wp.port === 3500) Object.assign(s, { serving: false, error: "port 3500 can't be taken (address already in use): uni-switch tries again every few seconds" });
    else s.serving = true;
    return s;
  };
  return async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const json = (data, status = 200) => route.fulfill({ status, json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:${web},sideGateway:{url:"http://127.0.0.1:3425",running:true,boot:true}};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" }, web });
    if (url.pathname === "/api/web-page") {
      if (req.method() === "GET") { gets.n++; return json(say()); }
      const body = JSON.parse(req.postData());
      posts.push(body);
      if (body.mode !== undefined) wp.mode = body.mode;
      if (body.port !== undefined) wp.port = body.port;
      if (body.newKey || !wp.key) wp.key = body.newKey && wp.key ? "fedcba9876543210fedcba9876543210" : KEY;
      return json(say());
    }
    if (url.pathname === "/api/settings") return json({ lang, theme: "light", gateway: "http://127.0.0.1:3425", version: "0.1.900", dir: "/config/uni-switch", fx: { rate: 7.2, at: new Date().toISOString(), stale: false } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": Browser access turns the app's page on for a browser, with its key", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const pages = [];
    t.after(async () => {
      if (process.env.ARTIFACT_DIR) {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        for (const [i, p] of pages.entries()) await p.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-browser-access-${i}.png`) });
      }
      await browser.close();
    });

    const open = async (lang, web, width = 900) => {
      const page = await (await browser.newContext({ viewport: { width, height: 700 }, reducedMotion: "reduce" })).newPage();
      pages.push(page);
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [], posts = [], gets = { n: 0 };
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(lang, web, posts, gets));
      await watchSettingsReads(page);
      await page.goto("http://localhost:3430/?view=settings&tab=network");
      await settingsDrawn(page);
      // the page's own word for an English string
      const tr = (s, vars) => page.evaluate(([s, vars]) => t(s, vars), [s, vars]);
      return { page, errors, posts, gets, tr };
    };

    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      await t.test(lang, async () => {
        const { page, errors, posts, tr } = await open(lang, false);
        const head = page.locator("#webPageHead");
        const list = page.locator("#webPageList");
        await list.locator(".row.pref").first().waitFor();
        assert.equal((await head.textContent()).trim(), await tr("Browser access"));
        if (lang !== "en") assert.notEqual(await tr("Browser access"), "Browser access", "translated");
        const first = list.locator(".row.pref").first();
        assert.equal((await first.locator(".name").textContent()).trim(), await tr("Open uni-switch in a browser"));
        assert.equal((await first.locator(".sub").textContent()).trim(), await tr("Only browsers on this computer can open the page. Address: http://127.0.0.1:{port}", { port: 3430 }));
        assert.equal(await list.locator(".row.pref").count(), 1, "off: one row");

        const local = first.getByRole("button", { name: await tr("This computer") });
        await reach(page, local);
        await local.click();
        await list.locator(".web-page-link-row").waitFor();
        assert.deepEqual(posts.at(-1), { mode: "local" });
        const linkRow = list.locator(".web-page-link-row");
        const code = linkRow.locator("code");
        assert.equal(await code.count(), 1);
        assert.equal((await code.textContent()).trim(), "http://127.0.0.1:3430/?k=…", "the key isn't shown");
        assert.equal(await linkRow.locator("button.copy").count(), 1);
        assert.equal((await linkRow.locator(".sub").textContent()).trim(), await tr("Served; anyone with this link can change every setting"));
        assert.equal(await list.locator(".web-page-warn").count(), 0, "this computer alone: no warning");

        const rekey = linkRow.getByRole("button", { name: await tr("New key") });
        await reach(page, rekey);
        await rekey.click();
        // the posts are counted here, not a time waited
        for (let i = 0; i < 250 && !posts.some((p) => p.newKey); i++) await page.waitForTimeout(20);
        assert(posts.some((p) => p.newKey), "New key posts newKey");

        // a port another program has: said in the page's language
        const field = list.locator("input.gateway-port");
        await reach(page, field);
        await field.fill("3500");
        await list.getByRole("button", { name: await tr("Apply") }).click();
        const why = await tr("Port {p} is taken by another program, uni-switch web perhaps: uni-switch tries again every few seconds", { p: 3500 });
        await list.locator(".web-page-link-row .sub.err").filter({ hasText: why }).waitFor();
        assert.deepEqual(posts.at(-1), { port: 3500 });

        assert.equal(await list.getByRole("button", { name: "Local network", exact: true }).count(), 0);
        assert.equal(await list.locator(".web-page-link-row code").count(), 1);

        assert.equal(await page.locator("#setPage-network select").count(), 0, "no native select");
        for (const r of await list.locator(".row.pref").all()) {
          const border = await r.evaluate((e) => getComputedStyle(e).borderLeftWidth);
          assert(border === "0px" || border === "", "no left border accent: " + border);
        }
        const off = list.getByRole("button", { name: await tr("Off") }).first();
        await reach(page, off);
        await off.click();
        await page.waitForFunction(() => document.querySelectorAll("#webPageList .row.pref").length === 1);
        assert.deepEqual(posts.at(-1), { mode: "" });
        assert.deepEqual(errors, []);
      });
    }

    await t.test("360px wide, Chinese", async () => {
      const { page, errors, tr } = await open("zh", false, 360);
      const list = page.locator("#webPageList");
      await list.locator(".row.pref").first().waitFor();
      const local = list.getByRole("button", { name: await tr("This computer") });
      await reach(page, local);
      await local.click();
      await list.locator(".web-page-link-row").waitFor();
      const over = await list.evaluate((l) => [...l.querySelectorAll(".row.pref")].filter((r) => r.scrollWidth > r.clientWidth + 1 || r.getBoundingClientRect().right > innerWidth + 1).map((r) => r.textContent.slice(0, 40)));
      assert.deepEqual(over, [], "rows wider than the window");
      const cut = await list.evaluate((l) => [...l.querySelectorAll(".row.pref .name")].filter((n) => n.scrollWidth > n.clientWidth).map((n) => n.textContent));
      assert.deepEqual(cut, [], "names cut off");
      assert.deepEqual(errors, []);
    });

    await t.test("a browser on the page has no Browser access", async () => {
      const { page, errors, gets } = await open("en", true);
      await page.locator("#portList .row").first().waitFor();
      assert.equal(await page.locator("#webPageHead").isVisible(), false);
      assert.equal(await page.locator("#webPageList .row").count(), 0);
      assert.equal(gets.n, 0, "never asks /api/web-page");
      assert.deepEqual(errors, []);
    });
  });
}

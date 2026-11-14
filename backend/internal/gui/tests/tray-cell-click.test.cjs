// Run with Node's test runner and Playwright on the module path; see README.md.
// Native ExecJS and main-window URLs select the requested allowance/account,
// including delayed data, focus refreshes, real scrolling and repeated openings. APIs are mocked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const quotas = (empty) => empty ? [] : [
  { provider: "claude", name: "Claude Code", icon: "claude-color", user: "a@b.c", windows: [{ name: "Weekly", used: 17.6 }, { name: "5-hour", used: 42.2 }] },
  { provider: "codex", name: "Codex", icon: "codex-color", user: "x@y.z", windows: [{ name: "5-hour", used: 8 }, { name: "Weekly", used: 100 }] },
  ...Array.from({ length: 6 }, (_, i) => ({ provider: "codex", name: "Codex", icon: "codex-color", user: `other-${i}@例子.test`, windows: [{ name: "Weekly", used: 25 }] })),
  { provider: "deepseek", name: "DeepSeek", icon: "deepseek-color", windows: [], balance: "¥12.30" },
  { provider: "kimi", name: "Kimi", icon: "kimi-color", windows: [{ name: "Weekly", used: 30 }] },
];

function serve(lang, empty, ready) {
  const settings = { theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd" };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "window.__runtimeReady = true; export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") { await ready; return json(quotas(empty)); }
    if (url.pathname === "/api/usage") return json({ days: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a menu-bar cell's click opens the panel's Allowances tab at that agent's card`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(async () => { await browser.close(); });
      const errors = [];
      const open = async (url, viewport, init, empty, ready) => {
        // no reduced-motion emulation here: it makes every animation a
        // frame long, and the flash the cards light up with is one
        const context = await browser.newContext({ viewport });
        // what a click lands on is said by its scrolling there
        await context.addInitScript(() => {
          window.__scrolled = [];
          const scroll = Element.prototype.scrollIntoView;
          Element.prototype.scrollIntoView = function (options) { window.__scrolled.push(this.dataset.card || this.dataset.provider || this.tagName); scroll.call(this, options); };
        });
        for (const script of init || []) await context.addInitScript(script);
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, empty, ready));
        await page.goto(url);
        return page;
      };

      const visible = async (page, selector) => {
        await page.waitForFunction((selector) => {
          const e = document.querySelector(selector);
          if (!e) return false;
          const r = e.getBoundingClientRect(), v = e.closest(".view").getBoundingClientRect();
          return r.top >= v.top - 1 && r.bottom <= v.bottom + 1;
        }, selector).catch(async (err) => {
          const info = await page.locator(selector).evaluate((e) => {
            const v = e.closest(".view");
            return { target: e.getBoundingClientRect().toJSON(), view: v.getBoundingClientRect().toJSON(), scroll: v.scrollTop, max: v.scrollHeight - v.clientHeight, viewport: innerHeight };
          });
          throw new Error(err.message + " " + JSON.stringify(info));
        });
      };

      // the panel: Go's ExecJS runs panelQuotaFocus, the global Go names —
      // a click on the Codex cell is the Allowances tab at Codex's card
      const panel = await open("http://uni-switch.test/?mode=panel", { width: 440, height: 640 });
      await panel.waitForFunction(() => document.querySelectorAll("#panelQuota .pq-card").length >= 4);
      await panel.waitForFunction(() => window.__runtimeReady === true); // native ExecJS can now drain
      // dragged by its header on Linux alone (panel-drag-linux.test.cjs, #1430)
      const linux = await panel.evaluate(() => /^Linux/.test(navigator.platform));
      assert.equal(await panel.locator("header.top").evaluate((e) => getComputedStyle(e).getPropertyValue("--wails-draggable").trim()), linux ? "drag" : "no-drag", "the runtime must not make the panel draggable off Linux");
      assert.equal(await panel.evaluate(() => typeof panelQuotaFocus), "function", "panelQuotaFocus is Go's to call by name");
      assert.equal(await panel.evaluate(() => document.body.dataset.ptab), "agents", "the panel starts on its agents");
      await panel.evaluate(() => panelQuotaFocus("codex|x@y.z"));
      await panel.waitForFunction(() => document.body.dataset.ptab === "usage");
      assert(await panel.locator('#ptabs [data-ptab="usage"]').evaluate((e) => e.classList.contains("on")), "the Allowances tab is the one picked");
      assert.equal(await panel.evaluate(() => localStorage.getItem("uni-switch.panelTab")), "usage");
      await panel.waitForFunction(() => document.querySelector('#panelQuota .pq-card[data-card="codex|x@y.z"]')?.classList.contains("flash"));
      assert.deepEqual(await panel.evaluate(() => window.__scrolled.slice(-1)), ["codex|x@y.z"], "the card is scrolled to");
      assert(await panel.locator('#panelQuota [data-card="codex|x@y.z"]').evaluate((card) => {
        flashCard(card); // repeated focus restarts the same card
        card.firstElementChild.dispatchEvent(new AnimationEvent("animationend", { bubbles: true, animationName: "flash" }));
        card.dispatchEvent(new AnimationEvent("animationend", { animationName: "unrelated" }));
        return card.classList.contains("flash");
      }), "child or unrelated animations must not end the card's highlight");
      // Showing a native panel gives it focus, which refreshes its state and quotas.
      // The first click must remain highlighted across that refresh.
      await panel.evaluate(() => {
        window.__beforeFocusRefresh = document.querySelector('#panelQuota [data-card="codex|x@y.z"]');
        window.__beforeFocusRefresh.getAnimations().find((a) => a.animationName === "flash").currentTime = 600;
        window.dispatchEvent(new Event("focus"));
      });
      // the refresh read the same allowances, so the card is kept as drawn
      // (#3192); one drawn again would have to keep the highlight too
      await panel.waitForFunction(() => !loadBack.flight && !quotasLoading);
      await panel.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
      assert(await panel.locator('#panelQuota [data-card="codex|x@y.z"]').evaluate((e) => e.classList.contains("flash")), "first-open focus refresh must preserve the highlight");

      assert(await panel.locator('#panelQuota [data-card="codex|x@y.z"]').evaluate((e) => e.getAnimations().find((a) => a.animationName === "flash")?.currentTime >= 600), "refresh must continue the animation rather than restart it");
      await panel.waitForFunction(() => !document.querySelector('#panelQuota [data-card="codex|x@y.z"]').classList.contains("flash"));

      // a balance alone is a cell too, and its card is among the Balances
      await panel.evaluate(() => panelQuotaFocus("deepseek"));
      await panel.waitForFunction(() => document.querySelector('#panelQuota .pq-card.bal[data-card="deepseek"]')?.classList.contains("flash"));
      assert.deepEqual(await panel.evaluate(() => window.__scrolled.slice(-1)), ["deepseek"]);
      await visible(panel, '#panelQuota [data-card="deepseek"]');
      // a card that isn't there picks the tab and stops: nothing flashed,
      // nothing broken
      await panel.evaluate(() => panelQuotaFocus("nobody"));
      await panel.waitForFunction(() => !document.querySelector('#panelQuota .pq-card[data-card="deepseek"]')?.classList.contains("flash"));
      assert.equal(await panel.evaluate(() => document.body.dataset.ptab), "usage");
      assert.equal(await panel.evaluate(() => window.__scrolled.filter((s) => s === "nobody").length), 0);

      // A click before the first quota response must survive the loading cards.
      let release;
      const ready = new Promise((resolve) => { release = resolve; });
      const slow = await open("http://uni-switch.test/?mode=panel", { width: 440, height: 300 }, [], false, ready);
      await slow.waitForFunction(() => typeof panelQuotaFocus === "function");
      await slow.evaluate(() => panelQuotaFocus("kimi"));
      await slow.evaluate(() => new Promise(requestAnimationFrame));
      release();
      await slow.waitForFunction(() => document.querySelector('#panelQuota [data-card="kimi"]')?.classList.contains("flash"));

      await visible(slow, '#panelQuota [data-card="kimi"]');
      await slow.waitForFunction(() => !document.querySelector('#panelQuota [data-card="kimi"]').classList.contains("flash"));
      await visible(slow, '#panelQuota [data-card="kimi"]');

      await panel.emulateMedia({ reducedMotion: "reduce" });
      assert.equal(await panel.locator('#panelQuota [data-card="codex|other-5@例子.test"]').count(), 0, "the requested account starts folded");
      await panel.evaluate(() => panelQuotaFocus("codex|other-5@例子.test"));
      await panel.waitForFunction(() => window.__scrolled.at(-1) === "codex|other-5@例子.test");
      await visible(panel, '#panelQuota [data-card="codex|other-5@例子.test"]');
      assert.deepEqual(await panel.evaluate(() => JSON.parse(localStorage.getItem("uni-switch.usageAccounts"))), { "codex\nother-5@例子.test": true }, "navigation remembers the account opened");
      await panel.waitForFunction(() => !document.querySelector('#panelQuota [data-card="codex|other-5@例子.test"]').classList.contains("flash"));

      // no allowances to show: no tab to pick, the panel keeps its agents
      const none = await open("http://uni-switch.test/?mode=panel", { width: 440, height: 640 }, [], true);
      await none.waitForFunction(() => document.querySelectorAll("#agents").length === 1);
      await none.evaluate(() => panelQuotaFocus("codex|x@y.z"));
      await none.waitForFunction(() => document.body.dataset.ptab === "agents");

      // the window: opened on its allowances (view=usage&tab=usage), the
      // Overview is the pane whatever the page was last left on, at the
      // provider's card
      const win = await open("http://uni-switch.test/?view=usage&tab=usage&provider=kimi", { width: 900, height: 700 }, [
        () => { try { localStorage.setItem("uni-switch.usageTab", "requests"); } catch {} },
      ]);
      await win.waitForFunction(() => !document.querySelector("#usagePane").hidden);
      assert.equal(await win.evaluate(() => document.querySelector("#ledgerPane").hidden), true, "the Requests the page was left on are not the pane");
      await win.waitForFunction(() => document.querySelector('.subscription-card[data-provider="kimi"]')?.classList.contains("flash"));
      assert.deepEqual(await win.evaluate(() => window.__scrolled.slice(-1)), ["kimi"], "the provider's card is scrolled to");
      await win.evaluate(async () => {
        document.querySelector('.subscription-card[data-provider="kimi"]').getAnimations().find((a) => a.animationName === "flash").currentTime = 600;
        await loadQuotas();
      });
      assert(await win.locator('.subscription-card[data-provider="kimi"]').evaluate((e) => e.classList.contains("flash") && e.getAnimations().find((a) => a.animationName === "flash")?.currentTime >= 600), "main-window quota refresh must preserve the remaining highlight");
      assert.equal(await win.evaluate(() => location.search.includes("tab=")), false, "the parameters are the page's own again");
      await visible(win, '.subscription-card[data-provider="kimi"]');

      // Same vendor, a different account: scroll within the tall vendor card.
      const id = "codex|other-5@例子.test";
      const account = await open("http://uni-switch.test/?view=usage&tab=usage&provider=codex&card=" + encodeURIComponent(id), { width: 900, height: 500 });
      await account.waitForFunction(() => document.querySelector('.subscription-card[data-provider="codex"]')?.classList.contains("flash"));
      assert.deepEqual(await account.evaluate(() => window.__scrolled.slice(-1)), [id]);
      await visible(account, '.subscription-account[data-card="codex|other-5@例子.test"]');
      assert.deepEqual(await account.evaluate(() => JSON.parse(localStorage.getItem("uni-switch.usageAccounts"))), { "codex\nother-5@例子.test": true });
      assert.equal(await account.evaluate(() => location.search.includes("card=")), false);
      await account.waitForFunction(() => !document.querySelector('.subscription-card[data-provider="codex"]').classList.contains("flash"));
      await visible(account, '.subscription-account[data-card="codex|other-5@例子.test"]');

      // A slow response must not pull the reader back after the request expires.
      let finish;
      const expired = await open("http://uni-switch.test/?mode=panel", { width: 440, height: 300 }, [], false, new Promise((resolve) => { finish = resolve; }));
      await expired.waitForFunction(() => typeof panelQuotaFocus === "function");
      await expired.clock.install();
      await expired.evaluate(() => panelQuotaFocus("codex|other-5@例子.test"));
      await expired.clock.fastForward(6000);
      finish();
      await expired.waitForFunction(() => document.querySelector('#panelQuota [data-card="kimi"]'));
      await expired.evaluate(() => new Promise(requestAnimationFrame));
      assert.deepEqual(await expired.evaluate(() => window.__scrolled), [], "expired requests cannot scroll or expand accounts");
      assert.equal(await expired.locator('#panelQuota [data-card="codex|other-5@例子.test"]').count(), 0);

      // A purposeful user scroll supersedes a still-pending menu-bar request.
      let resume;
      const scrolled = await open("http://uni-switch.test/?mode=panel", { width: 440, height: 300 }, [], false, new Promise((resolve) => { resume = resolve; }));
      await scrolled.waitForFunction(() => typeof panelQuotaFocus === "function");
      await scrolled.evaluate(() => panelQuotaFocus("kimi"));
      await scrolled.mouse.move(200, 150);
      await scrolled.mouse.wheel(0, 200);
      // Flush input delivery before allowing the data to arrive.
      await scrolled.waitForFunction(() => !quotaFocus);
      resume();
      await scrolled.waitForFunction(() => document.querySelector('#panelQuota [data-card="kimi"]'));
      await scrolled.evaluate(() => new Promise(requestAnimationFrame));
      assert.deepEqual(await scrolled.evaluate(() => window.__scrolled), [], "manual scrolling cancels pending navigation");

      assert.deepEqual(errors, []);
    });
  }
}

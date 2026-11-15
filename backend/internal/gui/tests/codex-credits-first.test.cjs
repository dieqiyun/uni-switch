// Run with Node's test runner and Playwright on the module path; see README.md.
// jojonas_xyz on X: a Codex account's allowance ran out and uni-switch moved to
// another account by itself, with no setting to be found to keep spending
// the first one's credits. Under Use credits, while it is on, a line says
// "Before moving to another account" with its switch: off until turned on,
// it posts settings/codex-credits with first for that account, says so,
// and turns; it and "Only after every other account, key and fallback"
// are never on together. The same row is under the account's ⋯ on
// Providers, where its other settings are, and the account uni-switch moved
// off says, on its tag, where the setting is. The panel's folded line says
// it. At narrow widths nothing is cut off or wider than its card, a click
// moves nothing, no native select, no left-border accent. Every language,
// Chromium and WebKit; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const later = new Date(Date.now() + 3 * 864e5).toISOString();
const now = new Date();
const quotas = [
  { provider: "codex", name: "Codex", icon: "codex-color", user: "Me@example.com", plan: "Plus", lastServedAt: now.toISOString(),
    windows: [{ name: "5 hours", used: 100, resetsAt: later }, { name: "7 days", used: 40, resetsAt: later }],
    balance: "1.2K credits" },
];
// uni-switch moved Codex off Me@example.com (returns) to work@example.com
const logins = [
  { user: "work@example.com", plan: "PLUS", active: true, on: true },
  { user: "Me@example.com", plan: "PLUS", on: true, returns: true },
];
const codex = {
  id: "codex", name: "Codex", icon: "codex-color", chat: "", responses: "", anthropic: "", catalog: "", routing: "smart",
  models: [{ id: "gpt-6", name: "GPT-6", on: true }], agents: [], fallback: [], headers: {}, keyList: [],
  account: { agent: "codex", agentName: "Codex", agentIcon: "codex-color", user: "work@example.com", plan: "PLUS", logins },
};

function serve(lang, posts) {
  let off = [], first = [], last = [];
  const settings = () => ({ theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd", trayUsages: ["codex"], trayUsageEvery: 3,
    codexNoCredits: off, codexCreditsFirst: first, codexCreditsLast: last });
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: settings() });
    if (url.pathname === "/api/settings") return json(settings());
    if (url.pathname === "/api/settings/codex-credits") {
      // as the server: one of first and last at most; off clears both,
      // on alone keeps either
      const body = req.postDataJSON();
      posts.push(body);
      const who = body.user.toLowerCase();
      const was = first.includes(who) ? "first" : last.includes(who) ? "last" : "";
      off = off.filter((u) => u !== who);
      first = first.filter((u) => u !== who);
      last = last.filter((u) => u !== who);
      if (!body.on) off.push(who);
      else if (body.last) last.push(who);
      else if (body.first) first.push(who);
      else if (body.last === undefined && body.first === undefined && was) (was === "first" ? first : last).push(who);
      return json(settings());
    }
    if (url.pathname === "/api/usage/quotas") return json(quotas);
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) await new Promise((r) => setTimeout(r, 20e3));
      return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname === "/api/providers") return json({ providers: [codex], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { line: "Before moving to another account", on: (who) => `${who} spends its credits before moving to another account`, brief: "Use credits: On, before another account" },
  zh: { line: "先用完积分再换账号", on: (who) => `${who} 将先用完积分再换账号`, brief: "使用积分： 开，先于其他账号" },
  "zh-TW": { line: "先用完積分再換帳號", on: (who) => `${who} 將先用完積分再換帳號`, brief: null },
  ja: { line: "別のアカウントに移る前に使う", on: (who) => `${who} は別のアカウントに移る前にクレジットを使います`, brief: null },
  de: { line: "Bevor ein anderes Konto drankommt", on: (who) => `${who} verbraucht Credits, bevor ein anderes Konto drankommt`, brief: null },
};

const keys = [
  "Before moving to another account",
  "On: past its windows, it keeps its place and Codex's sign-in while it holds credits. Click to turn it off.",
  "Off: nearly used up, it gives way to an account with room, Codex's sign-in too. Click to turn it on.",
  "Off: past its windows, it answers on its credits when Routing comes to it.",
  "{who} spends its credits before moving to another account",
  "On, before another account",
  "To spend its credits first: ⋯ › Use credits › Before moving to another account.",
];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(words)) {
    const w = words[lang];
    test(`${engine} ${lang}: a Codex account's credits spent before moving to another account`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const errors = [];
      const open = async (url, viewport, posts) => {
        const page = await (await browser.newContext({ viewport, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, posts));
        await page.goto(url);
        return page;
      };
      const wait = async (posts, n) => { for (let i = 0; i < 60 && posts.length < n; i++) await new Promise((r) => setTimeout(r, 50)); };
      const text = (loc) => loc.evaluate((e) => e.textContent.replace(/\s+/g, " ").trim());
      // a click: it posts, turns, says so, and moves nothing
      const flip = async (page, row, posts, sel, want, body, said) => {
        const b = page.locator(row + " " + sel);
        const spot = () => b.evaluate((e) => [e.getBoundingClientRect().top, document.scrollingElement.scrollTop, ...[...document.querySelectorAll(".view")].map((v) => v.scrollTop)]);
        const was = await spot();
        const n = posts.length;
        await b.click();
        await wait(posts, n + 1);
        assert.deepEqual(posts.at(-1), body);
        await page.locator(row + " " + sel + `[aria-checked="${want}"]`).waitFor();
        assert.equal(await page.locator("#status").textContent(), said);
        await page.waitForTimeout(200);
        assert.deepEqual(await spot(), was, "the click moved the page");
      };
      // both lines fit their card, their switches whole and at their
      // line's end, and nothing cut off
      const fits = (page, row, card) => page.evaluate(([row, card]) => {
        const r = document.querySelector(row);
        const c = r.closest(card).getBoundingClientRect(), b = r.getBoundingClientRect();
        if (b.right > c.right + 0.5) return "row";
        for (const [line, sw] of [[".ar-credits-first", ".credits-first"], [".ar-credits-last", ".credits-last"]]) {
          const l = r.querySelector(line), s = l.querySelector(sw).getBoundingClientRect(), words = l.querySelector(".ar-case-say");
          if (s.right > c.right + 0.5 || s.width < 27 || s.height < 15) return sw;
          if (words.scrollWidth > words.clientWidth + 1) return line + " cut off";
        }
        return "";
      }, [row, card]);
      const noStripe = (page, sel) => page.evaluate((sel) => [...document.querySelectorAll(sel + ", " + sel + " *")].map((e) => getComputedStyle(e).borderLeftWidth).filter((b) => parseFloat(b) > 1), sel);

      // the Usage page's card
      for (const width of [900, 440]) {
        const posts = [];
        const page = await open("http://uni-switch.test/?view=usage", { width, height: 760 }, posts);
        const row = '.quota-credits[data-user="Me@example.com"]';
        await page.locator(row + " .ar-credits-first").waitFor();
        assert.equal(await text(page.locator(row + " .ar-credits-first")), w.line);
        const sw = page.locator(row + " .credits-first");
        assert.equal(await sw.getAttribute("role"), "switch");
        assert.equal(await sw.getAttribute("aria-checked"), "false", "off until set");
        assert(await sw.getAttribute("title"));
        assert.equal(await fits(page, row, ".subscription-card"), "", `the lines fit the card at ${width}px`);
        await flip(page, row, posts, ".credits-first", true, { user: "Me@example.com", on: true, first: true }, w.on("Me@example.com"));
        assert.equal(await page.locator(row + " .ar-credits-first.on").count(), 1);
        assert.equal(await fits(page, row, ".subscription-card"), "", `the lines, on, fit the card at ${width}px`);
        // last takes its place: one of the two at most
        await page.locator(row + " .credits-last").click();
        await wait(posts, 2);
        assert.deepEqual(posts.at(-1), { user: "Me@example.com", on: true, last: true });
        await page.locator(row + ' .credits-last[aria-checked="true"]').waitFor();
        assert.equal(await page.locator(row + " .credits-first").getAttribute("aria-checked"), "false");
        const back = await page.evaluate(() => t("{who} spends its credits when its windows run out", { who: "Me@example.com" }));
        await flip(page, row, posts, ".credits-first", true, { user: "Me@example.com", on: true, first: true }, w.on("Me@example.com"));
        assert.equal(await page.locator(row + " .credits-last").getAttribute("aria-checked"), "false");
        await flip(page, row, posts, ".credits-first", false, { user: "Me@example.com", on: true, first: false }, back);
        // Use credits off: no lines, as nothing is spent at all
        await page.locator(row + " .use-credits").click();
        await wait(posts, 5);
        assert.deepEqual(posts.at(-1), { user: "Me@example.com", on: false });
        await page.locator(row + ' .use-credits[aria-checked="false"]').waitFor();
        assert.equal(await page.locator(row + " .ar-credits-first").count(), 0);
        assert.deepEqual(await noStripe(page, ".quota-credits"), [], "no left-border accent");
        await page.context().close();
      }

      // Providers: the account's ⋯, where its other settings are, and the
      // account uni-switch moved off says where it is
      for (const width of [900, 440]) {
        const posts = [];
        const page = await open("http://uni-switch.test/?view=providers", { width, height: 760 }, posts);
        await page.locator(".row.provider", { hasText: "Codex" }).first().click();
        const acc = page.locator(".editor .acc", { hasText: "Me@example.com" });
        await acc.waitFor();
        const tag = acc.locator(".acc-line .using").first();
        assert(String(await tag.getAttribute("title")).includes(w.line), "the moved-off account's tag names the setting");
        await acc.hover();
        await acc.locator(".acc-more").click();
        const row = '.editor .acc .acc-credits[data-user="Me@example.com"]';
        await page.locator(row + " .ar-credits-first").waitFor();
        assert.equal(await text(page.locator(row + " .ar-credits-first")), w.line);
        assert.equal(await fits(page, row, ".acc"), "", `the lines fit the account at ${width}px`);
        await flip(page, row, posts, ".credits-first", true, { user: "Me@example.com", on: true, first: true }, w.on("Me@example.com"));
        assert.equal(await page.locator(row).count(), 1, "the ⋯ stays open");
        assert.equal(await fits(page, row, ".acc"), "", `the lines, on, fit the account at ${width}px`);
        // the account Codex is signed in to has its own
        assert.equal(await page.locator('.editor .acc .acc-credits[data-user="work@example.com"]').count(), 1);
        assert.equal(await page.locator("select").count(), 0, "no native select");
        assert.deepEqual(await noStripe(page, ".acc-credits"), [], "no left-border accent");
        await page.context().close();
      }

      // the menu bar panel's folded line says it
      {
        const posts = [];
        const panel = await open("http://uni-switch.test/?mode=panel", { width: 360, height: 640 }, posts);
        const row = '.pq-credits[data-user="Me@example.com"]';
        await panel.locator('#ptabs [data-ptab="usage"]').click();
        await panel.locator(row + " .ar-credits-first").waitFor();
        await panel.waitForFunction(() => !quotasLoading);
        assert.equal(await fits(panel, row, ".pq-card"), "", "the lines fit the panel's card at 360px");
        await flip(panel, row, posts, ".credits-first", true, { user: "Me@example.com", on: true, first: true }, w.on("Me@example.com"));
        const id = await panel.locator(row).evaluate((r) => r.closest(".pq-card").dataset.card);
        const card = `#panelQuota .pq-card[data-card="${id}"]`;
        await panel.locator(card + " .pq-opts-fold").click();
        const brief = panel.locator(card + " .pq-opts-brief .ar-say");
        await brief.waitFor();
        const said = await text(brief);
        const want = w.brief || (await panel.evaluate(() => t("Use credits:") + " " + t("On, before another account")));
        assert(said.endsWith(want), said);
        if (lang !== "en") {
          const missing = await panel.evaluate(([keys, lang]) => keys.filter((k) => !I18N[lang][k]), [keys, lang]);
          assert.deepEqual(missing, [], `every string has its ${lang}`);
        }
        await panel.context().close();
      }
      assert.deepEqual(errors, []);
    });
  }
}

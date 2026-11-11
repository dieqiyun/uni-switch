// Run with Node's test runner and Playwright on the module path; see README.md.
// A Codex account spending a reset by itself (StringKe on Discord: resets
// gifted to the account, used once its week is out), a standing say of the
// account's (#719, thedavidweng): every named Codex account's card has a
// row that names both times it uses one — the week running out, a reset
// about to expire — each with its own switch (#2078, alanwalk), off until
// turned on, that posts settings/codex-auto-reset for that account and that
// time alone, the other kept; an account holding no reset
// has it too, so it can be set ahead of one. Holding resets, the row of
// them says whether the one expiring first is auto-used, beside "Use a
// reset" on the Usage page's card and "Use one…" on the menu bar panel's.
// An account in brief says its say beside its name, with one switch for
// both (#801: the account a week ran out on is the one in brief). A GLM team's resets,
// spent on its own site, a plugin's, and an account with no name get none,
// nor a button to use one. No click moves the page; no left-border accent.
// Five languages, 1100 and 420 wide, Chromium and WebKit; no backend, the
// API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const later = new Date(Date.now() + 3 * 864e5).toISOString();
const now = new Date().toISOString();
const quotas = [
  { provider: "codex", name: "Codex", icon: "codex-color", user: "Me@example.com", plan: "Plus", lastServedAt: now,
    windows: [{ name: "5 hours", used: 100, resetsAt: later }, { name: "7 days", used: 100, resetsAt: later }],
    resets: { count: 2, until: later } },
  // a second account, holding no reset: its say is set ahead of one
  { provider: "codex", name: "Codex", icon: "codex-color", user: "two@example.com", plan: "Plus",
    windows: [{ name: "5 hours", used: 10, resetsAt: later }, { name: "7 days", used: 20, resetsAt: later }] },
  { provider: "zhipu", name: "GLM Coding", icon: "zhipu-color", user: "team@example.com", plan: "Team",
    windows: [{ name: "5 hours", used: 30 }], resets: { count: 1, byWindow: true, fiveHour: 1 } },
  // a plugin's, its resets told but not spent from uni-switch: never sent to Codex's
  { provider: "codex-plugin", name: "Codex (plugin)", user: "Me@example.com", plan: "Plus",
    windows: [{ name: "7 days", used: 100, resetsAt: later }], resets: { count: 3, until: later } },
];

function serve(lang, posts) {
  // as the settings keep it: the accounts on, and the ones kept to one time
  let auto = [], only = {};
  const when = (who) => auto.includes(who) ? { week: only[who] !== "expiring", expiring: only[who] !== "week" } : { week: false, expiring: false };
  const settings = () => ({ theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd", trayUsages: ["codex", "zhipu", "codex-plugin"], trayUsageEvery: 3, codexAutoReset: auto, codexAutoResetOnly: only });
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: settings() });
    if (url.pathname === "/api/settings") return json(settings());
    if (url.pathname === "/api/settings/codex-auto-reset") {
      const body = req.postDataJSON();
      posts.push(body);
      const who = body.user.toLowerCase();
      const w = body.when ? { ...when(who), [body.when]: body.on } : { week: body.on, expiring: body.on };
      auto = auto.filter((u) => u !== who);
      only = { ...only };
      delete only[who];
      if (w.week || w.expiring) {
        auto = [...auto, who];
        if (!w.expiring) only[who] = "week";
        else if (!w.week) only[who] = "expiring";
      }
      return json(settings());
    }
    if (url.pathname === "/api/usage/quotas") return json(quotas);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// en and zh spelled out; zh-TW, ja and de read from their own dictionary,
// every one of which is checked to be there
const words = {
  en: {
    say: "Auto-use resets:", week: "when its week runs out", expiry: "before one expires",
    keptOn: "· auto-used before it expires", keptOff: "· not auto-used", keptWeek: "· auto-used when the week runs out",
    brief: "Auto-use", label: "Auto-use resets", on: "On", off: "Off",
    both: (who) => `${who} uses a reset by itself when its week runs out, or before one expires`,
    weekOnly: (who) => `${who} uses a reset by itself only when its week runs out`,
    expiryOnly: (who) => `${who} uses a reset by itself only before one expires`,
    none: (who) => `${who} no longer uses a reset by itself`,
    use: "Use a reset", useOne: "Use one…",
  },
  zh: {
    say: "自动使用重置卡：", week: "周额度用完时", expiry: "重置卡到期前",
    keptOn: "· 到期前自动使用", keptOff: "· 不会自动使用", keptWeek: "· 周额度用完时自动使用",
    brief: "自动用卡", label: "自动使用重置卡", on: "开启", off: "关闭",
    both: (who) => `${who} 会在周额度用完时、或重置卡到期前自动使用重置卡`,
    weekOnly: (who) => `${who} 仅在周额度用完时自动使用重置卡`,
    expiryOnly: (who) => `${who} 仅在重置卡到期前自动使用`,
    none: (who) => `${who} 不再自动使用重置卡`,
    use: "用一张重置卡", useOne: "用一张…",
  },
};
const keys = {
  say: "Auto-use resets:", week: "when its week runs out", expiry: "before one expires",
  keptOn: "auto-used before it expires", keptOff: "not auto-used", keptWeek: "auto-used when the week runs out", brief: "Auto-use", label: "Auto-use resets", on: "On", off: "Off",
  both: "{who} uses a reset by itself when its week runs out, or before one expires",
  weekOnly: "{who} uses a reset by itself only when its week runs out",
  expiryOnly: "{who} uses a reset by itself only before one expires",
  none: "{who} no longer uses a reset by itself",
  use: "Use a reset", useOne: "Use one…",
};
const strings = [
  ...Object.values(keys),
  "On: a reset is used by itself when this account's week is used up and no other account can answer, one a week at most. The five hours running out never uses one. Click to turn it off.",
  "Off: a used-up week is waited out, and the vendor may start it again sooner; requests go to your other accounts, or are refused with why. Click to turn it on.",
  "On: the reset that runs out first is used {lead} before it does, if this account's windows have been used, or at once when the account is held up until after then. Click to turn it off.",
  "Off: a reset that runs out unused is lost. Click to turn it on.",
  "Auto-use is on: the reset that runs out first is used {lead} before it does, if this account's windows have been used, so what they have left can be used until then and it isn't lost; at once if the account is held up until after then.",
  "Auto-use is off: the reset that runs out first is lost unless it's used by hand before then.",
  "Auto-use is on for a used-up week: a reset is used by itself when this account's week is used up, before its credits are spent. One about to run out is otherwise lost unless it's used by hand or the week runs out before then.",
  "On: a reset is used by itself when this account's week is used up and no other account can answer, one a week at most, and the one about to run out is used {lead} before it does, if the account has been used, or at once when the account is held up until after then. Otherwise the five hours running out never uses one. Click to turn it off.",
  "Off: this account's resets are used only by hand. Turned on, one is used by itself when its week is used up and no other account can answer, one a week at most, and the one about to run out {lead} before it does, if the account has been used, or at once when the account is held up until after then. Otherwise the five hours running out never uses one.",
];
// wordsOf: a language's words, from its dictionary when not spelled out here
async function wordsOf(page, lang) {
  if (words[lang]) return words[lang];
  const d = await page.evaluate(([lang, keys]) => Object.fromEntries(Object.entries(keys).map(([k, v]) => [k, I18N[lang][v]])), [lang, keys]);
  const fill = (s) => (who) => s.replace("{who}", who);
  return { ...d, keptOn: "· " + d.keptOn, keptOff: "· " + d.keptOff, keptWeek: "· " + d.keptWeek, both: fill(d.both), weekOnly: fill(d.weekOnly), expiryOnly: fill(d.expiryOnly), none: fill(d.none) };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [1100, 420]) {
    test(`${engine} ${lang} ${width}: a Codex account's resets used by themselves, each time its own switch, set held or not`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const pages = [];
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          for (const [i, p] of pages.entries()) await p.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-codex-auto-reset-${i}.png`) });
        }
        await browser.close();
      });
      const errors = [];
      let w;
      const open = async (url, viewport, posts) => {
        const page = await (await browser.newContext({ viewport, reducedMotion: "reduce" })).newPage();
        pages.push(page);
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, posts));
        await page.goto(url);
        return page;
      };
      const wait = async (posts, n) => { for (let i = 0; i < 60 && posts.length < n; i++) await new Promise((r) => setTimeout(r, 50)); };
      const text = (loc) => loc.evaluate((e) => e.textContent.replace(/\s+/g, " ").trim());
      // a switch's click: it posts, turns, says what the account does now,
      // and moves nothing on the page
      const flip = async (page, sel, posts, body, want, said) => {
        const b = page.locator(sel);
        await b.scrollIntoViewIfNeeded();
        const at = await b.evaluate((e) => [e.getBoundingClientRect().top, document.scrollingElement.scrollTop, ...[...document.querySelectorAll(".view")].map((v) => v.scrollTop)]);
        const n = posts.length;
        await b.click();
        await wait(posts, n + 1);
        assert.deepEqual(posts.at(-1), body);
        await page.locator(sel + `[aria-checked="${want}"]`).waitFor();
        assert.equal(await page.locator(sel).evaluate((e) => e.classList.contains("on")), want);
        assert.equal(await page.locator("#status").textContent(), said);
        await page.waitForTimeout(200);
        assert.deepEqual(await page.locator(sel).evaluate((e) => [e.getBoundingClientRect().top, document.scrollingElement.scrollTop, ...[...document.querySelectorAll(".view")].map((v) => v.scrollTop)]), at, "the click moved the page");
      };
      const checked = (page, sel) => page.locator(sel).getAttribute("aria-checked");
      // fits: sel lies inside its card, cut off nowhere
      const fits = (page, sel, card) => page.locator(sel).evaluate((e, card) => {
        const c = e.closest(card).getBoundingClientRect(), b = e.getBoundingClientRect();
        return b.width > 0 && b.left >= c.left - 0.5 && b.right <= c.right + 0.5;
      }, card);

      // the Usage page's card: Me in full, two in brief
      const posts = [];
      const page = await open("http://uni-switch.test/?view=usage", { width, height: 800 }, posts);
      w = await wordsOf(page, lang);
      const me = '.quota-autoreset[data-user="Me@example.com"]', two = '.quota-autoreset[data-user="two@example.com"]';
      const week = (row) => row + ' .ar-case[data-case="week"] .auto-reset', expiry = (row) => row + ' .ar-case[data-case="expiring"] .auto-reset';
      await page.locator(me).waitFor();
      assert.equal(await page.locator(".quota-autoreset").count(), 1, "only Codex's own named accounts in full have the say; the GLM team's and the plugin's don't");
      assert.equal(await page.locator(".quota-resets").count(), 3, "every account's resets in full are told");
      assert.equal(await page.locator(".quota-resets button").count(), 1, "only Codex's resets are used from here");
      // both times it uses one, each on its own line in words, with its own switch
      assert.equal(await text(page.locator(me + " .ar-say")), w.say);
      assert.deepEqual(await page.locator(me + " .ar-case-say").allTextContents(), [w.week, w.expiry]);
      for (const sel of [week(me), expiry(me)]) {
        assert.equal(await page.locator(sel).getAttribute("role"), "switch");
        assert.equal(await checked(page, sel), "false", "off until turned on");
        assert(await page.locator(sel).getAttribute("title"));
        assert(await fits(page, sel, ".subscription-card"), `${width}: the switch fits the card`);
      }
      assert.equal(await page.locator(week(me)).getAttribute("aria-label"), w.label + " · " + w.week);
      // the resets held: the one expiring first not kept until that time is on
      const kept = '.quota-resets .resets-kept';
      assert.equal((await page.locator(kept).textContent()).trim(), w.keptOff);
      assert.equal((await page.locator(".quota-resets .text").textContent()).trim(), w.use);
      // #2078's case: before one expires alone, a used-up week waited out
      await flip(page, expiry(me), posts, { user: "Me@example.com", on: true, when: "expiring" }, true, w.expiryOnly("Me@example.com"));
      assert.equal(await checked(page, week(me)), "false", "the week's kept off");
      assert.equal((await page.locator(kept).textContent()).trim(), w.keptOn);
      assert.equal(await page.locator(kept).evaluate((e) => e.classList.contains("on")), true);
      // then the week too, and off again: the one about to expire kept
      await flip(page, week(me), posts, { user: "Me@example.com", on: true, when: "week" }, true, w.both("Me@example.com"));
      await flip(page, week(me), posts, { user: "Me@example.com", on: false, when: "week" }, false, w.expiryOnly("Me@example.com"));
      assert.equal(await checked(page, expiry(me)), "true");
      assert.equal((await page.locator(kept).textContent()).trim(), w.keptOn);

      // the account in brief says its say beside its name, with one
      // switch for both: the account a week ran out on is the one in brief (#801)
      const brief = '.subscription-account.brief[data-card="codex|two@example.com"]';
      assert.equal((await page.locator(brief + " .acct-auto").textContent()).trim(), w.brief);
      assert.equal(await page.locator(brief + " .auto-reset").getAttribute("role"), "switch");
      assert.equal(await page.locator(brief + " .auto-reset").getAttribute("aria-label"), w.label);
      await flip(page, brief + " .auto-reset", posts, { user: "two@example.com", on: true }, true, w.both("two@example.com"));
      assert.equal(await page.locator(brief + " .acct-auto").evaluate((e) => e.classList.contains("on")), true);
      // its title names each time and whether it's on
      const briefTitle = await page.locator(brief + " .acct-auto").getAttribute("title");
      assert(briefTitle.includes(w.week + ": " + w.on) && briefTitle.includes(w.expiry + ": " + w.on), briefTitle);
      // in full, holding no reset, it has the say and both switches, and no reset row
      await page.locator(brief + " .quota-acct-fold").click();
      await page.locator(two).waitFor();
      assert.equal(await text(page.locator(two + " .ar-say")), w.say);
      assert.equal(await checked(page, week(two)), "true");
      assert.equal(await checked(page, expiry(two)), "true");
      assert.equal(await page.locator(".quota-resets .text").count(), 1, "nothing to use for the account holding none");
      await flip(page, expiry(two), posts, { user: "two@example.com", on: false, when: "expiring" }, false, w.weekOnly("two@example.com"));
      // back in brief: on, as the week is
      await page.locator('.subscription-account[data-card="codex|two@example.com"] .quota-acct-fold').click();
      await page.locator(brief + ' .auto-reset[aria-checked="true"]').waitFor();
      const after = await page.locator(brief + " .acct-auto").getAttribute("title");
      assert(after.includes(w.week + ": " + w.on) && after.includes(w.expiry + ": " + w.off), after);
      await flip(page, brief + " .auto-reset", posts, { user: "two@example.com", on: false }, false, w.none("two@example.com"));
      assert.equal(await page.locator(brief + " .acct-auto").evaluate((e) => e.classList.contains("on")), false);
      // and Me in brief: the resets it holds, and on
      await page.locator('.subscription-account[data-card="codex|Me@example.com"] .quota-acct-fold').click();
      const meBrief = '.subscription-account.brief[data-card="codex|Me@example.com"] .acct-auto';
      await page.locator(meBrief).waitFor();
      assert.equal((await page.locator(meBrief).textContent()).trim(), "↺ 2 ·" + w.brief);
      assert.equal(await page.locator(meBrief + " .auto-reset").getAttribute("aria-checked"), "true");
      const border = await page.evaluate(() => [...document.querySelectorAll(".quota-resets, .quota-resets *, .quota-autoreset, .quota-autoreset *, .acct-auto")].map((e) => getComputedStyle(e).borderLeftWidth).filter((b) => parseFloat(b) > 1));
      assert.deepEqual(border, [], "no left-border accent");
      assert.equal(await page.locator("select").count(), 0, "no native select");
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true, "no sideways scroll");

      // the menu bar panel's card
      const panelPosts = [];
      const panel = await open("http://uni-switch.test/?mode=panel", { width: Math.min(width, 440), height: 600 }, panelPosts);
      const psel = '.pq-autoreset[data-user="Me@example.com"]';
      await panel.locator('#ptabs [data-ptab="usage"]').click();
      await panel.locator(psel).waitFor();
      // the tab, opened, reads the accounts again (quotasOpened) and draws
      // its cards anew when they come: a switch found before that is gone
      // by the time it is scrolled to (#2360), as codex-credits found (#1622)
      await panel.waitForFunction(() => !quotasLoading);
      assert.equal(await text(panel.locator(psel + " .ar-say")), w.say);
      assert.deepEqual(await panel.locator(psel + " .ar-case-say").allTextContents(), [w.week, w.expiry]);
      assert.equal(await checked(panel, week(psel)), "false");
      assert.equal(await panel.locator(".pq-resets").count(), 3);
      assert.equal(await panel.locator(".pq-resets button").count(), 1, "only Codex's resets are used from the panel");
      assert.equal((await panel.locator(".pq-resets button").textContent()).trim(), w.useOne);
      assert.equal((await panel.locator(".pq-resets .resets-kept").textContent()).trim(), w.keptOff);
      await flip(panel, week(psel), panelPosts, { user: "Me@example.com", on: true, when: "week" }, true, w.weekOnly("Me@example.com"));
      // the week alone (tongzhenbao on X): the card says it is used when
      // the week runs out, not "not auto-used" beside that switch on
      assert.equal((await panel.locator(".pq-resets .resets-kept").textContent()).trim(), w.keptWeek, "the week alone says the reset is used when the week runs out");
      assert.equal(await panel.locator(".pq-resets .resets-kept").evaluate((e) => e.classList.contains("on")), true);
      await flip(panel, expiry(psel), panelPosts, { user: "Me@example.com", on: true, when: "expiring" }, true, w.both("Me@example.com"));
      assert.equal((await panel.locator(".pq-resets .resets-kept").textContent()).trim(), w.keptOn);
      // the switches and their words fit the card: none of it cut off or wider
      for (const sel of [psel, week(psel), expiry(psel), psel + ' .ar-case[data-case="week"] .ar-case-say', psel + ' .ar-case[data-case="expiring"] .ar-case-say']) assert(await fits(panel, sel, ".pq-card"), `${sel} fits the panel's card`);
      // the account holding none, shown: its say too
      // its group's Show more, not the tab's Arrange (490d2ea3), which is a .pq-more too
      await panel.locator(".pq-group .pq-more").click();
      const ptwo = '.pq-autoreset[data-user="two@example.com"]';
      await panel.locator(ptwo).waitFor();
      await flip(panel, expiry(ptwo), panelPosts, { user: "two@example.com", on: true, when: "expiring" }, true, w.expiryOnly("two@example.com"));

      const missing = await page.evaluate((strings) => ["zh", "zh-TW", "ja", "de"].flatMap((l) => strings.filter((k) => !I18N[l][k]).map((k) => l + ": " + k)), strings);
      assert.deepEqual(missing, [], "every string has its translations");
      // the credit is 重置卡 in Chinese wherever it is counted or spent
      const zhCredit = await page.evaluate(() => ["1 reset", "{n} resets", "Use a reset", "Use one…", "Use a Codex reset?", "{who} no longer uses a reset by itself"].map((k) => I18N.zh[k]));
      for (const s of zhCredit) assert.match(s, /张|重置卡/, s);
      assert.deepEqual(errors, []);
    });
    }
  }
}

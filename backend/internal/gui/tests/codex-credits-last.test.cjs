// Run with Node's test runner and Playwright on the module path; see README.md.
// gokoshiki on Discord: a Codex subscription, then an OpenAI API key, then
// the subscription's credits. Under a Codex account's Use credits, while
// it is on, a line of its own says "Only after every other account, key
// and fallback" with its switch: off until turned on, it posts
// settings/codex-credits with last for that account, says so, and turns;
// Use credits turned off takes the line away. The panel's card has it
// too, and its folded line says it. On the Routing page a request that
// went to another while the account was on its credits says why it was
// behind, and why the conversation moved. At narrow widths nothing is cut
// off or wider than its card, a click moves nothing, and no left-border
// accent. Every language, Chromium and WebKit; no backend, the API is
// faked here.
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
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const at = new Date(now.getTime() - 60e3).toISOString();
const key = { id: "openai", provider: "openai", name: "OpenAI", kind: "key", model: "gpt-5.5", fallback: true };
const me = { id: "codex", provider: "codex", name: "Codex", kind: "account", who: "Me@example.com", model: "gpt-5.5",
  known: true, used: 100, credits: true, creditsLast: true };
const routes = [{ id: 7, seq: 7, time: at, agent: "codex", model: "gpt-5.5", provider: "codex", order: [key, me],
  affinity: { why: "credits", last: "codex", at: new Date(now.getTime() - 120e3).toISOString() },
  tries: [{ id: key.id, model: "gpt-5.5", start: at, done: true, status: 200, ms: 400 }], done: true, status: 200, ms: 400 }];

function serve(lang, posts) {
  let off = [], last = [];
  const settings = () => ({ theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd", trayUsages: ["codex"], trayUsageEvery: 3, codexNoCredits: off, codexCreditsLast: last });
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: settings() });
    if (url.pathname === "/api/settings") return json(settings());
    if (url.pathname === "/api/settings/codex-credits") {
      // as the server: off clears last, last left out keeps it
      const body = req.postDataJSON();
      posts.push(body);
      const who = body.user.toLowerCase();
      const wasLast = last.includes(who);
      off = off.filter((u) => u !== who);
      last = last.filter((u) => u !== who);
      if (!body.on) off.push(who);
      else if (body.last ?? wasLast) last.push(who);
      return json(settings());
    }
    if (url.pathname === "/api/usage/quotas") return json(quotas);
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) await new Promise((r) => setTimeout(r, 20e3));
      return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname === "/api/gateway/history") {
      const d = url.searchParams.get("day");
      return json({ cut: false, days: [{ day, requests: routes.length }], routes: d ? routes : [] });
    }
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { line: "Only after every other account, key and fallback", on: (who) => `${who} spends its credits only after everything else`, brief: "Use credits: On, after everything else",
    behind: "Me@example.com has used up its allowance and goes on on its credits, which it is set to spend only after everything else: it is tried after every other account, key and fallback.",
    moved: "Me@example.com answered this conversation last, but it is on its credits now, set to spend them only after everything else, so the conversation moves to" },
  zh: { line: "仅在其他账号、密钥和备用都用不上时", on: (who) => `${who} 仅在其他都用不上时才使用积分`, brief: "使用积分： 开，最后使用",
    behind: "Me@example.com 额度已用尽、改用积分，且设为最后使用积分：排在所有其他账号、密钥和备用之后。",
    moved: "此会话上次由 Me@example.com 回答，但它现在在用积分，且设为最后使用积分，所以会话换到" },
  "zh-TW": { line: "僅在其他帳號、金鑰和備用都用不上時", on: (who) => `${who} 僅在其他都用不上時才使用積分`, brief: null,
    behind: "Me@example.com 額度已用盡、改用積分，且設為最後使用積分：排在所有其他帳號、金鑰和備用之後。",
    moved: "此會話上次由 Me@example.com 回答，但它現在在用積分，且設為最後使用積分，所以會話換到" },
  ja: { line: "他のアカウント・キー・フォールバックをすべて使った後のみ", on: (who) => `${who} は他をすべて使った後のみクレジットを使用します`, brief: null,
    behind: "Me@example.com は利用枠を使い切りクレジットで続行中ですが、クレジットは最後に使う設定のため、他のアカウント・キー・フォールバックのすべての後に試します。",
    moved: "この会話に前回応答したのは Me@example.com ですが、今はクレジットで続行中で、クレジットは最後に使う設定のため、会話は" },
  de: { line: "Erst nach allen anderen Konten, Schlüsseln und Fallbacks", on: (who) => `${who} verbraucht Credits erst nach allem anderen`, brief: null,
    behind: "Me@example.com hat sein Kontingent aufgebraucht und läuft mit Credits weiter, die es erst zuletzt verbrauchen soll: Es wird nach allen anderen Konten, Schlüsseln und Fallbacks versucht.",
    moved: "Me@example.com hat diese Unterhaltung zuletzt beantwortet, läuft aber jetzt mit Credits, die es erst zuletzt verbrauchen soll, daher wechselt die Unterhaltung zu" },
};

const keys = [
  "Only after every other account, key and fallback",
  "On: past its windows, it goes after every other account, key and fallback, so its credits are spent last. Click to turn it off.",
  "Off: past its windows, it answers on its credits when Routing comes to it.",
  "{who} spends its credits when its windows run out",
  "{who} spends its credits only after everything else",
  "On, after everything else",
  "Every one here has used up its allowance and goes on on its credits, set to spend them only after everything else, so {who}, first in line, goes first.",
  "{who} has used up its allowance and goes on on its credits, which it is set to spend only after everything else: it is tried after every other account, key and fallback.",
  "{who} answered this conversation last, but it is on its credits now, set to spend them only after everything else, so the conversation moves to {first}.",
  "{n} used · on credits · after the others", "{n} left · on credits · after the others",
];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(words)) {
    const w = words[lang];
    test(`${engine} ${lang}: a Codex account's credits spent only after everything else`, async (t) => {
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
      // the line fits its card, its switch whole and on the line's end,
      // the say's switch still by the say, and nothing cut off
      const fits = (page, row, card) => page.evaluate(([row, card]) => {
        const r = document.querySelector(row);
        const c = r.closest(card).getBoundingClientRect(), b = r.getBoundingClientRect();
        const line = r.querySelector(".ar-credits-last"), s = line.querySelector(".credits-last").getBoundingClientRect();
        const main = r.querySelector(".use-credits").getBoundingClientRect(), say = r.querySelector(".ar-say").getBoundingClientRect();
        const words = line.querySelector(".ar-case-say");
        return b.right <= c.right + 0.5 && s.right <= c.right + 0.5 && s.width >= 27 && s.height >= 15
          && main.top < say.bottom && s.top >= main.bottom - 0.5 && words.scrollWidth <= words.clientWidth + 1;
      }, [row, card]);

      for (const width of [900, 440]) {
        const posts = [];
        const page = await open("http://uni-switch.test/?view=usage", { width, height: 760 }, posts);
        const row = '.quota-credits[data-user="Me@example.com"]';
        await page.locator(row + " .ar-credits-last").waitFor();
        assert.equal(await text(page.locator(row + " .ar-credits-last")), w.line);
        const sw = page.locator(row + " .credits-last");
        assert.equal(await sw.getAttribute("role"), "switch");
        assert.equal(await sw.getAttribute("aria-checked"), "false", "in its place until set");
        assert(await sw.getAttribute("title"));
        assert.equal(await page.locator(row + " .use-credits").count(), 1);
        assert(await fits(page, row, ".subscription-card"), `the line fits the card at ${width}px`);
        await flip(page, row, posts, ".credits-last", true, { user: "Me@example.com", on: true, last: true }, w.on("Me@example.com"));
        assert(await page.locator(row + " .ar-credits-last.on").count(), 1);
        assert(await fits(page, row, ".subscription-card"), `the line, on, fits the card at ${width}px`);
        const back = await page.evaluate(() => t("{who} spends its credits when its windows run out", { who: "Me@example.com" }));
        await flip(page, row, posts, ".credits-last", false, { user: "Me@example.com", on: true, last: false }, back);
        // Use credits off: no line, as nothing is spent at all
        await page.locator(row + " .use-credits").click();
        await wait(posts, 3);
        assert.deepEqual(posts.at(-1), { user: "Me@example.com", on: false });
        await page.locator(row + ' .use-credits[aria-checked="false"]').waitFor();
        assert.equal(await page.locator(row + " .ar-credits-last").count(), 0);
        const border = await page.evaluate(() => [...document.querySelectorAll(".quota-credits, .quota-credits *")].map((e) => getComputedStyle(e).borderLeftWidth).filter((b) => parseFloat(b) > 1));
        assert.deepEqual(border, [], "no left-border accent");
        await page.context().close();
      }

      // the menu bar panel's card, at its own width and narrower, and its
      // folded line
      for (const width of [440, 360]) {
        const posts = [];
        const panel = await open("http://uni-switch.test/?mode=panel", { width, height: 640 }, posts);
        const row = '.pq-credits[data-user="Me@example.com"]';
        await panel.locator('#ptabs [data-ptab="usage"]').click();
        await panel.locator(row + " .ar-credits-last").waitFor();
        await panel.waitForFunction(() => !quotasLoading);
        assert(await fits(panel, row, ".pq-card"), `the line fits the panel's card at ${width}px`);
        await flip(panel, row, posts, ".credits-last", true, { user: "Me@example.com", on: true, last: true }, w.on("Me@example.com"));
        assert(await fits(panel, row, ".pq-card"), `the line, on, fits the panel's card at ${width}px`);
        const id = await panel.locator(row).evaluate((r) => r.closest(".pq-card").dataset.card);
        const card = `#panelQuota .pq-card[data-card="${id}"]`;
        await panel.locator(card + " .pq-opts-fold").click();
        const brief = panel.locator(card + " .pq-opts-brief .ar-say");
        await brief.waitFor();
        const said = await text(brief);
        const want = w.brief || (await panel.evaluate(() => t("Use credits:") + " " + t("On, after everything else")));
        assert(said.endsWith(want), said);
        await panel.context().close();
      }

      // the Routing page says why the account went behind the key, and why
      // the conversation moved
      const routing = await open("http://uni-switch.test/?view=routing", { width: 1100, height: 760 }, []);
      await routing.locator(".rt-days .rt-day").nth(1).click();
      await routing.locator(".rt-req").first().click();
      await routing.waitForTimeout(300);
      const steps = await routing.locator(".rt-steps li").evaluateAll((ls) => ls.map((l) => l.textContent));
      assert(steps.some((s) => s.includes(w.behind)), JSON.stringify(steps));
      assert(steps.some((s) => s.includes(w.moved)), JSON.stringify(steps));
      if (lang !== "en") {
        const missing = await routing.evaluate(([keys, lang]) => keys.filter((k) => !I18N[lang][k]), [keys, lang]);
        assert.deepEqual(missing, [], `every string has its ${lang}`);
      }
      assert.deepEqual(errors, []);
    });
  }
}

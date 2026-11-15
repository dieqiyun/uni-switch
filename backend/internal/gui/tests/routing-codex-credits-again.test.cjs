// Run with Node's test runner and Playwright on the module path; see README.md.
// A Codex account that holds credits goes on past its allowance (豆子 and
// Zhenzhen on Discord): the Routing page says why it went first of the
// accounts all but used up, that the one beside it without credits waits
// behind it, and that its refusal as its allowance ran out was asked again
// at once onto its credits rather than ended for the agent, the try that
// then answered told as the one the agent got. English, Chinese, Japanese
// and German, Chromium and WebKit; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const later = new Date(now.getTime() + 3 * 864e5).toISOString();
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const at = new Date(now.getTime() - 60e3).toISOString();
const credits = { id: "codex@me@example.com", provider: "codex", name: "Codex", kind: "account", who: "me@example.com", model: "gpt-5.5",
  known: true, used: 100, renews: [later], usedRenews: later, credits: true };
const spent = { id: "codex@spare@example.com", provider: "codex", name: "Codex", kind: "account", who: "spare@example.com", model: "gpt-5.5",
  known: true, used: 99, renews: [later], usedRenews: later };
const routes = [{ id: 7, seq: 7, time: at, agent: "codex", model: "gpt-5.5", provider: "codex", order: [credits, spent],
  tries: [
    { id: credits.id, model: "gpt-5.5", start: at, done: true, status: 429, ms: 300, fail: "quota", credits: true },
    { id: credits.id, model: "gpt-5.5", start: at, done: true, status: 200, ms: 400 },
  ], done: true, status: 200, ms: 700 },
  // one with allowance left goes first; the credits account is next, ahead
  // of the one used up with none
  { id: 8, seq: 8, time: new Date(now.getTime() - 30e3).toISOString(), agent: "codex", model: "gpt-5.5", provider: "codex",
    order: [{ ...spent, id: "codex@fine@example.com", who: "fine@example.com", used: 20 }, credits, spent],
    tries: [{ id: "codex@fine@example.com", model: "gpt-5.5", start: at, done: true, status: 200, ms: 400 }], done: true, status: 200, ms: 400 }];

function serve(lang) {
  const settings = () => ({ theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd" });
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: settings() });
    if (url.pathname === "/api/settings") return json(settings());
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

// a few words of each line, in each language, enough to know it
const words = {
  en: { first: "goes on on its credits, which the vendor spends", aside: "goes on on its credits — ahead of those used up", again: "so it is asked again at once", answered: "never saw the 1 that failed first" },
  zh: { first: "额度用完后供应商会改扣其 Credits", aside: "但仍可用 Credits 继续", again: "因此立即重试", answered: "" },
  ja: { first: "利用枠を使い切るとベンダーがクレジットを使うので", aside: "クレジットで続けられます", again: "すぐもう一度依頼します", answered: "" },
  de: { first: "es macht mit seinen Credits weiter", aside: "macht aber mit seinen Credits weiter", again: "also wird es sofort erneut gefragt", answered: "" },
};

const keys = [
  "Its allowance had just run out and it was refused, so it was asked again at once to spend its credits.",
  "{who} answered {status}: its allowance had just run out, but the account holds credits, which the vendor spends once the allowance is used up, so it is asked again at once, before any of the reply reaches {agent}.",
  "It holds credits, so it rests only {d} and its usage is read again, not until its allowance renews",
  "{who} is at {n} of its allowance but goes on on its credits — ahead of those used up with nothing to go on with, after those with allowance left.",
  "{n} used · on credits", "{n} left · on credits",
  "Every account is all but used up, so {who} goes first: it goes on on its credits, which the vendor spends once its allowance is used up.",
];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "ja", "de"]) {
    const w = words[lang];
    test(`${engine} ${lang}: a Codex account on its credits goes first of the spent, and its refusal at the edge is asked again`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const errors = [];
      const page = await (await browser.newContext({ viewport: { width: 1100, height: 760 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      await page.goto("http://uni-switch.test/?view=routing");
      await page.locator(".rt-days .rt-day").nth(1).click();
      let steps = "";
      await page.locator(".rt-req").nth(1).waitFor();
      assert.equal(await page.locator(".rt-req").count(), 2);
      for (const i of [0, 1]) {
        await page.locator(".rt-req").nth(i).click();
        await page.waitForTimeout(300);
        steps += (await page.locator(".rt-steps li").evaluateAll((ls) => ls.map((l) => l.textContent.replace(/\s+/g, " ")))).join("\n") + "\n";
      }
      for (const k of ["first", "aside", "again", "answered"]) if (w[k]) assert(steps.includes(w[k]), `${k}: ${steps}`);
      // the account on credits is never told as all but used up
      if (lang === "en") assert(!steps.includes("me@example.com is at 100% — all but used up"), steps);
      if (lang !== "en") {
        const missing = await page.evaluate(([keys, lang]) => keys.filter((k) => !I18N[lang][k]), [keys, lang]);
        assert.deepEqual(missing, [], `every string has its ${lang}`);
      }
      assert.deepEqual(errors, []);
    });
  }
}

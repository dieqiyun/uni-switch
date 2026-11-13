// Run with Node's test runner and Playwright on the module path; see README.md.
// Until used up (routes.Drain, dzxx0496 on Discord): on the Routing page a
// request routed so says the one in use answers everything until it is
// used up, and the mode line says the next keeps the requests once the one
// before renews. The option is named in every language, in both engines,
// at the window's width and a phone's.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const ago = (minutes) => new Date(now.getTime() - minutes * 60e3).toISOString();
const acct = (who, more) => ({ id: "wb#" + who, provider: "wb", name: "WorkBuddy", who, kind: "account", model: "glm-5", routing: "drain", known: true, used: 30, ...more });

function serve(lang) {
  // the second account is in use: the first renewed after it was used up
  const order = [acct("two@example.test"), acct("three@example.test", { used: 5 }), acct("one@example.test", { used: 0 })];
  const route = {
    id: 1, seq: 1, time: ago(1), agent: "codex", model: "glm-5", provider: "wb", done: true, status: 200, ms: 900, tokens: 1200, order,
    tries: [{ id: order[0].id, model: "glm-5", start: ago(1), done: true, status: 200, ms: 900 }],
  };
  return async (request) => {
    const url = new URL(request.request().url());
    const json = (data) => request.fulfill({ json: data });
    if (url.pathname === "/boot.js") return request.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return request.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) return new Promise(() => {}); // nothing more comes
      return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 1 }, routes: [route] });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await request.fulfill({ body: await fs.readFile(file), contentType });
  };
}

async function open(t, engine, lang, width) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  t.after(() => browser.close());
  const page = await browser.newPage({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.route("**/*", serve(lang));
  await page.goto("http://uni-switch.test/?view=routing");
  const req = page.locator(".rt-req").first();
  await req.waitFor();
  if (await req.getAttribute("aria-pressed") !== "true") await req.click();
  await page.locator(".rt-steps li.why").first().waitFor();
  const say = (key, vars = {}) => page.evaluate(([lang, key, vars]) => {
    const s = (lang !== "en" && I18N[lang]?.[key]) || key;
    return s.replace(/\{(\w+)\}/g, (_, k) => vars[k] ?? `{${k}}`);
  }, [lang, key, vars]);
  return { page, errors, say };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [1100, 420]) {
      test(`${engine} ${lang} ${width}: Until used up says the one in use keeps the requests`, async (t) => {
        const { page, errors, say } = await open(t, engine, lang, width);
        const why = (await page.locator(".rt-steps li.why").first().textContent()).trim();
        assert.equal(why, await say("Until used up: {who} is the one in use, and answers everything until it is used up.", { who: "two@example.test" }));
        assert.equal((await page.locator(".rt-mode").textContent()).trim(),
          await say("Until used up: one answers everything until its quota is used up; then the next, which keeps them even after the one before renews."));
        if (lang !== "en") {
          for (const k of ["Until used up", "Requests stay on one until it is used up, then go to the next in the list and stay there: one before it that renews meanwhile doesn't take them back. One rate limited or failing is passed over while it rests, and requests come back to it after."])
            assert.notEqual(await say(k), k, `${lang} says ${k}`);
        }
        assert.ok(await page.evaluate(() => document.scrollingElement.scrollWidth <= innerWidth + 1), "fits");
        assert.deepEqual(errors, []);
      });
    }
  }
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// Speed first (routes.Fastest) on the Routing page: a request's story says
// why the first went first in the words of its speed lately, as the
// gateway's trace gives it (Weighed.lateTtft, lateTps): "answered fastest
// lately — first token in 0.8s, 62 tokens a second". One not measured
// lately that took a probe's turn says so, and with none measured the
// listed first says it is measured by its answer. The mode line says what
// Speed first does. In both engines and every language.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const ago = (minutes) => new Date(now.getTime() - minutes * 60e3).toISOString();
const key = (who, more) => ({ id: "relay#" + who, provider: "relay", name: "Relay", who, kind: "key", model: "deepseek-v4", routing: "fast", ...more });

const scenes = {
  // b answered fastest lately, so it went first though a is listed first
  measured: { order: [key("relay-b", { lateTtft: 820, lateTps: 61.6 }), key("relay-a", { lateTtft: 2400, lateTps: 40 })] },
  // a told no pace lately: its first token alone
  noPace: { order: [key("relay-a", { lateTtft: 1450 }), key("relay-b", { lateTtft: 3000 })] },
  // c not measured lately took the probe's turn ahead of the measured a
  probe: { order: [key("relay-c"), key("relay-a", { lateTtft: 900 })] },
  // none measured: the list's first
  none: { order: [key("relay-a"), key("relay-b")] },
};

function serve(lang, scene) {
  const s = scenes[scene];
  const route = {
    id: 1, seq: 1, time: ago(1), agent: "codex", model: "deepseek-v4", provider: "relay", done: true, status: 200, ms: 900, tokens: 1200, order: s.order,
    tries: [{ id: s.order[0].id, model: "deepseek-v4", start: ago(1), done: true, status: 200, ms: 900 }],
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

async function open(t, engine, lang, scene, width = 1100) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  t.after(() => browser.close());
  const page = await browser.newPage({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.route("**/*", serve(lang, scene));
  await page.goto("http://uni-switch.test/?view=routing");
  const req = page.locator(".rt-req").first();
  await req.waitFor();
  if (await req.getAttribute("aria-pressed") !== "true") await req.click();
  await page.locator(".rt-steps li.why").first().waitFor();
  // what a string says in lang, its {names} filled in; a language without
  // it would say it in English, which the i18n-keys test catches
  const say = (key, vars = {}) => page.evaluate(([lang, key, vars]) => {
    const s = (lang !== "en" && I18N[lang]?.[key]) || key;
    return s.replace(/\{(\w+)\}/g, (_, k) => vars[k] ?? `{${k}}`);
  }, [lang, key, vars]);
  return { page, errors, say, why: async () => (await page.locator(".rt-steps li.why").first().textContent()).trim() };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${engine} ${lang}: the fastest lately says its first token and pace`, async (t) => {
      const { page, errors, say, why } = await open(t, engine, lang, "measured");
      const pace = await say("first token in {s}s, {n} tokens a second", { s: "0.8", n: "62" });
      assert.equal(await why(), await say("Speed first: {who} answered {model} fastest lately — {pace}.", { who: "relay-b", model: "deepseek-v4", pace }));
      assert.equal((await page.locator(".rt-mode").textContent()).trim(),
        await say("Requests go to the one that has answered this model fastest lately, by its time to the first token and how fast it writes; those within a tenth of each other keep their listed order. One not measured lately gets one request in five, so it is measured again, and one that fails is passed over while it rests."));
      if (lang !== "en") assert.notEqual(await say("Speed first"), "Speed first", `${lang} names Speed first`);
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: no pace lately, its first token alone`, async (t) => {
      const { errors, say, why } = await open(t, engine, lang, "noPace");
      const pace = await say("first token in {s}s", { s: "1.4" });
      assert.equal(await why(), await say("Speed first: {who} answered {model} fastest lately — {pace}.", { who: "relay-a", model: "deepseek-v4", pace }));
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: one not measured lately took its turn to be measured`, async (t) => {
      const { errors, say, why } = await open(t, engine, lang, "probe");
      assert.equal(await why(), await say("Speed first: {who} hasn't answered {model} lately, so it takes this request to be measured — one request in five goes to one not measured.", { who: "relay-c", model: "deepseek-v4" }));
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: none measured, the list's first`, async (t) => {
      const { errors, say, why } = await open(t, engine, lang, "none");
      assert.equal(await why(), await say("Speed first: none has answered {model} lately, so {who}, first in the list, goes first and its answer measures it.", { who: "relay-a", model: "deepseek-v4" }));
      assert.deepEqual(errors, []);
    });
  }
}

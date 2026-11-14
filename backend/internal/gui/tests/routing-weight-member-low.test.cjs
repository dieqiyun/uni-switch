// Run with Node's test runner and Playwright on the module path; see README.md.
// A group routed by weight, or in order, takes each member whole: a member
// behind the first is there by the group's round or its order, not by
// what its accounts have left. The Routing page told a group in a group by
// weight, its accounts at 91%, as "kept for when the others can't" when
// the round gave the request to the other member (Creat.wu on Discord),
// and it read as passed over. Now it says the member isn't passed over
// and takes its share, once for the member. Within one member, and in a
// Smart group that pools its members' accounts, the low line stays. In
// all five languages, in Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const ago = (minutes) => new Date(now.getTime() - minutes * 60e3).toISOString();
const ahead = (hours) => new Date(now.getTime() + hours * 36e5).toISOString();

const acct = (user, used, extra = {}) => ({ id: "codex#" + user, provider: "codex", name: "Codex", who: user, kind: "account", agent: "codex", model: "gpt-6-astra",
  known: true, used, renews: [ahead(3), ahead(90)], ...extra });
const key = (extra = {}) => ({ id: "xai", provider: "xai", name: "XKey", kind: "provider", model: "gpt-6-astra", ...extra });
const sub = { id: "sb", name: "Pool B", routing: "", in: "pa" };

const scenes = {
  // the round gave this request to the key: the group in it, all near
  // its limit, is behind by weight
  weight: { group: { id: "pa", name: "Parent", routing: "weight", subs: [sub] },
    order: [key({ turn: true }), acct("me@example.com", 91, { via: ["sb"] }), acct("spare@example.com", 92, { via: ["sb"] })] },
  // the round gave it to the group in it: its second account is behind
  // its first by Smart, within the member
  weightInner: { group: { id: "pa", name: "Parent", routing: "weight", subs: [sub] },
    order: [acct("me@example.com", 91, { via: ["sb"], turn: true }), acct("spare@example.com", 92, { via: ["sb"] }), key()] },
  // in order: the account's member comes after the key's
  order: { group: { id: "pa", name: "Parent", routing: "order" },
    order: [key(), acct("me@example.com", 91)] },
  // Smart pools the members' accounts: low is why
  smart: { group: { id: "pa", name: "Parent", routing: "" },
    order: [key(), acct("me@example.com", 91)] },
};

function serve(lang, scene) {
  const s = scenes[scene];
  const route = {
    id: 1, seq: 1, time: ago(1), agent: "codex", model: "gpt-6-astra", provider: s.order[0].provider, done: true, status: 200, ms: 900, tokens: 1200,
    group: s.group, order: s.order,
    tries: [{ id: s.order[0].id, model: "gpt-6-astra", start: ago(1), done: true, status: 200, ms: 900 }],
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
  const say = (key, vars = {}) => page.evaluate(([lang, key, vars]) => {
    const s = (lang !== "en" && I18N[lang]?.[key]) || key;
    return s.replace(/\{(\w+)\}/g, (_, k) => vars[k] ?? `{${k}}`);
  }, [lang, key, vars]);
  const steps = () => page.locator(".rt-steps li").allTextContents();
  return { page, errors, say, steps };
}

const LOW = "{who} is at {n} — kept for when the others can't.";
const WEIGHT = "{member} isn't passed over for being near its limit: by weight this request was {first}'s share, and {member} takes its own share of the requests while it can answer.";
const ORDER = "{member} isn't passed over for being near its limit: it comes after {first} in {group}'s order.";

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${engine} ${lang}: a group in a group by weight, near its limit, is told as taking its share`, async (t) => {
      const { errors, say, steps } = await open(t, engine, lang, "weight");
      const all = await steps();
      const want = await say(WEIGHT, { member: "Pool B", first: "XKey" });
      if (lang !== "en") assert.ok(!want.startsWith("Pool B isn't"), `not translated: ${want}`);
      assert.equal(all.filter((s) => s.includes(want)).length, 1, all.join("\n"));
      for (const [user, n] of [["me@example.com", "91%"], ["spare@example.com", "92%"]]) {
        const low = await say(LOW, { who: user, n });
        assert.ok(!all.some((s) => s.includes(low)), `told as kept for last: ${low}\n${all.join("\n")}`);
      }
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: within the member by weight, its second account is still told as kept for last`, async (t) => {
      const { errors, say, steps } = await open(t, engine, lang, "weightInner");
      const all = await steps();
      const low = await say(LOW, { who: "spare@example.com", n: "92%" });
      assert.ok(all.some((s) => s.includes(low)), all.join("\n"));
      const head = (await say(WEIGHT, { member: "Pool B", first: "XKey" })).slice(0, 12);
      assert.ok(!all.some((s) => s.startsWith(head)), all.join("\n"));
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: a member behind another in order is told by the order`, async (t) => {
      const { errors, say, steps } = await open(t, engine, lang, "order");
      const all = await steps();
      const want = await say(ORDER, { member: "Codex", first: "XKey", group: "Parent" });
      assert.ok(all.some((s) => s.includes(want)), `want ${want}\n${all.join("\n")}`);
      const low = await say(LOW, { who: "me@example.com", n: "91%" });
      assert.ok(!all.some((s) => s.includes(low)), all.join("\n"));
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: a Smart group that pools its members still says low is why`, async (t) => {
      const { errors, say, steps } = await open(t, engine, lang, "smart");
      const all = await steps();
      const low = await say(LOW, { who: "me@example.com", n: "91%" });
      assert.ok(all.some((s) => s.includes(low)), all.join("\n"));
      assert.deepEqual(errors, []);
    });
  }
}

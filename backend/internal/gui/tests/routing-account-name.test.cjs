// An account's name of the user's own (#1515) is what the Routing page
// calls it (upstream plugins #310, molvqingtai): a Kiro account
// signed in as "example@gmail.com (foo)" and renamed in uni-switch to
// "example@gmail.com (bar)" still read "(foo)" on every request made
// after. The trace keeps the vendor's name (who), the key the user's name
// is kept by; the page looks the name up as it draws, so a request from
// before the rename reads by the new name too, and the two stay one
// account. A key's name and an unnamed account read as they were. An
// unnamed account a plugin's usage names (#4416) reads by that name.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const VENDOR = "example@gmail.com (foo)", NAMED = "example@gmail.com (bar)", OTHER = "other@example.com";

function routes(now) {
  const reset = new Date(now.getTime() + 4 * 3600e3).toISOString();
  const kiro = { provider: "kiro", name: "Kiro", kind: "account", icon: "kiro", agent: "kiro", plan: "Pro", model: "claude-sonnet-4.5", known: true, used: 10, renews: [reset] };
  const named = { ...kiro, id: "kiro", who: VENDOR };
  const other = { ...kiro, id: "kiro@" + OTHER, who: OTHER, used: 20 };
  const key = { provider: "relay", name: "Relay", kind: "key", icon: "generic", id: "relay#k1", who: "team key", model: "claude-sonnet-4.5", routing: "" };
  return Array.from({ length: 4 }, (_, i) => {
    const time = new Date(now.getTime() - (4 - i) * 1000).toISOString();
    const id = i === 3 ? other.id : "kiro";
    return { id: i + 1, seq: i + 1, time, agent: "claude", model: "kiro/claude-sonnet-4.5", provider: "kiro", order: [named, other, key], done: true,
      status: 200, ms: 20, tries: [{ id, model: "claude-sonnet-4.5", start: time, done: true, status: 200, ms: 20 }] };
  }).reverse();
}

function serve(lang, now, names, users = () => ({})) {
  const assets = path.resolve(__dirname, "../assets");
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs={lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window={};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.has("wait")) return;
      return json({ mine: true, now: now.toISOString(), seq: 4, totals: { requests: 4, rerouted: 0, errors: 0 }, routes: routes(now) });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [{
      id: "kiro", name: "Kiro", icon: "kiro", models: [{ id: "claude-sonnet-4.5", on: true }], keyList: [],
      account: { agent: "plugin", user: VENDOR, logins: [{ user: VENDOR, active: true, on: true }, { user: OTHER, on: true }] },
      accountNames: names(), accountUsers: users(),
    }], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file).catch(() => ""), contentType });
  };
}

for (const engine of process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"]) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: Routing names an account by the name the user gave it`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await browser.newPage({ viewport: { width: 1100, height: 1000 } });
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const now = new Date();
      let names = {}, users = {};
      await page.route("**/*", serve(lang, now, () => names, () => users));
      await page.goto("http://uni-switch.test/?view=routing");
      await page.locator(".rt-act").first().waitFor();
      const acts = page.locator(".rt-acts");
      assert.ok((await acts.innerText()).includes(VENDOR), "unnamed, the account reads as its vendor names it");

      names = { [VENDOR.toLowerCase()]: NAMED };
      await page.reload();
      await page.locator(".rt-act").first().waitFor();
      const text = await acts.innerText();
      assert.ok(text.includes(NAMED), "renamed, the account reads by its new name: " + text);
      assert.ok(!text.includes(VENDOR), "and no longer by the old one: " + text);
      assert.equal(await page.locator(".rt-act").count(), 3, "the requests from before and after are one account's, beside the other and the key");
      assert.equal(await page.locator(".rt-act", { hasText: OTHER }).count(), 1, "an account with no name of its own reads as before");
      assert.equal(await page.locator(".rt-act", { hasText: "team key" }).count(), 1, "a key reads by its own name");
      // the list of requests says who answered by the same name
      const list = await page.locator(".said").allInnerTexts();
      assert.ok(list.some((s) => s.includes(NAMED)), "the requests list names who answered by the new name: " + list.join(" | "));
      assert.ok(!list.some((s) => s.includes(VENDOR)), "and never by the old: " + list.join(" | "));
      // a plugin's usage names an account (a masked phone for an IAM id,
      // #4416): the account reads by it, and the user's own name still wins
      users = { [OTHER.toLowerCase()]: "180****2361", [VENDOR.toLowerCase()]: "139****0000" };
      await page.reload();
      await page.locator(".rt-act").first().waitFor();
      const byPlugin = await acts.innerText();
      assert.equal(await page.locator(".rt-act", { hasText: "180****2361" }).count(), 1, "the plugin's name for an unnamed account: " + byPlugin);
      assert.ok(!byPlugin.includes(OTHER) && !byPlugin.includes("139****0000") && byPlugin.includes(NAMED), "the id gives way, the user's own name doesn't: " + byPlugin);
      if (process.env.ARTIFACT_DIR) {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-routing-account-name.png`) });
      }
      assert.deepEqual(errors, []);
    });
  }
}

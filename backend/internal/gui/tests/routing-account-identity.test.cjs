const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

// The agent's current login changes its trace ID from provider@user to
// provider. Old requests keep both IDs, and provider used to name another
// account. Counts and cooldowns must follow each request's account identity.
function switchedRoutes(now, resting) {
  const reset = new Date(now.getTime() + 4 * 3600e3).toISOString();
  const university = { provider: "claude", name: "Claude Code", kind: "account", icon: "claude-color", who: "uni@example.test · University", plan: "enterprise", model: "claude-opus-5-5", known: true, used: 100, renews: [reset] };
  const team = { ...university, who: "seat@example.test · Research Group", plan: "team", used: 1 };
  const oldTeamID = "claude@" + team.who;
  const old = [{ ...university, id: "claude" }, { ...team, id: oldTeamID }];
  const rest = { why: "quota", until: new Date(now.getTime() + 15 * 60e3).toISOString(), key: oldTeamID.toLowerCase() };
  return Array.from({ length: 60 }, (_, i) => {
    const time = new Date(now.getTime() - (60 - i) * 1000).toISOString();
    const done = i < 58;
    const failed = i === 57;
    const id = i < 40 ? "claude" : oldTeamID;
    const order = i < 58 ? old : [{ ...university, id: "claude@" + university.who }, { ...team, id: "claude", ...(resting ? { rest } : {}) }];
    return { id: i + 1, seq: i + 1, time, agent: "codex", model: "claude/claude-opus-5-5", provider: "claude", order, done,
      status: failed ? 429 : 200, ms: 20, tries: done ? [{ id, model: "claude-opus-5-5", start: time, done: true, status: failed ? 429 : 200, ms: 20, ...(failed ? { fail: "quota", rest } : {}) }] : [] };
  }).reverse();
}

function serve(lang, getRoutes, now) {
  const assets = path.resolve(__dirname, "../assets");
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs={lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window={};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.has("wait")) return;
      return json({ mine: true, now: now.toISOString(), seq: 60, totals: { requests: 60, rerouted: 0, errors: 1 }, routes: getRoutes() });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file).catch(() => ""), contentType });
  };
}

for (const engine of process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"]) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: account statistics survive a current-login switch`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await browser.newPage({ viewport: { width: 1100, height: 1000 } });
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const now = new Date();
      let routes = switchedRoutes(now, true);
      await page.route("**/*", serve(lang, () => routes, now));
      await page.goto("http://uni-switch.test/?view=routing");
      await page.locator(".rt-act").first().waitFor();
      assert.equal(await page.locator(".rt-act").count(), 2, "two logins stay two rows when their current-login IDs exchange");
      const uni = page.locator(".rt-act", { hasText: "uni@example.test" });
      const team = page.locator(".rt-act", { hasText: "seat@example.test" });
      const counts = async (row) => (await row.locator(".tally > span").allInnerTexts()).slice(0, 2).map((v) => Number(v.match(/\d+/)[0]));
      assert.deepEqual(await counts(uni), [40, 40], "the previous current login keeps its own replies");
      assert.deepEqual(await counts(team), [18, 17], "the new current login includes its saved-login requests");
      assert.equal(await uni.locator(".st.rest").count(), 0, "another account's failure does not transfer to the reused base ID");
      assert.equal(await team.locator(".st.rest").count(), 1, "a cooldown still present in the newest snapshot follows its account");
      assert.equal(await team.locator(".tally .bad").count(), 1);
      if (process.env.ARTIFACT_DIR) {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        await page.locator(".rt-acts").screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-account-identity.png`) });
      }

      routes = switchedRoutes(now, false);
      await page.reload();
      await page.locator(".rt-act").first().waitFor();
      assert.equal(await team.locator(".st.rest").count(), 0, "a newer snapshot that cleared the cooldown wins over the old alias");
      assert.deepEqual(await counts(team), [18, 17], "clearing rest does not erase the historical failure or replies");

      // Equal emails on different subscriptions or organizations are still
      // separate identities; an API key's name is not an account identity.
      routes = [{ ...routes[0], id: 61, seq: 61, order: [routes[0].order[1],
        { ...routes[0].order[1], id: "codex", provider: "codex", name: "Codex" },
        { ...routes[0].order[1], id: "claude@other-org", who: "seat@example.test · Other Group" },
        { ...routes[0].order[1], id: "claude#key1", kind: "key" }] }];
      await page.reload();
      await page.locator(".rt-act").first().waitFor();
      assert.equal(await page.locator(".rt-act").count(), 4, "provider, organization, and API-key boundaries are preserved");
      assert.deepEqual(errors, []);
    });
  }
}

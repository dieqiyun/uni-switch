// Run with Node's test runner and Playwright on the module path; see README.md.
// The Sessions tab reads its list and stats again every 15 s, and over a long
// history the list is ~8 MB, parsed and compared whole to find it the same.
// Each ask says the tag of the answer it has (X-Uni-Switch-Had); an answer that
// is the same comes back with no body (X-Uni-Switch-Same) and is the object read
// before, and one that changed is read and drawn. Chromium and WebKit, in
// English and Chinese; no backend, the API is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const iso = (d) => [d.getFullYear(), d.getMonth() + 1, d.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const now = new Date();
const session = (id, title) => ({ agent: "claude", id, title, path: "/test/" + id + ".jsonl", cwd: "/work", start: now.toISOString(), t: now.getTime(), models: ["claude-opus-5-5"], tokens: { input: 10, output: 2 }, name: "Claude Code", icon: "claude-color" });

function serve(lang, back) {
  const state = { agents: [{ id: "claude", name: "Claude Code", path: "/test/settings.json", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    // as writeJSONSame answers: the tag of the bytes, and no body for the
    // tag the page has; and as writeJSONRows, the list (rows) asked with
    // the tag of one sent before has only its new rows, and [from, count]
    // for each run of the old
    const hash = (text) => String(text.length) + ":" + [...text].reduce((h, c) => (h * 31 + c.charCodeAt(0)) >>> 0, 7);
    const same = (data, rows) => {
      const body = JSON.stringify(data);
      const tag = hash(body);
      const had = req.headers()["x-uni-switch-had"];
      back.asks.push({ path: url.pathname, had: had || "" });
      if (had === tag) return route.fulfill({ status: 200, headers: { "X-Uni-Switch-Tag": tag, "X-Uni-Switch-Same": "1" }, body: "" });
      back.bodies++;
      const was = rows && back.sent.get(had);
      if (rows) back.sent.set(tag, data[rows].map((r) => JSON.stringify(r)));
      if (!was) return route.fulfill({ status: 200, headers: { "X-Uni-Switch-Tag": tag, "Content-Type": "application/json" }, body });
      back.deltas++;
      const list = [];
      for (const r of data[rows]) {
        const j = was.indexOf(JSON.stringify(r));
        const last = list.at(-1);
        if (j < 0) list.push(r);
        else if (Array.isArray(last) && last[0] + last[1] === j) last[1]++;
        else list.push([j, 1]);
      }
      return route.fulfill({ status: 200, headers: { "X-Uni-Switch-Tag": tag, "X-Uni-Switch-Delta": rows, "Content-Type": "application/json" }, body: JSON.stringify({ ...data, [rows]: list }) });
    };
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/sessions/progress") return json({ indexing: false });
    if (url.pathname === "/api/sessions") return same({ sessions: back.sessions, dirs: ["/test/sessions"] }, "sessions");
    if (url.pathname === "/api/sessions/stats") return same({ from: iso(now), to: iso(now), days: [{ date: iso(now), usage: [], active: [] }], agents: { claude: "Claude Code" } });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    const body = await fs.readFile(file).catch(() => null);
    await (body ? route.fulfill({ body, contentType }) : route.fulfill({ status: 404, body: "" }));
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: the Sessions tab's list and stats are sent again only when they changed`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 1100, height: 800 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const back = { sessions: [session("s1", "first session")], asks: [], bodies: 0, deltas: 0, sent: new Map() };
      await page.route("**/*", serve(lang, back));
      await page.addInitScript(() => { try { localStorage.setItem("uni-switch.usageTab", "sessions"); localStorage.setItem("uni-switch.sessRange", "all"); } catch {} });
      await page.goto("http://uni-switch.test/?view=usage");
      await page.waitForFunction(() => typeof sessions !== "undefined" && sessions?.sessions?.length === 1 && sessStats);
      assert.equal(back.bodies, 2, "the first read is sent whole");

      // read again, unchanged: no body, and the very objects the tab has
      const before = await page.evaluateHandle(() => [sessions, sessStats]);
      back.asks.length = 0;
      await page.evaluate(() => loadSessions());
      assert.equal(back.bodies, 2, "an unchanged read is sent again");
      assert.deepEqual(back.asks.map((a) => [a.path, a.had !== ""]).sort(), [["/api/sessions", true], ["/api/sessions/stats", true]]);
      assert.equal(await page.evaluate(([s, st]) => s === sessions && st === sessStats, before), true);

      // a session more: only its row is sent, the one before is the row the
      // tab has, and the list is drawn; the stats still aren't sent
      back.sessions = [session("s2", "second session"), ...back.sessions];
      await page.evaluate(() => loadSessions());
      assert.equal(back.bodies, 3);
      assert.equal(back.deltas, 1, "the list is sent whole for one new row");
      assert.equal(await page.evaluate(() => sessions.sessions.map((s) => s.id).join()), "s2,s1");
      assert.equal(await page.evaluate(([s]) => sessions.sessions[1] === s.sessions[0] && sessions !== s, before), true);
      await page.waitForFunction(() => document.querySelector("#view-usage")?.textContent.includes("second session"));
      assert.equal(await page.evaluate(([, st]) => st === sessStats, before), true);
      // and the tag of that new list is the one asked with next
      back.asks.length = 0;
      await page.evaluate(() => loadSessions());
      assert.equal(back.bodies, 3);
      assert.deepEqual(errors, []);
    });
  }
}

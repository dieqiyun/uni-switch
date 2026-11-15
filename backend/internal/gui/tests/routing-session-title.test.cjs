// Run with Node's test runner and Playwright on the module path; see README.md.
// The Routing page's stage says which session has an account (connor_yang on
// Discord: "回答了这个请求" beside the account, with the current session's
// title, shows which session is using it). A row answering or that answered
// the request shown says the title of that request's session after its
// status, on one line cut short, the whole of it under the pointer. The
// title is the one the agent's own files give (Claude Code's by agent:id,
// Codex's from its name index by thread id), read from
// /api/gateway/session-titles once a session is new and with the list's
// 15s refresh, not on each of the stage's passes; a Codex title kept through
// the trace's next update of the same request. A session with no title, and
// a Codex request whose session was only matched to it by its prompt
// (parentMatched), says nothing more. English, Chinese (simplified and
// traditional), Japanese and German, at 1100px and 420px; no backend, the
// API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const M = "gpt-5.5";
const seat = { id: "acct#a1", provider: "acct", name: "ChatGPT", who: "me@example.com", kind: "account", model: M };
const TITLE = "Fix the login redirect that loops back to the sign-in page after the token refresh fails on Safari";

function serve(lang, agent, live) {
  return async (request) => {
    const url = new URL(request.request().url());
    const json = (data) => request.fulfill({ json: data });
    if (url.pathname === "/boot.js") return request.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return request.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [{ id: agent, name: agent === "codex" ? "Codex" : "Claude Code", fields: [] }], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) {
        if (live.queued.length) return json(live.queued.shift());
        live.pending.push(request);
        return;
      }
      return json({ mine: true, now: new Date().toISOString(), seq: 1, totals: { requests: 1, rerouted: 0, errors: 0 }, routes: [live.first] });
    }
    if (url.pathname === "/api/gateway/session-titles") {
      const input = request.request().postDataJSON();
      live.asked.push(input);
      return json(live.titles(input));
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await request.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// what the gateway answers for each agent: Claude Code's sessions by
// agent:id (sessions.Titles), Codex's by thread id (CodexTitles), with the
// parent a title request was matched to by its prompt
const titlesFor = (agent) => (input) => {
  if (agent === "claude") return { titles: input.sessions.includes("claude:s-1") ? { "claude:s-1": TITLE } : {} };
  const names = {}, parents = {}, matched = {};
  for (const id of input.ids) if (id === "s-1") names[id] = TITLE;
  // request 3 is a title request matched to s-1's chat by its prompt only
  if (input.routeIds?.includes(3)) { parents[3] = "s-1"; matched[3] = true; names["s-1"] = TITLE; }
  return { names, parents, matched };
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const agent of ["claude", "codex"]) {
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      for (const width of [1100, 420]) {
        test(`${engine} ${agent} ${lang} ${width}px: the account's row names the session it answers, only one its agent named`, async (t) => {
          const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
          t.after(() => browser.close());
          const page = await browser.newPage({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
          page.setDefaultTimeout(8000);
          page.setDefaultNavigationTimeout(60000);
          const errors = [];
          page.on("pageerror", (error) => errors.push(error.message));
          const at = (s) => new Date(Date.now() - s * 1e3).toISOString();
          const route = (id, session, done, extra = {}) => ({ id, seq: id, time: at(2), agent, session, model: M, provider: "acct", done, status: done ? 200 : 0, ms: done ? 900 : 0,
            tokens: 1200, order: [seat], tries: [{ id: seat.id, model: M, start: at(2), done, ...(done ? { status: 200, ms: 900 } : {}) }], ...extra });
          const live = { pending: [], queued: [], seq: 1, asked: [], first: route(1, "s-1", false), titles: titlesFor(agent) };
          await page.route("**/*", serve(lang, agent, live));
          const send = (...routes) => {
            const d = { mine: true, now: new Date().toISOString(), seq: ++live.seq, totals: { requests: live.seq }, routes };
            const r = live.pending.shift();
            if (r) r.fulfill({ json: d }); else live.queued.push(d);
          };
          const say = (key) => page.evaluate(([lang, key]) => (lang !== "en" && I18N[lang]?.[key]) || key, [lang, key]);
          await page.goto("http://uni-switch.test/?view=routing");
          const em = page.locator(".rt-accts li:not(.rt-fold) > em");

          // in flight: answering…, then its session's title once asked for
          const answering = await say("answering…");
          await page.waitForFunction((want) => document.querySelector(".rt-accts li:not(.rt-fold) > em")?.textContent === want, `${answering} · ${TITLE}`);
          assert.equal(await em.getAttribute("title"), `${answering} · ${TITLE}`, "the whole title under the pointer");

          // done: the trace's next update brings the request anew, and its title stays
          const answered = await say("answered this request");
          send(route(1, "s-1", true));
          await page.waitForFunction((want) => document.querySelector(".rt-accts li:not(.rt-fold) > em")?.textContent === want, `${answered} · ${TITLE}`);
          assert.equal(await em.getAttribute("title"), `${answered} · ${TITLE}`);
          // one line, cut short at its column, the row and the page in their width
          // (the title's own span: a narrow stage lets the status wrap under the name)
          const box = await em.evaluate((em) => { const e = em.querySelector(".ses"); return { text: e?.textContent, ws: getComputedStyle(e).whiteSpace, over: getComputedStyle(e).textOverflow, h: e.getBoundingClientRect().height,
            lh: parseFloat(getComputedStyle(e).lineHeight) || 16, sw: e.scrollWidth, cw: e.clientWidth, li: em.parentElement.scrollWidth <= em.parentElement.clientWidth + 1 }; });
          assert.equal(box.text, TITLE);
          assert.equal(box.ws, "nowrap");
          assert.equal(box.over, "ellipsis");
          assert(box.h < box.lh * 1.6, `one line: ${JSON.stringify(box)}`);
          assert(box.sw > box.cw, `a long title is cut short: ${JSON.stringify(box)}`);
          assert(box.li, "the row holds its status");
          const fit = await page.evaluate(() => ({ client: document.documentElement.clientWidth, scroll: document.documentElement.scrollWidth }));
          assert(fit.scroll <= fit.client + 1, `the page overflows at ${width}: ${JSON.stringify(fit)}`);
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${agent}-${lang}-${width}-session-title.png`) });
          }

          // the stage's passes ask for nothing more: names are read when a
          // session is new and with the list's refresh, not each second
          const asked = live.asked.length;
          await page.evaluate(() => new Promise((r) => setTimeout(r, 3000)));
          assert(live.asked.length <= asked + 1, `asked ${live.asked.length - asked} more times in 3s`);

          // a session its agent gave no title: the status alone
          const before = live.asked.length;
          send(route(2, "s-2", true));
          await page.waitForFunction((want) => document.querySelector(".rt-accts li:not(.rt-fold) > em")?.textContent === want, answered);
          await new Promise((r) => { const tick = () => live.asked.length > before ? r() : setTimeout(tick, 50); tick(); });
          await page.evaluate(() => new Promise((r) => setTimeout(r, 1500)));
          assert.equal(await em.textContent(), answered, "no title for a session with none");
          assert.equal(await em.getAttribute("title"), null);

          if (agent === "codex") {
            // a title request matched to a chat by its prompt alone: a guess, so no title
            const n = live.asked.length;
            send(route(3, "t-3", true, { kind: "title" }));
            await new Promise((r) => { const tick = () => live.asked.slice(n).some((x) => x.routeIds?.includes(3)) ? r() : setTimeout(tick, 50); tick(); });
            await page.evaluate(() => new Promise((r) => setTimeout(r, 1500)));
            const text = await em.textContent();
            assert(!text.includes(TITLE), `a matched session's title is not shown: ${text}`);
          }
          assert.deepEqual(errors, []);
        });
      }
    }
  }
}

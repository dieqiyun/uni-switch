// Run with Node's test runner and Playwright on the module path; see README.md.
// A lit row's wire is drawn in the colour of the agent whose request is
// on it, and a row kept from one update to the next never keeps another
// agent's colour (Pkh on Discord, QwenPaw and others on one stage: trails
// in the wrong agent's colour). An account's row is kept while any staged
// request weighed it, and its wire took the colour of the latest request
// that weighed it, not of the one it was answering: Codex's request at an
// account, then ZCode's weighing it and going to another, drew Codex's
// trail in ZCode's colour, in the flows layer too, which copies it. Once
// the row is off, its wire is left with no agent's colour at all.
// English, Chinese (simplified and traditional), Japanese and German, at
// 1100px and 420px; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(process.env.UNI_SWITCH_ROUTING_ASSETS || path.join(__dirname, "../assets"));
const M = "glm-5.1";
const order = [
  { id: "solo#s1", provider: "solo", name: "Solo", who: "solo-1", kind: "key", model: M },
  { id: "ag#a1", provider: "ag", name: "Other", who: "ag-1", kind: "key", model: M },
];
const ago = (s) => new Date(Date.now() - s * 1e3).toISOString();
const route = (id, agent, tries) => ({ id, seq: id, time: ago(3 - id), agent, model: M, provider: tries[0].id.split("#")[0],
  done: tries.every((x) => x.done), status: 200, ms: 900, tokens: 1200, order, tries });
const at = (seat, done) => ({ id: seat, model: M, start: ago(2), done, ...(done ? { status: 200, ms: 900 } : {}) });

function serve(lang, live) {
  return async (request) => {
    const url = new URL(request.request().url());
    const json = (data) => request.fulfill({ json: data });
    if (url.pathname === "/boot.js") return request.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return request.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [{ id: "codex", name: "Codex", path: "", fields: [] }, { id: "zcode", name: "ZCode", path: "", fields: [] }], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) {
        if (live.queued.length) return json(live.queued.shift());
        live.pending.push(request);
        return;
      }
      return json({ mine: true, now: new Date().toISOString(), seq: 1, totals: { requests: 1, rerouted: 0, errors: 0 }, routes: [route(1, "codex", [at("solo#s1", false)])] });
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

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [1100, 420]) {
      test(`${engine} ${lang} ${width}px: a kept row's wire is in the colour of the agent on it, and in none once off`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        // motion on: the flows layer draws a flowing wire's copy
        const page = await browser.newPage({ viewport: { width, height: 900 } });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (error) => errors.push(error.message));
        const live = { pending: [], queued: [], seq: 1 };
        await page.route("**/*", serve(lang, live));
        const send = (...routes) => {
          const d = { mine: true, now: new Date().toISOString(), seq: ++live.seq, totals: { requests: live.seq }, routes };
          const r = live.pending.shift();
          if (r) r.fulfill({ json: d }); else live.queued.push(d);
        };
        const hue = (name) => page.locator(".rt-src", { hasText: name }).evaluate((n) => n.style.getPropertyValue("--agent").trim());
        // each drawn wire's colour, lit or flowing, and the flows layer's
        const wires = () => page.evaluate(() => {
          const c = (p) => p.style.getPropertyValue("--agent").trim();
          const ps = [...document.querySelectorAll(".rt-wires path[d]")];
          return {
            on: ps.filter((p) => p.matches(".lit, .live")).map(c).sort(),
            off: ps.filter((p) => !p.matches(".lit, .live")).map(c).filter(Boolean),
            flows: [...document.querySelectorAll(".rt-flows path[d]")].map(c).sort(),
          };
        });
        const row = (who) => page.locator(".rt-accts li", { has: page.locator(".who", { hasText: new RegExp(`^${who}$`) }) });
        await page.goto("http://uni-switch.test/?view=routing");
        await row("solo-1").waitFor();
        await page.waitForFunction(() => document.querySelector(".rt-accts li.now"));
        const codex = await hue("Codex");
        assert(codex, "Codex's node has no colour");

        // ZCode's request weighs the account Codex's is at, and goes to the
        // other: the account's row is kept, and it still answers Codex
        send(route(1, "codex", [at("solo#s1", false)]), route(2, "zcode", [at("ag#a1", false)]));
        await page.locator(".rt-src", { hasText: "ZCode" }).waitFor();
        await page.waitForFunction(() => document.querySelectorAll(".rt-accts li.now").length === 2);
        const zcode = await hue("ZCode");
        assert(zcode && zcode !== codex, `Codex and ZCode marked alike: ${codex} ${zcode}`);
        const want = [codex, codex, zcode, zcode].sort(); // each agent's wire and its account's
        await page.waitForFunction((n) => document.querySelectorAll(".rt-flows path[d]").length === n, want.length).catch(() => {});
        const w = await wires();
        assert.deepEqual(w.on, want, `the lit wires' colours: Codex ${codex}, ZCode ${zcode}`);
        assert.deepEqual(w.flows, want, `the flows layer's colours: Codex ${codex}, ZCode ${zcode}`);

        // Codex's is answered; ZCode's, the latest to weigh the account,
        // didn't go to it: its row is off, and so is its colour
        send(route(1, "codex", [at("solo#s1", true)]), route(2, "zcode", [at("ag#a1", false)]));
        await page.waitForFunction(() => document.querySelectorAll(".rt-accts li.now").length === 1);
        assert.equal(await row("solo-1").evaluate((li) => li.classList.contains("on")), false);
        // Codex's own wire is its colour always; no other wire that is off has one
        assert.deepEqual((await wires()).off, [codex]);
        assert.deepEqual(errors, []);
      });
    }
  }
}

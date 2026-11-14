// Run with Node's test runner and Playwright on the module path; see README.md.
// Routing: the context window card's head says whose request it is and the
// model it went to (the owner): the agent's icon and name, then the model's
// lab icon before its id. A model from a lab uni-switch doesn't know gets no
// icon, so a wrong logo never stands for it. In a narrow card the agent's
// name and the › go first and its icon stays, at the head's 14px; folded, the route is still in
// the head. Nothing in the head runs past the card. Every GUI language,
// Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const iso = (ms) => new Date(ms).toISOString();
const prompt = {
  window: 200000, tokens: 52000, counted: true, turns: 3,
  parts: [
    { kind: "system", tokens: 12000, items: [{ name: "prompt", tokens: 12000 }] },
    { kind: "tools", tokens: 15000, items: [{ name: "Bash", tokens: 15000 }] },
    { kind: "chat", tokens: 25000, items: [{ name: "turn", tag: "turns", n: 3, tokens: 25000 }] },
  ],
};
function req(agent, model) {
  const seat = { id: "acc@me", provider: "anthropic", name: "Acme", who: "me@example.com", kind: "account", model, known: true };
  return {
    id: 101, seq: 1010, time: iso(Date.now() - 5000), agent, session: "s-1", model, provider: "anthropic",
    order: [seat], tries: [{ id: seat.id, model, start: iso(Date.now() - 5000), done: true, status: 200, ms: 3000 }],
    done: true, status: 200, ms: 3000, tokens: 900, usage: [{ in: 52000, out: 900 }], prompt,
  };
}

function serve(lang, r) {
  const state = { agents: [{ id: "claude", name: "Claude Code", icon: "claudecode-color", path: "/test/claude", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) return; // no more requests come in
      return json({ mine: true, now: new Date().toISOString(), seq: r.seq, totals: { requests: 1, rerouted: 0, errors: 0 }, routes: [r] });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file).catch(() => ""), contentType });
  };
}

// what the head shows: each part's text, whether it is drawn, its icons
const look = (page) => page.evaluate(() => {
  const q = (s) => document.querySelector(".rt-ctx .ctx-head " + s);
  const shown = (e) => !!e && e.getClientRects().length > 0 && e.getBoundingClientRect().width > 0;
  const head = q("") || document.querySelector(".rt-ctx .ctx-head"), card = document.querySelector(".rt-ctx .ctx-card");
  const hr = head.getBoundingClientRect(), cr = card.getBoundingClientRect();
  const past = [...head.querySelectorAll("*")].filter((e) => shown(e) && getComputedStyle(e).overflow !== "hidden" && e.getBoundingClientRect().right > hr.right + 0.5).map((e) => e.className);
  return {
    name: q(".ctx-who-n")?.textContent, nameShown: shown(q(".ctx-who-n")), sepShown: shown(q(".ctx-sep")),
    agentIcon: q(".ctx-who .ic")?.dataset.icon, agentIconShown: shown(q(".ctx-who .ic")), agentIconW: q(".ctx-who .ic")?.getBoundingClientRect().width,
    labIcons: [...head.querySelectorAll(".ctx-what .ic")].map((e) => e.dataset.icon), model: q(".ctx-model")?.textContent, modelShown: shown(q(".ctx-model")),
    routeShown: shown(q(".ctx-route")), stateShown: shown(q(".ctx-state")),
    inCard: hr.right <= cr.right + 0.5, past, headH: hr.height,
  };
});

const cases = [
  ["claude", "anthropic/claude-sonnet-5-5", "claude-color"],
  ["claude", "openrouter/openai/gpt-6.1-sol", "openai"],
  ["claude", "deepseek/deepseek-v4", "deepseek-color"],
  ["claude", "zai/glm-5", "zhipu-color"],
  ["claude", "acme/zz-model-1", null],
];
const langs = ["en", "zh", "zh-TW", "ja", "de"];

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: the context window's head names the agent and the model's lab`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of langs) {
      for (const [agent, model, lab] of cases) {
        await t.test(`${lang} ${model}`, async () => {
          const context = await browser.newContext({ viewport: { width: 1100, height: 900 } });
          t.after(() => context.close());
          const page = await context.newPage();
          await page.addInitScript(() => { try { localStorage.setItem("uni-switch.ctxShut", "0"); } catch {} });
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          const errors = [];
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", serve(lang, req(agent, model)));
          await page.goto("http://uni-switch.test/?view=routing");
          await page.locator(".rt-ctx .ctx-head .ctx-route").waitFor();
          const lab$ = await look(page);
          assert.equal(lab$.name, "Claude Code");
          assert.ok(lab$.nameShown && lab$.sepShown, "a wide card names the agent before the ›");
          assert.equal(lab$.agentIcon, "claudecode-color");
          // the head's 14px, not the Context tab's 20px agent cards (the owner: Pi's icon was oversized)
          assert.ok(Math.abs(lab$.agentIconW - 14) < 0.5, `the agent icon is 14px wide (${lab$.agentIconW})`);
          assert.deepEqual(lab$.labIcons, lab ? [lab] : [], "the model's lab icon, none when the lab isn't known");
          assert.equal(lab$.model, model);
          assert.ok(lab$.modelShown && lab$.stateShown && lab$.inCard, JSON.stringify(lab$));
          assert.deepEqual(lab$.past, [], "nothing runs past the head");
          if (model !== cases[0][1]) return;

          // folded: the route is still in the head, on its one line
          await page.locator(".rt-ctx .ctx-fold").click();
          await page.locator(".rt-ctx .ctx-card.shut").waitFor();
          const shut = await look(page);
          assert.ok(shut.routeShown && shut.nameShown && shut.modelShown && shut.inCard, JSON.stringify(shut));
          assert.deepEqual(shut.labIcons, [lab]);
          assert.ok(shut.headH <= 20.5, `folded, the head is one line (${shut.headH})`);
          await page.locator(".rt-ctx .ctx-fold").click();
          await page.locator(".rt-ctx .ctx-card:not(.shut)").waitFor();

          // narrow: the agent's name and the › go, its icon stays
          await page.setViewportSize({ width: 400, height: 900 });
          await page.locator(".rt-ctx .ctx-card.max420").waitFor();
          const narrow = await look(page);
          assert.ok(!narrow.nameShown && !narrow.sepShown, "a narrow card leaves the name out");
          assert.ok(narrow.agentIconShown && narrow.modelShown && narrow.stateShown && narrow.inCard, JSON.stringify(narrow));
          assert.deepEqual(narrow.past, [], "nothing runs past a narrow head");
          assert.deepEqual(errors, []);
        });
      }
    }
  });
}

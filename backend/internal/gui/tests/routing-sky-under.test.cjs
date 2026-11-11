// Run with Node's test runner and Playwright on the module path; see README.md.
// penershu on X (v0.1.2123): with two agents sending at once through one
// group, the Routing stage's uni-switchs were drawn over the cards — one over
// the hub's "127.0.0.1:3425", its request's dot beside it, another over a
// member's row — and the row smart routing's intent picked ran its long
// intent off the row's right edge, its name squeezed out. The uni-switchs fly
// under the cards now: nothing in the sky covers a card's words, a dot held
// at its seat or waiting at uni-switch sits whole beside the card, and a long
// intent is cut short on its row, read whole in its tooltip, the name kept.
// Three agents at once (one still waiting at uni-switch), at a wide window (the
// reporter's, where a row says it on its name's line), at 1100px, at the
// narrow stage and at a phone's width, in every language, both engines.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const INTENT = "Design architecture, define module boundaries, or compare substantial technical approaches. Exclude routine implementation";
const models = [
  { id: "a/fast", name: "fast", providerName: "A", icon: "generic", context: 256000, ready: true },
  { id: "b/opus", name: "opus", providerName: "B", icon: "generic", context: 1000000, ready: true },
];
const groups = () => ({
  models,
  groups: [{ id: "jd", name: "jd-auto-balanced", members: ["a/fast", "b/opus"], routing: "order", ready: true, classifier: "c/cls",
    memberInfo: [{ id: "a/fast", ready: true, context: 256000 }, { id: "b/opus", ready: true, context: 1000000 }],
    rules: [{ use: "b/opus", intent: INTENT }] }],
  pools: [],
});
const now = Date.now();
const order = () => [{ id: "b", provider: "b", name: "B", kind: "account", model: "opus", known: true, used: 10 },
  { id: "a", provider: "a", name: "A", kind: "account", model: "fast", known: true, used: 20 }];
const group = { id: "jd", name: "jd-auto-balanced", members: ["a/fast", "b/opus"] };
const at = (ms) => new Date(now - ms).toISOString();
// claude's request is with b (the intent's pick), codex's with a, and
// kimi's waits at uni-switch for routing to pick
const routes = () => [
  { id: 7, seq: 7, time: at(3000), agent: "claude", model: "group/jd", provider: "b", tokens: 9000, group, order: order(),
    tries: [{ id: "b", model: "opus", start: at(2500) }], done: false,
    rule: { n: 1, use: "b/opus", when: [`intent "${INTENT}"`], turn: 3, tokens: 9000, classified: { by: "c/cls", intents: [INTENT], intent: INTENT, ms: 300 } } },
  { id: 8, seq: 8, time: at(2000), agent: "codex", model: "group/jd", provider: "a", tokens: 4000, group, order: order().slice(1),
    tries: [{ id: "a", model: "fast", start: at(1500) }], done: false },
  { id: 9, seq: 9, time: at(1000), agent: "kimi", model: "group/jd", provider: "", tokens: 2000, group, order: [], tries: [], done: false },
];

function serve(lang) {
  const state = { agents: [
    { id: "claude", name: "Claude Code", path: "/test/claude", fields: [] },
    { id: "codex", name: "Codex", path: "/test/codex", fields: [] },
    { id: "kimi", name: "Kimi Code", path: "/test/kimi", fields: [] },
  ], profiles: [], settings: { lang, theme: "light" } };
  let first = true;
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) return new Promise(() => {}); // nothing more comes
      const rs = first ? routes() : [];
      first = false;
      return json({ mine: true, now: new Date().toISOString(), seq: 9, totals: { requests: 3, rerouted: 0, errors: 0 }, routes: rs });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json(groups());
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3425" } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await r.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// covered: the words on the stage's cards that something in the sky would
// be drawn over. The sky is filled whole with a probe, made hittable, and
// each card's words asked what is on top of them.
const covered = () => {
  const sky = document.querySelector(".rt-sky");
  const NS = "http://www.w3.org/2000/svg";
  const probe = document.createElementNS(NS, "rect");
  probe.setAttribute("x", "-5000"); probe.setAttribute("y", "-5000");
  probe.setAttribute("width", "10000"); probe.setAttribute("height", "10000");
  probe.style.pointerEvents = "all";
  sky.append(probe);
  sky.style.pointerEvents = "auto";
  const out = [];
  try {
    const words = [...document.querySelectorAll(".rt-stage .rt-node b, .rt-stage .rt-node small, .rt-stage .rt-node i, .rt-accts li:not(.rt-gap) > b, .rt-accts li:not(.rt-gap) > em, .rt-accts li.rt-sub > b")];
    for (const w of words) {
      const r = w.getBoundingClientRect();
      if (!r.width || !r.height) continue;
      for (const [x, y] of [[r.left + 2, r.top + r.height / 2], [r.left + r.width / 2, r.top + r.height / 2], [r.right - 2, r.top + r.height / 2]]) {
        const hit = document.elementFromPoint(x, y);
        if (hit && sky.contains(hit)) { out.push(`${w.tagName.toLowerCase()} "${w.textContent.slice(0, 30)}"`); break; }
      }
    }
  } finally { probe.remove(); sky.style.pointerEvents = ""; }
  return out;
};

// dots: each request's dot at rest, and the cards it overlaps
const dots = () => {
  const cards = [...document.querySelectorAll(".rt-stage .rt-node, .rt-accts li:not(.rt-gap)")].map((e) => [e, e.getBoundingClientRect()]);
  return [...document.querySelectorAll(".rt-sky .pkt")].filter((d) => d.getAttribute("visibility") !== "hidden").map((d) => {
    const r = d.getBoundingClientRect(), cx = r.left + r.width / 2, cy = r.top + r.height / 2, rad = Number(d.getAttribute("r")) || 4.5;
    const over = cards.filter(([, c]) => cx + rad > c.left + .5 && cx - rad < c.right - .5 && cy + rad > c.top + .5 && cy - rad < c.bottom - .5)
      .map(([e]) => e.className || e.tagName);
    // the nearest card: how far the dot is from it
    const gap = Math.min(...cards.map(([, c]) => Math.hypot(Math.max(c.left - cx, 0, cx - c.right), Math.max(c.top - cy, 0, cy - c.bottom))));
    return { held: d.classList.contains("held"), over, gap };
  });
};

// the row the intent picked: what it says, and whether its name kept room
const intentRow = (intent) => {
  const em = [...document.querySelectorAll(".rt-accts li > em")].find((e) => e.textContent.includes(intent));
  if (!em) return null;
  const li = em.parentElement, b = li.querySelector(":scope > b"), e = em.getBoundingClientRect(), l = li.getBoundingClientRect();
  return { emRight: e.right, liRight: l.right, cardRight: document.querySelector(".rt").getBoundingClientRect().right, name: b.getBoundingClientRect().width, title: em.title, text: em.textContent, cut: em.scrollWidth > em.clientWidth + 1, wraps: getComputedStyle(em).whiteSpace !== "nowrap" };
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [1600, 1100, 560, 390]) {
      test(`${engine} ${lang} ${width}px: three agents at once, the uni-switchs fly under the cards and a long intent stays in its row`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const page = await (await browser.newContext({ viewport: { width, height: 1000 } })).newPage();
        page.setDefaultTimeout(8000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-sky-under.png`) }).catch(() => {});
          }
          await browser.close();
        });
        await page.route("**/*", serve(lang));
        await page.goto("http://uni-switch.test/?view=routing");
        // the two requests routing picked for are held at their seats, the
        // third's dot waits at uni-switch
        await page.waitForFunction(() => document.querySelectorAll(".rt-sky .pkt.held").length === 2);
        await page.waitForFunction(() => document.querySelectorAll(".rt-sky .pkt:not([visibility])").length === 3);
        await page.waitForTimeout(900); // the third's flight to uni-switch lands, the carriers fly off
        const flown = await page.evaluate(() => document.querySelectorAll(".rt-sky .rt-flier").length);
        assert(flown >= 1, "the uni-switch carrying the waiting request stays with it at uni-switch");

        assert.deepEqual(await page.evaluate(covered), [], "nothing in the sky may be drawn over a card's words");
        const ds = await page.evaluate(dots);
        assert.equal(ds.length, 3, JSON.stringify(ds));
        for (const d of ds) {
          assert.deepEqual(d.over, [], `a ${d.held ? "held" : "waiting"} dot must sit whole beside its card, not under or over it: ${JSON.stringify(ds)}`);
          assert(d.gap <= 12, `a ${d.held ? "held" : "waiting"} dot must sit at its card (${d.gap.toFixed(1)}px off): ${JSON.stringify(ds)}`);
        }

        const row = await page.evaluate(intentRow, INTENT);
        assert(row, "the row the intent picked says the intent");
        assert(row.emRight <= row.liRight + .5, `the intent runs off its row: ${JSON.stringify(row)}`);
        assert(row.liRight <= row.cardRight + .5, `the row runs off the stage's card: ${JSON.stringify(row)}`);
        if (width >= 1600) assert(!row.wraps, `a wide list says it on the name's line: ${JSON.stringify(row)}`);
        if (!row.wraps) {
          assert(row.name >= 60, `the row's name keeps room beside the intent: ${JSON.stringify(row)}`);
          assert(row.cut, `a long intent is cut short on one line: ${JSON.stringify(row)}`);
        }
        assert.equal(row.title, row.text, "the whole intent is in the tooltip");
        const wide = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
        assert(wide <= 0, `no sideways scroll: ${wide}px`);
        assert.deepEqual(errors, []);
      });
    }
  }
}

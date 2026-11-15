// Run with Node's test runner and Playwright on the module path; see README.md.
// A Claude Code subagent's turn, Desktop's or a terminal's, names no kind,
// but the trace keeps its x-claude-code-agent-id (subagentId). The Routing
// page tags it as Codex's subagents are, with Codex's reasons: in the request
// list by request and by session, in the story, the replay and the tray
// panel. Its purpose stays unmarked, as the ledger keeps it. A kind wins over
// a stray id; a route kept before the id has no tag. All APIs are faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = Date.now();
const at = (min) => new Date(now - min * 60e3).toISOString();
const seat = (model) => ({ id: "fake", provider: "fake", name: "Fake", who: "Test key", kind: "key", model });
const req = (id, agent, session, model, sent, extra = {}) => ({ id, seq: id, time: at(9 - id), agent, session, model, provider: "fake",
  order: [seat(sent)], tries: [{ id: "fake", model: sent, start: at(9 - id), done: true, status: 200, ms: 900 }],
  done: true, status: 200, ms: 900, tokens: 1200, ...extra });
const team = { id: "team", name: "Team", routing: "order", affinity: "", members: ["fake/m1", "fake/m2"] };
// Newest first, as the list shows them. The oldest replays first.
const routes = [
  req(8, "claude-desktop", "desk", "fake/m1", "m2", { subagentId: "worker-a", subagent: { asked: "fake/m1", to: "fake/m2" } }),
  req(7, "claude-desktop", "desk", "fake/m1", "m1"),
  req(6, "claude-desktop", "desk", "group/team", "m1", { subagentId: "worker-b", group: team }),
  req(5, "claude", "term", "fake/m1", "m1", { subagentId: "worker-c" }),
  req(4, "codex", "cx", "fake/m1", "m1", { kind: "collab_spawn" }),
  req(3, "codex", "cx", "fake/m1", "m1", { kind: "review", subagentId: "stray" }),
  // kept before the trace had the id
  req(2, "claude-desktop", "desk", "fake/m1", "m2", { subagent: { asked: "fake/m1", to: "fake/m2" } }),
  req(1, "claude-desktop", "desk", "fake/m1", "m1", { subagentId: "worker-d", subagentEffort: "high" }),
];

function serve(lang) {
  const state = { agents: [{ id: "claude-desktop", name: "Claude Desktop", fields: [] }, { id: "claude", name: "Claude Code", fields: [] }, { id: "codex", name: "Codex", fields: [] }],
    profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.has("wait")) return new Promise(() => {});
      return json({ mine: true, now: new Date().toISOString(), seq: 8, totals: { requests: 8, rerouted: 0, errors: 0 }, routes });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/gateway/session-titles") return json({ titles: {} });
    if (url.pathname === "/api/usage" && url.searchParams.get("period") === "today") return json({ calls: 8, input: 0, output: 0, cache_read: 0, cache_write: 0, reasoning: 0, cost: 0, series: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/window/fit") return route.fulfill({ status: 204 });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    return route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// Expected reader-facing words, independent of the translation lookup.
const W = {
  en: { tag: "Subagent", review: "Review",
    moved: "Claude Desktop requested a subagent on fake/m1; uni-switch put it on fake/m2, the model set for its subagents.",
    group: "Claude Desktop requested a subagent; uni-switch selects its model within this routing group.",
    terminal: "Claude Code requested a subagent on fake/m1.", codex: "Codex requested a subagent on fake/m1.",
    desktop: "Claude Desktop requested a subagent on fake/m1." },
  zh: { tag: "子代理", review: "审查",
    moved: "Claude Desktop 请求了使用 fake/m1 的子代理；uni-switch 将其改用 fake/m2，即为它的子代理设定的模型。",
    group: "Claude Desktop 请求了子代理，由 uni-switch 在此路由组内选择模型。",
    terminal: "Claude Code 请求了使用 fake/m1 的子代理。", codex: "Codex 请求了使用 fake/m1 的子代理。",
    desktop: "Claude Desktop 请求了使用 fake/m1 的子代理。" },
  "zh-TW": { tag: "子代理", review: "審查",
    moved: "Claude Desktop 請求了使用 fake/m1 的子代理；uni-switch 將其改用 fake/m2，即為它的子代理設定的模型。",
    group: "Claude Desktop 請求了子代理，由 uni-switch 在此路由組內選擇模型。",
    terminal: "Claude Code 請求了使用 fake/m1 的子代理。", codex: "Codex 請求了使用 fake/m1 的子代理。",
    desktop: "Claude Desktop 請求了使用 fake/m1 的子代理。" },
  ja: { tag: "サブエージェント", review: "レビュー",
    moved: "Claude Desktop が fake/m1 のサブエージェントをリクエストしました。uni-switch はサブエージェント用に設定された fake/m2 に切り替えました。",
    group: "Claude Desktop がサブエージェントをリクエストしました。モデルは uni-switch がこのルーティンググループ内で選びます。",
    terminal: "Claude Code が fake/m1 のサブエージェントをリクエストしました。", codex: "Codex が fake/m1 のサブエージェントをリクエストしました。",
    desktop: "Claude Desktop が fake/m1 のサブエージェントをリクエストしました。" },
  de: { tag: "Subagent", review: "Review",
    moved: "Claude Desktop hat einen Subagent mit fake/m1 angefordert; uni-switch hat ihn auf fake/m2 gesetzt, das für seine Subagenten festgelegte Modell.",
    group: "Claude Desktop hat einen Subagenten angefordert; uni-switch wählt sein Modell innerhalb dieser Weiterleitungsgruppe.",
    terminal: "Claude Code hat einen Subagent mit fake/m1 angefordert.", codex: "Codex hat einen Subagent mit fake/m1 angefordert.",
    desktop: "Claude Desktop hat einen Subagent mit fake/m1 angefordert." },
};

// Wheel a control into view before clicking: the app undoes unsolicited
// auto-scroll, and a narrow window has the stage above the list.
async function click(page, target) {
  await target.waitFor({ state: "visible" });
  const v = await page.locator("#view-routing").boundingBox();
  await page.mouse.move(v.x + 20, v.y + v.height / 2);
  for (let i = 0; i < 80; i++) {
    const b = await target.boundingBox();
    if (b.y >= v.y && b.y + b.height <= v.y + v.height) break;
    await page.mouse.wheel(0, b.y < v.y ? -120 : 120);
    await page.waitForTimeout(40);
  }
  await page.waitForTimeout(300);
  await target.click();
}

const engines = process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"];
const langs = process.env.LANGS ? process.env.LANGS.split(",") : Object.keys(W);

for (const engine of engines) {
  for (const lang of langs) {
    for (const width of [1100, 420]) {
      test(`${engine} ${lang} ${width}px: a Claude Code subagent's turn is tagged as Codex's subagents are`, async (t) => {
        const w = W[lang];
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const errors = [], pages = [];
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            for (const [i, p] of pages.entries()) await p.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-subagent-tag-${i}.png`) });
          }
          await browser.close();
        });
        const open = async (url, viewport, bySession = false) => {
          const page = await browser.newPage({ viewport, reducedMotion: "reduce" });
          pages.push(page);
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", serve(lang));
          await page.addInitScript((on) => localStorage.setItem("uni-switch.routingBySession", on ? "1" : "0"), bySession);
          await page.goto(url);
          return page;
        };
        const tags = (page, sel) => page.locator(sel).evaluateAll((rows) => rows.map((r) => {
          const k = r.querySelectorAll(".kind");
          return k.length > 1 ? "two tags" : k[0] ? [k[0].textContent, k[0].title] : ["", ""];
        }));
        // measured once the list holds still: the rows arriving and the
        // accounts column beside them resize it for a few frames
        const still = (page) => page.evaluate(async () => {
          let last = "", same = 0;
          for (let i = 0; i < 180 && same < 10; i++) {
            await new Promise((r) => requestAnimationFrame(r));
            const sig = [...document.querySelectorAll(".rt-req .asked")].map((a) => {
              const b = a.getBoundingClientRect(), k = a.querySelector(".kind")?.getBoundingClientRect();
              return [b.left, b.right, k?.left, k?.right].join();
            }).join("|");
            same = sig === last ? same + 1 : 0;
            last = sig;
          }
        });
        const want = [[w.tag, w.moved], ["", ""], [w.tag, w.group], [w.tag, w.terminal], [w.tag, w.codex], [w.review], ["", ""], [w.tag, w.desktop]];
        const same = (got) => assert.deepEqual(got.map((x, i) => want[i].length === 1 ? [x[0]] : x), want);

        // the list, by request: the tag beside the model, Codex's reasons
        // under the pointer, nothing pushed out of the row
        let page = await open("http://uni-switch.test/?view=routing", { width, height: 760 });
        await page.locator(".rt-req").nth(routes.length - 1).waitFor();
        await still(page);
        same(await tags(page, ".rt-req"));
        const look = await page.locator(".rt-req").evaluateAll((rows) => rows.filter((r) => r.querySelector(".asked .kind")).map((r) => {
          const a = r.querySelector(".asked"), m = a.querySelector(".m"), k = a.querySelector(".kind");
          const mb = m.getBoundingClientRect(), kb = k.getBoundingClientRect(), ab = a.getBoundingClientRect();
          return { kinded: a.classList.contains("kinded"), after: mb.right <= kb.left + 0.5, inside: kb.right <= ab.right + 0.5,
            model: m.textContent, shown: m.clientWidth >= m.scrollWidth, fits: r.scrollWidth <= r.clientWidth + 1 };
        }));
        assert.equal(look.length, 6, JSON.stringify(look));
        for (const x of look) {
          assert(x.kinded && x.after && x.inside && x.fits, JSON.stringify(x));
          if (width === 1100) assert(x.shown, `model cut at 1100: ${JSON.stringify(x)}`);
        }
        const list = await page.locator(".rt-reqs").evaluate((e) => e.scrollWidth <= e.clientWidth + 1);
        assert(list, "the request list scrolls sideways");

        // the story: the tag and Codex's reason, said once; a narrow window
        // covers the list with the story, so each is opened afresh. The
        // lead has no tag, nor has a route kept before the id, which still
        // says its model was moved
        const stories = [[0, w.moved, true], [2, w.group, true], [3, w.terminal, true], [7, w.desktop, true], [1, null, false], [6, w.moved, false]];
        for (const [n, [i, said, tagged]] of stories.entries()) {
          if (n) {
            await page.close();
            page = await open("http://uni-switch.test/?view=routing", { width, height: 760 });
            await page.locator(".rt-req").nth(routes.length - 1).waitFor();
          }
          await page.locator(".rt-req").nth(i).evaluate((e) => e.click());
          await page.waitForFunction((i) => document.querySelectorAll(".rt-req")[i]?.getAttribute("aria-pressed") === "true", i);
          const story = await page.locator(".rt-steps").textContent();
          assert.equal(await page.locator(".rt-steps li.kind").count(), tagged ? 1 : 0, `story tag of row ${i}`);
          if (tagged) assert.equal(await page.locator(".rt-steps li.kind .kind").textContent(), w.tag);
          if (said) assert.equal(story.split(said).length - 1, 1, `row ${i}: ${said}\nonce in\n${story}`);
        }

        // the Purpose filter: Claude Code names no purpose, so the tagged
        // turns stay under Unmarked, Codex's subagent not
        await page.close();
        page = await open("http://uni-switch.test/?view=routing", { width, height: 760 });
        await page.locator(".rt-req").nth(routes.length - 1).waitFor();
        await click(page, page.locator("#rtPurpose"));
        const unmarked = await page.evaluate(() => t("Unmarked"));
        const items = page.locator(".rt-purpose-menu .pm-item");
        await items.first().waitFor();
        const names = await items.evaluateAll((es) => es.map((e) => e.querySelector(".pm-name").textContent));
        assert.equal(names.filter((x) => x === unmarked).length, 1, JSON.stringify(names));
        await items.nth(names.indexOf(unmarked)).click();
        const label = await page.evaluate((name) => t("Purpose: {name}", { name }), unmarked);
        await page.waitForFunction((label) => document.querySelector("#rtPurpose span").textContent === label, label);
        await page.keyboard.press("Escape");
        assert.deepEqual((await tags(page, ".rt-req")).map((x) => x[0]), [w.tag, "", w.tag, w.tag, "", w.tag], "Unmarked lists the tagged turns");

        // the replay: the oldest, a Desktop subagent's turn on its effort,
        // goes first with its tag
        await page.close();
        page = await open("http://uni-switch.test/?view=routing", { width, height: 760 });
        await page.locator(".rt-req").nth(routes.length - 1).waitFor();
        const replay = await page.evaluate(() => t("Replay them all"));
        await click(page, page.getByRole("button", { name: replay, exact: true }));
        const played = await page.waitForFunction(() => {
          const e = document.querySelector(".rp-what .rp-req");
          return e && e.querySelector(".mdl") && { text: e.textContent, tag: e.querySelector(".kind")?.textContent || "", title: e.querySelector(".kind")?.title || "" };
        }).then((h) => h.jsonValue());
        assert.equal(played.tag, w.tag, JSON.stringify(played));
        assert.equal(played.title, w.desktop);
        assert.match(played.text, /Claude Desktop/);
        // the bar can sit under the window's header at 420
        await page.getByRole("button", { name: await page.evaluate(() => t("Stop replay")), exact: true }).evaluate((e) => e.click());
        await page.waitForFunction(() => document.querySelector(".rt-replay").hidden);

        // by session: the same rows under their sessions
        await page.close();
        page = await open("http://uni-switch.test/?view=routing", { width, height: 760 }, true);
        await page.locator(".rt-session").nth(2).waitFor();
        await page.locator(".rt-req").nth(routes.length - 1).waitFor();
        const grouped = await tags(page, ".rt-req");
        const sorted = (xs) => xs.map((x) => x[0] === w.review ? [x[0]] : x).map((x) => JSON.stringify(x)).sort();
        assert.deepEqual(sorted(grouped), sorted(want), "the same tags and reasons by session");

        // the tray panel's newest rows, from the live trace
        const panel = await open("http://uni-switch.test/?mode=panel", { width: Math.min(width, 440), height: 600 });
        await panel.locator('[data-ptab="routing"]').click();
        await panel.locator(".pr-req").nth(5).waitFor();
        const pr = await panel.locator(".pr-req").evaluateAll((rows) => rows.map((r) => [r.querySelector(".pr-a .kind")?.textContent || "", r.querySelector(".pr-a .kind")?.title || ""]));
        assert.deepEqual(pr, [[w.tag, w.moved], ["", ""], [w.tag, w.group], [w.tag, w.terminal], [w.tag, w.codex], [w.review, pr[5][1]]]);
        assert.deepEqual(errors, []);
      });
    }
  }
}

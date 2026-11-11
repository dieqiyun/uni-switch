// Run with Node's test runner and Playwright on the module path; see README.md.
// Codex's subagents square is [agents] default_subagent_model, only the
// model a subagent starts on when its lead names none in spawn_agent. Unset
// it said "same as model", and a GPT-6.1-Sol lead spawned GPT-6-Astra all
// the same (willz on Discord). So unset it says the lead picks, else the
// model; its title points at the subagent model, which holds every
// subagent to one; its picker opens on "Lead's pick" with the model in the
// note. Codex in WSL says the same. Claude Code's and omp's subagents still
// say "same as model". A click scrolls nothing and the row fits a narrow
// window, in every language. No backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { reach } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const models = [{ value: "gpt-6.1-sol", label: "GPT-6.1 Sol", ref: "openai/gpt-6.1-sol" }, { value: "gpt-6-astra", label: "GPT-6 Astra", ref: "openai/gpt-6-astra" }];
const codexRow = (id) => ({
  id, name: "Codex", path: "/test/config.toml", icon: "codex-color", wired: true,
  fields: [
    { key: "model", label: "model", value: "gpt-6.1-sol", options: models },
    { key: "subagent", label: "subagents", value: "", options: models },
  ],
});
const fresh = () => ({
  // another agent with a subagents field, which keeps "same as model". It
  // isn't id "claude": that row's opened view moves the subagents square into
  // a tiers line of its own.
  agents: [codexRow("codex"), codexRow("codex@wsl:Ubuntu"), {
    id: "cc-uni-switch", name: "Claude Code", path: "/test/settings.json", icon: "claudecode-color", wired: true,
    fields: [{ key: "model", label: "model", value: "uni-switch/deepseek/pro", options: [{ value: "uni-switch/deepseek/pro" }] },
      { key: "subagent", label: "subagents", value: "", options: [{ value: "uni-switch/deepseek/flash", label: "DeepSeek Flash", ref: "deepseek/flash" }] }],
  }],
  profiles: [],
});

function server(lang, sets) {
  let cur = fresh();
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ ...cur, settings: { lang, theme: "light" } });
    if (url.pathname === "/api/set") {
      const body = req.postDataJSON();
      sets.push(body);
      cur = JSON.parse(JSON.stringify(cur));
      cur.agents.find((a) => a.id === body.agent).fields.find((f) => f.key === body.field).value = body.value;
      return json({ ...cur, settings: { lang, theme: "light" } });
    }
    if (url.pathname === "/api/gateway/trace") return json({ mine: true, routes: [] });
    if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// the words as i18n.js has them
const W = {
  en: { lead: "subagents: the lead's pick, else same as model", set: "subagents: GPT-6.1 Sol", same: "subagents: same as model",
    hint: "The model a subagent starts on when Codex's lead names none; the lead may still name another. To hold every subagent to one model, set the subagent model",
    pick: "Lead's pick", note: "GPT-6.1 Sol unless the lead names another" },
  zh: { lead: "子代理：由主代理自选，未指定时同主模型", set: "子代理：GPT-6.1 Sol", same: "子代理：同主模型",
    hint: "Codex 主代理没指定模型时子代理用的模型；主代理仍可指定别的。要让所有子代理固定用一个模型，请设置子代理模型",
    pick: "由主代理自选", note: "GPT-6.1 Sol，主代理指定其他模型时除外" },
  "zh-TW": { lead: "子代理：由主代理自選，未指定時同主模型", set: "子代理：GPT-6.1 Sol", same: "子代理：同主模型",
    hint: "Codex 主代理沒指定模型時子代理用的模型；主代理仍可指定別的。要讓所有子代理固定用一個模型，請設定子代理模型",
    pick: "由主代理自選", note: "GPT-6.1 Sol，主代理指定其他模型時除外" },
  ja: { lead: "サブエージェント：リードが選ぶモデル、指定がなければモデルと同じ", set: "サブエージェント：GPT-6.1 Sol", same: "サブエージェント：モデルと同じ",
    hint: "Codex のリードがモデルを指定しないときにサブエージェントが使うモデルです。リードは別のモデルを指定することもできます。すべてのサブエージェントを 1 つのモデルに固定するには、サブエージェントのモデルを設定してください",
    pick: "リードが選ぶ", note: "GPT-6.1 Sol（リードが別のモデルを指定した場合を除く）" },
  de: { lead: "Subagenten: vom Haupt-Agenten gewählt, sonst wie Modell", set: "Subagenten: GPT-6.1 Sol", same: "Subagenten: wie Modell",
    hint: "Das Modell, auf dem ein Subagent startet, wenn der Haupt-Agent von Codex keins nennt; er kann trotzdem ein anderes nennen. Um alle Subagenten auf ein Modell festzulegen, setze das Modell der Subagenten",
    pick: "Wahl des Haupt-Agenten", note: "GPT-6.1 Sol, außer der Haupt-Agent nennt ein anderes" },
};

const square = (id) => `.row.agent[data-id="${id}"] .field.extra[data-key="subagent"]`;
const engines = process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"];
const langs = process.env.LANGS ? process.env.LANGS.split(",") : Object.keys(W);

for (const engine of engines) {
  for (const lang of langs) {
    for (const width of [1100, 560]) {
      test(`${engine} ${lang} ${width}px: Codex's subagents unset say the lead picks, not same as model`, async (t) => {
        const w = W[lang];
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const page = await browser.newPage({ viewport: { width, height: 760 } });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], sets = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, sets));
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-codex-subagents-lead.png`) });
          }
          await browser.close();
        });
        await page.goto("http://uni-switch.test/");
        // a row's squares are in the row opened, one row open at a time; an
        // opened Codex row holds Codex's own settings and is tall, so the link
        // and the square are wheeled into sight
        const open = async (id) => {
          const link = page.locator(`.row.agent[data-id="${id}"] .ag-link`);
          if (await link.getAttribute("aria-expanded") !== "true") {
            await page.waitForFunction(() => !document.getAnimations().length);
            await reach(page, link);
            await link.click();
          }
          await page.locator(square(id)).waitFor();
          await page.waitForFunction(() => !document.getAnimations().length);
          await reach(page, page.locator(square(id)));
        };
        await open("codex@wsl:Ubuntu");
        assert.equal(await page.locator(square("codex@wsl:Ubuntu")).getAttribute("aria-label"), w.lead + "\n" + w.hint);
        // Claude Code's subagents follow the model as before
        await open("cc-uni-switch");
        assert.equal(await page.locator(square("cc-uni-switch")).getAttribute("aria-label"), w.same);
        await open("codex");

        const sq = page.locator(square("codex"));
        assert.equal(await sq.getAttribute("aria-label"), w.lead + "\n" + w.hint);
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, "the page scrolls sideways");

        const y = await page.evaluate(() => scrollY);
        await sq.click();
        await page.locator("#pop:not([hidden]) #list li").first().waitFor();
        assert.equal(await page.evaluate(() => scrollY), y, "the click scrolled the page");
        const first = await page.locator("#pop #list li").first().innerText();
        assert(first.includes(w.pick) && first.includes(w.note), first);
        const pop = await page.locator("#pop").boundingBox();
        assert(pop.x >= 0 && pop.x + pop.width <= width, JSON.stringify(pop));

        // a model picked: the default for a spawn that names none, and the
        // title still says the lead may name another
        await page.locator("#pop #list li", { hasText: "GPT-6.1 Sol" }).nth(1).click();
        await page.waitForFunction((s) => document.querySelector(s + ".set"), square("codex"));
        assert.deepEqual(sets, [{ agent: "codex", field: "subagent", value: "gpt-6.1-sol" }]);
        assert.equal(await sq.getAttribute("aria-label"), w.set + "\n" + w.hint);

        // Lead's pick takes it out again
        await sq.click();
        await page.locator("#pop:not([hidden]) #list li").first().waitFor();
        await page.locator("#pop #list li").first().click();
        await page.waitForFunction((s) => !document.querySelector(s + ".set"), square("codex"));
        assert.deepEqual(sets.at(-1), { agent: "codex", field: "subagent", value: "" });

        // Claude Code's picker still opens on Same as model
        await open("cc-uni-switch");
        await page.locator(square("cc-uni-switch")).click();
        await page.locator("#pop:not([hidden]) #list li").first().waitFor();
        const cc = await page.locator("#pop #list li").first().innerText();
        assert(!cc.includes(w.pick), cc);
        assert.deepEqual(errors, []);
      });
    }
  }
}

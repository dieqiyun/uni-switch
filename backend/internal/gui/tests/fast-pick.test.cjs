// Run with Node's test runner and Playwright on the module path; see README.md.
// A model with a fast mode is sent fast, or not, for the agent, from the
// model's own row in the agent's picker (#954, Gyarados4157): a bolt beside
// the star, on the GPT row only (not the mini without a fast mode, not GLM).
// A click posts /api/agent-fast for the agent and the model's catalog id and
// leaves the pick and the picker as they are; the field then shows a bolt
// after the model. A refusal puts the bolt back. The same row, opened from
// the provider's row on the Providers page, has it too. Window and 440px tray
// panel, en zh ja de, Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function fixtures() {
  const options = [
    { value: "uni-switch/codex/gpt-5.5", ref: "codex/gpt-5.5", label: "GPT-5.5", group: "ChatGPT", fastFor: "codex" },
    { value: "uni-switch/codex/gpt-5.4-mini", ref: "codex/gpt-5.4-mini", label: "GPT-5.4 mini", group: "ChatGPT" },
    { value: "uni-switch/zai/glm-5", ref: "zai/glm-5", label: "GLM-5", group: "Z.ai" },
    // a GPT model on a provider the user added by its address (#1615)
    { value: "uni-switch/rl/o4-mini", ref: "rl/o4-mini", label: "o4-mini", group: "Relay", fastFor: "codex", ownFast: true },
  ];
  const agents = [{ id: "codex", name: "Codex", path: "/test/codex", wired: true, fields: [{ key: "model", label: "model", value: "uni-switch/codex/gpt-5.5", options }] }];
  const providers = { providers: [{
    id: "codex", name: "ChatGPT", icon: "codex-color", chat: "", responses: "", anthropic: "", catalog: "",
    models: [{ id: "gpt-5.5", name: "", on: true }], fallback: [], headers: {}, keyList: [], proxy: "",
    agents: [{ id: "codex", name: "Codex", current: true, model: "gpt-5.5" }],
    account: { agent: "codex", agentName: "Codex", user: "me@example.com", plan: "PLUS", logins: [{ user: "me@example.com", plan: "PLUS", active: true, on: true }] },
  }], presets: [], excluded: [], gateway: { running: true, window: true } };
  return { agents, providers };
}

function server(lang, posts, refuse) {
  const { agents, providers } = fixtures();
  return async (route) => {
    const req = route.request();
    const url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    // the panel fitting its window is no setting
    if (req.method() === "POST" && url.pathname.startsWith("/api/") && !url.pathname.startsWith("/api/window/")) {
      posts.push({ path: url.pathname, body: req.postDataJSON() });
      if (url.pathname === "/api/agent-fast") {
        if (refuse()) return route.fulfill({ status: 500, contentType: "text/plain", body: "codex/gpt-5.5 has no fast mode uni-switch can ask for" });
        return json({ fast: req.postDataJSON().fast });
      }
      return json({});
    }
    if (url.pathname === "/api/state") return json({ agents, profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const said = {
  en: { on: "GPT-5.5 is sent fast for Codex: quicker, at a higher price. Click to send it at the usual speed", off: "Send GPT-5.5 fast for Codex: quicker, at a higher price",
    ownOff: 'Ask your provider for OpenAI\'s fast mode (service_tier "priority") on o4-mini for Codex: quicker, at a higher price, where it serves it' },
  zh: { on: "Codex 用 GPT-5.5 时走快速模式：更快，价格更高。点一下改回标准速度", off: "Codex 用 GPT-5.5 时走快速模式：更快，价格更高",
    ownOff: 'Codex 用 o4-mini 时向你的服务商请求 OpenAI 的快速模式（service_tier "priority"）：服务商支持时更快，价格更高' },
  ja: { on: "Codex の GPT-5.5 は高速モードで送ります：速く、料金は高め。クリックで標準速度に戻します", off: "Codex の GPT-5.5 を高速モードで送る：速く、料金は高め",
    ownOff: 'Codex の o4-mini で、プロバイダーに OpenAI の高速モード（service_tier "priority"）を求める：対応していれば速く、料金は高め' },
  de: { on: "GPT-5.5 geht für Codex im Schnellmodus: schneller, zu einem höheren Preis. Klicken für die normale Geschwindigkeit", off: "GPT-5.5 für Codex im Schnellmodus senden: schneller, zu einem höheren Preis",
    ownOff: 'Deinen Anbieter für Codex bei o4-mini nach OpenAIs Schnellmodus (service_tier "priority") fragen: schneller, zu einem höheren Preis, wo er ihn anbietet' },
};
const row = '.row.agent[data-id="codex"]';
const pop = "#pop:not([hidden]) #list li";

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a model is sent fast from its row in the picker", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(() => browser.close());
    for (const lang of ["en", "zh", "ja", "de"]) for (const panel of [false, true]) {
      await t.test(`${lang} ${panel ? "tray panel 440px" : "window"}`, async () => {
        const posts = [];
        let refusing = false;
        const page = await (await browser.newContext({ viewport: panel ? { width: 440, height: 560 } : { width: 980, height: 600 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, posts, () => refusing));
        await page.goto("http://uni-switch.test/" + (panel ? "?mode=panel" : "?view=agents"));
        await page.locator(row).waitFor();
        const field = panel ? `${row} .ag-open .field[data-key="model"]` : `${row} > .field.ag-start[data-key="model"]`;
        const openPicker = async () => {
          if (panel && !(await page.locator(field).isVisible())) {
            await page.locator(`${row} .ag-sum`).click();
            await page.waitForTimeout(700);
          }
          await page.locator(field).click();
          await page.locator(pop).first().waitFor();
        };
        await openPicker();
        const gpt = page.locator(pop).filter({ hasText: "GPT-5.5" }).first();
        const bolt = gpt.locator("button.fast");
        assert.equal(await page.locator(pop).filter({ hasText: "GPT-5.4 mini" }).locator("button.fast").count(), 0, "no fast mode: no bolt");
        assert.equal(await page.locator(pop).filter({ hasText: "GLM-5" }).locator("button.fast").count(), 0, "no fast mode: no bolt");
        // the current model's bolt shows without hover, and fits in its row
        await page.mouse.move(2, 2);
        assert(await bolt.isVisible(), "the current row shows its bolt");
        const fits = await gpt.evaluate((li) => {
          const r = li.getBoundingClientRect(), b = li.querySelector("button.fast").getBoundingClientRect();
          return b.width > 0 && b.left >= r.left && b.right <= r.right + 0.5;
        });
        assert(fits, "the bolt fits in its row");
        assert.equal(await bolt.getAttribute("aria-pressed"), "false");
        assert.equal(await bolt.getAttribute("title"), said[lang].off);
        // on the user's own provider fast is only asked for, and says so
        const own = page.locator(pop).filter({ hasText: "o4-mini" }).first();
        await own.hover();
        assert.equal(await own.locator("button.fast").getAttribute("title"), said[lang].ownOff);

        await bolt.click();
        await page.waitForTimeout(150);
        assert.deepEqual(posts, [{ path: "/api/agent-fast", body: { for: "codex", ref: "codex/gpt-5.5", fast: true } }], "only the fast switch is posted, the pick is not");
        assert(await page.locator("#pop").isVisible(), "the picker stays open");
        assert.equal(await bolt.getAttribute("aria-pressed"), "true");
        assert.equal(await bolt.getAttribute("title"), said[lang].on);
        assert.match(await bolt.evaluate((b) => b.className), /\bon\b/);
        assert.equal(await page.locator(`${field} .fast-tag`).count(), 1, "the field says the model goes fast");

        // a refusal puts it back
        refusing = true;
        await bolt.click();
        await page.waitForTimeout(150);
        assert.equal(posts.length, 2);
        assert.deepEqual(posts[1].body, { for: "codex", ref: "codex/gpt-5.5", fast: false });
        assert.equal(await bolt.getAttribute("aria-pressed"), "true", "refused: still fast");
        assert.equal(await page.locator(`${field} .fast-tag`).count(), 1);
        refusing = false;
        await page.keyboard.press("Escape");

        // from the provider's row: the same row, the same bolt
        if (!panel) {
          await page.goto("http://uni-switch.test/?view=providers");
          await page.locator('.row.provider[data-id="codex"] .uses .use').click();
          await page.locator(pop).first().waitFor();
          const there = page.locator(pop).filter({ hasText: "GPT-5.5" }).first().locator("button.fast");
          assert(await there.isVisible(), "the provider's picker has the bolt");
          await there.click();
          await page.waitForTimeout(150);
          assert.deepEqual(posts.at(-1), { path: "/api/agent-fast", body: { for: "codex", ref: "codex/gpt-5.5", fast: true } });
        }
        await page.context().close();
      });
    }
    assert.deepEqual(errors, []);
  });
}

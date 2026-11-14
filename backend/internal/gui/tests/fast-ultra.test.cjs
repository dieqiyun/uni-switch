// Run with Node's test runner and Playwright on the module path; see README.md.
// #1954, zhenghaowen0967: a fast pick of a ChatGPT model an account offers
// as Ultrafast (ultraOk) steps usual → Fast → Ultrafast → usual at the
// bolt, as a camera's flash button steps its modes. Ultrafast posts
// /api/agent-fast with ultra, shows two bolts in the row and after the
// field's model, and says so in the tooltip and status. A refusal puts it
// back. Window and 440px tray panel, en zh ja de, Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

function fixtures() {
  const options = [
    { value: "uni-switch/codex/gpt-5.5", ref: "codex/gpt-5.5", label: "GPT-5.5", group: "ChatGPT", fastFor: "codex", ultraOk: true },
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
        return json({ fast: req.postDataJSON().fast, ultra: !!req.postDataJSON().ultra });
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
  en: { fast: "GPT-5.5 is sent fast for Codex: quicker, at a higher price. Click to send it Ultrafast, where the account offers it",
    ultra: "GPT-5.5 is sent Ultrafast for Codex where the account offers it, and fast where it doesn't. Click to send it at the usual speed", status: "GPT-5.5 is sent Ultrafast for Codex", name: "Ultrafast" },
  zh: { fast: "Codex 用 GPT-5.5 时走快速模式：更快，价格更高。点一下改为极速（账号支持时）",
    ultra: "Codex 用 GPT-5.5 时，账号支持极速就走极速，不支持则走快速模式。点一下改回标准速度", status: "Codex 用 GPT-5.5 时走极速", name: "极速" },
  ja: { fast: "Codex の GPT-5.5 は高速モードで送ります：速く、料金は高め。クリックで超高速にします（アカウントが対応している場合）",
    ultra: "Codex の GPT-5.5 は、アカウントが対応していれば超高速、していなければ高速モードで送ります。クリックで標準速度に戻します", status: "Codex の GPT-5.5 は超高速で送ります", name: "超高速" },
  de: { fast: "GPT-5.5 geht für Codex im Schnellmodus: schneller, zu einem höheren Preis. Klicken für Ultrafast, wo das Konto es anbietet",
    ultra: "GPT-5.5 geht für Codex als Ultrafast, wo das Konto es anbietet, sonst im Schnellmodus. Klicken für die normale Geschwindigkeit", status: "GPT-5.5 geht für Codex als Ultrafast", name: "Ultrafast" },
};
const row = '.row.agent[data-id="codex"]';
const pop = "#pop:not([hidden]) #list li";

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a fast pick steps on to Ultrafast where an account offers it", async (t) => {
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
        if (panel && !(await page.locator(field).isVisible())) {
          await page.locator(`${row} .ag-sum`).click();
          await page.waitForTimeout(700);
        }
        await page.locator(field).click();
        await page.locator(pop).first().waitFor();
        const gpt = page.locator(pop).filter({ hasText: "GPT-5.5" }).first();
        const bolt = gpt.locator("button.fast");
        const body = (fast, ultra) => ({ for: "codex", ref: "codex/gpt-5.5", fast, ...(ultra ? { ultra: true } : {}) });

        // usual → Fast, which says the next click is Ultrafast
        await bolt.click();
        await page.waitForTimeout(150);
        assert.deepEqual(posts.map((p) => p.body), [body(true)]);
        assert.equal(await bolt.getAttribute("title"), said[lang].fast);
        assert.equal(await bolt.locator("svg").count(), 1);

        // Fast → Ultrafast: two bolts, in the row and after the field's model
        await bolt.click();
        await page.waitForTimeout(150);
        assert.deepEqual(posts.at(-1).body, body(true, true));
        assert.match(await bolt.evaluate((b) => b.className), /\bon\b.*\bultra\b/);
        assert.equal(await bolt.getAttribute("aria-label"), said[lang].name);
        assert.equal(await bolt.getAttribute("title"), said[lang].ultra);
        assert.equal(await bolt.locator("svg").count(), 2);
        assert.equal(await page.locator("#status").textContent(), said[lang].status);
        const fits = await gpt.evaluate((li) => {
          const r = li.getBoundingClientRect(), b = li.querySelector("button.fast").getBoundingClientRect();
          return [...li.querySelectorAll("button.fast svg")].every((s) => { const q = s.getBoundingClientRect(); return q.left >= b.left - 0.5 && q.right <= b.right + 0.5; }) && b.right <= r.right + 0.5;
        });
        assert(fits, "the two bolts fit in the button and the row");
        const tag = page.locator(`${field} .fast-tag`);
        assert.equal(await tag.count(), 1);
        assert.match(await tag.evaluate((s) => s.className), /\bultra\b/);
        assert.equal(await tag.locator("svg").count(), 2);
        assert.equal(await tag.getAttribute("title"), said[lang].name);

        // a refusal puts Ultrafast back
        refusing = true;
        await bolt.click();
        await page.waitForTimeout(150);
        assert.deepEqual(posts.at(-1).body, body(false));
        assert.equal(await bolt.locator("svg").count(), 2, "refused: still Ultrafast");
        refusing = false;

        // Ultrafast → usual
        await bolt.click();
        await page.waitForTimeout(150);
        assert.deepEqual(posts.at(-1).body, body(false));
        assert.equal(await bolt.getAttribute("aria-pressed"), "false");
        assert.equal(await page.locator(`${field} .fast-tag`).count(), 0);
        await page.context().close();
      });
    }
    assert.deepEqual(errors, []);
  });
}

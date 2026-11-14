// Run with Node's test runner and Playwright on the module path; see README.md.
// Codex's own models that uni-switch's list shows it too, a ChatGPT account's
// (served), stay in its model picker when its own models are switched off
// in its list (shiqi_40904 on Discord: with a routing group on, Codex's
// picker was the group alone). The picker drops the others at once on
// closing the list, as the server does (agent.Picks); the served ones, the
// group and the one it is set to stay, before the state is read again and
// after. In English and Chinese, at a narrow and a wide window; no backend,
// the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

const served = ["gpt-5.5", "gpt-5.4", "gpt-5.4-mini"].map((v) => ({ value: v, note: "", group: "OpenAI", icon: "openai", via: true, served: true }));
// one Codex lists that uni-switch doesn't serve: the switch's to take out
const lone = { value: "gpt-oss-lone", note: "", group: "OpenAI", icon: "openai", via: true };
const grp = { value: "group/mygpt", label: "MyGPT", ref: "group/mygpt", note: "routing group", group: "Routing groups", icons: ["codex-color"] };
const list = [
  { id: "group/mygpt", name: "MyGPT", group: "Routing groups", icons: ["codex-color"] },
  ...served.map((o, i) => ({ id: "codex/" + o.value, name: o.value, group: "Codex", icon: "codex-color", own: true, inUse: i === 0 })),
];

function fixture(lang) {
  let ownHidden = false, late = 0;
  const posts = [];
  const count = { shown: list.length, listed: list.length, by: [{ name: "Routing groups", icons: ["codex-color"], n: 1 }, { name: "Codex", icon: "codex-color", n: 3 }] };
  // the server's agent.Picks: the agent's own left out, but served ones
  const options = () => [...served, ...(ownHidden ? [] : [lone]), grp];
  const state = () => ({
    agents: [
      { id: "codex", name: "Codex", icon: "codex-color", path: "/fixture/codex", wired: true, models: count,
        fields: [{ key: "model", label: "model", value: "gpt-5.5", options: options() }] },
    ],
    profiles: [], settings: { lang, theme: "light" },
  });
  async function serve(route) {
    const req = route.request();
    const url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") {
      // the state read after a switch comes late, so what the picker
      // offers before it is the page's own doing
      if (Date.now() < late) await new Promise((r) => setTimeout(r, late - Date.now()));
      return json(state());
    }
    const m = url.pathname.match(/^\/api\/agent-models\/(\w+)$/);
    if (m) {
      if (req.method() === "POST") {
        const body = req.postDataJSON();
        posts.push({ id: m[1], ...body });
        if (body.ownHidden !== undefined) {
          ownHidden = body.ownHidden;
          late = Date.now() + 1500;
          return json({ ownHidden });
        }
        return json({ models: list, count });
      }
      return json({ models: list, ordered: false, only: false, own: true, ownHidden });
    }
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  }
  return { serve, posts };
}

const W = {
  en: { show: "Show Codex's own models" },
  zh: { show: "显示 Codex 自带的模型" },
};
const row = '.row.agent[data-id="codex"]';

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(W)) {
    for (const width of [420, 1100]) {
      test(`${engine} ${lang} ${width}: Codex's served models stay with its own switched off`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const page = await browser.newPage({ viewport: { width, height: 760 } });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const fx = fixture(lang);
        await page.route("**/*", fx.serve);
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-own-served.png`) });
          }
          await browser.close();
        });
        await page.goto("http://uni-switch.test/?view=agents");
        await page.locator(row).waitFor();
        const pop = page.locator(".am-pop:not(.leaving)");
        const picked = async () => {
          await page.locator(`${row} .field[data-key="model"]`).click();
          await page.locator("#pop:not([hidden]) #list li").first().waitFor();
          const texts = await page.locator("#pop:not([hidden]) #list li").allTextContents();
          await page.keyboard.press("Escape");
          await page.locator("#pop").waitFor({ state: "hidden" });
          return texts.map((s) => s.trim());
        };
        // a row by its model: gpt-5.4, not gpt-5.4-mini
        const at = (rows, v) => rows.some((s) => s.startsWith(v) && !s.startsWith(v + "-"));
        const has = (rows) => {
          for (const v of ["gpt-5.4", "gpt-5.4-mini", "MyGPT"]) assert(at(rows, v), `${v} gone from Codex's picker: ${JSON.stringify(rows)}`);
        };

        let rows = await picked();
        has(rows);
        assert(at(rows, "gpt-oss-lone"));

        const r = page.locator(row);
        const entry = r.locator(".ag-exp .ag-chips .ag-quiet");
        if (!(await entry.isVisible())) await r.locator(".ag-link").click();
        await entry.waitFor();
        await page.waitForFunction(() => document.getAnimations().length === 0);
        await entry.click();
        await pop.waitFor();
        const box = pop.locator(".am-own");
        assert.equal(await box.innerText(), W[lang].show);
        await box.locator(".lib-switch").click();
        await page.waitForTimeout(250);
        assert.deepEqual(fx.posts.at(-1), { id: "codex", ownHidden: true });
        await page.keyboard.press("Escape");
        await pop.waitFor({ state: "detached" });

        // at once, before the state comes: the lone one gone, served stay
        rows = await picked();
        assert(!at(rows, "gpt-oss-lone"), "Codex's own not served still offered");
        has(rows);
        // and in the state read again
        await page.waitForTimeout(1800);
        rows = await picked();
        assert(!at(rows, "gpt-oss-lone"));
        has(rows);

        assert.equal(await page.locator(".status.err, #status.err").count(), 0);
        assert.deepEqual(errors, []);
      });
    }
  }
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// Hide accounts hides them in the whole window, not only on Routing and
// Usage (Zhenbao TONG on Discord: with it on, the tray panel hid the
// accounts while the window's Providers page showed every one): the
// Providers list, a provider's editor with its accounts, and a menu or
// dialog that sits outside any page. Turned off, each is back as it was.
// Every language, wide and narrow; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const MAIL = ["dev.one@example.com", "dev.two@example.com"];

function serve(lang) {
  const base = { icon: "generic", chat: "", responses: "", anthropic: "", catalog: "", agents: [], fallback: [], headers: {}, keyList: [], proxy: "" };
  const logins = MAIL.map((user, i) => ({ agent: "codex", user, plan: "PLUS", active: i === 0, on: true }));
  const providers = { providers: [
    { ...base, id: "codex", name: "Codex", icon: "codex-color", models: [{ id: "gpt-6", name: "GPT-6", on: true }], account: { agent: "codex", agentName: "Codex", user: MAIL[0], plan: "PLUS", logins } },
    { ...base, id: "workbuddy", name: "WorkBuddy", icon: "workbuddy-color", models: [], account: { agent: "workbuddy", agentName: "WorkBuddy", user: "李雷", plan: "Free", logins: [{ agent: "workbuddy", user: "李雷", plan: "Free", active: true, on: true }] } },
  ], presets: [], excluded: [], gateway: { running: true, window: true } };
  const settings = { lang, theme: "light" };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

// what a reader sees of the window: its text and its tooltips, input values aside
const seen = (page) => page.evaluate(() => document.body.innerText + "\n" + [...document.querySelectorAll("[title]")].map((e) => e.title).join("\n"));

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [1000, 420]) {
      test(`${engine} ${lang} ${width}: Hide accounts hides them on every page of the window`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const context = await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" });
        await context.addInitScript(() => { try { if (!sessionStorage.getItem("seeded")) { localStorage.setItem("uni-switch.maskEmails", "1"); sessionStorage.setItem("seeded", "1"); } } catch {} });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));

        // the Providers list
        await page.goto("http://uni-switch.test/?view=providers");
        await page.waitForFunction(() => document.querySelector("#view-providers")?.innerText.includes("Codex"));
        await page.waitForFunction(() => document.querySelector("#view-providers .pii"));
        let text = await seen(page);
        for (const name of [...MAIL, "李雷"]) assert.ok(!text.includes(name), `${name} in sight on Providers: ${text}`);

        // a provider's editor, its accounts listed
        await page.goto("http://uni-switch.test/?view=providers&edit=codex");
        await page.waitForFunction((m) => [...document.querySelectorAll(".pii")].filter((e) => m.includes(e.dataset.raw)).length >= 2, MAIL);
        text = await seen(page);
        for (const name of MAIL) assert.ok(!text.includes(name), `${name} in sight in the editor: ${text}`);

        // a menu or dialog the page opens outside any page
        await page.evaluate((m) => {
          const d = document.createElement("div");
          d.id = "floating";
          d.textContent = "Switch to " + m;
          d.title = m;
          document.body.append(d);
        }, MAIL[1]);
        await page.waitForFunction(() => document.querySelector("#floating .pii"));
        assert.ok(!(await page.locator("#floating").textContent()).includes(MAIL[1]));
        assert.ok(!(await page.locator("#floating").getAttribute("title")).includes("dev.two"));

        // turned off with the Providers page's own switch (under the
        // editor here): back as they were
        await page.evaluate(() => document.querySelector("#pvMask").click());
        await page.waitForFunction(() => !document.querySelector(".pii"));
        assert.equal(await page.locator("#floating").textContent(), "Switch to " + MAIL[1]);
        assert.equal(await page.locator("#floating").getAttribute("title"), MAIL[1]);
        text = await seen(page);
        for (const name of MAIL) assert.ok(text.includes(name), `${name} back in the editor`);
        assert.deepEqual(errors, []);
      });
    }
  }
}

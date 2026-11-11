// Run with Node's test runner and Playwright on the module path; see README.md.
// Usage's per-account eye, with Hide accounts on (outloudvi on Discord):
// - a Kiro plugin account is named "email (way it was signed in to)"; its
//   eye showed nothing, since the address inside the name was found and
//   hidden alone while the eye kept the whole name. Pressed, it shows that
//   account, and the account of the same email signed in another way
//   stays hidden.
// - one address on two services is two accounts: the eye on one card shows
//   that card's, the other card's stays hidden. A sentence elsewhere in the
//   window naming the address follows the account shown, as before.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const SAME = "same@example.com", KG = "kiro@example.com (Google)", KH = "kiro@example.com (GitHub)";
const inHours = (h) => new Date(Date.now() + h * 3600e3 + 60e3).toISOString();
const card = (provider, name, user, plan) => ({ provider, name, icon: provider, plan, user, windows: [{ name: "5 hours", used: 30, resetsAt: inHours(3) }] });
const quotas = () => [
  card("kiro", "Kiro", KG, "Pro"),
  card("kiro", "Kiro", KH, "Free"),
  card("codex", "Codex", SAME, "Plus"),
  card("zcode", "ZCode", SAME, "Lite"),
];

function serve(lang) {
  const settings = { theme: "light", lang, tray: "panel", quotaLeft: false, currency: "usd" };
  const providers = { providers: [], presets: [], excluded: [], gateway: { running: true, window: true } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/usage/quotas") return json(quotas());
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a Usage card's eye shows its own provider's account, a Kiro plugin account included`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      for (const width of [900, 360]) {
        const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        await page.goto("http://uni-switch.test/?view=usage");
        await page.locator(".subscription-account").nth(3).waitFor();
        const row = (provider, user) => page.locator(`.subscription-account[data-card="${provider}|${user}"]`);
        const hidden = (provider, user) => row(provider, user).evaluate((e) => e.querySelectorAll(".pii").length > 0 && !e.textContent.includes(e.dataset.card.split("|")[1]));
        const shown = (provider, user) => row(provider, user).evaluate((e) => !e.querySelector(".pii") && e.textContent.includes(e.dataset.card.split("|")[1]));
        const settle = () => page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));

        await page.locator("#usageMask").click();
        await page.evaluate((same) => {
          const d = document.createElement("div");
          d.id = "sample";
          d.textContent = `mail ${same} today`;
          document.querySelector("main, body").append(d);
        }, SAME);
        await page.waitForFunction(() => document.querySelectorAll(".subscription-account .pii").length >= 4 && document.querySelector("#sample .pii"));
        for (const [p, u] of [["kiro", KG], ["kiro", KH], ["codex", SAME], ["zcode", SAME]]) assert.ok(await hidden(p, u), `${p} ${u} is hidden first`);

        // Kiro's account named by its email and its way in: the eye shows it
        await row("kiro", KG).locator(".acct-eye").click();
        await settle();
        assert.ok(await shown("kiro", KG), "the Kiro account's eye shows it");
        assert.equal(await row("kiro", KG).locator(".acct-eye").getAttribute("aria-pressed"), "true");
        assert.ok(await hidden("kiro", KH), "the same email signed in with GitHub stays hidden");
        assert.equal(await row("kiro", KH).locator(".acct-eye").getAttribute("aria-pressed"), "false");

        // one address on two services: the Codex card's eye shows Codex's only
        await row("codex", SAME).locator(".acct-eye").click();
        await settle();
        assert.ok(await shown("codex", SAME), "Codex's account shows");
        assert.ok(await hidden("zcode", SAME), "the same address on ZCode stays hidden");
        assert.equal(await row("zcode", SAME).locator(".acct-eye").getAttribute("aria-pressed"), "false");
        assert.equal(await page.locator("#sample").textContent(), `mail ${SAME} today`, "a sentence elsewhere follows the account shown");

        // pressed again: hidden again, everywhere
        await row("codex", SAME).locator(".acct-eye").click();
        await page.waitForFunction(() => document.querySelector("#sample .pii"));
        assert.ok(await hidden("codex", SAME));
        assert.ok(await shown("kiro", KG), "the Kiro account stays shown");

        assert.equal(await page.locator("select").count(), 0, "no native select");
        assert.deepEqual(errors, []);
        await context.close();
      }
    });
  }
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// lileo0668 on Discord: with Hide accounts on, OpenCode Zen Free's name was
// blurred on the add sheet, on the Plugins page and in the line after its
// sign-in. The sign-in's answer named the plugin as its user, Hide accounts
// took every answer's user for an account, and only listed providers'
// names were spared, so a plugin not listed (yet) had its own name hidden
// as if it were someone's account. A plugin's name, signed in or not, is a
// provider's: it is never blurred. An account's name still is.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const ZEN = "OpenCode Zen Free", ACCOUNT = "lileo.sample";

function serve(lang, signed) {
  const settings = { theme: "light", lang };
  const plugins = () => [{ id: "opencode-zen-free", pid: "opencode-zen-free", name: ZEN, icon: "generic", spec: "the retired opencode-zen-free-auth", methods: [{ type: "api", label: "Zen free access", keyless: true }], single: true, signedIn: signed.on, models: 1 }];
  // an account of another provider's, to show the blur is still on
  const providers = () => ({
    providers: [{ id: "codex", name: "Codex", icon: "openai", models: [], agents: [], key: {}, account: { agent: "codex", agentName: "Codex", agentIcon: "openai", user: ACCOUNT, logins: [{ user: ACCOUNT, active: true, on: true }] } }],
    presets: [], excluded: [], gateway: { running: true, window: true }, plugins: plugins(),
  });
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings });
    if (url.pathname === "/api/settings") return json(settings);
    if (url.pathname === "/api/providers") return json(providers());
    if (url.pathname === "/api/plugin-signin/prompt") return json({ prompt: null, inputs: {} });
    // what uni-switch answered before lileo0668: the plugin's name as its user
    if (url.pathname === "/api/plugin-signin") { signed.on = true; return json({ agent: "opencode-zen-free", state: "done", user: ZEN }); }
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: plugins() });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${engine} ${lang}: with Hide accounts on, a plugin's own name is not blurred as an account`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      for (const width of [900, 360]) {
        const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
        await context.addInitScript(() => { try { localStorage.setItem("uni-switch.maskEmails", "1"); } catch {} });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang, { on: false }));
        await page.goto("http://uni-switch.test/?view=providers");

        // the blur is on: an account's name is hidden
        await page.waitForFunction((a) => document.querySelector(".pii") && !document.body.innerText.includes(a), ACCOUNT);

        await page.locator("#addProvider").click();
        await page.locator(`#addSheet .tile[data-pick="${ZEN}"]`).click();
        const choice = page.getByRole("button", { name: "Zen free access", exact: true });
        if (await choice.count()) await choice.click();
        // the line after the sign-in names the plugin, unblurred
        await page.waitForFunction((z) => document.querySelector("#status").textContent.includes(z), ZEN);
        // what is drawn is blurred a frame later, if at all
        await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
        assert.equal(await page.locator("#status .pii").count(), 0, "the status line's plugin name is not blurred");
        assert.ok((await page.locator("#status").textContent()).includes(ZEN));

        // and the add sheet's tile, opened again, names it in the clear
        await page.keyboard.press("Escape");
        await page.locator("#modal").waitFor({ state: "hidden" });
        await page.locator("#addProvider").click();
        const tile = page.locator(`#addSheet .tile[data-pick="${ZEN}"]`);
        await tile.waitFor();
        await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
        assert.equal(await tile.locator(".pii").count(), 0, "the add sheet's tile is not blurred");
        assert.ok((await tile.innerText()).includes(ZEN), "the tile names the plugin");
        assert.equal(await page.evaluate((a) => document.body.innerText.includes(a), ACCOUNT), false, "an account stays hidden");

        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "no sideways scroll");
        assert.deepEqual(errors, []);
        await context.close();
      }
    });
  }
}

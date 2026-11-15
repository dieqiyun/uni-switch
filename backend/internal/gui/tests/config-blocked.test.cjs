// Run with Node's test runner and Playwright on the module path; see README.md.
// YouWhat on Discord: ~/.config wasn't the user's (a command run with sudo
// had made it root's), so adding a custom provider failed with "mkdir
// ~/.config/uni-switch: permission denied". /api/state names the folder that
// blocks uni-switch (configBlocked), and the window asks once a load to Repair
// it, which POSTs /api/config/repair. A save refused for it (code
// config_blocked) says so in the reader's language and asks again, over
// the editor whose save failed; Not now goes back to that editor as it was
// left. The panel is too small for a dialog. No backend, the API is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const KEYS = [
  "uni-switch can't save into {path}: it belongs to another user",
  "uni-switch can't save its settings",
  "{path} belongs to another user, often after a command was run with sudo, so uni-switch can't save providers or settings there. Repair makes it yours again; the system may ask for your password.",
  "Repair",
  "Repaired: uni-switch can save again",
];
const LANGS = ["en", "zh", "zh-TW", "ja", "de"];

function server(lang, calls) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data, status = 200) => route.fulfill({ status, json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") {
      const s = { agents: [], profiles: [], settings: { lang, theme: "light" } };
      if (calls.blocked) s.configBlocked = "~/.config";
      return json(s);
    }
    if (url.pathname === "/api/config/repair") { calls.repair++; calls.blocked = false; return route.fulfill({ status: 204 }); }
    if (url.pathname === "/api/test-save") return json({ error: "mkdir /Users/u/.config/uni-switch: permission denied", code: "config_blocked", blocked: "~/.config" }, 400);
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/update" || url.pathname === "/api/drift") return json({});
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a folder uni-switch can't write is repaired in one click`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(async () => { await browser.close(); });
      const errors = [];
      const open = async (calls, url = "http://uni-switch.test/", width = 900) => {
        const page = await (await browser.newContext({ viewport: { width, height: 700 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, calls));
        await page.goto(url);
        return page;
      };
      const tr = (page, k) => page.evaluate(([k, lang]) => (lang === "en" ? k : I18N[lang][k]), [k, lang]);

      // blocked at load: asked, and Repair repairs
      const calls = { blocked: true, repair: 0 };
      const page = await open(calls);
      const dialog = page.locator("#modal:not([hidden])");
      await dialog.waitFor();
      assert.match(await dialog.textContent(), /~\/\.config/, "the dialog names the folder");
      assert.ok((await dialog.textContent()).includes(await tr(page, "uni-switch can't save its settings")));
      await dialog.getByRole("button", { name: await tr(page, "Repair"), exact: true }).click();
      await page.waitForFunction(() => document.querySelector("#modal").hidden);
      assert.equal(calls.repair, 1, "Repair reached the API");
      assert.equal(await page.locator("#status").textContent(), await tr(page, "Repaired: uni-switch can save again"));

      // a save refused for it: said in the reader's language, asked over
      // the editor, and Not now goes back to that editor as it was
      const p2 = await open({ blocked: false, repair: 0 });
      await p2.waitForTimeout(300);
      assert.ok(await p2.locator("#modal").evaluate((m) => m.hidden), "nothing asked while uni-switch can save");
      const said = await p2.evaluate(async () => {
        const ed = el("div", "editor");
        const head = el("div", "ehead"); head.append(el("b", "", "Add provider"));
        const input = el("input"); input.id = "typed"; input.value = "my provider";
        ed.append(head, input);
        openModal(ed);
        try { await api("test-save", {}); } catch (e) { return e.message; }
        return "";
      });
      assert.equal(said, (await tr(p2, "uni-switch can't save into {path}: it belongs to another user")).replace("{path}", "~/.config"));
      const ask = p2.locator("#modal .cfg-blocked");
      await ask.waitFor();
      await ask.getByRole("button", { name: await tr(p2, "Not now"), exact: true }).click();
      await p2.locator("#modal #typed").waitFor();
      assert.equal(await p2.locator("#modal #typed").inputValue(), "my provider", "back to the editor as it was left");

      // the panel stays quiet
      const panel = await open({ blocked: true, repair: 0 }, "http://uni-switch.test/?mode=panel", 380);
      await panel.waitForTimeout(500);
      assert.ok(await panel.locator("#modal").evaluate((m) => m.hidden), "the panel stays quiet");

      const missing = await page.evaluate(([ks, langs]) => langs.filter((l) => l !== "en").flatMap((l) => ks.filter((k) => !I18N[l][k]).map((k) => l + ": " + k)), [KEYS, LANGS]);
      assert.deepEqual(missing, [], "every language has the dialog's words");
      assert.deepEqual(errors, []);
    });
  }
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// A model taken out of an agent's model list is gone from its model picker
// at once (#927, cjyrainbow): the click on the picker is what closes the
// list, and the picker opened on what the row had before the state came
// back, every model still in it.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const IDS = ["m1", "m2", "m3"];

function fixture(lang) {
  const list = IDS.map((m, i) => ({ id: "relay/" + m, name: m.toUpperCase(), group: "Relay", icon: "generic", inUse: i === 0 }));
  const posts = [];
  let states = 0;
  const count = () => ({ shown: list.filter((m) => !m.hidden).length, listed: list.length, by: [{ name: "Relay", icon: "generic", n: list.filter((m) => !m.hidden).length }] });
  const state = () => ({
    agents: [{
      id: "claude@wsl:Ubuntu", name: "Claude Code · WSL Ubuntu", path: "/test/settings.json", icon: "claude-color", wired: true,
      fields: [{ key: "model", label: "model", value: "relay/m1", options: list.map((m) => ({ value: m.id, label: m.name, ref: m.id, group: "Relay", note: "Relay · via uni-switch" })) }],
      models: count(),
    }],
    profiles: [], settings: { lang, theme: "light" },
  });
  async function serve(route) {
    const req = route.request();
    const url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") {
      // the state after the list closes is slow to come: the picker opened
      // meanwhile has only what the row holds
      if (states++ > 0) await new Promise((r) => setTimeout(r, 3000));
      return json(state()).catch(() => {});
    }
    if (url.pathname === "/api/agent-models/" + encodeURIComponent("claude@wsl:Ubuntu")) {
      if (req.method() === "POST") {
        const { hidden } = req.postDataJSON();
        posts.push(hidden);
        for (const m of list) m.hidden = hidden.includes(m.id) && !m.inUse;
        return json({ models: list, count: count() });
      }
      return json({ models: list });
    }
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  }
  return { serve, posts };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a model taken out is gone from the picker at once`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const page = await browser.newPage({ viewport: { width: 1100, height: 700 } });
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const fx = fixture(lang);
      await page.route("**/*", fx.serve);
      t.after(() => browser.close());
      await page.goto("http://uni-switch.test/");
      const row = page.locator('.row.agent[data-id="claude@wsl:Ubuntu"]');
      const picker = row.locator('.field[data-key="model"]').first();
      const shown = async () => {
        await page.locator("#pop:not([hidden]) #list li").first().waitFor();
        return (await page.locator("#pop #list li .option-words > .v").allTextContents()).filter((s) => /^M\d$/.test(s));
      };
      await picker.click();
      assert.deepEqual(await shown(), ["M1", "M2", "M3"]);
      await page.keyboard.press("Escape");

      await row.locator(".ag-link").click();
      await row.locator(".ag-exp .ag-chips .ag-quiet").click();
      const pop = page.locator(".am-pop:not(.leaving)");
      await pop.waitFor();
      await pop.locator(".am-mr", { hasText: "M2" }).click();
      await pop.locator(".am-mr", { hasText: "M3" }).click();
      await page.waitForTimeout(150);
      assert.deepEqual(fx.posts.at(-1), ["relay/m2", "relay/m3"]);

      // the click on the picker closes the list and opens the picker
      await picker.click();
      await pop.waitFor({ state: "detached" });
      assert.deepEqual(await shown(), ["M1"], "the models taken out are gone from the picker");
      assert.deepEqual(errors, []);
    });
  }
}

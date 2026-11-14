// Defaults keep the authored cascade, including a code span under bold UI.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const assets = path.resolve(__dirname, "../assets");
const choice = (family, weight, style) => ({ family, name: "Chosen", weight, style, stretch: 100 });

for (const [name, engine] of [["chromium", chromium], ["webkit", webkit]]) {
  test(`${name}: font reset restores the original cascade and explicit emphasis`, async () => {
    const css = (await Promise.all(["app.css", "plugins/library/library.css", "plugins/routing/routing.css", "plugins/sessions/sessions.css", "plugins.css"].map(f => fs.readFile(path.join(assets, f), "utf8")))).join("\n");
    const js = await fs.readFile(path.join(assets, "fonts.js"), "utf8");
    const browser = await engine.launch();
    try {
      const page = await browser.newPage();
      await page.setContent(`<style>${css}</style>
        <div class="sess-line"><b><code id="code">model-id</code></b></div>
        <b><span class="key none" id="ui">No key</span></b>
        <div class="acc"><input id="emphasis" class="rename mono" value="Account"></div>
        <span id="badge" class="mk-badge">Code badge</span>`);
      await page.addScriptTag({ content: js });
      const read = () => page.evaluate(() => ["code", "ui", "emphasis", "badge"].map(id => {
        const s = getComputedStyle(document.getElementById(id));
        return { weight: s.fontWeight, style: s.fontStyle, family: s.fontFamily };
      }));
      const defaults = await read();
      assert.equal(defaults[0].weight, "400", "code's authored regular weight must not inherit its bold parent");
      assert.equal(defaults[1].weight, "400", "a UI family override must not discard the key's authored weight");
      assert.equal(defaults[2].weight, "450");
      await page.evaluate(s => window.desktopFonts.apply(s), { uiFont: choice("Arial", 500, "normal"), codeFont: choice("Consolas", 700, "italic") });
      const picked = await read();
      assert.equal(picked[0].weight, "700");
      assert.equal(picked[0].style, "italic");
      assert.equal(picked[1].weight, "500");
      assert.equal(picked[2].weight, "450", "component emphasis stays explicit");
      await page.evaluate(() => window.desktopFonts.apply({}));
      assert.deepEqual(await read(), defaults, "reset restores every original font trait");
      await page.evaluate(s => window.desktopFonts.apply(s), { uiFont: choice("Arial", 600, "italic"), codeFont: choice("Consolas", 400, "normal") });
      const independent = await read();
      assert.equal(independent[3].weight, "400");
      assert.equal(independent[3].style, "normal", "code badges don't inherit the chosen interface slope");
    } finally { await browser.close(); }
  });
}

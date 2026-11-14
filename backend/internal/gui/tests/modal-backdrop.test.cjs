// Full-window backdrop filtering made provider tests and sign-in dialogs
// stutter in the Windows app. Check the computed effect, not a frame-rate
// threshold: browser scheduling and the host GPU vary between test runners.
const assert = require("node:assert/strict");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const assets = path.resolve(__dirname, "../assets");

for (const engine of process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"]) {
  for (const platform of ["Win32", "MacIntel", "Linux x86_64"]) {
    for (const web of [false, true]) {
      for (const mode of ["window", "panel"]) {
        test(`${engine} ${platform} ${web ? "web" : "desktop"} ${mode}: modal backdrop`, async (t) => {
          const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
          t.after(() => browser.close());
          const page = await browser.newPage({ viewport: { width: 1552, height: 905 } });
          await page.route("**/*", (route) => route.request().isNavigationRequest()
            ? route.fulfill({ contentType: "text/html", body: '<div class="modal"><div class="dialog">Provider editor</div></div><dialog class="action-confirm"><h2>Remove?</h2></dialog>' })
            : route.abort());
          await page.goto(`http://uni-switch.test/?mode=${mode}`);
          await page.evaluate(({ platform, web }) => {
            Object.defineProperty(navigator, "platform", { value: platform });
            window.bootPrefs = { web };
          }, { platform, web });
          await page.addScriptTag({ path: path.join(assets, "layout.js") });
          await page.addStyleTag({ path: path.join(assets, "app.css") });
          const backdrop = await page.locator(".modal").evaluate((element) => {
            const style = getComputedStyle(element);
            return { filter: style.backdropFilter || style.webkitBackdropFilter,
              background: style.backgroundColor, pointerEvents: style.pointerEvents };
          });
          // the confirmation dialog's ::backdrop covers the window too
          const confirm = await page.locator("dialog.action-confirm").evaluate((element) => {
            element.showModal();
            const style = getComputedStyle(element, "::backdrop");
            return style.backdropFilter || style.webkitBackdropFilter;
          });
          if (platform === "Win32" && !web) {
            assert.equal(backdrop.filter, "none", "the Windows app must not filter the full-window backdrop");
            assert.equal(confirm, "none", "nor the confirmation's");
          } else {
            assert.match(backdrop.filter, /blur\(8px\)/, "other surfaces keep their existing blur");
            assert.match(backdrop.filter, /saturate\(1\.1\)/);
            assert.match(confirm, /blur\(4px\)/, "the confirmation keeps its blur");
          }
          assert.equal(backdrop.background, "rgba(0, 0, 0, 0.28)", "the dimming veil remains");
          assert.equal(backdrop.pointerEvents, "auto", "the backdrop still receives clicks");
        });
      }
    }
  }
}

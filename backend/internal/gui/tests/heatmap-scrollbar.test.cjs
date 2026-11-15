// Run with Node's test runner and Playwright on the module path; see README.md.
// cybercaius on Discord: the usage heatmap's scrollbar looked out of place
// beside the rest of the window. Its weeks scrolled under scrollbar-width:
// thin, which in Chromium (so WebView2) drops every ::-webkit-scrollbar rule
// and draws the system's bar, and on a Mac left the overlay bar a mouse never
// shows. The weeks now scroll under the same thin thumb as the page around
// them: the same gutter as a .view's, on a Mac and off one, in both engines.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const playwright = require("playwright");

const css = path.resolve(__dirname, "../assets/app.css");
const engines = process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"];

async function gutters(engine, mac) {
  // headless Chromium hides scrollbars unless asked not to
  const browser = await playwright[engine].launch(engine === "chromium" ? { ignoreDefaultArgs: ["--hide-scrollbars"] } : {});
  try {
    const page = await browser.newPage({ viewport: { width: 420, height: 400 } });
    const cells = Array.from({ length: 53 * 7 }, () => "<i></i>").join("");
    await page.setContent(`<style>${await fs.readFile(css, "utf8")}</style>
      <body class="window${mac ? " mac" : ""}">
        <div class="view" id="v" style="height:300px;overflow:auto;display:block"><div style="height:900px;width:2000px">
          <div class="led-heat" style="width:360px"><div class="heat-scroll" id="h"><div class="heat-grid"><span></span>${cells}</div></div></div>
        </div></div>
      </body>`);
    return await page.evaluate(() => {
      const bar = (el, side) => {
        const cs = getComputedStyle(el);
        return side === "bottom"
          ? el.offsetHeight - el.clientHeight - parseFloat(cs.borderTopWidth) - parseFloat(cs.borderBottomWidth)
          : el.offsetWidth - el.clientWidth - parseFloat(cs.borderLeftWidth) - parseFloat(cs.borderRightWidth);
      };
      const h = document.querySelector("#h");
      const v = document.querySelector("#v");
      return { scrolls: h.scrollWidth > h.clientWidth, width: getComputedStyle(h).scrollbarWidth, heat: bar(h, "bottom"), view: bar(v, "right") };
    });
  } finally {
    await browser.close();
  }
}

for (const engine of engines) {
  for (const mac of [false, true]) {
    test(`${engine}${mac ? ", on a Mac" : ", off a Mac"}: the heatmap's weeks scroll under the app's thin thumb, as the page does`, async () => {
      const g = await gutters(engine, mac);
      assert.ok(g.scrolls, "a narrow card's weeks overflow: " + JSON.stringify(g));
      assert.equal(g.width, "auto", "no scrollbar-width, or Chromium draws the system's bar: " + JSON.stringify(g));
      assert.ok(g.view > 0, "the page draws its thumb: " + JSON.stringify(g));
      assert.equal(g.heat, g.view, "the heatmap's bar is the page's: " + JSON.stringify(g));
    });
  }
}

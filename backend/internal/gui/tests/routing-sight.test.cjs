// Run with Node's test runner and Playwright on the module path; see README.md.
// A model that turned an image away (xyenon: a relay's DeepSeek-V4.1-Flash
// answering 400 "unknown variant `image_url`") is asked again with the image
// described, not handed to another provider: the Routing page's story says
// so, in the page's language, and that it went on to no other provider.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const now = new Date();
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const at = (i) => new Date(now.getTime() - (i + 1) * 60e3).toISOString();
const vendor = "provider-1: Failed to deserialize the JSON body into the target type: messages[1]: unknown variant `image_url`, expected `text` at line 1 column 162";
const p1 = { id: "provider-1", provider: "provider-1", name: "provider-1", kind: "provider", model: "DeepSeek-V4.1-Flash" };
const p2 = { id: "provider-2", provider: "provider-2", name: "provider-2", kind: "provider", model: "DeepSeek-V4.1-Flash" };
const routes = [{
  id: 100, seq: 100, time: at(0), agent: "codex", model: "provider-1/DeepSeek-V4.1-Flash", provider: "provider-1", order: [p1, p2],
  tries: [
    { id: p1.id, model: p1.model, start: at(0), done: true, status: 400, ms: 90, fail: "sight", error: vendor },
    { id: p1.id, model: p1.model, start: at(0), done: true, status: 200, ms: 800 },
  ],
  done: true, status: 200, ms: 900,
}, {
  id: 99, seq: 99, time: at(1), agent: "codex", model: "provider-1/DeepSeek-V4.1-Flash", provider: "provider-1", order: [p1, p2],
  tries: [{ id: p1.id, model: p1.model, start: at(1), done: true, status: 200, ms: 500 }], done: true, status: 200, ms: 500,
}];

function serve(lang) {
  const state = { agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) await new Promise((r) => setTimeout(r, 20e3));
      return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    }
    if (url.pathname === "/api/gateway/history") {
      const d = url.searchParams.get("day");
      return json({ cut: false, days: [{ day, requests: routes.length }], routes: d ? routes : [] });
    }
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const want = {
  en: { story: "it takes no images, so it is asked again with the images described by the image recognition model", rest: "so it doesn't rest" },
  zh: { story: "它不接受图片，已改用看图模型描述图片后重新询问", rest: "不休息" },
  "zh-TW": { story: "它不接受圖片，已改用看圖模型描述圖片後重新詢問", rest: "不休息" },
  ja: { story: "画像を受け付けないため、画像認識モデルが画像を説明したうえで", rest: "休止しません" },
  de: { story: "Es nimmt keine Bilder, daher wird es erneut gefragt", rest: "pausiert es nicht" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(want)) {
    test(`${engine} ${lang}: a model that took no image is asked again with it described`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const context = await browser.newContext({ viewport: { width: 1100, height: 760 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-sight.png`), fullPage: true });
        }
        await browser.close();
      });
      await page.goto("http://uni-switch.test/?view=routing");
      await page.locator(".rt-days .rt-day").nth(1).click();
      await page.locator(".rt-req").nth(routes.length - 1).waitFor();

      const row = page.locator(".rt-req").nth(0);
      // measure once the page holds still: the day's rows arriving grow the
      // page above the list, and the day button's click is kept in place by
      // scrolling the view, which can land after the last row is in
      const was = await row.evaluate(async (e) => {
        let last = NaN;
        for (let i = 0; i < 60; i++) {
          await new Promise((r) => requestAnimationFrame(r));
          const top = e.getBoundingClientRect().top;
          if (top === last) return top;
          last = top;
        }
        return last;
      });
      await row.click();
      await page.waitForTimeout(400);
      assert(Math.abs((await row.evaluate((e) => e.getBoundingClientRect().top)) - was) <= 1, "picking the request moved the page");
      const steps = await page.locator(".rt-steps li").allTextContents();
      const story = steps.find((s) => s.includes(want[lang].story));
      assert(story, JSON.stringify(steps));
      assert(story.includes(want[lang].rest) && story.includes("400"), story);
      // the same model answered after it: its row says so, not a failure
      assert(!steps.some((s) => s.includes("went on to the next") || s.includes("转给下一个")), JSON.stringify(steps));
      assert.deepEqual(errors, []);
    });
  }
}

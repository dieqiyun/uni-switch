// Run with Node's test runner and Playwright on the module path; see README.md.
// #2636: in Library → Instructions, typing with a Chinese input method made
// the page jump up or down while a word was being composed. Each step of a
// composition is an input, and the textarea's autosize set its height to
// auto to measure it: the page went short for a moment, the browser clamped
// the view's scroll to the shorter page, and the scroll guard's putting it
// back pulled the textarea, taller than the view, to one of its edges. The
// page stays where it is while a word is composed and committed, in the
// Default set's text near the page's foot and in an agent's own text at the
// very foot. Chromium composes through its input-method protocol
// (Input.imeSetComposition); WebKit, which has none, gets the composition's
// events and inputs as an input method sends them. English and Chinese, wide
// and narrow; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/aimer";
const ids = ["claude", "codex", "gemini", "opencode", "pi", "kimi", "qwen", "droid", "crush", "goose", "amp", "cline"];
const named = (id) => id[0].toUpperCase() + id.slice(1);
const lines = (n, what) => Array.from({ length: n }, (_, i) => `- ${what} ${i + 1}: keep it short`).join("\n");
const library = () => ({
  dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
  agents: ids.map((id) => ({ id, name: named(id), icon: "", instructions: `${HOME}/.${id}/AGENTS.md` })),
  instructions: {
    shared: "",
    sets: [{ id: "default", name: "", active: true, text: lines(60, "rule") }],
    agents: ids.map((id) => ({ agent: id, name: named(id), icon: "", path: `${HOME}/.${id}/AGENTS.md`, on: true, extra: id === "cline" ? lines(10, "note") : "" })),
  },
  foundServers: [], projects: [], foundSkills: [], skills: [], servers: [],
});

function server(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: [], profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/plugins") return route.fulfill({ json: { plugins: [] } });
    if (url.pathname === "/api/library") return route.fulfill({ json: library() });
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const view = "#view-library";
// pinyin as it is typed, then the word picked
const words = [["n", "ni", "nih", "niha", "nihao"], "你好", ["s", "sh", "shi", "shij", "shiji", "shijie"], "世界"];

// A word composed and committed at the caret, as an input method does it.
async function compose(page, engine, cdp, ta) {
  for (let i = 0; i < words.length; i += 2) {
    if (engine === "chromium") {
      for (const p of words[i]) {
        await cdp.send("Input.imeSetComposition", { text: p, selectionStart: p.length, selectionEnd: p.length });
        await page.waitForTimeout(40);
      }
      await cdp.send("Input.insertText", { text: words[i + 1] });
      await page.waitForTimeout(40);
      continue;
    }
    await ta.evaluate((e) => {
      e.dispatchEvent(new CompositionEvent("compositionstart", { bubbles: true, data: "" }));
      e.dataset.from = e.selectionStart;
      e.dataset.len = 0;
    });
    const step = (data, done) => ta.evaluate((e, [data, done]) => {
      const from = +e.dataset.from;
      e.setRangeText(data, from, from + +e.dataset.len, "end");
      e.dataset.len = data.length;
      if (!done) e.dispatchEvent(new CompositionEvent("compositionupdate", { bubbles: true, data }));
      e.dispatchEvent(new InputEvent("input", { bubbles: true, data, inputType: done ? "insertFromComposition" : "insertCompositionText", isComposing: !done }));
      if (done) e.dispatchEvent(new CompositionEvent("compositionend", { bubbles: true, data }));
    }, [data, done]);
    for (const p of words[i]) { await step(p, false); await page.waitForTimeout(40); }
    await step(words[i + 1], true);
    await page.waitForTimeout(40);
  }
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": composing a word in the Library's instructions leaves the page where it is", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const lang of ["en", "zh"]) for (const width of [980, 560]) for (const field of ["set:default", "extra:cline"]) {
      await t.test(`${lang}, ${width}px wide, ${field}`, async () => {
        const ctx = await browser.newContext({ viewport: { width, height: 640 }, reducedMotion: "reduce" });
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "instructions"); } catch {} });
        const page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang));
        await page.goto("http://uni-switch.test/");
        await page.locator('button[data-view="library"]').click();
        await page.locator(`${view} .lib-set`).first().click();
        await page.locator(`${view} textarea[data-lib="set:default"]`).waitFor();
        await page.waitForTimeout(300);
        const v = page.locator(view);
        const ta = page.locator(`${view} textarea[data-lib="${field}"]`);
        const wheel = async (n) => { for (let i = 0; i < n; i++) { await page.mouse.move(width / 2, 320); await page.mouse.wheel(0, 200); await page.waitForTimeout(20); } };
        if (field === "set:default") {
          // down by the wheel till the set's last lines, and the caret at
          // their end, are in sight
          for (let i = 0; i < 60; i++) {
            const [bottom, foot] = await ta.evaluate((e) => [e.getBoundingClientRect().bottom, e.closest(".view").getBoundingClientRect().bottom]);
            if (bottom < foot - 60) break;
            await wheel(1);
          }
        } else {
          // Cline's own text, open at the foot of the page
          await wheel(30);
          await page.waitForTimeout(300);
          const row = page.locator(`${view} .lib-row:not(.lib-set)`).filter({ hasText: "Cline" }).first();
          const b = await row.boundingBox();
          await page.mouse.click(b.x + 80, b.y + b.height / 2);
          await ta.waitFor();
          await page.waitForTimeout(300);
          await wheel(10); // to the new foot, the click's hold let go of
        }
        await page.waitForTimeout(400);
        const before = await v.evaluate((e) => e.scrollTop);
        assert(before > 100, "the page didn't scroll down: " + before);
        // the caret at the text's end, in sight
        await ta.evaluate((e) => { e.focus({ preventScroll: true }); e.setSelectionRange(e.value.length, e.value.length); });
        const caret = await ta.evaluate((e) => {
          const r = e.getBoundingClientRect(), b = e.closest(".view").getBoundingClientRect();
          return [r.bottom, b.top, b.bottom];
        });
        assert(caret[0] > caret[1] && caret[0] < caret[2], `the text's end isn't in sight: ${caret}`);
        await page.evaluate(() => {
          const v = document.querySelector("#view-library");
          window.__seen = [];
          const look = () => window.__seen?.push(v.scrollTop);
          v.addEventListener("scroll", look, { passive: true });
          (function frame() { look(); if (window.__seen) requestAnimationFrame(frame); })();
        });
        const cdp = engine === "chromium" ? await ctx.newCDPSession(page) : null;
        await compose(page, engine, cdp, ta);
        await page.waitForTimeout(400);
        const seen = await page.evaluate(() => { const s = window.__seen; window.__seen = null; return s; });
        assert.match(await ta.inputValue(), /你好世界$/, "the words weren't typed");
        const moved = seen.filter((s) => Math.abs(s - before) > 1);
        assert.deepEqual(moved, [], `the page moved while a word was composed, from ${before} (the page went to ${[...new Set(moved)].join(", ")})`);
        await ctx.close();
      });
    }
    assert.deepEqual(errors, []);
  });
}

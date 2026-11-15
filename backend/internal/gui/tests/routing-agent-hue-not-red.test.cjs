// Run with Node's test runner and Playwright on the module path; see README.md.
// The Routing stage marks each agent with its colour: a dot before its
// name, the wire its requests take, the uni-switch flying them (hueOf). An
// agent with no maker's colour is given one of the spares by its id, and
// "openai" (a caller that names itself OpenAI) drew rose #e11d48: zhouatie
// on X saw a red dot on its node and a red dotted wire from uni-switch and read
// them as something wrong ("这个红色颜色，让人以为是状态异常"). Red on this
// page means a failure (an answer ringed red, a request that failed), so no
// agent's colour may look like it: every colour hueOf can give (PALETTE) sits well
// away from --red, in the light theme and the dark, and the reporter's
// agent, drawn on the stage in the dark theme, is marked in one of them.
// Every language and narrow windows; the API is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

// OKLCH hue and chroma of a colour, from its sRGB
function oklch(css) {
  let r, g, b;
  const hex = css.match(/^#([0-9a-f]{6})$/i);
  if (hex) [r, g, b] = [0, 2, 4].map((i) => parseInt(hex[1].slice(i, i + 2), 16));
  else [r, g, b] = css.match(/[\d.]+/g).map(Number);
  const lin = (v) => { v /= 255; return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; };
  [r, g, b] = [r, g, b].map(lin);
  const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
  const A = 1.9779984951 * l - 2.4285922050 * m + 0.4505937099 * s;
  const B = 0.0259040371 * l + 0.7827717662 * m - 0.8086757660 * s;
  return { h: (Math.atan2(B, A) * 180 / Math.PI + 360) % 360, c: Math.hypot(A, B) };
}
const apart = (a, b) => { const d = Math.abs(a - b) % 360; return Math.min(d, 360 - d); };
// a colour reads as the failure red when its OKLCH hue is within this many
// degrees of --red's and it has colour enough to show a hue at all. The
// rose that was given out sat 7 to 9 degrees off; Claude's clay, its
// maker's own and read as orange, sits 13 to 14 off and stays.
const NEAR = 12;
const reads = (css, reds) => oklch(css).c > 0.04 && reds.some((red) => apart(oklch(css).h, oklch(red).h) < NEAR);

async function redsOf() {
  const css = await fs.readFile(path.join(assets, "app.css"), "utf8");
  const reds = [...new Set([...css.matchAll(/--red:\s*(#[0-9a-f]{6})/gi)].map((m) => m[1].toLowerCase()))];
  assert(reds.length >= 2, "no light and dark --red in app.css: " + reds);
  return reds;
}

test("no colour an agent can be marked with reads as the failure red", async () => {
  const reds = await redsOf();
  const js = await fs.readFile(path.join(assets, "plugins/routing/client.js"), "utf8");
  const palette = js.match(/const PALETTE = \[([^\]]*)\]/);
  assert(palette, "hueOf's palette not found in routing/client.js");
  const all = [...palette[1].matchAll(/"(#[0-9a-f]{6})"/gi)].map((m, i) => [i, m[1]]);
  assert(all.length > 10, "palette too small: " + JSON.stringify(all));
  const red = all.filter(([, c]) => reads(c, reds));
  assert.deepEqual(red, [], "agent colours that read as --red " + reds.join("/"));
});

const now = new Date();
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const at = (i) => new Date(now.getTime() - (i + 1) * 60e3).toISOString();
const seat = { id: "ag", provider: "ag", name: "OpenAI", kind: "provider", model: "gemini-3-pro" };
const routes = [{
  id: 100, seq: 100, time: at(0), agent: "openai", model: "ag/antigravity/gemini-3-pro", provider: "ag", order: [seat],
  tries: [{ id: seat.id, model: seat.model, start: at(0), done: true, status: 200, ms: 800 }], done: true, status: 200, ms: 800,
}];

function serve(lang) {
  const state = { agents: [{ id: "openai", name: "OpenAI", path: "", fields: [] }], profiles: [], settings: { lang, theme: "dark" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"dark",web:true};` });
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
    if (url.pathname === "/api/providers") return json({ providers: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [1100, 420]) {
      test(`${engine} ${lang} ${width}: an agent named OpenAI isn't marked in the failure red`, async (t) => {
        const reds = await redsOf();
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        const context = await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce", colorScheme: "dark" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-agent-hue.png`), fullPage: true });
          }
          await browser.close();
        });
        await page.goto("http://uni-switch.test/?view=routing");
        await page.locator(".rt-days .rt-day").nth(1).click();
        await page.locator(".rt-req").first().click();
        const src = page.locator(".rt-src").filter({ hasText: "OpenAI" });
        await src.waitFor();
        const got = await src.evaluate((n) => ({
          dot: getComputedStyle(n.querySelector("b"), "::before").backgroundColor,
          hue: getComputedStyle(n).getPropertyValue("--agent").trim(),
        }));
        assert(!reads(got.dot, reds), `the agent's dot ${got.dot} reads as --red`);
        assert(!reads(got.hue, reds), `the agent's colour ${got.hue} reads as --red`);
        const wires = await page.locator(".rt-wires path").evaluateAll((ps) => ps.map((p) => p.style.getPropertyValue("--agent").trim()).filter(Boolean));
        for (const w of wires) assert(!reads(w, reds), `a wire in ${w} reads as --red`);
        assert.deepEqual(errors, []);
      });
    }
  }
}

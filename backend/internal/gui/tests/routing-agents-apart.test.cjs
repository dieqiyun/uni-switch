// Run with Node's test runner and Playwright on the module path; see README.md.
// The agents on the Routing stage together are each marked in a colour of
// their own (Pkh on Discord: trails in the same colour, or the wrong one).
// An agent with no maker's colour was given one of eight by its id, and 49
// of uni-switch's 59 agents have none: QwenPaw, ZCode, Qwen Code and AstrBot all
// drew #65a30d, and Gemini's and DevEco's own blues sat 8 degrees of hue
// apart. Four agents with requests at once each get a colour at least 20
// degrees of OKLCH hue from every other's, and an agent's colour stays its
// own from one request to the next. English, Chinese (simplified and traditional), Japanese and
// German, at 1100px and 420px, in the light and the dark; the API is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(process.env.UNI_SWITCH_ROUTING_ASSETS || path.join(__dirname, "../assets"));

// OKLCH hue of an sRGB colour, #rrggbb or rgb()
function hueOf(css) {
  const hex = css.match(/^#([0-9a-f]{6})$/i);
  let [r, g, b] = hex ? [0, 2, 4].map((i) => parseInt(hex[1].slice(i, i + 2), 16)) : css.match(/[\d.]+/g).map(Number);
  const lin = (v) => { v /= 255; return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; };
  [r, g, b] = [r, g, b].map(lin);
  const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
  const A = 1.9779984951 * l - 2.4285922050 * m + 0.4505937099 * s;
  const B = 0.0259040371 * l + 0.7827717662 * m - 0.8086757660 * s;
  return (Math.atan2(B, A) * 180 / Math.PI + 360) % 360;
}
const apart = (a, b) => { const d = Math.abs(hueOf(a) - hueOf(b)) % 360; return Math.min(d, 360 - d); };

const AGENTS = [["qwenpaw", "QwenPaw"], ["zcode", "ZCode"], ["gemini", "Gemini CLI"], ["deveco", "DevEco"]];
const ago = (s) => new Date(Date.now() - s * 1e3).toISOString();
const route = (id, agent) => {
  const seat = { id: `p${id}#k`, provider: `p${id}`, name: `Provider ${id}`, who: `key-${id}`, kind: "key", model: "glm-5.1" };
  return { id, seq: id, time: ago(5 - id), agent, model: "glm-5.1", provider: seat.provider, done: false, order: [seat],
    tries: [{ id: seat.id, model: seat.model, start: ago(4), done: false }] };
};

function serve(lang, theme, live) {
  return async (request) => {
    const url = new URL(request.request().url());
    const json = (data) => request.fulfill({ json: data });
    if (url.pathname === "/boot.js") return request.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"${theme}",web:true};` });
    if (url.pathname === "/wails/runtime.js") return request.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: AGENTS.map(([id, name]) => ({ id, name, path: "", fields: [] })), profiles: [], settings: { lang, theme } });
    if (url.pathname === "/api/gateway/trace") {
      if (url.searchParams.get("wait")) {
        if (live.queued.length) return json(live.queued.shift());
        live.pending.push(request);
        return;
      }
      return json({ mine: true, now: new Date().toISOString(), seq: 1, totals: { requests: 4, rerouted: 0, errors: 0 }, routes: AGENTS.map(([id], i) => route(i + 1, id)) });
    }
    if (url.pathname === "/api/gateway/history") return json({ cut: false, days: [], routes: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await request.fulfill({ body: await fs.readFile(file), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const [width, theme] of [[1100, "light"], [420, "dark"]]) {
      test(`${engine} ${lang} ${width}px ${theme}: the agents on the stage together are each in a colour of their own`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await browser.newPage({ viewport: { width, height: 900 }, reducedMotion: "reduce", colorScheme: theme });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (error) => errors.push(error.message));
        const live = { pending: [], queued: [], seq: 1 };
        await page.route("**/*", serve(lang, theme, live));
        const colours = () => page.evaluate(() => Object.fromEntries([...document.querySelectorAll(".rt-src")]
          .map((n) => [n.querySelector("b").textContent, getComputedStyle(n.querySelector("b"), "::before").backgroundColor])));
        await page.goto("http://uni-switch.test/?view=routing");
        await page.waitForFunction(() => document.querySelectorAll(".rt-src").length === 4);
        const got = await colours();
        assert.deepEqual(Object.keys(got).sort(), AGENTS.map(([, n]) => n).sort());
        const names = Object.keys(got);
        for (let i = 0; i < names.length; i++) {
          for (let j = i + 1; j < names.length; j++) {
            const [a, b] = [names[i], names[j]];
            assert(apart(got[a], got[b]) >= 20, `${a} ${got[a]} and ${b} ${got[b]} are ${apart(got[a], got[b]).toFixed(1)}° apart`);
          }
        }
        // each agent's wire is its node's colour
        const wired = await page.evaluate(() => [...document.querySelectorAll(".rt-src")].map((n) => n.style.getPropertyValue("--agent").trim()));
        assert.equal(new Set(wired).size, 4, "agents' colours: " + wired);

        // QwenPaw sends again: every agent keeps the colour it had
        send(live, AGENTS.map(([id], i) => route(i + 1, id)).concat(route(5, "qwenpaw")));
        await page.waitForFunction(() => [...document.querySelectorAll(".rt-accts .who")].some((w) => w.textContent === "key-5"));
        assert.deepEqual(await colours(), got);
        assert.deepEqual(errors, []);
      });
    }
  }
}

function send(live, routes) {
  const d = { mine: true, now: new Date().toISOString(), seq: ++live.seq, totals: { requests: live.seq }, routes };
  const r = live.pending.shift();
  if (r) r.fulfill({ json: d }); else live.queued.push(d);
}

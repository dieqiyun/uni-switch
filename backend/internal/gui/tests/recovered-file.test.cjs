// Run with Node's test runner and Playwright on the module path; see README.md.
// A file of uni-switch's own a crash left unreadable (#1505: logins.json and
// plugin-auth.json at their full length, every byte zero) is read back from
// its last good copy, and /api/state's recovered says so: the window tells
// the user once which file, from when, that a later change may be missing
// and the damaged file is kept. A later load doesn't tell it again; a
// recovery noted later is told. In every language, Chromium and WebKit;
// the API is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

const BAK = "2026-10-09T21:14:03+08:00";
const LOGINS = { file: "logins.json", path: "/home/u/.config/uni-switch/logins.json", why: "10783 bytes, every one zero", bak: BAK, at: "2026-10-10T08:00:00Z" };
const AUTH = { file: "plugin-auth.json", path: "/home/u/.config/uni-switch/plugin-auth.json", why: "29926 bytes, every one zero", bak: BAK, at: "2026-10-10T08:00:05Z" };

function serve(lang, ctl) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light"};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") {
      ctl.states++;
      return json({ agents: [], profiles: [], settings: { lang, theme: "light" }, recovered: ctl.recovered });
    }
    if (url.pathname === "/api/whatsnew") return json({ show: false, releases: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// what each language says: the start of the message, and that the damaged
// file is kept
const words = {
  en: ["logins.json was damaged", "the damaged file is kept beside it"],
  zh: ["logins.json 已损坏", "损坏的文件保留在旁边"],
  "zh-TW": ["logins.json 已損壞", "損壞的檔案保留在旁邊"],
  ja: ["logins.json が破損していました", "破損したファイルはそのそばに残してあります"],
  de: ["logins.json war beschädigt", "die beschädigte Datei liegt daneben"],
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const [lang, [start, kept]] of Object.entries(words)) {
    test(`${engine} ${lang}: a file restored from its backup is told once`, async () => {
      const browser = await { chromium, webkit }[engine].launch();
      try {
        const page = await browser.newPage({ viewport: { width: 420, height: 760 } });
        const ctl = { states: 0, recovered: [LOGINS] };
        await page.route("**/*", serve(lang, ctl));
        await page.goto("http://uni-switch.test/");
        const st = page.locator("#status");
        await st.filter({ hasText: start }).waitFor({ timeout: 10000 });
        const said = await st.textContent();
        assert.ok(said.includes(kept), said);
        // the backup's time, in the page's language: its year and day
        assert.match(said, /2026/);
        assert.match(said, /9|10/);
        assert.equal(await st.getAttribute("class"), "status warn");
        // narrow: the pill stays in the window
        const box = await st.boundingBox();
        assert.ok(box.x >= 0 && box.x + box.width <= 420 + 1, JSON.stringify(box));

        // loaded again: not told again
        await page.evaluate(() => { const s = document.querySelector("#status"); s.textContent = ""; s.className = "status"; });
        const before = ctl.states;
        await page.evaluate(() => load());
        assert.ok(ctl.states > before);
        assert.equal(await st.textContent(), "");

        // a recovery noted later (the plugin host's) is told
        ctl.recovered = [LOGINS, AUTH];
        await page.evaluate(() => load());
        await st.filter({ hasText: "plugin-auth.json" }).waitFor({ timeout: 10000 });
        assert.ok(!(await st.textContent()).includes("logins.json"), await st.textContent());
      } finally {
        await browser.close();
      }
    });
  }
}

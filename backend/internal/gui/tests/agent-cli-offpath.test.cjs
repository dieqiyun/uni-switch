// Run with Node's test runner and Playwright on the module path; see README.md.
// 悠悠哥 on Discord (macOS): the Agents page listed "Codex 0.162.1" under Not
// set up, while `which -a codex` found nothing and ~/.codex wasn't there.
// uni-switch had found a codex in a folder it adds to its own PATH (an npm
// install under a Node nvm no longer uses) that the terminal's PATH lacks.
// Now a CLI found off the terminal's PATH says so beside its version, its
// tip (and the version's) naming where it is, in every language; one on the
// terminal's PATH is as it was. At the window's narrow width the name keeps
// its room and nothing scrolls sideways. No backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const models = ["model-a", "model-b"].map((m) => ({ value: m, label: m }));
const agent = (id, name) => ({ id, name, path: "~/." + id + "/config.toml", fields: [{ key: "model", label: "model", value: "model-a", options: models }] });
const AGENTS = [agent("codex", "Codex"), agent("gemini", "Gemini CLI")];
const BIN = "/Users/u/.nvm/versions/node/v22.12.0/bin/codex";
const CLIS = {
  codex: { version: "0.162.1", path: BIN, offPath: true },
  gemini: { version: "0.60.0", latest: "0.60.0", via: "npm", command: "npm install -g @google/gemini-cli@latest", path: "/opt/homebrew/bin/gemini" },
};

function server(lang) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: AGENTS, profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/agents/cli" && req.method() === "GET") return route.fulfill({ json: { agents: CLIS, pending: false } });
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const row = (id) => `.row.agent[data-id="${id}"]`;
const esc = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a CLI found off the terminal's PATH says where it is", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      await browser.close();
    });
    const open = async (lang, width = 980) => {
      const page = await (await browser.newContext({ viewport: { width, height: 640 } })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(lang));
      await page.goto("http://uni-switch.test/");
      await page.locator(`${row("codex")} .ag-ver`).first().waitFor();
      return page;
    };

    for (const [lang, said, tip] of [
      ["en", "Not on your terminal's PATH", `uni-switch found Codex 0.162.1 at ${BIN}. That folder is one uni-switch looks in, but your terminal's PATH doesn't have it`],
      ["zh", "不在终端的 PATH 里", `uni-switch 在 ${BIN} 找到了 Codex 0.162.1。`],
      ["zh-TW", "不在終端機的 PATH 裡", `uni-switch 在 ${BIN} 找到了 Codex 0.162.1。`],
      ["ja", "ターミナルの PATH にありません", `uni-switch は Codex 0.162.1 を ${BIN} で見つけました。`],
      ["de", "Nicht im PATH des Terminals", `uni-switch hat Codex 0.162.1 unter ${BIN} gefunden.`],
    ]) {
      await t.test(lang + ": the note and where, beside the version", async () => {
        const page = await open(lang);
        const note = page.locator(`${row("codex")} .ag-offpath`);
        assert.equal(await note.textContent(), said);
        assert.match(await note.getAttribute("title"), new RegExp(esc(tip)));
        assert.match(await page.locator(`${row("codex")} .ag-ver`).getAttribute("title"), new RegExp(esc(tip)));
        // one on the terminal's PATH: no note, its tip as before
        assert.equal(await page.locator(`${row("gemini")} .ag-offpath`).count(), 0);
        assert.equal(await page.locator(`${row("gemini")} .ag-ver`).textContent(), "0.60.0");
        // the version, then the note, on one line with the name
        const order = await page.locator(`${row("codex")} .ag-cli > *`).evaluateAll((xs) => xs.map((x) => x.classList.contains("ag-offpath") ? "note" : "ver"));
        assert.deepEqual(order, ["ver", "note"]);
        const mids = await page.evaluate((r) => [".name", ".ag-ver", ".ag-offpath"].map((s) => {
          const b = document.querySelector(r + " " + s).getBoundingClientRect();
          return b.top + b.height / 2;
        }), row("codex"));
        assert(Math.max(...mids) - Math.min(...mids) <= 2, `not on one line: ${mids}`);
        await page.close();
      });
    }

    for (const width of [560, 440]) {
      await t.test(`at ${width}px the name keeps its room and nothing scrolls sideways`, async () => {
        const page = await open("de", width);
        assert(await page.locator(`${row("codex")} .name`).evaluate((n) => n.scrollWidth <= n.clientWidth + 1), "Codex's name is cut");
        const note = await page.locator(`${row("codex")} .ag-offpath`).evaluate((a) => {
          const box = a.closest(".ag-cli").getBoundingClientRect(), r = a.getBoundingClientRect();
          return { shown: getComputedStyle(a).display !== "none", inside: r.right <= box.right + 1, ellipsis: getComputedStyle(a).textOverflow === "ellipsis" };
        });
        assert(!note.shown || (note.inside && note.ellipsis), `the note is cut: ${JSON.stringify(note)}`);
        const side = await page.evaluate(() => document.scrollingElement.scrollWidth - document.scrollingElement.clientWidth);
        assert(side <= 0, `the page scrolls ${side}px sideways`);
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-cli-offpath-${width}.png`) });
        }
        await page.close();
      });
    }
    assert.deepEqual(errors, []);
  });
}

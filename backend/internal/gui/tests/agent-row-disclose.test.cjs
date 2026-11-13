// Run with Node's test runner and Playwright on the module path; see README.md.
// A connected agent's row in the window opens and closes on a click
// anywhere on it that isn't one of its controls, as a disclosure row does
// on the Mac (NG_shiyu on X: the space between the name and the model did
// nothing, only the small Models › link opened it, which is far to reach
// on a large screen). Its controls still do only their own thing: the
// model picker opens its menu, the grip its menu, the switch asks to
// disconnect, and a click inside the opened part leaves it open. The link
// stays the control the keyboard uses, its aria-expanded true while the
// row is open. A row that isn't connected has nothing to open. No click
// moves the page. Wide and narrow windows, every language, Chromium and
// WebKit. No backend: faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const opts = [
  { value: "group/steady", label: "Never stuck", ref: "group/steady", group: "Routing groups", icons: ["deepseek"] },
  { value: "ds/pro", label: "DeepSeek Pro", ref: "ds/pro", note: "DeepSeek", group: "DeepSeek", icon: "deepseek" },
];
const models = { shown: 2, listed: 2, by: [{ name: "Routing groups", icons: ["deepseek"], n: 1 }, { name: "DeepSeek", icon: "deepseek", n: 1 }], names: ["Never stuck", "DeepSeek Pro"] };
// enough agents under them that the page scrolls
const more = Array.from({ length: 12 }, (_, i) => ({ id: "a" + i, name: "Agent " + i, icon: "generic", path: "/f/a" + i, wired: true, models, fields: [{ key: "model", label: "model", value: "ds/pro", options: opts }] }));
const state = (lang) => ({
  agents: [
    { id: "codex", name: "Codex", icon: "generic", path: "/f/codex", wired: true, models, fields: [{ key: "model", label: "model", value: "group/steady", options: opts }] },
    { id: "gemini", name: "Gemini CLI", icon: "generic", path: "/f/gemini", wired: false, fields: [{ key: "model", label: "model", value: "", options: opts }] },
    ...more,
  ],
  profiles: [], settings: { lang, theme: "light" },
});

const serve = (lang) => async (route) => {
  const url = new URL(route.request().url());
  const json = (data) => route.fulfill({ json: data });
  if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
  if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
  if (url.pathname === "/api/state") return json(state(lang));
  if (url.pathname === "/api/usage/quotas") return json([]);
  if (url.pathname === "/api/groups") return json({ groups: [] });
  if (url.pathname === "/api/plugins") return json({ plugins: [] });
  if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
  if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
  if (url.pathname.startsWith("/api/")) return json({});
  const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
  const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
  const body = await fs.readFile(file).catch(() => null);
  await route.fulfill(body ? { body, contentType } : { status: 404, body: "" });
};

const codex = '.row.agent[data-id="codex"]';
// the page's scroll, wherever the page scrolls
const scrolled = (page) => page.evaluate(() => [document.scrollingElement.scrollTop, ...[...document.querySelectorAll(".view")].map((v) => v.scrollTop)].join());
// the middle of the row's empty space between the name and the link, or,
// in a window too narrow for one, the end of the line under the name
const gap = (page, r) => page.evaluate((r) => {
  const row = document.querySelector(r), who = row.querySelector(":scope > .who").getBoundingClientRect();
  const next = row.querySelector(":scope > .ag-link, :scope > .field").getBoundingClientRect();
  const words = Math.max(...[...row.querySelectorAll(":scope > .who > *")].map((e) => { const g = document.createRange(); g.selectNodeContents(e); return g.getBoundingClientRect().right; }));
  if (next.left - words > 16 && Math.abs(next.top - who.top) < who.height) return { x: (words + next.left) / 2, y: next.top + next.height / 2, empty: true };
  const name = row.querySelector(".name").getBoundingClientRect();
  return { x: name.left + name.width / 2, y: name.top + name.height / 2, empty: false };
}, r);
const isOpen = (page, r = codex) => page.evaluate((r) => {
  const row = document.querySelector(r);
  return { open: row.classList.contains("expanded") && !!row.querySelector(":scope > .ag-exp"), aria: row.querySelector(":scope > .ag-link")?.getAttribute("aria-expanded") ?? null };
}, r);
const quiet = (page) => page.waitForTimeout(600); // the sheet slides

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    for (const width of [1100, 560]) {
      test(`${engine} ${lang} ${width}px: a click on a connected row's empty space opens and closes it`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const page = await browser.newPage({ viewport: { width, height: 560 } });
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", serve(lang));
        await page.goto("http://uni-switch.test/?view=agents");
        await page.locator(`${codex} .ag-link`).waitFor();
        const y = await scrolled(page);
        assert.deepEqual(await isOpen(page), { open: false, aria: "false" });

        // the row's empty space: open, and the link says so
        let at = await gap(page, codex);
        if (width === 1100) assert.ok(at.empty, "no empty space between the name and the link at 1100px");
        await page.mouse.click(at.x, at.y);
        await quiet(page);
        assert.deepEqual(await isOpen(page), { open: true, aria: "true" }, "the row didn't open");
        // a click inside what opened leaves it open
        const exp = await page.locator(`${codex} > .ag-exp`).boundingBox();
        await page.mouse.click(exp.x + exp.width - 6, exp.y + exp.height - 6);
        await quiet(page);
        assert.equal((await isOpen(page)).open, true, "a click inside the opened part closed it");
        // and again on the row: closed
        at = await gap(page, codex);
        await page.mouse.click(at.x, at.y);
        await quiet(page);
        assert.deepEqual(await isOpen(page), { open: false, aria: "false" }, "the row didn't close");

        // its name opens it too: the whole row is the disclosure
        await page.locator(`${codex} .name`).click();
        await quiet(page);
        assert.equal((await isOpen(page)).open, true, "a click on the name didn't open it");
        await page.locator(`${codex} > .ag-link`).click();
        await quiet(page);
        assert.equal((await isOpen(page)).open, false, "the link didn't close it");

        // its controls do only their own thing
        await page.locator(`${codex} > .field.ag-start`).click();
        await page.locator("#pop.model-picker").waitFor();
        assert.equal((await isOpen(page)).open, false, "the model picker opened the row");
        await page.keyboard.press("Escape");
        await page.locator("#pop.model-picker").waitFor({ state: "hidden" });
        await page.locator(`${codex} > .ag-handle`).click();
        await page.locator(".pop.row-menu").waitFor();
        assert.equal((await isOpen(page)).open, false, "the grip opened the row");
        await page.keyboard.press("Escape");
        await page.locator(".pop.row-menu").waitFor({ state: "detached" }).catch(() => page.locator(".pop.row-menu").waitFor({ state: "hidden" }));
        await page.locator(`${codex} > .ag-conn`).click();
        await page.locator("#modal .disconnect-ask").waitFor();
        assert.equal((await isOpen(page)).open, false, "the switch opened the row");
        await page.keyboard.press("Escape");
        await page.locator("#modal .disconnect-ask").waitFor({ state: "detached" });
        await quiet(page);
        assert.equal((await isOpen(page)).open, false);

        // the keyboard: the link, Enter
        await page.locator(`${codex} > .ag-link`).focus();
        await page.keyboard.press("Enter");
        await quiet(page);
        assert.deepEqual(await isOpen(page), { open: true, aria: "true" }, "Enter on the link didn't open it");
        await page.locator(`${codex} > .ag-link`).focus();
        await page.keyboard.press("Enter");
        await quiet(page);
        assert.deepEqual(await isOpen(page), { open: false, aria: "false" });

        // one not connected has nothing to open
        const g = await page.locator('.row.agent[data-id="gemini"] .name').boundingBox();
        await page.mouse.click(g.x + g.width / 2, g.y + g.height / 2);
        await quiet(page);
        assert.equal(await page.locator('.row.agent[data-id="gemini"] > .ag-exp').count(), 0);
        assert.equal(await page.locator(".row.agent.expanded").count(), 0);

        assert.equal(await scrolled(page), y, "a click moved the page");
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "the row runs past the window");
        assert.deepEqual(errors, []);
      });
    }
  }
}

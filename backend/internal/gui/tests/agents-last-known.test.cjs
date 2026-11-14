// Run with Node's test runner and Playwright on the module path; see README.md.
// #1519 (fatkun): after a start, or after light mode let the window go, the
// Agents and Providers pages waited ~2 s for uni-switch to read every agent's
// config again (seconds more with WSL agents on). The Agents page now draws
// the state kept from the last read at once and the fresh one replaces it:
// an agent leaves the list only once the fresh read says it is gone. The
// Providers page no longer waits on the fresh read. With nothing kept, the
// page waits as before.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const SLOW = 2000; // the fresh read

const field = (v) => ({ key: "model", label: "model", value: v, options: [{ value: v, label: v }] });
const agent = (id, name, v) => ({ id, name, path: `/test/${id}/${v}`, icon: "", fields: [field(v)] });
const lastState = {
  last: true,
  agents: [agent("codex", "Codex", "gpt-5.4"), agent("claude", "Claude Code", "opus")],
  profiles: [], settings: { lang: "en", theme: "dark" },
};
const freshState = {
  agents: [agent("codex", "Codex", "gpt-5.5")],
  profiles: [], settings: { lang: "en", theme: "dark" },
};

function serve(kept, read) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"en",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") {
      if (url.searchParams.get("last") === "1") return json(kept ? lastState : null);
      await new Promise((r) => setTimeout(r, SLOW));
      read.done = true;
      return json(freshState);
    }
    if (url.pathname === "/api/providers") return json({ providers: [{ id: "openai", name: "OpenAI", icon: "openai", preset: "openai", models: [], agents: [], key: { set: true, masked: "sk-…ab12" } }], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

async function open(engine, t, kept, view) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  t.after(() => browser.close());
  const page = await browser.newPage({ viewport: { width: 1000, height: 700 } });
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const read = { done: false }; // the fresh read is answered
  await page.route("**/*", serve(kept, read));
  await page.goto("http://uni-switch.test/" + (view ? "?view=" + view : ""));
  return { page, errors, read };
}

const rows = (page) => page.locator("#agents .row.agent:not(.ag-sk-row)");
const ids = (page) => rows(page).evaluateAll((els) => els.map((e) => e.dataset.id));
const value = (page, id) => page.locator(`#agents .row.agent[data-id="${id}"]`).getAttribute("title"); // its path, read with it

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: the Agents page draws the last-known agents at once`, async (t) => {
    const { page, errors, read } = await open(engine, t, true);
    await rows(page).first().waitFor();
    assert(!read.done, "the rows came only with the fresh read");
    assert.deepEqual(await ids(page), ["codex", "claude"], "the last-known agents, both");
    assert.match(await value(page, "codex"), /gpt-5\.4/);
    assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), "dark", "the settings that came with it are applied");
    // the fresh read: claude is gone, codex's model changed
    await page.waitForFunction(() => !document.querySelector('#agents .row.agent[data-id="claude"]'), null, { timeout: SLOW + 3000 });
    assert(read.done, "claude left only once the fresh read said so");
    assert.deepEqual(await ids(page), ["codex"]);
    assert.match(await value(page, "codex"), /gpt-5\.5/);
    assert.deepEqual(errors, []);
  });

  test(`${engine}: the Providers page doesn't wait on the fresh read`, async (t) => {
    const { page, errors, read } = await open(engine, t, true, "providers");
    await page.locator("#providers .row.provider:not(.pv-sk-row)").first().waitFor();
    assert(!read.done, "the providers came only with the fresh read");
    await page.waitForFunction(() => !document.querySelector('#agents .row.agent[data-id="claude"]'), null, { timeout: SLOW + 3000 });
    assert.equal(await page.locator("#providers .row.provider:not(.pv-sk-row)").count(), 1, "still drawn once the fresh read is in");
    assert.deepEqual(errors, []);
  });

  test(`${engine}: with nothing kept, the Agents page waits for the read`, async (t) => {
    const { page, errors, read } = await open(engine, t, false);
    await page.waitForTimeout(600);
    assert.equal(await rows(page).count(), 0, "no rows before the read");
    await rows(page).first().waitFor({ timeout: SLOW + 3000 });
    assert(read.done);
    assert.deepEqual(await ids(page), ["codex"]);
    assert.deepEqual(errors, []);
  });
}

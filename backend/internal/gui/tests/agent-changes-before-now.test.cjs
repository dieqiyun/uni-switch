// Run with Node's test runner and Playwright on the module path; see README.md.
// Zhenzhen (Pi on DeepSeek, omarchy): a connected agent's row says
// "Changed ~/.pi/agent/settings.json · models.json", and its diff showed
// every line uni-switch added red, "−" and struck through, with Pi's own
// `"providers": {}` tagged "value before connecting" — the direction
// disconnecting goes, so it read as uni-switch deleting Pi's settings. Under
// "Changed" the diff reads before → now: what uni-switch put in is green "+",
// a value it replaced is "−" old then "+" new, nothing struck through. The
// way back (− taken out, ↺ put back) stays in Disconnect and restore's
// dialog. The lines are diffLines' own output for Pi's two files; no
// backend, the API is faked here, and every language is drawn.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
// /api/agents/preview/pi: what disconnecting does, from diffLines
const changes = [
  {
    path: "~/.pi/agent/settings.json",
    lines: [
      { op: "~", text: '  "theme": "dark"', was: '  "theme": "dark",' },
      { op: "-", text: '  "defaultModel": "deepseek/deepseek-v4-pro",' },
      { op: "-", text: '  "defaultProvider": "uni-switch",' },
      { op: "-", text: '  "defaultThinkingLevel": "high"' },
    ],
  },
  {
    path: "~/.pi/agent/models.json",
    lines: [
      { op: "~", text: '  "providers": {}', was: '  "providers": {' },
      { op: "-", text: '    "uni-switch": {' },
      { op: "-", text: '      "baseUrl": "http://127.0.0.1:3425/v1",' },
      { op: "-", text: '      "api": "openai-completions",' },
    ],
  },
  { path: "~/.pi/agent/uni-switch.json", removed: true },
];

function serve(lang) {
  const state = {
    agents: [{
      id: "pi", name: "Pi", icon: "generic", wired: true, path: "~/.pi/agent/settings.json",
      fields: [{ key: "model", label: "model", value: "uni-switch/deepseek/deepseek-v4-pro", options: [{ value: "uni-switch/deepseek/deepseek-v4-pro", label: "DeepSeek V4 Pro", ref: "deepseek/deepseek-v4-pro" }] }],
    }],
    profiles: [], settings: { lang, theme: "light" },
  };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = ${JSON.stringify({ lang, theme: "light", web: false })};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/settings") return json(state.settings);
    if (url.pathname === "/api/agents/preview/pi") return json({ changes });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

// each line as drawn: its sign, its text, whether it is struck through,
// its colour and its tags
const drawn = (box) => box.evaluate((b) => [...b.querySelectorAll(".ag-diff-file")].map((f) => ({
  path: f.querySelector(".ag-diff-path span").textContent,
  tags: [...f.querySelectorAll(".ag-diff-path .ag-tag")].map((x) => x.textContent),
  lines: [...f.querySelectorAll(".ag-diff-line")].map((l) => ({
    op: l.querySelector(".op").textContent,
    text: l.querySelector("code").textContent,
    struck: getComputedStyle(l.querySelector("code .t")).textDecorationLine.includes("line-through"),
    color: getComputedStyle(l.querySelector("code")).color,
    tags: [...l.querySelectorAll(".ag-tag")].map((x) => x.textContent),
  })),
})));

const LANGS = { en: ["Created", "Deleted", "value before connecting"], zh: ["新建", "删除", "接入前的值"], "zh-TW": ["新增", "刪除"], ja: ["作成", "削除"], de: ["Erstellt", "Gelöscht"] };

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(`${engine}: a connected agent's changes read before → now, and the way back only in Disconnect`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const [lang, [created, deleted, before]] of Object.entries(LANGS)) {
      for (const width of [890, 420]) {
        const context = await browser.newContext({ viewport: { width, height: 900 }, reducedMotion: "reduce" });
        const page = await context.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        page.on("console", (m) => { if (m.type() === "error") errors.push(m.text()); });
        await page.route("**/*", serve(lang));
        await page.goto("http://uni-switch.test/?view=agents");
        const where = `${engine} ${lang} ${width}`;
        const row = page.locator('.row.agent[data-id="pi"]');
        if (!(await row.locator(".ag-exp").count())) await row.locator(".ag-link").click();
        const view = row.locator(".ag-foot .ag-quiet").first();
        await view.waitFor();
        await page.waitForFunction(() => !document.querySelector('.row.agent[data-id="pi"] .ag-foot .ag-quiet').hidden);
        await view.click();
        const box = row.locator(".ag-diff-box");
        await box.locator(".ag-diff-file").last().waitFor();
        const files = await drawn(box);
        const green = await page.evaluate(() => { const s = document.createElement("span"); s.style.color = "var(--green)"; document.body.append(s); const c = getComputedStyle(s).color; s.remove(); return c; });
        const red = await page.evaluate(() => { const s = document.createElement("span"); s.style.color = "var(--red)"; document.body.append(s); const c = getComputedStyle(s).color; s.remove(); return c; });
        const signs = (f) => f.lines.map((l) => l.op + " " + l.text.trim());
        assert.deepEqual(signs(files[0]), [
          '− "theme": "dark"', '+ "theme": "dark",',
          '+ "defaultModel": "deepseek/deepseek-v4-pro",', '+ "defaultProvider": "uni-switch",', '+ "defaultThinkingLevel": "high"',
        ], `${where}: settings.json reads before → now`);
        assert.deepEqual(signs(files[1]), [
          '− "providers": {}', '+ "providers": {', '+ "uni-switch": {',
          '+ "baseUrl": "http://127.0.0.1:3425/v1",', '+ "api": "openai-completions",',
        ], `${where}: models.json's uni-switch block is put in, Pi's empty providers replaced`);
        for (const f of files.slice(0, 2)) for (const l of f.lines) {
          assert.equal(l.struck, false, `${where}: nothing is struck through: ${l.text}`);
          assert.deepEqual(l.tags, [], `${where}: no line is tagged as the way back: ${l.text}`);
          assert.equal(l.color, l.op === "+" ? green : red, `${where}: + is green, − is red: ${l.text}`);
        }
        assert.deepEqual(files[2].tags, [created], `${where}: a file uni-switch made is Created, not Deleted`);
        assert.ok(!files.some((f) => f.lines.some((l) => l.op === "↺")), `${where}: no ↺ under Changed`);

        // Disconnect and restore's dialog keeps the way back
        await row.locator(".ag-foot .ag-quiet.red").click();
        const ask = page.locator(".editor.disconnect-ask");
        await ask.locator(".ag-diff-file").last().waitFor();
        const back = await drawn(ask);
        assert.deepEqual(back[0].lines.map((l) => l.op), ["↺", "−", "−", "−"], `${where}: the dialog shows what disconnecting does`);
        assert.ok(back[0].lines.slice(1).every((l) => l.struck), `${where}: the lines disconnecting takes out are struck`);
        if (before) assert.deepEqual(back[1].lines[0].tags, [before], `${where}: the value put back is tagged`);
        assert.deepEqual(back[2].tags, [deleted], `${where}: disconnecting deletes the file uni-switch made`);
        assert.deepEqual(errors, [], `${where}: no runtime or console errors`);
        await context.close();
      }
    }
  });
}

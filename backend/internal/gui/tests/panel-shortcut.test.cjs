// Run with Node's test runner and Playwright on the module path; see README.md.
// ben hanks on Discord: a key combination that opens the tray's quick panel
// from any app. Settings › General has one row for it, none set by default,
// in both engines and every language, on a Mac and off it, with the API
// faked:
// - the field listens once clicked; a key with no Ctrl, Alt or Cmd/Win is
//   not taken, and the window's own keys (text size) don't fire meanwhile;
// - a combination is sent as Wails names it, and shown in the system's way
//   (⇧⌘K on a Mac, Ctrl+Alt+K elsewhere); Esc stops listening, sending none;
// - one the system refuses says so in the status line, the saved one kept;
// - the clear button takes it away; nothing the row does scrolls the page;
// - on Wayland the field is off and the row says the command to bind;
// - a browser tab (uni-switch web) has no row; a narrow window keeps it inside.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const words = {
  en: { name: "Quick panel shortcut", record: "Record shortcut", type: "Type shortcut", taken: "Another app already uses this shortcut" },
  zh: { name: "快捷面板快捷键", record: "录制快捷键", type: "请按下快捷键", taken: "其他应用已占用这个快捷键" },
  "zh-TW": { name: "快捷面板快速鍵", record: "錄製快速鍵", type: "請按下快速鍵", taken: "其他應用程式已占用這個快速鍵" },
  ja: { name: "クイックパネルのショートカット", record: "ショートカットを記録", type: "キーを押してください", taken: "このショートカットは他のアプリが使用中です" },
  de: { name: "Tastenkürzel für das Schnellpanel", record: "Kürzel aufnehmen", type: "Kürzel drücken", taken: "Eine andere App nutzt dieses Kürzel bereits" },
};

function serve(lang, web, posted, st) {
  const settings = () => ({ lang, theme: "light", tray: "panel", searchVendors: [], searchAPIs: [], panelKeys: { can: true }, ...st });
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data, status = 200) => r.fulfill({ status, json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:${web}};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: settings() });
    if (url.pathname === "/api/settings/panel-shortcut") {
      const { shortcut } = JSON.parse(r.request().postData());
      posted.push(shortcut);
      // the fake system holds Ctrl+Alt+J / ⌃⌥J for another app
      if (/^Ctrl\+(Alt|Option)\+J$/.test(shortcut)) return json({ error: "Another app already uses this shortcut" }, 400);
      st.panelShortcut = shortcut;
      return json(settings());
    }
    if (url.pathname.startsWith("/api/settings/")) { posted.push(url.pathname); return json(settings()); }
    if (url.pathname === "/api/settings") return json(settings());
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname === "/api/groups") return json({ models: [], groups: [], pools: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await r.fulfill({ body: await fs.readFile(file), contentType }); } catch { await r.fulfill({ status: 404, body: "" }); }
  };
}

const langs = Object.keys(words);
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  langs.forEach((lang) => {
    for (const platform of ["MacIntel", "Win32"]) {
      const mac = platform === "MacIntel";
      test(`${engine} ${lang} ${mac ? "mac" : "windows"}: the quick panel's shortcut is recorded, refused, cleared`, async (t) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const w = words[lang];
        const open = async (st, { web = false, width = 1000 } = {}) => {
          const page = await (await browser.newContext({ viewport: { width, height: 700 }, reducedMotion: "reduce" })).newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          const errors = [];
          page.on("pageerror", (e) => errors.push(e.message));
          await page.addInitScript((p) => Object.defineProperty(navigator, "platform", { get: () => p }), platform);
          const posted = [];
          await page.route("**/*", serve(lang, web, posted, st));
          await watchSettingsReads(page);
          await page.goto("http://uni-switch.test/?view=settings&tab=general");
          await settingsDrawn(page);
          return { page, posted, errors };
        };
        const { page, posted, errors } = await open({});
        const rec = page.locator("#panelKey .key-rec"), clear = page.locator("#panelKey .key-clear");
        assert.equal(await page.locator("#panelKeyRow .name").textContent(), w.name);
        assert.equal(await rec.textContent(), w.record, "none is set by default");
        assert.equal(await clear.isHidden(), true);
        assert.equal(await page.locator("#panelKeyRow .sub").isHidden(), true);
        await page.evaluate(() => Promise.all(document.getAnimations().map((a) => a.finished.catch(() => {}))));
        await page.locator("#panelKeyRow").hover();
        await page.mouse.wheel(0, 120);
        await page.waitForTimeout(300);
                // where the row is on the screen: nothing the row does moves it
        const top = () => page.evaluate(() => { const v = document.querySelector("#view-settings"); return `${Math.round(document.querySelector("#panelKeyRow").getBoundingClientRect().top)} (${v.scrollTop}/${v.scrollHeight - v.clientHeight})`; });
        const at = await top();

        // listening: a key with no Ctrl, Alt or Cmd isn't taken, nor are
        // the window's text size keys run
        await rec.click();
        assert.equal(await rec.textContent(), w.type);
        await page.keyboard.press("Shift+K");
        await page.keyboard.press((mac ? "Meta" : "Control") + "+Equal");
        await page.keyboard.press("Digit5");
        await page.waitForTimeout(100);
        assert.deepEqual(posted, [], "a key typing needs was sent, or a window key ran");
        assert.equal(await rec.textContent(), w.type);
        // Esc stops it, sending none
        await page.keyboard.press("Escape");
        assert.equal(await rec.textContent(), w.record);
        assert.equal(await page.locator("#view-settings").isVisible(), true, "Esc left Settings");
        assert.deepEqual(posted, []);

        // a combination, sent as Wails names it and shown the system's way
        await rec.click();
        await page.keyboard.press(mac ? "Meta+Shift+KeyK" : "Control+Alt+KeyK");
        const want = mac ? "Shift+Cmd+K" : "Ctrl+Alt+K";
        for (let i = 0; i < 50 && posted.length < 1; i++) await page.waitForTimeout(50);
        assert.deepEqual(posted, [want]);
        await page.waitForFunction((s) => document.querySelector("#panelKey .key-rec").textContent === s, mac ? "⇧⌘K" : "Ctrl+Alt+K");
        assert.equal(await clear.isVisible(), true);

        // one the system refuses: said, the saved one kept
        await page.waitForFunction(() => prefsBusy === 0);
        await rec.click();
        await page.keyboard.press("Control+Alt+KeyJ");
        for (let i = 0; i < 50 && posted.length < 2; i++) await page.waitForTimeout(50);
        assert.deepEqual(posted, [want, mac ? "Ctrl+Option+J" : "Ctrl+Alt+J"]);
        await page.waitForFunction((s) => document.querySelector("#status")?.textContent.includes(s), w.taken);
        await page.waitForFunction((s) => document.querySelector("#panelKey .key-rec").textContent === s, mac ? "⇧⌘K" : "Ctrl+Alt+K");

        // clicking away stops listening
        await rec.click();
        await page.locator("#panelKeyRow .name").click();
        await page.waitForFunction((s) => document.querySelector("#panelKey .key-rec").textContent === s, mac ? "⇧⌘K" : "Ctrl+Alt+K");

        // cleared
        await page.waitForFunction(() => prefsBusy === 0);
        await clear.click();
        for (let i = 0; i < 50 && posted.length < 3; i++) await page.waitForTimeout(50);
        assert.equal(posted[2], "");
        await page.waitForFunction((s) => document.querySelector("#panelKey .key-rec").textContent === s, w.record);
        assert.equal(await clear.isHidden(), true);
        assert.equal(await top(), at, "the row moved on the screen");
        assert.deepEqual(errors, []);

        // the saved one the system refused as uni-switch started is said
        const taken = await open({ panelShortcut: "Ctrl+Alt+K", panelKeys: { can: true, taken: "Ctrl+Alt+K" } });
        assert.equal(await taken.page.locator("#panelKeyRow .sub").textContent(), w.taken);

        // Wayland: off, with the command to bind
        const way = await open({ panelKeys: { can: false, command: "/usr/bin/uni-switch panel" } });
        assert.equal(await way.page.locator("#panelKey .key-rec").isDisabled(), true);
        const sub = way.page.locator("#panelKeyRow .sub");
        assert.equal(await sub.isVisible(), true);
        assert.ok((await sub.textContent()).includes("/usr/bin/uni-switch panel"), await sub.textContent());
        assert.ok(lang === "en" || !(await sub.textContent()).includes("keyboard settings"), "the Wayland line isn't translated");
        assert.deepEqual(way.errors, []);

        // a browser tab has no row
        const tab = await open({}, { web: true });
        assert.equal(await tab.page.locator("#panelKeyRow").isHidden(), true);

        // a narrow window keeps the field inside its row
        const narrow = await open({ panelShortcut: mac ? "Ctrl+Option+Shift+Cmd+K" : "Ctrl+Alt+Shift+Super+K" }, { width: 420 });
        const [row, field, x] = await narrow.page.evaluate(() => ["#panelKeyRow", "#panelKey .key-rec", "#panelKey .key-clear"]
          .map((s) => { const b = document.querySelector(s).getBoundingClientRect(); return { l: b.left, r: b.right }; }));
        assert.ok(field.l >= row.l && x.r <= row.r + 0.5, `${JSON.stringify({ row, field, x })}`);
        assert.equal(await narrow.page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, "the page scrolls sideways");
        assert.deepEqual(narrow.errors, []);
      });
    }
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// Settings › About › Command line (PAMI on Discord): whether `uni-switch` in a
// terminal opened now runs this app, said for each shell (a dot, no left
// border), and Add to PATH, which posts /api/cli and shows the answer. On
// Windows a program not named uni-switch.exe (the site's portable download,
// #942) is said to get a uni-switch.cmd that runs it, and a uni-switch.com that
// runs the app is said to run it (#3737). A
// shell whose PATH lacks the folder has its own button naming its profile,
// and only that button posts that shell. On Windows PowerShell and cmd are
// listed, with no profile to write; a translocated app gets no button. No
// click moves the page, nothing is a native select. English and Chinese; no
// backend, the API is faked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn, openPart } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");

const words = {
  en: { row: "Command line", add: "Add to PATH", none: "can't find the uni-switch command", ours: "runs this app", fish: "Add to ~/.config/fish/config.fish",
    added: "open a new terminal window", ps: "PowerShell and cmd take up a change in new windows", moved: "moved to Applications",
    shimTitle: "writes uni-switch.cmd there, which runs uni-switch-windows-amd64.exe", shimAdded: "uni-switch.cmd beside uni-switch-windows-amd64.exe runs it as uni-switch", shimRuns: "which runs uni-switch-windows-amd64.exe", comRuns: "which runs uni-switch.exe" },
  zh: { row: "命令行", add: "添加到 PATH", none: "新终端里找不到 uni-switch 命令", ours: "运行这个 app", fish: "添加到 ~/.config/fish/config.fish",
    added: "新开一个终端窗口", ps: "PowerShell 和 cmd 在新窗口里生效", moved: "移到「应用程序」",
    shimTitle: "写一个 uni-switch.cmd 来运行 uni-switch-windows-amd64.exe", shimAdded: "uni-switch-windows-amd64.exe 旁边的 uni-switch.cmd", shimRuns: "它运行 uni-switch-windows-amd64.exe", comRuns: "它运行 uni-switch.exe" },
};

const EXE = "/Applications/uni-switch.app/Contents/MacOS/uni-switch";

function view(ctl) {
  if (ctl.windows) {
    // the site's portable download (#942) runs as uni-switch through the
    // uni-switch.cmd Add writes beside it
    const dir = ctl.portable ? "D:\\portable_app" : "C:\\Users\\u\\AppData\\Local\\uni-switch";
    const exe = dir + (ctl.portable ? "\\uni-switch-windows-amd64.exe" : "\\uni-switch.exe"), shim = ctl.portable ? dir + "\\uni-switch.cmd" : "";
    // a release build runs as uni-switch through the uni-switch.com it writes
    // beside the app, which a shell waits for (#3737)
    const cmd = ctl.added ? shim || (ctl.com ? dir + "\\uni-switch.com" : exe) : "";
    return { exe, ours: ctl.added, command: cmd, dir, windows: true, shim,
      shells: ["PowerShell", "cmd"].map((name, i) => ({ name, default: i === 0, known: true, ours: ctl.added, command: cmd })) };
  }
  const link = "/Users/u/.local/bin/uni-switch";
  return {
    exe: EXE, ours: ctl.added, command: ctl.added ? link : "", dir: ctl.added ? "" : "/Users/u/.local/bin", stuck: ctl.stuck || "",
    shells: [
      { name: "zsh", default: true, known: true, ours: ctl.added, command: ctl.added ? link : "", profile: "~/.zshrc", hasDir: !ctl.added },
      { name: "fish", known: true, ours: ctl.fish, command: ctl.fish ? link : "/opt/old/uni-switch", profile: "~/.config/fish/config.fish" },
    ],
  };
}

function server(lang, ctl) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { theme: "light", lang, version: "0.1.900", dir: "~/.config/uni-switch", gateway: "http://127.0.0.1:3425", lanURLs: [], fx: { rate: 7.2 } } });
    if (url.pathname === "/api/cli") {
      if (req.method() === "POST") {
        const body = req.postDataJSON();
        ctl.posts.push(body);
        if (body.shell === "fish") ctl.fish = true;
        else if (!body.shell) ctl.added = true;
      } else ctl.gets++;
      return json(view(ctl));
    }
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

async function showRow(page, row) {
  await page.locator("#view-settings").hover();
  for (let i = 0; i < 40; i++) {
    const box = await row.boundingBox();
    const v = await page.locator("#view-settings").boundingBox();
    if (box && v && box.y >= v.y + 4 && box.y + box.height <= v.y + v.height - 4) break;
    await page.mouse.wheel(0, 180);
    await page.waitForTimeout(16);
  }
  await page.waitForTimeout(250);
}
const scrolls = (page) => page.evaluate(() => [window.scrollY, document.scrollingElement.scrollTop, ...[...document.querySelectorAll(".view")].map((v) => v.scrollTop)].join(","));

async function open(browser, lang, ctl, errors) {
  const page = await (await browser.newContext({ viewport: { width: 900, height: 760 }, reducedMotion: "reduce" })).newPage();
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/*", server(lang, ctl));
  await watchSettingsReads(page);
  await page.goto("http://uni-switch.test/?view=settings");
  await settingsDrawn(page); // rows found are the ones that stay (#1659)
  await openPart(page, "about");
  await page.locator("#setPage-about").waitFor({ state: "visible" });
  const row = page.locator("#about .row.pref.cli-row");
  await row.locator(".cli-shell").first().waitFor();
  return { page, row };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": Command line says what `uni-switch` runs, and adds it", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh"]) {
      const w = words[lang];
      await t.test(lang + ": Mac, Add to PATH then fish's own profile", async () => {
        const ctl = { posts: [], gets: 0 };
        const errors = [];
        const { page, row } = await open(browser, lang, ctl, errors);
        assert.equal((await row.locator(".name").innerText()).trim(), w.row);
        await row.locator(".sub", { hasText: w.none }).waitFor();
        const zsh = row.locator('.cli-shell[data-shell="zsh"]'), fish = row.locator('.cli-shell[data-shell="fish"]');
        assert.equal(await zsh.evaluate((e) => e.classList.contains("on")), false);
        assert.match(await fish.innerText(), /\/opt\/old\/uni-switch/, "fish names the other uni-switch it runs");
        assert.equal(await row.locator("select").count(), 0, "no native select");
        assert.equal(await zsh.locator("button").count(), 0, "zsh, whose PATH has the folder, is reached by Add to PATH");
        assert.equal(await fish.locator("button", { hasText: w.fish }).count(), 1, "fish, whose PATH hasn't, has its profile's button");
        for (const sel of [".cli-shell", ".cli-dot", ".row.pref.cli-row"]) {
          const widths = await page.locator(sel).evaluateAll((es) => es.map((e) => parseFloat(getComputedStyle(e).borderLeftWidth) || 0));
          assert.ok(widths.every((x) => x === 0), sel + " has no left border");
        }

        const add = row.locator("button.cli-add");
        assert.equal((await add.innerText()).trim(), w.add);
        await showRow(page, row);
        let before = await scrolls(page);
        await add.click();
        await row.locator(".sub", { hasText: "/Users/u/.local/bin/uni-switch" }).waitFor();
        await zsh.locator(".cli-state", { hasText: w.ours }).waitFor();
        assert.deepEqual(ctl.posts, [{}], "Add to PATH posts no shell");
        assert.equal(await zsh.evaluate((e) => e.classList.contains("on")), true);
        assert.ok(await add.isHidden(), "Add is gone once it runs this app");
        assert.match(await page.locator("#status").innerText(), new RegExp(w.added));
        assert.equal(await scrolls(page), before, "the click must not scroll the page");
        assert.equal(await zsh.locator("button").count(), 0, "zsh, which runs it, has no profile button");

        // fish's own button names its profile and posts fish alone
        const fb = fish.locator("button", { hasText: w.fish });
        before = await scrolls(page);
        await fb.click();
        await fish.locator(".cli-state", { hasText: w.ours }).waitFor();
        assert.deepEqual(ctl.posts, [{}, { shell: "fish" }]);
        assert.equal(await scrolls(page), before, "the click must not scroll the page");
        assert.deepEqual(errors, []);
        await page.context().close();
      });

      await t.test(lang + ": Windows lists PowerShell and cmd, no profile", async () => {
        const ctl = { posts: [], gets: 0, windows: true };
        const errors = [];
        const { page, row } = await open(browser, lang, ctl, errors);
        assert.deepEqual(await row.locator(".cli-shell").evaluateAll((es) => es.map((e) => e.dataset.shell)), ["PowerShell", "cmd"]);
        assert.equal(await row.locator(".cli-shell button").count(), 0);
        assert.match(await row.locator(".cli-note").innerText(), new RegExp(w.ps));
        await row.locator("button.cli-add").click();
        await row.locator('.cli-shell.on[data-shell="cmd"]').waitFor();
        assert.deepEqual(ctl.posts, [{}]);
        assert.deepEqual(errors, []);
        await page.context().close();
      });

      await t.test(lang + ": Windows' portable download gets a uni-switch.cmd, and says so (#942)", async () => {
        const ctl = { posts: [], gets: 0, windows: true, portable: true };
        const errors = [];
        const { page, row } = await open(browser, lang, ctl, errors);
        const add = row.locator("button.cli-add");
        assert.match(await add.getAttribute("title"), new RegExp(w.shimTitle));
        await add.click();
        await row.locator(".sub", { hasText: "D:\\portable_app\\uni-switch.cmd" }).waitFor();
        assert.match(await row.locator(".sub").innerText(), new RegExp(w.shimRuns));
        assert.match(await page.locator("#status").innerText(), new RegExp(w.shimAdded));
        assert.deepEqual(ctl.posts, [{}]);
        assert.deepEqual(errors, []);
        await page.context().close();
      });

      await t.test(lang + ": Windows' uni-switch.com is said to run the app (#3737)", async () => {
        const ctl = { posts: [], gets: 0, windows: true, com: true };
        const errors = [];
        const { page, row } = await open(browser, lang, ctl, errors);
        await row.locator("button.cli-add").click();
        await row.locator(".sub", { hasText: "AppData\\Local\\uni-switch\\uni-switch.com" }).waitFor();
        assert.match(await row.locator(".sub").innerText(), new RegExp(w.comRuns));
        assert.deepEqual(errors, []);
        await page.context().close();
      });

      await t.test(lang + ": a translocated app gets no button", async () => {
        const ctl = { posts: [], gets: 0, stuck: "translocated" };
        const errors = [];
        const { page, row } = await open(browser, lang, ctl, errors);
        await row.locator(".sub", { hasText: w.moved }).waitFor();
        assert.ok(await row.locator("button.cli-add").isHidden());
        assert.equal(await row.locator(".cli-shell button").count(), 0);
        const missing = await page.evaluate(() => {
          const src = String(renderCLI);
          const keys = [...src.matchAll(/t\("((?:[^"\\]|\\.)*)"/g)].map((m) => JSON.parse('"' + m[1] + '"'));
          return keys.filter((k) => !I18N.zh[k] || !I18N.ja[k] || !I18N.de[k]);
        });
        assert.deepEqual(missing, [], "every string has zh, ja and de");
        assert.deepEqual(errors, []);
        await page.context().close();
      });
    }
  });
}

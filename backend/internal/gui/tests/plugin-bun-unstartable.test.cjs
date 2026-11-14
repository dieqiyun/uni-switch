// Run with Node's test runner and Playwright on the module path; see README.md.
// A plugin's install on a machine whose Windows or processor can't start
// Bun at all (zgcwkj on Discord: bun add … exit status 0xc0000139 on an
// older Windows) says in the reader's language why, and what to do,
// instead of bun's exit status: in the desktop window and in uni-switch web.
// No backend: the API is faked here, and nothing is ever installed.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const PKG = "the retired opencode-raccoon-auth";
const listings = [{ package: PKG, name: "Raccoon", icon: "raccoon", providers: ["raccoon"], community: true,
  summary: { en: "Your Raccoon plan." }, npm: { version: "0.1.3", weekly: 10, license: "MIT" } }];

// what the server answers, its English message as plugin.BunUnstartableError says it
const ANSWER = {
  windows: { error: "bun add " + PKG + "@latest: Bun can't start on this version of Windows: … (C:\\uni-switch\\cache\\bun\\1.4.2\\uni-switch-bun.exe exited with 0xc0000139, entry point not found) …", code: "bun_unstartable", bun: "C:\\uni-switch\\cache\\bun\\1.4.2\\uni-switch-bun.exe", bunWhy: "windows", said: "" },
  cpu: { error: "bun add " + PKG + "@latest: Bun can't start on this computer's processor: …", code: "bun_unstartable", bun: "/root/.cache/uni-switch/bun/1.4.2/uni-switch-bun", bunWhy: "cpu", said: "" },
};

// said is the i18n.js key the server chose (bunSaid in api.go)
ANSWER.windows.said = "Bun, which runs plugins, can't start on this version of Windows: it needs Windows 10 version 1809 or later, or Windows Server 2019 or later. Update Windows to use plugins, or set UNI_SWITCH_BUN to a bun that runs here.";
ANSWER.cpu.said = "Bun, which runs plugins, can't start on this computer's processor: it needs an x64 processor with SSE4.2 (Intel or AMD from 2008 on; in a virtual machine, a CPU type that passes it through). Run uni-switch on such a processor to use plugins, or set UNI_SWITCH_BUN to a bun that runs here.";

function server(lang, web, why, asked) {
  const state = { bun: true, bunVersion: "1.4.2", plugins: [] };
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:${web}};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/plugins/market") return json({ listings, state });
    if (url.pathname === "/api/plugins") return json(state);
    if (url.pathname === "/api/plugins/listings") return json({ listings });
    if (url.pathname === "/api/plugins/add") {
      asked.push(req.postDataJSON());
      return route.fulfill({ status: 400, json: ANSWER[why] });
    }
    if (url.pathname.startsWith("/api/")) return json({});
    if (!/^\/[\w./-]*$/.test(url.pathname) || url.host !== "uni-switch.test") return route.fulfill({ status: 404, body: "" });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const SAID = {
  en: {
    install: "Install",
    windows: "Bun, which runs plugins, can't start on this version of Windows: it needs Windows 10 version 1809 or later, or Windows Server 2019 or later. Update Windows to use plugins, or set UNI_SWITCH_BUN to a bun that runs here.",
    cpu: "Bun, which runs plugins, can't start on this computer's processor: it needs an x64 processor with SSE4.2 (Intel or AMD from 2008 on; in a virtual machine, a CPU type that passes it through). Run uni-switch on such a processor to use plugins, or set UNI_SWITCH_BUN to a bun that runs here.",
  },
  zh: { install: "安装", windows: /^运行插件的 Bun 无法在这个版本的 Windows 上启动：它需要 Windows 10 1809 或更新版本/, cpu: /^运行插件的 Bun 无法在这台电脑的处理器上启动：它需要支持 SSE4\.2 的 x64 处理器/ },
  "zh-TW": { install: "安裝", windows: /^執行外掛的 Bun 無法在這個版本的 Windows 上啟動/, cpu: /^執行外掛的 Bun 無法在這台電腦的處理器上啟動/ },
  ja: { install: "インストール", windows: /^プラグインを動かす Bun は、このバージョンの Windows では起動できません/, cpu: /^プラグインを動かす Bun は、このコンピューターのプロセッサでは起動できません/ },
  de: { install: "Installieren", windows: /^Bun, das die Erweiterungen ausführt, kann auf dieser Windows-Version nicht starten/, cpu: /^Bun, das die Erweiterungen ausführt, kann auf dem Prozessor dieses Computers nicht starten/ },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": an install on a Bun this machine can't start says why", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    for (const [lang, want] of Object.entries(SAID)) {
      for (const [why, web] of [["windows", false], ["cpu", true], ["windows", true]]) {
        await t.test(`${lang} ${why} ${web ? "web" : "window"}`, async () => {
          const asked = [];
          const ctx = await browser.newContext({ viewport: { width: 980, height: 760 } });
          const page = await ctx.newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", server(lang, web, why, asked));
          await page.goto("http://uni-switch.test/?view=plugins");
          const act = page.locator(`#view-plugins .pm-card[data-pkg="${PKG}"] .pm-act`);
          await act.filter({ hasText: want.install }).waitFor();
          await act.click();
          await page.waitForFunction(() => /Bun/.test(document.querySelector("#status")?.textContent || ""));
          const said = (await page.locator("#status").textContent()).trim();
          if (typeof want[why] === "string") assert.equal(said, want[why]);
          else assert.match(said, want[why]);
          // bun's exit status and the server's English aren't what's shown
          assert.doesNotMatch(said, /0xc0000139|exit status|bun add/);
          assert.deepEqual(asked, [{ spec: PKG }]);
          await ctx.close();
        });
      }
    }
    assert.deepEqual(errors, []);
  });
}

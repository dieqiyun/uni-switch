// Run with Node's test runner and Playwright on the module path; see README.md.
// A repository tagged uni-switch-plugin that is gateway hooks alone
// (package.json's uni-switch.hooks and no entry of its own; MengXiangxi,
// #3821) is in "Unofficial, on GitHub" as an installed one's row has it:
// the gateway's chip saying Request hook, never Provider, and picked out
// under Middleware, as it runs in the gateway too. The repositories are
// seven of /api/plugins/github's real answer of 2026-10-27, from a uni-switch
// built at this change: two providers, three middleware and two of hooks
// alone (past six, the section has its kinds to pick from).
// The API is faked here and no request leaves the page. English, Chinese,
// Traditional Chinese, Japanese and German, 980px and 420px.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

const REPOS = [
  { "spec": "uni-switch-gemini-web", "repo": "BUKOWSKIREAL/uni-switch-gemini-web", "url": "https://github.com/BUKOWSKIREAL/uni-switch-gemini-web", "owner": "BUKOWSKIREAL", "ownerAvatar": "https://avatars.githubusercontent.com/u/233685554?v=4", "description": "Gemini Web provider for uni-switch with a local backend and experimental Argon support", "stars": 101, "license": "MIT", "pushed": "2026-10-24T05:28:43Z", "package": "uni-switch-gemini-web", "version": "0.5.6" },
  { "spec": "github:mu-zi-lee/uni-switch-doubao-translate", "repo": "mu-zi-lee/uni-switch-doubao-translate", "url": "https://github.com/mu-zi-lee/uni-switch-doubao-translate", "owner": "mu-zi-lee", "ownerAvatar": "https://avatars.githubusercontent.com/u/75201518?v=4", "description": "uni-switch 豆包翻译插件：通过 Cookie 直连豆包 AI、火山及微软翻译，无需 Docker 或额外 API Key。｜uni-switch plugin for Doubao web translation: access Doubao AI, Volcengine and Microsoft Translator via Cookie, without Docker or an extra API key.", "stars": 9, "license": "MIT", "pushed": "2026-10-09T19:22:04Z", "package": "uni-switch-doubao-translate", "version": "0.1.2" },
  { "spec": "github:euanguo/uni-switch-middleware-image-slim", "repo": "euanguo/uni-switch-middleware-image-slim", "url": "https://github.com/euanguo/uni-switch-middleware-image-slim", "owner": "euanguo", "ownerAvatar": "https://avatars.githubusercontent.com/u/82928935?v=4", "description": "uni-switch gateway middleware: keep the newest images and replace older inline images with a text placeholder — agents resend every screenshot on every turn otherwise", "stars": 2, "license": "MIT", "pushed": "2026-10-10T16:41:27Z", "package": "uni-switch-middleware-image-slim", "version": "0.2.0", "kind": "middleware" },
  { "spec": "github:ezdemo/uni-switch-haiku55-thinking", "repo": "ezdemo/uni-switch-haiku55-thinking", "url": "https://github.com/ezdemo/uni-switch-haiku55-thinking", "owner": "ezdemo", "ownerAvatar": "https://avatars.githubusercontent.com/u/41895036?v=4", "description": "uni-switch Haiku 5.5 thinking middleware with a verified OpenDesign compatibility workaround", "stars": 1, "license": "MIT", "pushed": "2026-10-12T15:07:50Z", "package": "uni-switch-opendesign-compatible-fix", "version": "0.1.0", "kind": "middleware" },
  { "spec": "github:MengXiangxi/uni-switch-openviking", "repo": "MengXiangxi/uni-switch-openviking", "url": "https://github.com/MengXiangxi/uni-switch-openviking", "owner": "MengXiangxi", "ownerAvatar": "https://avatars.githubusercontent.com/u/10251566?v=4", "description": "OpenViking memory recall and opt-in capture through uni-switch async gateway hooks", "stars": 1, "license": "MIT", "pushed": "2026-10-27T02:43:30Z", "package": "uni-switch-openviking", "version": "0.1.0", "kind": "hooks" },
  { "spec": "github:sdloghub/uni-switch-session-patcher", "repo": "sdloghub/uni-switch-session-patcher", "url": "https://github.com/sdloghub/uni-switch-session-patcher", "owner": "sdloghub", "ownerAvatar": "https://avatars.githubusercontent.com/u/52274876?v=4", "description": "uni-switch gateway middleware: opt-in outbound history filtering, optional reasoning cleanup and prompt append", "stars": 0, "pushed": "2026-10-25T07:15:36Z", "package": "uni-switch-session-patcher", "version": "0.1.1", "kind": "middleware" },
  { "spec": "github:IvanXxxxyyuffff/uni-switch-system-prompt-file", "repo": "IvanXxxxyyuffff/uni-switch-system-prompt-file", "url": "https://github.com/IvanXxxxyyuffff/uni-switch-system-prompt-file", "owner": "IvanXxxxyyuffff", "ownerAvatar": "https://avatars.githubusercontent.com/u/128815537?v=4", "description": "uni-switch plugin: a system prompt (persona) loaded from a local .md / .txt file and injected into every agent request. macOS + Windows.", "stars": 0, "license": "MIT", "pushed": "2026-10-26T15:01:39Z", "package": "uni-switch-system-prompt-file", "version": "0.3.0", "kind": "hooks" },
];
const name = (r) => r.repo.split("/")[1];

function server(lang, fetched) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname === "/api/plugins") return json({ bun: true, bunVersion: "1.4.2", plugins: [], picker: false, movable: [] });
    if (url.pathname === "/api/plugins/listings") return json({ listings: [] });
    if (url.pathname === "/api/plugins/npm") return json({ npm: {} });
    if (url.pathname === "/api/plugins/github") return json({ repos: REPOS, topic: "uni-switch-plugin" });
    if (url.pathname === "/api/plugins/search") return json({ hits: [] });
    if (url.pathname === "/api/plugins/updates") return json({ waiting: [], updated: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    if (url.host !== "uni-switch.test" || !/^\/[\w./-]*$/.test(url.pathname)) { fetched.push(url.href); return route.fulfill({ status: 404, body: "" }); }
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a repository of gateway hooks alone is a request hook", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) for (const width of [980, 420]) {
      await t.test(`${lang} ${width}px`, async () => {
        const ctx = await browser.newContext({ viewport: { width, height: 860 } });
        const page = await ctx.newPage();
        page.setDefaultTimeout(6000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], fetched = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, fetched));
        await page.addInitScript(() => { try { localStorage.setItem("uni-switch.pluginTab", "discover"); } catch {} });
        try {
          await page.goto("http://uni-switch.test/?view=plugins");
          const sec = page.locator("#view-plugins .pm-ghsec");
          const cards = sec.locator(".pm-card");
          await cards.first().waitFor();
          assert.deepEqual(await cards.locator(".pm-name b").allInnerTexts(), REPOS.map(name), "every one listed, in GitHub's order");
          const tr = (s) => page.evaluate((s) => t(s), s);
          const hook = await tr("Request hook");
          if (lang !== "en") assert.notEqual(hook, "Request hook", "Request hook in " + lang);

          // each card's one chip: the gateway's for middleware and hooks,
          // which say Request hook, as an installed one's row does
          for (const r of REPOS) {
            const chip = cards.filter({ has: page.locator(".pm-name b", { hasText: new RegExp("^" + name(r) + "$") }) }).locator(".pm-chip.kind");
            assert.equal(await chip.count(), 1, r.repo);
            const want = r.kind === "hooks" ? hook : await tr(r.kind === "middleware" ? "Middleware" : "Provider");
            assert.equal((await chip.innerText()).trim(), want, r.repo);
            assert.equal(await chip.evaluate((e) => e.classList.contains("mw")), !!r.kind, r.repo + "'s chip is the gateway's");
          }

          // picked out under Middleware, with the middleware
          const kinds = sec.locator(".pm-ghkinds .opt");
          assert.deepEqual(await kinds.allTextContents(), [await tr("All") + " · 7", await tr("Provider") + " · 2", await tr("Middleware") + " · 5"]);
          await kinds.nth(2).click();
          assert.deepEqual(await cards.locator(".pm-name b").allInnerTexts(), REPOS.filter((r) => r.kind).map(name));
          await kinds.nth(1).click();
          assert.deepEqual(await cards.locator(".pm-name b").allInnerTexts(), REPOS.filter((r) => !r.kind).map(name));

          const over = await page.evaluate(() => [document.querySelector("#view-plugins").scrollWidth - document.querySelector("#view-plugins").clientWidth, document.documentElement.scrollWidth - innerWidth]);
          assert.deepEqual(over.map((x) => Math.max(0, x)), [0, 0], "scrolls sideways: " + over);
          if (process.env.ARTIFACT_DIR) await sec.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `plugin-github-hooks-${engine}-${lang}-${width}.png`) });
          assert.deepEqual(errors, []);
        } finally {
          await ctx.close();
        }
      });
    }
  });
}

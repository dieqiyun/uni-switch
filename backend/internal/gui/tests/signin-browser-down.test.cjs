// Run with Node's test runner and Playwright on the module path; see README.md.
// #2043 (xiaobaifly7): on Windows the browser didn't start (rundll32 not
// found under a PATHEXT of ".CPL"), and the sign-in still said to finish in
// the browser, then timed out ten minutes later. Now a sign-in whose page
// didn't open says why, in the reader's language, with the link to open by
// hand: a subscription's sign-in (and its Open again, which clears the
// error once the browser opens) and an MCP server's. At 1100px and 420px,
// every language, Chromium and WebKit; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { reach } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
const LINK = "https://factory.example/device?code=WDJB-MJHT&state=" + "x".repeat(60);
const MCP_LINK = "https://dash.cloudflare.com/oauth2/auth?client_id=c1&state=" + "y".repeat(80);
const DOWN = 'exec: "rundll32": executable file not found in %PATH%';

function server(lang, st) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data, status) => route.fulfill({ status: status || 200, json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [{ id: "openai", name: "OpenAI", icon: "openai", preset: "openai", models: [], agents: [], key: { set: true, masked: "sk-…ab12" } }], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    const signing = { id: "s1", agent: "factory", state: "waiting", url: LINK, code: "WDJB-MJHT" };
    // the start says the browser didn't open; a poll knows nothing of it
    if (url.pathname === "/api/signin") return json({ ...signing, openError: DOWN });
    if (url.pathname.startsWith("/api/signin/")) return json(signing);
    if (url.pathname === "/api/open") {
      st.opens.push(req.postDataJSON().url);
      return st.browserUp ? route.fulfill({ status: 204, body: "" }) : json({ error: DOWN }, 400);
    }
    if (url.pathname === "/api/library") {
      return json({
        dir: "/h/.uni-switch/library", backups: "/h/.uni-switch/backups", home: "/h",
        agents: [{ id: "codex", name: "Codex", icon: "", skills: "/h/.codex/skills", mcp: "/h/.codex/mcp.json" }],
        instructions: { agents: [], sets: [] }, foundServers: [], projects: [], foundSkills: [], skills: [],
        servers: [{ name: "cloudflare-api", transport: "http", url: "https://mcp.cloudflare.com/mcp", agents: ["codex"], signIn: { signedIn: false } }],
      });
    }
    if (url.pathname === "/api/library/mcp-signin") return json({ id: "m1", server: "cloudflare-api", state: "waiting", url: MCP_LINK, openError: DOWN });
    if (url.pathname.startsWith("/api/library/mcp-signin/")) return json({ id: "m1", server: "cloudflare-api", state: "waiting", url: MCP_LINK });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const W = {
  en: { anyway: "Sign in anyway", again: "Open again", signIn: "Sign in", down: "uni-switch couldn't open your browser: " + DOWN + ". Open the sign-in link below in a browser yourself." },
  zh: { anyway: "仍然登录", again: "重新打开", signIn: "登录", down: "uni-switch 没能打开浏览器：" + DOWN + "。请自己在浏览器中打开下面的登录链接。" },
  "zh-TW": { anyway: "仍然登入", again: "重新開啟", signIn: "登入", down: "uni-switch 無法開啟瀏覽器：" + DOWN + "。請自行在瀏覽器中開啟下方的登入連結。" },
  ja: { anyway: "それでもサインイン", again: "もう一度開く", signIn: "サインイン", down: "uni-switch はブラウザを開けませんでした：" + DOWN + "。下のサインインリンクをご自身でブラウザで開いてください。" },
  de: { anyway: "Trotzdem anmelden", again: "Erneut öffnen", signIn: "Anmelden", down: "uni-switch konnte den Browser nicht öffnen: " + DOWN + ". Öffne den Anmeldelink unten selbst in einem Browser." },
};

// nothing reaches past the window's right edge
const overflow = (page) => page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(W)) {
    for (const width of [1100, 420]) {
      test(`${engine} ${lang} ${width}: a sign-in whose browser didn't open says why, with its link`, async (t) => {
        const w = W[lang];
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(() => browser.close());
        const errors = [];
        const st = { opens: [], browserUp: false };

        // a subscription's sign-in
        // the risk step before it is taken at 1100: at 420 its warning is
        // squeezed to nothing beside its buttons, filed apart from this
        let page = await (await browser.newContext({ viewport: { width: 1100, height: 800 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, st));
        await page.goto("http://uni-switch.test/?view=providers");
        await page.locator("#addProvider").click();
        const sheet = page.locator("#addSheet");
        await sheet.locator('.tile[data-pick="Factory"]').click();
        const box = sheet.locator(".signing");
        const anyway = box.locator("button", { hasText: w.anyway });
        await reach(page, anyway);
        await anyway.click();
        await page.setViewportSize({ width, height: 800 });
        const said = box.locator(".s", { hasText: w.down });
        await said.waitFor();
        assert.equal(await said.textContent(), w.down);
        const again = box.locator("button", { hasText: w.again });
        await reach(page, again);
        assert.ok(await box.locator(".signlink").isVisible(), "the link to open by hand is shown");
        assert.equal(await overflow(page), 0, "nothing past the right edge");
        // Open again fails the same way: still said
        await Promise.all([page.waitForResponse((r) => r.url().endsWith("/api/open")), again.click()]);
        assert.deepEqual(st.opens, [LINK]);
        await said.waitFor();
        // the browser opens: the error goes
        st.browserUp = true;
        await reach(page, again);
        await again.click();
        await said.waitFor({ state: "detached" });
        assert.deepEqual(st.opens, [LINK, LINK]);
        await page.context().close();

        // an MCP server's sign-in
        const ctx = await browser.newContext({ viewport: { width, height: 800 }, reducedMotion: "reduce" });
        await ctx.addInitScript(() => { try { localStorage.setItem("uni-switch.libTab", "mcp"); } catch {} });
        page = await ctx.newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, st));
        await page.goto("http://uni-switch.test/");
        await page.locator('button[data-view="library"]').click();
        await page.locator("#view-library .lib-row", { has: page.locator(".name", { hasText: /^cloudflare-api/ }) }).click();
        const lib = page.locator("#modal .lib-signin");
        await lib.getByRole("button", { name: w.signIn, exact: true }).click();
        const err = lib.locator(".lib-signin-err");
        await err.waitFor();
        assert.equal(await err.textContent(), w.down);
        const code = lib.locator(".lib-signin-link code");
        assert.equal(await code.textContent(), MCP_LINK);
        assert.ok(await lib.locator(".lib-signin-link button").isVisible(), "a copy button beside the link");
        const box2 = await code.boundingBox(), modal = await page.locator("#modal .lib-editor").boundingBox();
        assert.ok(box2.x + box2.width <= modal.x + modal.width + 1, "the link stays inside the editor");
        assert.equal(await overflow(page), 0, "nothing past the right edge");
        await ctx.close();
        assert.deepEqual(errors, []);
      });
    }
  }
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// sfss5362 on X: "支持 CLAUDE_CODE_OAUTH_TOKEN 吗". Claude's sign-in offers
// "Use a setup-token instead…", which ends the browser sign-in and asks for
// the token `claude setup-token` printed, said whole in the page's
// language. A refused token says why and stays to be fixed; one taken is
// added. The account it makes is uni-switch's only: Remove and "uni-switch only",
// no Use or Make first.
// Chromium and WebKit, English, Chinese (both), Japanese and German, at
// 420px; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const playwright = require("playwright");

const engines = (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"]);
const assets = path.resolve(__dirname, "../assets");
const TOKEN = "sk-ant-oat01-guitoken-0123456789";

for (const engine of engines) for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
  test(`claude setup-token account (${engine}, ${lang})`, async (t) => {
    const browser = await playwright[engine].launch();
    t.after(() => browser.close());
    const page = await browser.newPage({ viewport: { width: 420, height: 720 } });
    page.setDefaultTimeout(5000);
    page.setDefaultNavigationTimeout(60000);
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    const tokens = [];
    const canceled = [];
    await page.route("**/*", async (route) => {
      const url = new URL(route.request().url());
      const json = (data, status) => route.fulfill({ json: data, status: status || 200 });
      if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"dark",web:true};` });
      if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
      if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "dark" } });
      if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: {} });
      if (url.pathname === "/api/groups") return json({ groups: [] });
      if (url.pathname === "/api/signin/claude-token") {
        const body = route.request().postDataJSON();
        tokens.push(body.token);
        if (body.token.startsWith("sk-ant-api")) return json({ error: "this is an Anthropic API key, not a setup-token: add it as an API key provider instead" }, 400);
        return json({ user: "Claude setup-token 3204d0" });
      }
      if (url.pathname === "/api/signin") return json({ id: "c1", agent: "claude", state: "waiting", url: "https://claude.ai/oauth/authorize?x=1" });
      if (/^\/api\/signin\/c\d+\/cancel$/.test(url.pathname)) { canceled.push(url.pathname.split("/")[3]); return json({}); }
      if (/^\/api\/signin\/c\d+$/.test(url.pathname)) return new Promise(() => {}); // stays waiting
      if (url.pathname.startsWith("/api/")) return json({});
      const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
      const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
      return route.fulfill({ body: await fs.readFile(file), contentType });
    });
    await page.context().route((url) => url.hostname !== "uni-switch.test", (route) => route.fulfill({ contentType: "text/html", body: "" }));
    await page.goto("http://uni-switch.test/");
    await page.waitForFunction((l) => state.settings.lang === l, lang);
    await page.evaluate(() => {
      providers = providers || { providers: [], presets: [], plugins: [] };
      window.signedInWith = [];
      window.signedIn = async (st) => { window.signedInWith.push(st); signing = null; };
      window.renderProviders = () => {
        const host = document.getElementById("csign") || Object.assign(document.createElement("div"), { id: "csign" });
        host.style.cssText = "position:fixed;inset:0 auto auto 0;width:100%;z-index:1000;";
        host.replaceChildren(signing ? renderSigning(subOf("claude") || { agent: "claude" }) : "");
        document.body.append(host);
      };
      startSignIn("claude", true);
    });
    const box = page.locator("#csign");
    const said = (s, v) => page.evaluate(([s, v]) => t(s, v), [s, v]);
    const translated = async (s, v) => {
      const got = await said(s, v);
      if (lang !== "en") assert.notEqual(got, s.replace("{cmd}", "claude setup-token"), `${s} isn't in ${lang}`);
      return got;
    };

    // the browser sign-in offers the token
    await page.waitForFunction(() => signing?.state === "waiting");
    const offer = box.getByRole("button", { name: await translated("Use a setup-token instead…") });
    await offer.click();
    await page.waitForFunction(() => signing?.state === "token");
    assert.deepEqual(canceled, ["c1"]);
    const text = await box.innerText();
    assert.ok(text.includes(await translated("Add a Claude account by setup-token")), text);
    assert.ok(text.includes(await translated("Paste the token {cmd} printed. uni-switch gives it to Claude Code for this account's requests only; Claude Code itself stays signed in as it is.", { cmd: "claude setup-token" })), text);
    assert.ok(text.includes(await translated("A setup-token can't tell its email or allowance: the account is named by the token, and its allowance shows once a reply tells it.")), text);
    const field = box.locator(".claude-token input");
    const add = box.locator('.claude-token button[type="submit"]');
    assert.equal(await field.getAttribute("type"), "password");
    assert.equal(await add.isDisabled(), true);
    assert.equal(await box.locator("select").count(), 0);
    // inside the sheet at 420px
    const w = await box.evaluate((root) => Math.max(...[...root.querySelectorAll("*")].map((e) => e.getBoundingClientRect().right)));
    assert.ok(w <= 420, `sheet reaches ${w}px`);

    // a refused token says why and stays
    await field.fill("sk-ant-api03-key");
    await add.click();
    await page.waitForFunction(() => document.querySelector("#csign .claude-token .why")?.textContent);
    assert.match(await box.locator(".why").textContent(), /API key/);
    assert.equal(await page.evaluate(() => signing?.state), "token");

    // a setup-token is added
    await box.locator(".claude-token input").fill(TOKEN);
    await box.locator('.claude-token button[type="submit"]').click();
    await page.waitForFunction(() => window.signedInWith.length === 1);
    assert.deepEqual(tokens, ["sk-ant-api03-key", TOKEN]);
    assert.deepEqual(await page.evaluate(() => window.signedInWith), [{ agent: "claude", user: "Claude setup-token 3204d0", token: true }]);
    assert.equal(await box.innerText().then((s) => s.includes(TOKEN)), false);

    // its row is uni-switch's only
    const row = await page.evaluate(() => {
      const ls = [{ user: "pro@x.com", active: true, plan: "max" }, { user: "Claude setup-token 3204d0", on: true, token: true }];
      const a = { agent: "claude", agentName: "Claude Code", logins: ls };
      const list = renderAccounts(a, { id: "claude", account: { agent: "claude", user: "pro@x.com" } });
      document.body.append(list);
      const rows = [...list.querySelectorAll("*")].filter((e) => e.textContent.includes("Claude setup-token 3204d0") && !e.textContent.includes("pro@x.com") && e.querySelector("button"));
      const r = rows[0];
      return r ? { text: r.innerText, buttons: [...r.querySelectorAll("button")].map((b) => b.textContent) } : null;
    }).catch((e) => ({ error: e.message }));
    assert.ok(row && !row.error, JSON.stringify(row));
    assert.ok(row.text.includes(await translated("uni-switch only")), row.text);
    assert.ok(!row.buttons.includes(await said("Make first")) && !row.buttons.includes(await said("Use")), row.buttons.join(","));
    assert.deepEqual(errors, []);
  });
}

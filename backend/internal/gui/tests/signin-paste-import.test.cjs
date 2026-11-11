// Run with Node's test runner and Playwright on the module path; see README.md.
// LuvLetter on Discord (uni-switch in Docker, `uni-switch web`): ChatGPT sent the
// browser back to http://localhost:1455/auth/callback?code=…, and the address
// went into the Import box (Import from a file instead…) rather than the
// sign-in's Callback URL field. The box canceled the sign-in as it opened,
// so nothing was left for the address to finish, and it was read as a
// refresh token. Now the sign-in waits behind the box: the address pasted
// there is posted to the import, which finishes the sign-in, the account is
// listed as added, and Done ends the sign-in left behind. A sign-in that
// takes no pasted address is still canceled when the box opens. English
// and Chinese, Chromium and WebKit; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const LINK = "https://auth.openai.com/oauth/authorize?client_id=app&redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback&state=st1";
const BACK = "http://localhost:1455/auth/callback?code=ac_fake&scope=openid&state=st1";

function server(lang, log, paste) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [{ id: "openai", name: "OpenAI", icon: "openai", preset: "openai", models: [], agents: [], key: { set: true, masked: "sk-…ab12" } }], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname.endsWith("/cancel")) { log.push("cancel " + url.pathname.split("/")[3]); return route.fulfill({ status: 204 }); }
    if (url.pathname === "/api/signin/import") {
      log.push(route.request().postDataJSON());
      return json({ providers: { providers: [], presets: [], excluded: [] }, results: [{ user: "cx@example.com", status: "added", plan: "plus" }] });
    }
    if (url.pathname === "/api/signin" || url.pathname === "/api/signin/cx1") return json({ id: "cx1", agent: "codex", state: "waiting", url: LINK, pasteCallback: paste });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const W = {
  en: { instead: "Import from a file instead…", import: "Import", done: "Done", added: "Added" },
  zh: { instead: "改为从文件导入…", import: "导入", done: "完成", added: "已添加" },
};

async function open(engine, lang, log, paste, t) {
  const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
  const page = await (await browser.newContext({ viewport: { width: 900, height: 800 }, reducedMotion: "reduce" })).newPage();
  t.after(async () => {
    if (process.env.ARTIFACT_DIR) {
      await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
      await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-signin-paste-import-${paste}.png`) });
    }
    await browser.close();
  });
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/*", server(lang, log, paste));
  await page.context().route((url) => url.hostname !== "uni-switch.test", (route) => route.fulfill({ contentType: "text/html", body: "" }));
  await page.goto("http://uni-switch.test/?view=providers");
  await page.locator("#addProvider").click();
  const sheet = page.locator("#addSheet");
  await sheet.locator('.tile[data-pick="ChatGPT"]').click();
  await sheet.locator(".signing").getByRole("button", { name: W[lang].instead }).click();
  await sheet.locator(".signing.import textarea").waitFor();
  return { page, sheet, errors };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a sign-in's address pasted into the Import box finishes it`, async (t) => {
      const w = W[lang], log = [];
      const { page, sheet, errors } = await open(engine, lang, log, true, t);
      assert.deepEqual(log, [], "the sign-in waits behind the box, not canceled");
      await sheet.locator(".signing.import textarea").fill(BACK);
      await sheet.getByRole("button", { name: w.import, exact: true }).click();
      await sheet.locator(".signing .res.added").getByText("cx@example.com").waitFor();
      assert.deepEqual(log, [{ agent: "codex", files: [BACK] }]);
      await sheet.getByRole("button", { name: w.done, exact: true }).click();
      for (let i = 0; i < 40 && log.length < 2; i++) await page.waitForTimeout(50);
      assert.deepEqual(log.slice(1), ["cancel cx1"], "Done ends the sign-in left behind");
      assert.deepEqual(errors, []);
    });
  }
  test(`${engine}: a sign-in that takes no address is canceled as the box opens`, async (t) => {
    const log = [];
    const { page, errors } = await open(engine, "en", log, false, t);
    for (let i = 0; i < 40 && !log.length; i++) await page.waitForTimeout(50);
    assert.deepEqual(log, ["cancel cx1"]);
    assert.deepEqual(errors, []);
  });
}

// Run with Node's test runner and Playwright on the module path; see README.md.
// On Windows npm's global folder may be Node.js's own under Program Files,
// which only an administrator can change (新v学员 on Discord). An update
// there says Windows asks for an administrator first; one whose prompt was
// declined, or couldn't be shown, says the folder and what to do, in the
// reader's language; and Install another agent's npm commands say a
// terminal opened as administrator is needed. No backend: the API is faked
// here, and nothing is ever installed.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const models = ["model-a", "model-b"].map((m) => ({ value: m, label: m }));
const agent = (id, name) => ({ id, name, path: "~/." + id, fields: [{ key: "model", label: "model", value: "model-a", options: models }] });
const AGENTS = [agent("gemini", "Gemini CLI"), agent("codex", "Codex")];
const PF = "C:\\Program Files\\nodejs";
const CMD = `npm.cmd install -g --prefix "${PF}" @google/gemini-cli@latest`;
const CLIS = {
  gemini: { version: "0.60.0", latest: "0.61.0", via: "npm", command: CMD, update: true, admin: true },
  codex: { version: "0.155.1", latest: "0.159.0", via: "npm", command: "npm.cmd install -g @openai/codex@latest", update: true },
};
const INSTALLS = [{ id: "opencode", name: "OpenCode", icon: "opencode", commands: [{ via: "npm", command: "npm install -g opencode-ai", admin: PF }] }];

function server(lang, answer) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return route.fulfill({ json: { agents: AGENTS, profiles: [], settings: { lang, theme: "light" } } });
    if (url.pathname === "/api/agents/cli" && req.method() === "GET") return route.fulfill({ json: { agents: CLIS, pending: false } });
    if (url.pathname === "/api/agents/cli/gemini" && req.method() === "POST") return route.fulfill({ status: 400, json: answer });
    if (url.pathname === "/api/agents/install") return route.fulfill({ json: INSTALLS });
    if (url.pathname === "/api/usage/quotas") return route.fulfill({ json: [] });
    if (url.pathname === "/api/groups") return route.fulfill({ json: { groups: [] } });
    if (url.pathname === "/api/providers") return route.fulfill({ json: { providers: [], gateway: { running: true } } });
    if (url.pathname.startsWith("/api/")) return route.fulfill({ json: {} });
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const row = (id) => `.row.agent[data-id="${id}"]`;
const base = { error: "Gemini CLI is installed in C:\\Program Files\\nodejs, which only an administrator can change, and …", code: "needs_admin", agent: "Gemini CLI", prefix: PF, command: CMD };

const CASES = {
  en: {
    title: /^Updates with npm\.cmd install .*, as administrator: Windows asks first$/,
    declined: `Gemini CLI is installed in ${PF}, which only an administrator can change, and the administrator prompt was declined.`,
    none: `Gemini CLI is installed in ${PF}, which only an administrator can change, and uni-switch couldn't ask Windows for one (no administrator prompt: The service cannot be started).`,
    fix: `run ${CMD} in a terminal opened as administrator, or move npm's global folder to one of your own (npm config set prefix "%APPDATA%\\npm") and install Gemini CLI again.`,
    install: `npm installs into ${PF} here, which only an administrator can change`,
  },
  zh: {
    title: /^通过 npm\.cmd install .* 以管理员身份更新：Windows 会先询问$/,
    declined: `Gemini CLI 安装在 ${PF}，这个目录只有管理员能修改，而管理员授权提示被拒绝了。`,
    none: `Gemini CLI 安装在 ${PF}，这个目录只有管理员能修改，而 uni-switch 没能向 Windows 请求管理员权限（no administrator prompt: The service cannot be started）。`,
    fix: `或在以管理员身份打开的终端里运行 ${CMD}，或把 npm 的全局目录改到自己的文件夹（npm config set prefix "%APPDATA%\\npm"）后重新安装 Gemini CLI。`,
    install: `这里 npm 会安装到 ${PF}，这个目录只有管理员能修改`,
  },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": an npm under Program Files asks for an administrator", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const errors = [];
    t.after(async () => {
      if (errors.length) console.log(errors);
      await browser.close();
    });
    const open = async (lang, answer) => {
      const page = await (await browser.newContext({ viewport: { width: 980, height: 700 } })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(lang, answer));
      await page.goto("http://uni-switch.test/");
      await page.locator(`${row("gemini")} .ag-up`).waitFor();
      return page;
    };
    for (const [lang, want] of Object.entries(CASES)) {
      await t.test(lang, async () => {
        let page = await open(lang, { ...base, canceled: "1" });
        assert.match(await page.locator(`${row("gemini")} .ag-up`).getAttribute("title"), want.title);
        // one its user can write in says nothing of an administrator
        assert.doesNotMatch(await page.locator(`${row("codex")} .ag-up`).getAttribute("title"), /administrator|管理员/);
        await page.locator(`${row("gemini")} .ag-up`).click();
        await page.waitForFunction(() => /C:/.test(document.querySelector("#status")?.textContent || ""));
        let said = await page.locator("#status").textContent();
        assert(said.startsWith(want.declined), said);
        assert(said.endsWith(want.fix), said);
        // the pill is back, to be tried again
        await page.locator(`${row("gemini")} .ag-up:not(.busy)`).waitFor();

        // Install another agent's npm commands say what they need
        const box = page.locator("#agentsInstall");
        await box.locator(".ag-install-head").click();
        assert.match(await box.locator(".ag-install-note").textContent(), new RegExp(want.install.replace(/[\\.()]/g, "\\$&")));

        page = await open(lang, { ...base, reason: "no administrator prompt: The service cannot be started" });
        await page.locator(`${row("gemini")} .ag-up`).click();
        await page.waitForFunction(() => /C:/.test(document.querySelector("#status")?.textContent || ""));
        said = await page.locator("#status").textContent();
        assert(said.startsWith(want.none), said);
      });
    }
    assert.deepEqual(errors, []);
  });
}

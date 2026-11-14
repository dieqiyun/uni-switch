// Run with Node's test runner and Playwright on the module path; see README.md.
// A provider an OpenCode plugin signs in to: the add sheet lists it under
// "From plugins"; its sign-in asks the way to sign in, the method's
// questions (a pick, then a text the plugin checks), then opens the
// browser and takes the code its page shows; an "api" way takes a key and
// opens the account; another account is added beside it, as a built-in
// subscription's is, and can be put first. (The Plugins tab is
// plugin-market.test.cjs.) English and Chinese; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

const plugin = { id: "fakeco", pid: "fakeco", name: "FakeCo", icon: "generic", spec: "opencode-fakeco-auth", signedIn: false, models: 3,
  methods: [{ type: "api", label: "API key" }, { type: "oauth", label: "Browser sign-in" },
    { type: "api", label: "Paste an existing FakeCo session token from another device you are signed in on" }] };

function server(lang, asked) {
  const accts = []; // who is signed in, the first in use first
  const payload = () => {
    const providers = [{ id: "openai", name: "OpenAI", icon: "openai", preset: "openai", models: [], agents: [], key: { set: true, masked: "sk-…ab12" } }];
    if (accts.length) providers.push({ id: "fakeco", name: "FakeCo", icon: "generic", models: [], agents: [], key: {}, account: { agent: "fakeco", agentName: "FakeCo", agentIcon: "generic", user: accts[0], logins: accts.map((user, i) => ({ user, active: i === 0, on: true })) } });
    return { providers, presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [{ ...plugin, signedIn: accts.length > 0 }] };
  };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    const body = () => route.request().postDataJSON();
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json(payload());
    if (url.pathname === "/api/login/switch") {
      const b = body();
      asked.push(["switch", b]);
      accts.unshift(...accts.splice(accts.indexOf(b.user), 1));
      return json(payload());
    }
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname === "/api/plugin-signin/prompt") {
      const b = body();
      asked.push(["prompt", b]);
      const inputs = { ...(b.inputs || {}) };
      if (b.key === "team" && !b.value) return json({ error: "Required" });
      if (b.key) inputs[b.key] = b.value;
      if (b.method === 1 && !inputs.where) return json({ prompt: { type: "select", key: "where", message: "Where do you work?", options: [{ label: "Home", value: "home" }, { label: "Work", value: "work" }] }, inputs });
      if (b.method === 1 && inputs.where === "work" && !inputs.team) return json({ prompt: { type: "text", key: "team", message: "Your team", placeholder: "blue" }, inputs });
      return json({ prompt: null, inputs });
    }
    if (url.pathname === "/api/plugin-signin") {
      const b = body();
      asked.push(["signin", b]);
      if (b.key) { accts.push("API key …" + b.key); return json({ agent: "fakeco", state: "done", user: accts.at(-1) }); }
      return json({ id: "p1", agent: "fakeco", state: "waiting", url: "https://fake.test/auth", pasteCode: true, instructions: "Paste the code FakeCo shows." });
    }
    if (url.pathname === "/api/signin/p1/callback") { asked.push(["code", body()]); return route.fulfill({ status: 204 }); }
    if (url.pathname === "/api/signin/p1") return json({ id: "p1", agent: "fakeco", state: "waiting", url: "https://fake.test/auth", pasteCode: true, instructions: "Paste the code FakeCo shows." });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const L = {
  en: { section: "From plugins", how: "How do you sign in to FakeCo?", next: "Next", code: "Code", finish: "Finish sign-in", key: "FakeCo API key", signIn: "Sign in", add: "Add another FakeCo account", first: "Make first", note: "The gateway uses the first. Tick more" },
  zh: { section: "来自插件", how: "用哪种方式登录 FakeCo？", next: "下一步", code: "验证码", finish: "完成登录", key: "FakeCo API Key", signIn: "登录", add: "添加另一个 FakeCo 账号", first: "设为首选", note: "网关优先用第一个账号；勾选多个时" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a plugin's provider", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh"]) {
      await t.test(lang, async () => {
        const w = L[lang];
        const page = await (await browser.newContext({ viewport: { width: 900, height: 800 } })).newPage();
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const errors = [], asked = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, asked));
        await page.goto("http://uni-switch.test/?view=providers");
        await page.locator("#addProvider").click();
        const sheet = page.locator("#addSheet");
        await sheet.locator(".kind b", { hasText: w.section }).waitFor();
        const row = sheet.locator('.tile[data-pick="FakeCo"]');
        assert.match(await row.getAttribute("title"), /opencode-fakeco-auth/);

        // the browser way: a pick, a text the plugin checks, then the code
        await row.click();
        const box = sheet.locator(".signing");
        await box.locator(".n", { hasText: w.how }).waitFor();
        if (process.env.ARTIFACT_DIR) await sheet.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `plugin-method-${engine}-${lang}.png`) });
        assert.equal(asked.length, 0, "nothing asked before the way is picked");
        // however long the plugin names its ways, the question keeps its
        // line and every way stays inside the box
        const fit = await box.evaluate((b) => {
          const r = b.getBoundingClientRect(), n = b.querySelector(".tt").getBoundingClientRect();
          return { question: n.width / r.width, spill: [...b.querySelectorAll(".choices button")].map((x) => x.getBoundingClientRect().right - r.right) };
        });
        assert.ok(fit.question > 0.5, "the question squeezed to " + fit.question);
        assert.equal(fit.spill.length, 3);
        for (const s of fit.spill) assert.ok(s <= 0, "a way runs past the box by " + s);
        await box.locator("button", { hasText: "Browser sign-in" }).click();
        await box.locator(".n", { hasText: "Where do you work?" }).waitFor();
        await box.locator("button", { hasText: "Work" }).click();
        const team = box.getByRole("textbox", { name: "Your team" });
        await team.waitFor();
        if (process.env.ARTIFACT_DIR) await box.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `plugin-prompt-${engine}-${lang}.png`) });
        await box.locator("button", { hasText: w.next }).click();
        await box.locator(".why", { hasText: "Required" }).waitFor();
        await team.fill("blue");
        await team.press("Enter");
        await box.locator(".s", { hasText: "Paste the code FakeCo shows." }).waitFor();
        const signin = asked.find(([k]) => k === "signin")[1];
        assert.deepEqual(signin, { provider: "fakeco", method: 1, inputs: { where: "work", team: "blue" } });
        if (process.env.ARTIFACT_DIR) await box.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `plugin-code-${engine}-${lang}.png`) });
        const code = box.getByRole("textbox", { name: w.code });
        await code.fill("abc123");
        await box.locator("button", { hasText: w.finish }).click();
        for (let i = 0; i < 50 && !asked.some(([k]) => k === "code"); i++) await page.waitForTimeout(50);
        assert.deepEqual(asked.find(([k]) => k === "code")[1], { url: "abc123" });

        // the key way: the account opens once it's in
        await box.locator("button.text:not(.primary)").last().click();
        await row.click();
        await box.locator("button", { hasText: "API key" }).click();
        const key = box.getByLabel(w.key);
        await key.waitFor();
        assert.equal(await key.getAttribute("type"), "password");
        await key.fill("k1");
        await box.locator("button", { hasText: w.signIn }).click();
        await page.waitForFunction(() => editing === "fakeco");
        assert.deepEqual(asked.filter(([k]) => k === "signin").pop()[1], { provider: "fakeco", method: 0, inputs: {}, key: "k1" });

        // another account, beside the first: the editor adds it, lists
        // both, and puts the second first when asked
        const ed = page.locator("#modal .editor");
        const add = ed.locator(".accts .acc.add", { hasText: w.add });
        await add.waitFor();
        assert.match(await ed.innerText(), new RegExp(w.note));
        await add.click();
        await ed.locator(".signing button", { hasText: "API key" }).first().click();
        await ed.getByLabel(w.key).fill("k2");
        await ed.locator(".signing button", { hasText: w.signIn }).click();
        const rows = ed.locator(".accts .acc:not(.add)");
        await page.waitForFunction(() => document.querySelectorAll("#modal .editor .accts .acc:not(.add)").length === 2);
        assert.deepEqual(await rows.locator(".n").allInnerTexts(), ["API key …k1", "API key …k2"]);
        if (process.env.ARTIFACT_DIR) await ed.locator(".accts").screenshot({ path: path.join(process.env.ARTIFACT_DIR, `plugin-accounts-${engine}-${lang}.png`) });
        await rows.nth(1).locator("button", { hasText: w.first }).click();
        await page.waitForFunction(() => document.querySelector("#modal .editor .accts .acc .n")?.textContent === "API key …k2");
        assert.deepEqual(asked.filter(([k]) => k === "switch").pop()[1], { agent: "fakeco", user: "API key …k2" });

        assert.deepEqual(errors, []);
      });
    }
  });
}

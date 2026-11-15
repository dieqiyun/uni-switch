// Run with Node's test runner and Playwright on the module path; see README.md.
// A plugin's sign-in question of type "password" (a Qoder PAT, #1982) is
// typed unseen, as an API key is, while a "text" question beside it stays
// in the open; the answer still reaches the plugin as typed, the field
// fits a narrow window, and nothing scrolls. Every language, both
// engines, 1100 and 420 wide; the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { reach } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");

const plugin = { id: "fakeco", pid: "fakeco", name: "FakeCo", icon: "generic", spec: "opencode-fakeco-auth", signedIn: false, models: 3,
  methods: [{ type: "api", label: "API key" }, { type: "oauth", label: "Sign in with a FakeCo PAT" }] };

function server(lang, asked) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    const body = () => route.request().postDataJSON();
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [{ id: "openai", name: "OpenAI", icon: "openai", preset: "openai", models: [], agents: [], key: { set: true, masked: "sk-…ab12" } }], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [plugin] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname === "/api/plugin-signin/prompt") {
      const b = body();
      asked.push(["prompt", b]);
      const inputs = { ...(b.inputs || {}) };
      if (b.key) inputs[b.key] = b.value;
      if (!inputs.team) return json({ prompt: { type: "text", key: "team", message: "Your team", placeholder: "blue" }, inputs });
      if (!inputs.pat) return json({ prompt: { type: "password", key: "pat", message: "Your FakeCo Personal Access Token", placeholder: "pt-..." }, inputs });
      return json({ prompt: null, inputs });
    }
    if (url.pathname === "/api/plugin-signin") {
      asked.push(["signin", body()]);
      return json({ id: "p1", agent: "fakeco", state: "waiting", url: "https://fake.invalid/auth", instructions: "Finish in the browser." });
    }
    if (url.pathname === "/api/signin/p1") return json({ id: "p1", agent: "fakeco", state: "waiting", url: "https://fake.invalid/auth", instructions: "Finish in the browser." });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a plugin's password question is typed unseen", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      for (const width of [1100, 420]) {
        await t.test(`${lang} ${width}`, async () => {
          const ctx = await browser.newContext({ viewport: { width, height: 800 } });
          const page = await ctx.newPage();
          page.setDefaultTimeout(5000);
          page.setDefaultNavigationTimeout(60000);
          const errors = [], asked = [];
          page.on("pageerror", (e) => errors.push(e.message));
          await page.route("**/*", server(lang, asked));
          await page.goto("http://uni-switch.test/?view=providers");
          await page.locator("#addProvider").click();
          const sheet = page.locator("#addSheet");
          // at 420 the sheet's foot is past the window's: the reader
          // wheels down to each control, as the app puts other scrolls back
          const tile = sheet.locator('.tile[data-pick="FakeCo"]');
          await reach(page, tile);
          await tile.click();
          const box = sheet.locator(".signing");
          const way = box.locator("button", { hasText: "Sign in with a FakeCo PAT" });
          await reach(page, way);
          await way.click();

          // a text question stays in the open
          const team = box.getByLabel("Your team");
          await team.waitFor();
          assert.equal(await team.getAttribute("type"), "text");
          await reach(page, team);
          await team.fill("blue");
          await team.press("Enter");

          // the password question is typed unseen, and fits the window
          const pat = box.getByLabel("Your FakeCo Personal Access Token");
          await pat.waitFor();
          assert.equal(await pat.getAttribute("type"), "password");
          assert.equal(await pat.getAttribute("placeholder"), "pt-...");
          await reach(page, pat);
          await pat.click();
          const y = await page.evaluate(() => [scrollY, ...[...document.querySelectorAll("*")].filter((e) => e.scrollTop).map((e) => e.scrollTop)].join());
          await pat.fill("pt-secret");
          const fits = await pat.evaluate((i) => i.getBoundingClientRect().right <= document.documentElement.clientWidth);
          assert.ok(fits, "the field runs past a " + width + " window");
          assert.ok(!(await box.innerText()).includes("pt-secret"), "the token shows as text");
          assert.equal(await page.evaluate(() => [scrollY, ...[...document.querySelectorAll("*")].filter((e) => e.scrollTop).map((e) => e.scrollTop)].join()), y, "typing scrolled");
          if (process.env.ARTIFACT_DIR) await box.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `plugin-password-${engine}-${lang}-${width}.png`) });
          await pat.press("Enter");
          await box.locator(".s", { hasText: "Finish in the browser." }).waitFor();
          assert.deepEqual(asked.find(([k]) => k === "signin")[1], { provider: "fakeco", method: 1, inputs: { team: "blue", pat: "pt-secret" } });
          assert.deepEqual(errors, []);
          await ctx.close();
        });
      }
    }
  });
}

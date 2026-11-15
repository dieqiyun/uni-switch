// Run with Node's test runner and Playwright on the module path; see README.md.
// A market search one source of which didn't answer while the others did
// (#1589, Ti800: "prototype" listed two skills under "couldn't search
// skills.sh: … 503") says which source and that the list is only what the
// rest found, not a failed search in the error's amber bar. A search that
// found nothing at all keeps the error. An older search answered after a
// newer one (a 503 slower than the newer success) changes nothing. Skills
// and MCP servers, every language, a narrow window, both engines. No
// backend: the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const HOME = "/Users/tester";
const CAUSE = "www.skills.sh answered 503 Service Unavailable";
const skill = (source, n) => ({ id: `${source}/prototype`, name: "prototype", source, skillId: "prototype", installs: n, icon: "", description: "Build a throwaway prototype" });
const server = (id) => ({ id, title: id, publisher: "acme", description: "A server", transport: "stdio" });

// what the market answers each search, and after how long
function answer(kind, q) {
  if (kind === "skills") {
    if (q === "prototype") return { ms: 0, body: { items: [skill("mattpocock/skills", 917000), skill("emilkowalski/skills", 139000)], error: "couldn't search skills.sh: " + CAUSE, failed: "skills.sh", cause: CAUSE } };
    if (q === "zzzz") return { ms: 0, body: { items: [], error: "couldn't search skills.sh: " + CAUSE, failed: "skills.sh", cause: CAUSE } };
    // the older search, answered late with a 503 of its own
    if (q === "proto") return { ms: 1200, body: { items: [], error: "couldn't search skills.sh: " + CAUSE, failed: "skills.sh", cause: CAUSE } };
    if (q === "prototypes") return { ms: 0, body: { items: [skill("acme/skills", 5)] } };
    return { ms: 0, body: { items: [] } };
  }
  if (q === "github") return { ms: 0, body: { items: [server("github")], error: "couldn't search the MCP Registry: registry.modelcontextprotocol.io answered 503 Service Unavailable", failed: "the MCP Registry", cause: "registry.modelcontextprotocol.io answered 503 Service Unavailable" } };
  return { ms: 0, body: { items: [] } };
}

function serve(lang) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/library") {
      return json({
        dir: `${HOME}/.uni-switch/library`, backups: `${HOME}/.uni-switch/backups`, home: HOME,
        agents: [{ id: "claude", name: "Claude Code", icon: "claudecode-color", skills: `${HOME}/.claude/skills`, mcp: `${HOME}/.claude.json` }],
        instructions: { agents: [], sets: [] }, servers: [], foundServers: [], projects: [], foundSkills: [], skills: [], problems: [],
      });
    }
    const mk = url.pathname.match(/^\/api\/library\/market\/(skills|servers)$/);
    if (mk) {
      const a = answer(mk[1] === "skills" ? "skills" : "mcp", url.searchParams.get("q") || "");
      if (a.ms) await new Promise((ok) => setTimeout(ok, a.ms));
      return json(a.body).catch(() => {});
    }
    if (url.pathname === "/api/library/icon") return route.fulfill({ status: 404, body: "" });
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    const body = await fs.readFile(file).catch(() => null);
    await (body ? route.fulfill({ body, contentType }) : route.fulfill({ status: 404, body: "" }));
  };
}

// what a market box shows once its search has settled
async function settled(page, box, n) {
  await page.waitForFunction(({ sel, n }) => {
    const b = document.querySelector(sel);
    return b && !b.classList.contains("loading") && b.querySelectorAll(".mk-card:not(.mk-skel)").length === n;
  }, { sel: `#view-library .mk[data-market="${box}"]`, n });
}

async function search(page, box, q) {
  const inp = page.locator(`#view-library .mk[data-market="${box}"] input[type=search]`);
  const asked = page.waitForRequest((r) => r.url().includes("/api/library/market/") && new URL(r.url()).searchParams.get("q") === q);
  await inp.fill(q);
  await asked;
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${engine} ${lang}: a source that didn't answer beside results is a partial notice, not a failed search`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const context = await browser.newContext({ viewport: { width: 520, height: 760 }, reducedMotion: "reduce" });
      const page = await context.newPage();
      page.setDefaultTimeout(6000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang));
      await page.addInitScript(() => { if (!localStorage.getItem("uni-switch.libTab")) localStorage.setItem("uni-switch.libTab", "skills"); });
      t.after(() => browser.close());
      await page.goto("http://uni-switch.test/?view=library");
      const box = page.locator('#view-library .mk[data-market="skills"]');
      await box.waitFor();

      // the reporter's case: two found, skills.sh's search not answering
      await search(page, "skills", "prototype");
      await settled(page, "skills", 2);
      assert.equal(await box.locator(".mk-msg.err").count(), 0, "the partial list is shown as a failed search");
      const note = box.locator(".mk-msg.mk-partial");
      assert.equal(await note.count(), 1);
      const text = await note.textContent();
      assert.match(text, /skills\.sh/);
      assert.match(text, /503 Service Unavailable/);
      assert.doesNotMatch(text, /couldn't search/);
      // it fits the narrow window
      assert(await note.evaluate((e) => e.scrollWidth <= e.clientWidth + 1 && e.getBoundingClientRect().right <= window.innerWidth));

      // nothing found at all: that is the error, as it was
      await search(page, "skills", "zzzz");
      await settled(page, "skills", 0);
      assert.equal(await box.locator(".mk-msg.mk-partial").count(), 0);
      assert.match(await box.locator(".mk-msg.err").textContent(), /couldn't search skills\.sh: .*503/);

      // an older search's 503 answered after the newer one's results
      await search(page, "skills", "proto");
      const late = page.waitForResponse((r) => new URL(r.url()).searchParams.get("q") === "proto");
      await search(page, "skills", "prototypes");
      await settled(page, "skills", 1);
      await late;
      await page.evaluate(() => new Promise((ok) => requestAnimationFrame(() => requestAnimationFrame(ok))));
      assert.equal(await box.locator(".mk-msg.err, .mk-msg.mk-partial").count(), 0, "the older search's error was written over the newer results");
      assert.equal(await box.locator(".mk-card:not(.mk-skel)").count(), 1);

      // MCP servers: the registry not answering beside the featured ones
      await page.evaluate(() => { localStorage.setItem("uni-switch.libTab", "mcp"); });
      await page.reload();
      const mcp = page.locator('#view-library .mk[data-market="mcp"]');
      await mcp.waitFor();
      await search(page, "mcp", "github");
      await settled(page, "mcp", 1);
      assert.equal(await mcp.locator(".mk-msg.err").count(), 0, "the MCP partial list is shown as a failed search");
      assert.match(await mcp.locator(".mk-msg.mk-partial").textContent(), /MCP Registry.*503/);

      assert.deepEqual(errors, []);
    });
  }
}

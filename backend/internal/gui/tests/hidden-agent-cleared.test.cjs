// Run with Node's test runner and Playwright on the module path; see README.md.
// Hu9956, #842: an agent hidden by hand and put back to its defaults kept
// its place under Hidden and its Show (the eye) until its switch was turned
// on and off; only then did it go under Not set up. Now a hidden agent whose
// settings change so that nothing is set on it goes under Not set up at
// once, its hiding dropped and saved, even when the pick's answer still
// names it hidden (the arrangement is saved alongside). One hidden with
// nothing set on it, and left alone, stays hidden. No click moves the page.
// Chromium and WebKit, English and Chinese; no backend.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const options = [{ value: "relay/m1", label: "m1", ref: "relay/m1", note: "Relay" }];
const agent = (id, name, value) => ({ id, name, icon: "generic", path: "/fixture/" + id, fields: [{ key: "model", label: "model", value, options }] });

function server(lang, arranged) {
  // Main is connected; Hanako, hidden, is on a model of its own files, as the
  // reporter's OpenHanako was (mimo-v2.5-pro)
  let cur = { agents: [{ ...agent("main", "Main", "relay/m1"), wired: true }, agent("hanako", "Hanako", "mimo-v2.5-pro"), agent("idle", "Idle", "")], profiles: [] };
  const settings = { lang, theme: "dark", agentsHidden: ["hanako", "idle"] };
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"dark",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ ...cur, settings });
    if (url.pathname === "/api/set") {
      const b = req.postDataJSON();
      cur = JSON.parse(JSON.stringify(cur));
      cur.agents.find((a) => a.id === b.agent).fields.find((f) => f.key === b.field).value = b.value;
      // answered with the settings as read before the arrangement is saved
      const was = JSON.parse(JSON.stringify(settings));
      await new Promise((r) => setTimeout(r, 300));
      return json({ ...cur, settings: was });
    }
    if (url.pathname === "/api/agents/arrange") {
      const b = req.postDataJSON();
      arranged.push(b);
      settings.agentOrder = b.order;
      settings.agentsHidden = b.hidden;
      return json({ ...settings, agentsShown: b.shown });
    }
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const words = {
  en: { hidden: "Hidden", hidLine: "Hidden (1)", unset: "Not set up", def: "Default", said: "Hanako has nothing set on it now · it is under Not set up, no longer hidden" },
  zh: { hidden: "已隐藏", hidLine: "已隐藏（1）", unset: "未设置", def: "默认", said: "Hanako 已没有任何设置 · 已取消隐藏，移到「未设置」" },
};

// the fold's groups: each caption with the agents under it, the hidden
// ones' own disclosure (under the others) with the rows it holds
const groups = (page) => page.evaluate(() => {
  const out = [];
  for (const n of document.querySelectorAll(".agent-fold-inner > *, .agent-hid-inner > *")) {
    if (n.classList.contains("agent-fold-cap")) out.push([n.textContent.trim()]);
    else if (n.dataset.id) out.at(-1).push(n.dataset.id + (n.querySelector(".ag-show") ? "+show" : ""));
  }
  return out;
});

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: a hidden agent put back to its defaults goes under Not set up at once`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 1100, height: 700 }, colorScheme: "dark" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [], arranged = [], w = words[lang];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", server(lang, arranged));
      await page.goto("http://uni-switch.test/?view=agents");
      await page.locator(".agent-more").click();
      const hanako = page.locator('.agent-fold .row.agent[data-id="hanako"]');
      await hanako.locator(".ag-show").waitFor();
      assert.deepEqual(await groups(page), [[w.hidden, "hanako+show", "idle+show"]]);
      const view = page.locator("#view-agents");
      const top = await view.evaluate((v) => v.scrollTop);

      // Hanako's model back to its default
      await hanako.locator('> .field.ag-start[data-key="model"]').click();
      await page.locator("#pop:not([hidden]) #list li").first().waitFor();
      await page.locator("#list li", { hasText: w.def }).first().click();
      await page.waitForFunction((s) => (document.querySelector("#status")?.textContent || "").includes(s), w.said);
      assert.deepEqual(await groups(page), [[w.unset, "hanako"], [w.hidLine, "idle+show"]], "Hanako went under Not set up without its Show");
      assert.deepEqual(arranged.at(-1).hidden, ["idle"], "the hiding dropped is saved");

      // the pick's answer, read before that, still names it hidden: it stays put
      await page.waitForResponse((r) => r.url().endsWith("/api/set"));
      await page.waitForTimeout(150);
      assert.deepEqual(await groups(page), [[w.unset, "hanako"], [w.hidLine, "idle+show"]], "the pick's answer didn't hide it again");
      assert.equal(arranged.length, 1, "saved once");
      assert.equal(await view.evaluate((v) => v.scrollTop), top, "no click moved the page");
      if (process.env.SHOT) await page.screenshot({ path: `${process.env.SHOT}/${engine}-${lang}.png` });
      assert.deepEqual(errors, []);
    });
  }
}

// Gateway-only plugins have options and private-data-free diagnostics. APIs are fixtures.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = process.env.UNI_SWITCH_GATEWAY_ASSETS || path.resolve(__dirname, "../assets");

function server(lang, pluginState) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true }, plugins: [] });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname === "/api/plugins/market" || url.pathname === "/api/plugins" || url.pathname === "/api/plugins/listings") return json(url.pathname === "/api/plugins" ? pluginState : url.pathname === "/api/plugins/listings" ? { listings: [] } : { listings: [], state: pluginState });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const SPEC = "/fixtures/context-plugin";
for (const engine of ["chromium", "webkit"]) {
  test(engine + ": gateway configuration and diagnostics", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({channel:"chromium"}));
    t.after(() => browser.close());
    for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
      await t.test(lang, async () => {
        const page = await browser.newPage({viewport:{width:980,height:820}});
        page.setDefaultTimeout(5000);
        page.setDefaultNavigationTimeout(60000);
        const posted = [], errors = [];
        const state = {bun:true, bunVersion:"1.4.3", plugins:[{
          spec:SPEC, providers:[], moved:[], isGateway:true, gatewayOnly:true,
          optionsExample:{serverUrl:"http://127.0.0.1:8000",gatewayTimeoutMs:5000},
          gateway:{calls:3,failures:1,avgMillis:5.5,lastOutcome:"ready",lastBytes:120},
        }]};
        page.on("pageerror", e=>errors.push(e.message));
        await page.route("**/*",server(lang,state));
        await page.route("**/api/plugins/options", r=>{
          const data=r.request().postDataJSON(); posted.push(data);
          state.plugins[0].options=data.options;
          return r.fulfill({json:state});
        });
        await page.goto("http://uni-switch.test/?view=plugins");
        const installed = {en:"Installed",zh:"已安装","zh-TW":"已安裝",ja:"インストール済み",de:"Installiert"}[lang];
        await page.locator(":is(.lib-tabs .opt, .nav-s):visible",{hasText:installed}).click();
        const row=page.locator("#view-plugins .pm-row");
        assert.equal(await row.locator(".pm-chip.kind.pv").count(),0);
        const hook={en:"Request hook",zh:"请求 Hook","zh-TW":"請求 Hook",ja:"リクエストフック",de:"Anfrage-Hook"}[lang];
        assert.equal((await row.locator(".pm-chip.kind.mw").innerText()).trim(),hook);
        const line=row.locator(".pm-gateway");
        await line.waitFor();
        assert.match(await line.innerText(), /120/);
        if(lang!=="en") assert.doesNotMatch(await line.innerText(),/Context injected|calls,|bytes injected/);
        const options={en:"Options",zh:"选项","zh-TW":"選項",ja:"オプション",de:"Optionen"}[lang];
        await row.getByRole("button",{name:options,exact:true}).click();
        const textarea=row.locator(".pm-opts textarea");
        assert.deepEqual(JSON.parse(await textarea.inputValue()),state.plugins[0].optionsExample);
        await textarea.fill('{"serverUrl":"http://127.0.0.1:9000","gatewayTimeoutMs":6000}');
        const save={en:"Save",zh:"保存","zh-TW":"儲存",ja:"保存",de:"Speichern"}[lang];
        await row.locator(".pm-opts").getByRole("button",{name:save,exact:true}).click();
        await textarea.waitFor({state:"detached"});
        assert.deepEqual(posted,[{spec:SPEC,options:{serverUrl:"http://127.0.0.1:9000",gatewayTimeoutMs:6000}}]);
        assert.deepEqual(errors,[]);
        await page.close();
      });
    }
  });
}

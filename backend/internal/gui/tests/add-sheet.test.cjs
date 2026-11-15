// The reduced provider directory retains subscriptions, the relay, local servers
// and custom endpoints. No vendor API category is shipped. All API calls are mocked.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const assets = path.resolve(__dirname, "../assets");
const presets = [
 {id:"dieqiyun",name:"蝶祈云",icon:"dieqiyun",kind:"relay",chat:"https://relay.example/v1",anthropic:"https://relay.example",added:true},
 {id:"ollama",name:"Ollama",icon:"ollama",kind:"local",chat:"http://localhost:11434/v1",noKey:true},
 {id:"lmstudio",name:"LM Studio",icon:"lmstudio",kind:"local",chat:"http://localhost:1234/v1",noKey:true},
 {id:"omlx",name:"oMLX",icon:"omlx",kind:"local",chat:"http://localhost:8000/v1",noKey:true},
 {id:"mlx-serve",name:"MLX-Serve",icon:"mlx-serve",kind:"local",chat:"http://localhost:11234/v1",noKey:true},
];
const providers = [{id:"dieqiyun",name:"蝶祈云",icon:"dieqiyun",preset:"dieqiyun",models:[],agents:[],chat:"https://relay.example/v1",key:{set:true,masked:"sk-synthetic"}}];

function server(lang, list = providers) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    if (url.pathname === "/api/providers") return json({ providers: list, presets, excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/groups") return json({ groups: [], pools: [] });
    if (url.pathname === "/api/gateway/trace") return json({ routes: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}


for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
 test(engine + ": reduced add sheet", async t => {
  const browser=await (engine === "webkit" ? webkit.launch() : chromium.launch({channel:"chromium"}));
  t.after(()=>browser.close());
  for(const lang of ["en","zh","zh-TW","ja","de"]){
   await t.test(lang,async()=>{
    const context=await browser.newContext({viewport:{width:900,height:800},reducedMotion:"reduce"});
    try{
     const page=await context.newPage();
     const errors=[];page.on("pageerror",e=>errors.push(e.message));
     await page.route("**/*",server(lang));
     await page.goto("http://uni-switch.test/?view=providers");
     await page.locator("#addProvider").click();
     const sheet=page.locator("#addSheet"),search=sheet.locator(".find");
     await search.waitFor();
     const names=()=>sheet.locator(".tile .n").allTextContents();
     assert.ok((await names()).includes("蝶祈云"));
     for(const name of ["Ollama","LM Studio","oMLX","MLX-Serve"])assert.ok((await names()).includes(name));
     assert.equal(await sheet.locator('[data-pick="蝶祈云"] .have').count(),1);
     assert.doesNotMatch((await sheet.locator(".kind").allTextContents()).join(" "),/Vendors|模型厂商|模型廠商/);
     for(const removed of ["OpenAI","DeepSeek","Google Gemini","Bailian Decision","Ark Decision"]){
      await search.fill(removed);assert.deepEqual(await names(),[]);
      assert.equal(await sheet.locator(".custom-foot").count(),0);
      assert.equal(await sheet.locator("button.link").count(),1);
     }
     await search.fill("");
     const row=sheet.locator('[data-pick="Ollama"]');
     await row.focus();await page.keyboard.press("Enter");
     await page.locator("#modal .editor.new").waitFor();
     await page.keyboard.press("Escape");
     await page.locator("#modal").waitFor({state:"hidden"});
     assert.ok(await row.evaluate(el=>el===document.activeElement));
     await sheet.locator(".custom-foot button").click();
     await page.locator("#modal .editor.new").waitFor();
     await page.keyboard.press("Escape");
     await page.locator("#modal").waitFor({state:"hidden"});
     assert.deepEqual(errors,[]);
    }finally{await context.close();}
   });
  }
 });
}

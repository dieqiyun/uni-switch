// Exercise the compiled provider directory in an isolated home; never use live credentials.
import {spawnSync} from "node:child_process";
import {mkdirSync,writeFileSync} from "node:fs";
import {resolve} from "node:path";
import assert from "node:assert/strict";
import {chromium,webkit} from "playwright";
import {startIsolated} from "./qa-source-core.mjs";

const root=resolve(import.meta.dirname,"..");
const built=spawnSync(process.execPath,["scripts/build-go.mjs","--qa"],{cwd:root,encoding:"utf8",windowsHide:true});
if(built.status!==0)throw new Error(built.stderr||"QA core build failed");
const suffix=process.platform==="win32"?".exe":"";
const platform={win32:"pc-windows-msvc",linux:"unknown-linux-gnu",darwin:"apple-darwin"}[process.platform];
const arch=process.arch==="arm64"?"aarch64":"x86_64";
const app=await startIsolated(resolve(root,`src-tauri/binaries/uni-switch-core-${arch}-${platform}${suffix}`));
const checks=[];
mkdirSync(resolve(root,".qa"),{recursive:true});
try{
 await app.wait(()=>!!app.ready());
 const link=app.ready().url;
 for(const [engine,launcher] of [["chromium",chromium],["webkit",webkit]]){
  const browser=await launcher.launch(engine==="chromium"?{channel:"chromium"}:{});
  try{
   for(const language of ["en","zh","zh-TW","ja","de"]){
    const context=await browser.newContext({viewport:{width:1120,height:780},reducedMotion:"reduce"});
    const page=await context.newPage();
    page.setDefaultTimeout(5000);
    const errors=[];
    page.on("pageerror",e=>errors.push(e.message));
    // Capture submitted endpoint choices without storing providers or contacting them.
    await page.route("**/api/provider/save",route=>route.fulfill({status:400,contentType:"application/json",body:JSON.stringify({error:"QA capture: no provider was saved"})}));
    // Hold both the fresh and cached provider reads until the user has
    // clicked Add. The old local toolbar threw on a null provider list.
    let release;
    const held=new Promise(r=>release=r);
    await page.route(/\/api\/providers(?:\?|$)/,async route=>{await held;await route.continue();});
    try{
     const url=new URL(link);url.searchParams.set("locale",language);
     await page.goto(url.href);
     await page.locator('.uni-add').click();
     release();
     const editor=page.locator(".editor");
     await editor.waitFor();
     assert.equal(await page.locator('#addSheet').isVisible(),false,'the primary action opens Dieqiyun directly');
     assert.equal(await editor.locator(".ehead > b").innerText(),"蝶祈云API");
     assert.deepEqual(await editor.locator(".eps .ep code").allTextContents(),[
      "https://cfapi.dieqiyun.top/v1",
      "https://cfapi.dieqiyun.top/v1",
      "https://cfapi.dieqiyun.top",
     ]);
     assert.equal(await editor.locator(".site").getAttribute("data-url"),"https://www.dieqiyun.top");
     const endpoint=editor.locator(".service-endpoint");
     const requestEndpoint=editor.locator('.request-endpoint');
     assert.equal(await requestEndpoint.isVisible(),true);
     assert.equal(await requestEndpoint.inputValue(),'');
     assert.equal(await requestEndpoint.evaluate(e=>!!e.closest('.advanced')),false);
     assert.equal(await endpoint.locator('.service-endpoint-url').innerText(),'https://cfapi.dieqiyun.top/v1','the selected URL is visible without opening the menu');
     await endpoint.focus();
     await endpoint.press("Enter");
     assert.equal(await page.getByRole("menuitemradio").count(),3);
     assert.doesNotMatch(await page.getByRole('menuitemradio').allTextContents().then(x=>x.join(' ')),/cfimg\.dieqiyun\.top/,'image generation has a separate setup');
     assert.equal(await page.getByRole("menuitemradio").first().getAttribute("aria-checked"),"true");
     await page.keyboard.press("Escape");
     assert.ok(await endpoint.evaluate(e=>e===document.activeElement),"Escape restores endpoint focus");
     assert.equal(await endpoint.getAttribute("aria-expanded"),"false");
     for(const [index,host] of [[1,"haapi"],[2,"hw"],[0,"cfapi"]]){
      await endpoint.click();
      const choice=page.getByRole("menuitemradio").nth(index);
      assert.ok((await choice.innerText()).includes(`https://${host}.dieqiyun.top`));
      await choice.click();
      const base=`https://${host}.dieqiyun.top`;
      assert.equal(await endpoint.locator('.service-endpoint-url').innerText(),base+'/v1');
      assert.ok((await endpoint.getAttribute('aria-label')).includes(base+'/v1'));
      assert.deepEqual(await editor.locator(".eps .ep code").allTextContents(),[base+"/v1",base+"/v1",base]);
      assert.ok(await endpoint.evaluate(e=>e===document.activeElement),"selection keeps keyboard focus");
      const response=page.waitForResponse(r=>r.url().endsWith("/api/provider/save"));
      await editor.locator(":scope > .bar .primary").click();
      const body=(await response).request().postDataJSON();
      assert.equal(body.chat,base+"/v1");
      assert.equal(body.responses,base+"/v1");
      assert.equal(body.anthropic,base);
      assert.equal(body.endpointAPI,'auto');
      await editor.locator(".editor-error").waitFor();
     }
     for(const api of ['chat','responses','anthropic']){
      await requestEndpoint.selectOption(api);
      const selected='https://cfapi.dieqiyun.top'+(api==='anthropic'?'':'/v1');
      assert.equal(await endpoint.locator('.service-endpoint-url').innerText(),selected);
      assert.deepEqual(await editor.locator('.eps .ep code').allTextContents(),[selected]);
      const response=page.waitForResponse(r=>r.url().endsWith('/api/provider/save'));
      await editor.locator(':scope > .bar .primary').click();
      const body=(await response).request().postDataJSON();
      assert.equal(body.endpointAPI,api);
      for(const key of ['chat','responses','anthropic'])assert.equal(body[key],api===key?selected:'');
      await editor.locator('.editor-error').waitFor();
     }
     await requestEndpoint.selectOption('');
     assert.equal(await editor.locator('.eps .ep').count(),3);
     if(engine==="chromium"&&language==="zh"){
      await endpoint.click();
      await page.screenshot({path:resolve(root,".qa/dieqiyun-endpoints.png")});
      await page.keyboard.press("Escape");
      await page.setViewportSize({width:760,height:600});
      await endpoint.click();
      assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
      await page.screenshot({path:resolve(root,".qa/dieqiyun-endpoints-minimum.png")});
      await page.keyboard.press("Escape");
      await page.setViewportSize({width:1120,height:780});
     }
     await editor.locator(".bar button").first().click();
     await page.locator(".action-confirm .primary").click();
     await page.locator("#modal").waitFor({state:"hidden"});
     assert.ok(await page.locator('.uni-custom-add').isVisible(),"the secondary custom provider action remains available");
     const logo=await context.request.get(new URL('/icons/dieqiyun.png',link).href);assert.ok(logo.ok());
     const directory=await (await context.request.get(new URL("/api/providers",link).href)).json();
     assert.deepEqual(directory.presets.filter(p=>p.kind==="relay").map(p=>p.id),["dieqiyun"]);
     assert.equal(directory.presets.filter(p=>p.kind==="vendor").length,0);
     assert.deepEqual(directory.presets.map(p=>p.id),["dieqiyun","ollama","lmstudio","omlx","mlx-serve"]);
     assert.equal(directory.providers.length,0,"cancel must save nothing");
     if(engine==="chromium"&&language==="zh"){
      await page.screenshot({path:resolve(root,".qa/directory-relay-final.png")});
      await page.setViewportSize({width:760,height:600});
      assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
      await page.screenshot({path:resolve(root,".qa/directory-relay-minimum.png")});
     }
     assert.deepEqual(errors,[],`${engine} ${language}: script errors`);
     checks.push({engine,language,quickAdd:true,onlyRelay:"dieqiyun",endpointChoices:3,selectedURLVisible:true,submittedURLs:true,imageEndpointAbsent:true,keyboard:true,vendorPresetsRemoved:true,logo:true,website:true,cancelWithoutSave:true,removedRelaySearch:true});
    }finally{release();await context.close();}
   }
  }finally{await browser.close();}
 }
 writeFileSync(resolve(root,".qa/directory-ui-checks.json"),JSON.stringify(checks,null,2)+"\n","utf8");
 console.log(JSON.stringify({checks:checks.length,engines:["chromium","webkit"],languages:["en","zh","zh-TW","ja","de"],passed:true}));
}finally{await app.stop();}

// The compiled UI runs in an isolated HOME; model and client responses are synthetic.
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {resolve} from 'node:path';
import {writeFileSync} from 'node:fs';
import {chromium,webkit} from 'playwright';
import {startIsolated} from './qa-source-core.mjs';
const root=resolve(import.meta.dirname,'..');
const build=spawnSync(process.execPath,['scripts/build-go.mjs','--qa'],{cwd:root,encoding:'utf8',windowsHide:true});
if(build.status!==0)throw new Error(build.stderr||'QA core build failed');
const platform={win32:'pc-windows-msvc',linux:'unknown-linux-gnu',darwin:'apple-darwin'}[process.platform];
const arch=process.arch==='arm64'?'aarch64':'x86_64';
const app=await startIsolated(resolve(root,`src-tauri/binaries/uni-switch-core-${arch}-${platform}${process.platform==='win32'?'.exe':''}`));
const checks=[];
try {
 await app.wait(()=>!!app.ready());
 for(const [engine,launcher] of [['chromium',chromium],['webkit',webkit]]){
  const browser=await launcher.launch(engine==='chromium'?{channel:'chromium'}:{});
  try {for(const language of ['en','zh','zh-TW','ja','de']){
   const context=await browser.newContext({viewport:{width:1120,height:820},reducedMotion:'reduce'});
   try {
    const page=await context.newPage();page.setDefaultTimeout(8000);
    const url=new URL(app.ready().url);url.searchParams.set('locale',language);
    await context.request.get(url.href);
    const base=await (await context.request.get(new URL('/api/settings',url).href)).json();
    let settings={...base,imageGen:'',imageGenShown:'',imageGenAuto:'demo/image-one',imageGenMissing:'',imageGenModels:[{id:'demo/image-one',name:'Image One',providerName:'Demo Images',icon:'generic'}]};
    let library={agents:['codex','claude'].map(id=>({id,name:id,mcp:'/isolated/'+id+'.json'})),servers:[],problems:[]};
    const writes=[],errors=[];let failRead=false,failWrite=false;
    let imageSource={configured:false,baseUrl:'https://cfimg.dieqiyun.top/v1',models:[]},modelFailure='',heldRoute;
    const imageRequests=[];
    page.on('pageerror',e=>errors.push(e.message));
    await page.route('**/api/image-provider',async route=>{
     if(route.request().method()==='GET')return route.fulfill({json:imageSource});
     const body=route.request().postDataJSON();imageRequests.push(body);
     if(body.key==='qa-held-key'){heldRoute=route;return;}
     if(modelFailure)return route.fulfill({status:400,json:{error:modelFailure}});
     const model={id:'dieqiyun-image/gpt-image-1',name:'Dieqi Image',provider:'dieqiyun-image',providerName:'Dieqi Images',icon:'generic'};
     imageSource={...imageSource,configured:true,models:[model]};
     settings={...settings,imageGenModels:[...(settings.imageGenModels||[]).filter(m=>m.id!==model.id),model],imageGenAuto:model.id};
     await route.fulfill({json:imageSource});
    });
    await page.route(/\/api\/settings(?:\?|$)/,async route=>{
     if(route.request().method()==='POST'){
      const body=route.request().postDataJSON();writes.push({path:'settings',body});
      settings={...settings,...body,imageGenShown:body.imageGen||''};
     }
     await route.fulfill({json:settings});
    });
    await page.route(/\/api\/library(?:\?|$)/,route=>failRead?route.fulfill({status:500,json:{error:'synthetic configuration read failure'}}):route.fulfill({json:library}));
    for(const action of ['market/server','servers/agents'])await page.route('**/api/library/'+action,async route=>{
     const body=route.request().postDataJSON();writes.push({path:action,body});
     if(failWrite)return route.fulfill({status:500,json:{error:'synthetic configuration write failure'}});
     library={...library,servers:[{name:'uni-switch-image',agents:body.agents}]};
     await route.fulfill({json:library});
    });
    await page.goto(url.href);
    const entry=page.locator('.uni-image-open'), toggle=page.locator('.uni-image-toggle');
    await entry.waitFor();assert.equal(await entry.getAttribute('aria-expanded'),'false');
    assert.equal(await page.locator('#imageGenRow,#imageTimeoutRow,#imageMcpRow').count(),0,'image generation has no duplicate settings-page entry');
    await page.waitForFunction(()=>{const b=document.querySelector('.uni-image-open');return b&&getComputedStyle(b).cursor==='pointer';});
    await entry.locator('strong').click();
    await toggle.waitFor();assert.equal(writes.length,0,'opening writes no client configuration');
    assert.equal(await entry.getAttribute('aria-expanded'),'true');
    assert.equal(await entry.locator('.uni-disclosure-action').innerText(),await page.evaluate(()=>t('Collapse')));
    await entry.locator('.hint').click();
    assert.equal(await entry.getAttribute('aria-expanded'),'false','clicking the header description also collapses');
    await entry.focus();await entry.press('Enter');await toggle.waitFor();
    assert.equal(await toggle.getAttribute('aria-checked'),'false');
    assert.equal(await toggle.locator('.uni-switch-track').count(),1,'a visible switch distinguishes state from plain text');
    assert.equal(await page.locator('#uniImageSetup .lib-way').count(),0,'no Library detour');
    assert.equal(await page.locator('#uniImageTimeoutRow').isVisible(),false);
    await page.locator('[data-image-control="model"]').click();
    await page.locator('#list > li:not(.reset)').filter({hasText:'Image One'}).click();
    await page.waitForFunction(()=>prefs.imageGen==='demo/image-one');
    assert.ok(writes.some(w=>w.path==='settings'&&w.body.imageGen==='demo/image-one'));
    await page.locator('.uni-image-advanced > summary').click();
    await page.locator('#uniImageTimeoutRow button').nth(1).focus();
    await page.locator('#uniImageTimeoutRow button').nth(1).press('Enter');
    await page.waitForFunction(()=>prefs.imageTimeout===10);
    assert.equal(await page.locator('.uni-image-advanced').evaluate(e=>e.open),true);
    assert.equal(await page.evaluate(()=>document.activeElement?.dataset.imageControl),'timeout-1');
    await page.locator('.uni-image-advanced > summary').click();
    await toggle.click();
    await page.waitForFunction(()=>document.querySelector('.uni-image-toggle')?.getAttribute('aria-checked')==='true');
    assert.deepEqual(writes.find(w=>w.path==='market/server').body.agents,['codex']);
    await page.locator('[data-uni-client="claude"]').click();await toggle.waitFor();
    assert.equal(await toggle.getAttribute('aria-checked'),'false');
    await toggle.click();await page.waitForFunction(()=>document.querySelector('.uni-image-toggle')?.getAttribute('aria-checked')==='true');
    assert.deepEqual(library.servers[0].agents,['codex','claude']);
    await page.locator('[data-uni-client="codex"]').click();await toggle.waitFor();
    await toggle.click();await page.waitForFunction(()=>document.querySelector('.uni-image-toggle')?.getAttribute('aria-checked')==='false');
    assert.deepEqual(library.servers[0].agents,['claude'],'changing one client preserves another');
    failWrite=true;await toggle.click();await page.locator('.uni-image-error').waitFor();
    assert.equal(await toggle.getAttribute('aria-checked'),'false','failed writes never show enabled');
    failWrite=false;
    // A failed read is unknown; it must not offer a guessed configuration to save.
    await entry.click();failRead=true;await entry.click();await page.locator('.uni-image-error').waitFor();
    assert.equal(await toggle.isDisabled(),true);
    failRead=false;await page.locator('.uni-image-error button').click();await page.locator('.uni-image-error').waitFor({state:'detached'});
    if(engine==='chromium'&&language==='zh'){
     await page.screenshot({path:resolve(root,'.qa/client-image-setup.png')});
     await page.setViewportSize({width:760,height:600});
     assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
     await page.screenshot({path:resolve(root,'.qa/client-image-setup-minimum.png')});
    }
    await entry.click();assert.equal(await entry.getAttribute('aria-expanded'),'false');
    settings={...settings,imageGen:'',imageGenShown:'',imageGenAuto:'',imageGenModels:[]};
    await entry.click();await toggle.waitFor();assert.equal(await toggle.isDisabled(),true,'no image model explains why injection is unavailable');
    const key=page.locator('#uniImageKey'),fetchModels=page.locator('[data-image-control="fetch"]'),edit=page.locator('[data-image-control="edit"]'),cancelKey=page.locator('[data-image-control="cancel-key"]');
    assert.equal(await page.locator('#uniImageSetup input:not([type="password"])').count(),0,'the default URL needs no input');
    assert.equal(await key.getAttribute('type'),'password');
    await key.fill('qa-image-key');
    await page.waitForFunction(()=>document.querySelector('.uni-image-configured')&&prefs.imageGen==='dieqiyun-image/gpt-image-1');
    assert.equal(await key.count(),0,'a saved setup offers Edit instead of another key/add form');
    assert.equal(await fetchModels.count(),0,'model refresh is part of editing an existing setup');
    assert.equal(imageRequests.length,1,'typing triggers one debounced model request');
    assert.deepEqual(imageRequests[0],{key:'qa-image-key'},'setup sends only the key');
    assert.equal(await toggle.isDisabled(),false,'a fetched image model enables setup');
    const assignmentsBefore=writes.filter(w=>w.path!=='settings').length;
    await edit.focus();await edit.press('Enter');
    assert.equal(await key.inputValue(),'','editing never returns a saved key');
    assert.equal(await page.evaluate(()=>document.activeElement?.id),'uniImageKey');
    await fetchModels.click();
    await edit.waitFor();
    assert.deepEqual(imageRequests.at(-1),{},'refresh uses the saved key');
    assert.equal(writes.filter(w=>w.path!=='settings').length,assignmentsBefore,'loading models never changes client injection');
    modelFailure='image_models_failed: 401 Unauthorized (invalid API key)';
    await edit.click();
    await key.fill('qa-bad-key');await key.press('Enter');
    await page.locator('#uniImageKey[aria-invalid="true"]').waitFor();
    assert.equal(settings.imageGen,'dieqiyun-image/gpt-image-1','a bad key retains the selected model');
    assert.equal(await page.locator('#uniImageKeyStatus details').count(),1);
    const beforeCancel=imageRequests.length;
    await cancelKey.click();await edit.waitFor();
    assert.equal(imageRequests.length,beforeCancel,'canceling edits does not save or create a provider');
    assert.equal(await page.evaluate(()=>document.activeElement?.dataset.imageControl),'edit');
    await edit.click();
    modelFailure='image_models_empty: this key lists no image models';
    await key.fill('qa-no-images');await key.press('Enter');
    await page.locator('#uniImageKeyStatus').filter({hasText:/No image models|没有可用|沒有可用|ありません|keine Bildmodelle/}).waitFor();
    modelFailure='';
    // Replace an in-flight key and deliver its old response after the new one.
    await key.fill('qa-held-key');await key.press('Enter');
    await page.waitForFunction(()=>document.querySelector('.uni-image-source')?.getAttribute('aria-busy')==='true');
    await key.fill('qa-newest-key');await key.press('Enter');
    await edit.waitFor();
    assert.ok(heldRoute,'first request was held');
    await heldRoute.fulfill({json:{configured:true,baseUrl:'https://old.invalid/v1',models:[]}}).catch(()=>{});
    await page.evaluate(()=>window.refreshClientImageSetup());
    await edit.click();
    assert.ok((await page.locator('.uni-image-url').textContent()).includes('cfimg.dieqiyun.top'),'late responses cannot replace current setup');
    await cancelKey.click();
    await entry.click();await entry.click();await toggle.waitFor();
    assert.equal(await key.count(),0,'reopening a saved setup remains edit-only');
    await edit.click();
    assert.equal(await key.inputValue(),'','reopening never returns the stored key');
    assert.ok(await key.getAttribute('placeholder'));
    await cancelKey.click();
    if(engine==='chromium'&&language==='zh')await page.screenshot({path:resolve(root,'.qa/image-setup-edit-only.png')});
    await page.locator('[data-uni-client="pi"]').click();await toggle.waitFor();assert.equal(await toggle.isDisabled(),true,'unsupported clients cannot be injected');
    assert.deepEqual(errors,[],`${engine}/${language}: script errors`);
    checks.push({engine,language,directEntry:true,sharedModel:true,clientOnlyInjection:true,preservesOthers:true,failedRead:true,failedWrite:true,keyOnlySetup:true,automaticModels:true,refreshSavedKey:true,failedModels:true,staleResponse:true,editOnlyAfterSave:true,cancelEdits:true,singleSettingsEntry:true});
   } finally {await context.close();}
  }} finally {await browser.close();}
 }
 writeFileSync(resolve(root,'.qa/client-image-setup-checks.json'),JSON.stringify(checks,null,2)+'\n','utf8');
 console.log(JSON.stringify({checks:checks.length,passed:true}));
} finally {await app.stop();}

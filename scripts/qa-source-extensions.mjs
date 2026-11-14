// Verify the shipped management surface in a synthetic home, using no accounts.
import {spawnSync} from 'node:child_process';
import {mkdirSync,writeFileSync,readFileSync} from 'node:fs';
import {resolve} from 'node:path';
import assert from 'node:assert/strict';
import {chromium,webkit} from 'playwright';
import {startIsolated} from './qa-source-core.mjs';
const root=resolve(import.meta.dirname,'..');
const result=spawnSync(process.execPath,['scripts/build-go.mjs','--qa'],{cwd:root,encoding:'utf8',windowsHide:true});
if(result.status!==0)throw new Error(result.stderr||'QA build failed');
const platform={win32:'pc-windows-msvc',linux:'unknown-linux-gnu',darwin:'apple-darwin'}[process.platform];
const arch=process.arch==='arm64'?'aarch64':'x86_64';
const binary=resolve(root,`src-tauri/binaries/uni-switch-core-${arch}-${platform}${process.platform==='win32'?'.exe':''}`);
const app=await startIsolated(binary);const checks=[];
try{
 await app.wait(()=>!!app.ready());
 const link=app.ready().url;
 for(const [engine,launcher] of [['chromium',chromium],['webkit',webkit]]){
  const browser=await launcher.launch(engine==='chromium'?{channel:'chromium'}:{});
  try{for(const locale of ['en','zh','zh-TW','ja','de']){
   const context=await browser.newContext({viewport:{width:1120,height:780},reducedMotion:'reduce'});
   const page=await context.newPage();page.setDefaultTimeout(10000);const errors=[],extensionRequests=[];
   page.on('pageerror',e=>errors.push(e.message));
   page.on('request',r=>{if(/\/api\/(plugins|plugin-|settings\/plugin|usage\/plugin)/.test(r.url()))extensionRequests.push(r.url());});
   try{
    const url=new URL(link);url.searchParams.set('locale',locale);url.searchParams.set('view','plugins');
    await page.goto(url.href);await page.locator('.uni-add').waitFor();
    assert.equal(await page.locator('[data-view="plugins"],#view-plugins,#setTab-plugins,#warmTab-plugins').count(),0);
    for(const path of ['/api/plugins','/api/plugins/builtin','/api/plugin-signin','/api/plugin-pages','/api/plugin-panels','/api/settings/plugin-option','/api/settings/plugin-checkin','/api/usage/plugin-checkin','/plugins.js','/pluginpages.js','/plugins.css','/_uni-switch/plugin/old/index.html']){
     const response=await context.request.get(new URL(path,link).href);assert.equal(response.status(),404,path);
    }
    await page.locator('.uni-add').click();await page.locator('.editor').waitFor();
    assert.equal(await page.locator('#addSheet .more-plugins').count(),0);
    assert.ok((await page.locator('.editor').innerText()).includes('https://cfapi.dieqiyun.top/v1'));
    await page.locator('.editor .bar button').first().click();
    await page.keyboard.press('Escape');
    // Core pages are the statically compiled modules, with no install step.
    for(const view of ['library','sessions']){
     const button=page.locator(`#nav button[data-view="${view}"]`);
     {
      await button.click();await page.locator(`#view-${view}`).waitFor({state:'visible'});
     }
    }
    await page.locator('#prefs').click();await page.locator('#view-settings').waitFor({state:'visible'});
    assert.equal(await page.locator('#view-settings .plugin-panel,#view-settings .plugin-slot').count(),0);
    if(engine==='chromium'&&locale==='zh'){
     await page.screenshot({path:resolve(root,'.qa/extensions-settings.png')});
     await page.locator('[data-uni-client="accounts"]').click();
     await page.screenshot({path:resolve(root,'.qa/extensions-providers.png')});
     await page.setViewportSize({width:760,height:600});
     assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
     await page.screenshot({path:resolve(root,'.qa/extensions-minimum.png')});
    }
    assert.deepEqual(errors,[],`${engine} ${locale}: script errors`);
    assert.deepEqual(extensionRequests,[],`${engine} ${locale}: obsolete extension traffic`);
    checks.push({engine,locale,removedEndpoints:true,addProvider:true,corePages:true,settings:true,oldLinkFallback:true});
   }finally{await context.close();}
  }}finally{await browser.close();}
 }
 assert.equal(readFileSync(resolve(app.home,'.codex/config.toml'),'utf8'),'# isolated QA; disconnected client\n');
 mkdirSync(resolve(root,'.qa'),{recursive:true});writeFileSync(resolve(root,'.qa/extensions-ui-checks.json'),JSON.stringify(checks,null,2)+'\n','utf8');
 console.log(JSON.stringify({checks:checks.length,passed:true,isolatedClientUnchanged:true}));
}finally{await app.stop();}

// Verify the removed feature through the compiled core and both browser engines.
import {spawnSync} from 'node:child_process';
import {existsSync,mkdirSync,readFileSync,writeFileSync} from 'node:fs';
import {resolve} from 'node:path';
import assert from 'node:assert/strict';
import {chromium,webkit} from 'playwright';
import {startIsolated} from './qa-source-core.mjs';

const root=resolve(import.meta.dirname,'..');
const result=spawnSync(process.execPath,['scripts/build-go.mjs','--qa'],{cwd:root,encoding:'utf8',windowsHide:true});
if(result.status!==0)throw new Error(result.stderr||'QA core build failed');
const platform={win32:'pc-windows-msvc',linux:'unknown-linux-gnu',darwin:'apple-darwin'}[process.platform];
const arch=process.arch==='arm64'?'aarch64':'x86_64';
const binary=resolve(root,`src-tauri/binaries/uni-switch-core-${arch}-${platform}${process.platform==='win32'?'.exe':''}`);
const app=await startIsolated(binary),checks=[];
const removedPaths=['/api/davsync','/api/davsync/save','/api/davsync/now','/api/davsync/restore','/api/davsync/auto','/api/davsync/undo','/api/davsync/upload','/api/backup/export','/api/backup/import','/api/autobackup','/api/autobackup/set','/api/autobackup/now','/api/autobackup/restore','/api/agents/here','/syncfiles.js','/autobackup.js'];
try{
 await app.wait(()=>!!app.ready());const link=app.ready().url;
 for(const [engine,launcher]of [['chromium',chromium],['webkit',webkit]]){
  const browser=await launcher.launch(engine==='chromium'?{channel:'chromium'}:{});
  try{for(const locale of ['en','zh','zh-TW','ja','de']){
   const context=await browser.newContext({viewport:{width:1000,height:780},reducedMotion:'reduce'});
   await context.addInitScript(()=>localStorage.setItem('uni-switch.settingsTab','sync'));
   const page=await context.newPage();page.setDefaultTimeout(10000);
   const errors=[],obsoleteRequests=[];
   page.on('pageerror',e=>errors.push(e.message));
   page.on('request',r=>{if(/\/(?:api\/(?:davsync|autobackup|backup\/|agents\/here)|(?:syncfiles|autobackup)\.js)/.test(r.url()))obsoleteRequests.push(r.url());});
   try{
    const url=new URL(link);url.searchParams.set('locale',locale);url.searchParams.set('view','settings');url.searchParams.set('tab','sync');
    await page.goto(url.href);await page.locator('#view-settings').waitFor({state:'visible'});
    await page.locator('#setTab-general[aria-selected="true"]').waitFor({state:'attached'});
    assert.equal(new URL(page.url()).searchParams.get('tab'),'general');
    assert.equal(await page.locator('#setTab-sync,#setPage-sync,#syncList').count(),0);
    for(const path of removedPaths){
     for(const method of ['GET','POST']){
      const response=await context.request.fetch(new URL(path,link).href,{method,...(method==='POST'?{data:{}}:{})});
      assert.equal(response.status(),404,`${method} ${path}`);
     }
    }
    for(const tab of ['general','usage','network','models','privacy','about']){
     const button=page.locator(`#setTab-${tab}:visible,.nav-s[data-of="setTab-${tab}"]:visible`);if(await button.count()){
      await button.click();await page.locator('#setPage-'+tab).waitFor({state:'visible'});
     }
    }
    await page.locator('#setTab-general:visible,.nav-s[data-of="setTab-general"]:visible').click();
    await page.locator('#setQ').fill(locale==='zh'?'同步与备份':'Sync and backup');
    assert.equal(await page.locator('#setPage-sync').count(),0);
    await page.locator('#setQ').fill('');
    for(const view of ['library','sessions']){
     await page.locator(`#nav button[data-view="${view}"]`).click();
     await page.locator(`#view-${view}`).waitFor({state:'visible'});
     if(view==='library')assert.equal(await page.locator('.lib-backups,.lib-localbox').count(),0);
    }
    await page.locator('#prefs').click();await page.locator('#view-settings').waitFor({state:'visible'});
    await page.locator('#setTab-general:visible,.nav-s[data-of="setTab-general"]:visible').click();
    if(engine==='chromium'&&locale==='zh'){
     mkdirSync(resolve(root,'.qa'),{recursive:true});
     await page.screenshot({path:resolve(root,'.qa/sync-removal-settings.png')});
     await page.setViewportSize({width:760,height:600});
     assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
     await page.screenshot({path:resolve(root,'.qa/sync-removal-minimum.png')});
    }
    assert.deepEqual(errors,[],`${engine} ${locale}: page errors`);
    assert.deepEqual(obsoleteRequests,[],`${engine} ${locale}: removed feature was requested`);
    checks.push({engine,locale,oldLinkFallback:true,settingsTabs:true,removedEndpoints:true,noBackupManagement:true,corePages:true});
   }finally{await context.close();}
  }}finally{await browser.close();}
 }
 assert.equal(existsSync(resolve(app.home,'uni-switch/auto-backups')),false);
 assert.equal(existsSync(resolve(app.home,'uni-switch/sync-state.json')),false);
 assert.equal(readFileSync(resolve(app.home,'.codex/config.toml'),'utf8'),'# isolated QA; disconnected client\n');
 writeFileSync(resolve(root,'.qa/sync-removal-ui-checks.json'),JSON.stringify(checks,null,2)+'\n','utf8');
 console.log(JSON.stringify({checks:checks.length,passed:true,noAutomaticBackup:true,isolatedClientUnchanged:true}));
}finally{await app.stop();}

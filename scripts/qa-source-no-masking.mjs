// Check the compiled application using isolated settings and synthetic data.
import {spawnSync} from 'node:child_process';
import {readFileSync,writeFileSync,mkdirSync} from 'node:fs';
import {resolve} from 'node:path';
import assert from 'node:assert/strict';
import {chromium,webkit} from 'playwright';
import {startIsolated} from './qa-source-core.mjs';

const root=resolve(import.meta.dirname,'..');
const build=spawnSync(process.execPath,['scripts/build-go.mjs','--qa'],{cwd:root,encoding:'utf8',windowsHide:true});
if(build.status!==0)throw new Error(build.stderr||'QA core build failed');
const platform={win32:'pc-windows-msvc',linux:'unknown-linux-gnu',darwin:'apple-darwin'}[process.platform];
const arch=process.arch==='arm64'?'aarch64':'x86_64';
const binary=resolve(root,`src-tauri/binaries/uni-switch-core-${arch}-${platform}${process.platform==='win32'?'.exe':''}`);
const app=await startIsolated(binary),checks=[];
try{
 await app.wait(()=>!!app.ready());const link=app.ready().url;
 const settingsPath=resolve(app.home,'uni-switch/settings.json');
 const settings=JSON.parse(readFileSync(settingsPath,'utf8'));
 Object.assign(settings,{redact:true,redactPersonal:true,redactWords:['Nightjar'],redactModel:true,redactActions:{API_KEY:'block'},maskAlert:true});
 writeFileSync(settingsPath,JSON.stringify(settings),'utf8');
 for(const [engine,launcher]of [['chromium',chromium],['webkit',webkit]]){
  const browser=await launcher.launch(engine==='chromium'?{channel:'chromium'}:{});
  try{for(const locale of ['en','zh','zh-TW','ja','de']){
   const context=await browser.newContext({viewport:{width:1000,height:780},reducedMotion:'reduce'});
   await context.addInitScript(()=>localStorage.setItem('uni-switch.maskEmails','0'));
   const page=await context.newPage();page.setDefaultTimeout(10000);page.setDefaultNavigationTimeout(60000);
   const errors=[],retiredCalls=[];
   page.on('pageerror',e=>errors.push(e.message));
   page.on('request',r=>{if(/\/api\/(?:masking|privacy-model)(?:[/?]|$)|\/api\/settings\/redact-/.test(r.url()))retiredCalls.push(r.url());});
   try{
    const url=new URL(link);url.searchParams.set('locale',locale);url.searchParams.set('view','settings');url.searchParams.set('tab','privacy');
    await page.goto(url.href);
    await page.locator('#setPage-privacy').waitFor({state:'visible'});
    await page.waitForFunction(()=>document.querySelectorAll('#shareList .row').length>=2);
    assert.equal(await page.locator('[data-view="masking"],#redactList,#findList,#maskActList,#maskTryList,#maskAlertRow,#privacyModelRow,.mask-badge').count(),0);
    assert.equal(await page.locator('#nav [data-view="plugins"]').count(),0);
    assert.equal(await page.locator('#discoveryList,#signInOpenList,#sessionReadsList').count(),3);
    assert.equal(await page.evaluate(()=>hideAccounts.on()),false,'legacy personal-data switch must not hide screen accounts');
    const state=await (await context.request.get(new URL('/api/settings',link).href)).json();
    for(const key of Object.keys(state))assert.ok(!/^redact|^(?:maskAlert|maskRouteModels)$/.test(key),key);
    const before=!!state.noStats;
    // Exercise an ordinary remaining Privacy preference through its real UI.
    await page.locator('#shareList .row').nth(1).locator('.opt').nth(before?1:0).evaluate(b=>b.click());
    await page.waitForFunction(before=>!!prefs?.noStats!==before,before);
    const saved=JSON.parse(readFileSync(settingsPath,'utf8'));
    assert.equal(!!saved.noStats,!before);
    for(const key of Object.keys(saved))assert.ok(!/^redact|^maskAlert$/.test(key),key);
    // The screenshot-only account control remains an explicit local choice.
    await page.locator('#shareList .row').first().locator('.opt').nth(1).evaluate(b=>b.click());
    assert.equal(await page.evaluate(()=>hideAccounts.on()),true);
    if(engine==='chromium'&&locale==='zh'){
     mkdirSync(resolve(root,'.qa'),{recursive:true});
     await page.screenshot({path:resolve(root,'.qa/no-masking-privacy.png')});
     await page.setViewportSize({width:760,height:600});
     assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
     await page.screenshot({path:resolve(root,'.qa/no-masking-privacy-compact.png')});
    }
    assert.deepEqual(errors,[],`${engine} ${locale}: script errors`);
    assert.deepEqual(retiredCalls,[],`${engine} ${locale}: retired API requested`);
    checks.push({engine,locale,retiredUIAbsent:true,oldSettingsIgnored:true,sharingSaved:true,manualAccountDisplayWorks:true});
   }finally{await context.close();}
  }}finally{await browser.close();}
 }
 assert.equal(readFileSync(resolve(app.home,'.codex/config.toml'),'utf8'),'# isolated QA; disconnected client\n');
 writeFileSync(resolve(root,'.qa/no-masking-ui-checks.json'),JSON.stringify(checks,null,2)+'\n','utf8');
 console.log(JSON.stringify({checks:checks.length,passed:true,isolatedClientUnchanged:true}));
}finally{await app.stop();}

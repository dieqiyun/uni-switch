import {spawnSync} from 'node:child_process';
import {readFileSync,writeFileSync,mkdirSync} from 'node:fs';
import {resolve} from 'node:path';
import assert from 'node:assert/strict';
import {chromium,webkit} from 'playwright';
import {startIsolated} from './qa-source-core.mjs';

const root=resolve(import.meta.dirname,'..');
mkdirSync(resolve(root,'.qa'),{recursive:true});
const build=spawnSync(process.execPath,['scripts/build-go.mjs','--qa'],{cwd:root,encoding:'utf8',windowsHide:true});
if(build.status!==0)throw Error(build.stderr||build.stdout);
const platform={win32:'pc-windows-msvc',linux:'unknown-linux-gnu',darwin:'apple-darwin'}[process.platform];
const binary=resolve(root,`src-tauri/binaries/uni-switch-core-${process.arch==='arm64'?'aarch64':'x86_64'}-${platform}${process.platform==='win32'?'.exe':''}`);
const app=await startIsolated(binary),checks=[];
const at=new Date(Date.now()-3600e3).toISOString();
const session={agent:'codex',id:'qa-session-no-billing',cwd:'D:/qa/project',title:'Synthetic session for display verification',start:at,last:at,models:[{model:'qa-model',input:1234567,output:76543,cost:9876.54,priced:true}],input:1234567,output:76543,cache_read:888888,cache_write:777777,cost:9876.54,unpriced:2,messages:6,files:1,path:'~/synthetic/session.jsonl',size:4096,resume:'codex resume qa-session-no-billing',deletable:true,transcript:true};
try{
 await app.wait(()=>!!app.ready());
 const link=app.ready().url;
 for(const [engine,launcher] of [['chromium',chromium],['webkit',webkit]]){
  const browser=await launcher.launch(engine==='chromium'?{channel:'chromium'}:{});
  try{for(const locale of ['en','zh','zh-TW','ja','de']){
   const context=await browser.newContext({viewport:{width:1120,height:780},reducedMotion:'reduce'});
   try{
    await context.addInitScript(()=>localStorage.setItem('uni-switch.lastView','usage'));
    const page=await context.newPage();page.setDefaultTimeout(10000);page.setDefaultNavigationTimeout(60000);
    const errors=[],retired=[];
    page.on('pageerror',e=>{errors.push(e.message);console.error(`${engine} ${locale}: ${e.stack}`);});
    page.on('request',r=>{if(/\/api\/usage(?:[/?]|$)|\/api\/sessions\/(stats|overview)|\/api\/(context|tune)(?:[/?]|$)|\/plugins\/usage\//.test(r.url()))retired.push(r.url());});
    await page.route('**/api/sessions/manage*',r=>r.fulfill({json:{agent:'codex',agents:[{agent:'codex',name:'Codex',icon:'codex',count:1,deletable:true}],sessions:[session],terminal:false,trash:[],trashDir:'~/synthetic-trash'}}));
    await page.route('**/api/sessions/transcript?*',r=>r.fulfill({json:{parts:[{role:'user',kind:'text',text:'Synthetic conversation question'},{role:'assistant',kind:'text',text:'Synthetic conversation answer',model:'qa-model'}]}}));
    const url=new URL(link);url.searchParams.set('locale',locale);url.searchParams.set('view','usage');
    await page.goto(url.href);await page.locator('#uniClients').waitFor();await page.waitForFunction(()=>!!providers);
    assert.equal(await page.locator('#nav [data-view="usage"],#view-usage,#panelUsage,[data-ptab="stats"]').count(),0);
    assert.equal(await page.locator('#view-providers').isVisible(),true,'old Usage bookmark falls back');
    for(const p of ['/api/usage','/api/usage/requests','/api/usage/requests.csv','/api/usage/heatmap','/api/sessions/stats','/api/sessions/overview','/api/context','/api/tune','/plugins/usage/client.js','/plugins/usage/page.js'])assert.equal((await context.request.get(new URL(p,link).href)).status(),404,p);
    assert.equal((await context.request.get(new URL('/api/accounts/quotas',link).href)).status(),200);
    assert.equal((await context.request.get(new URL('/api/sessions/manage',link).href)).status(),200);
    await page.locator('#nav [data-view="sessions"]').click();
    const row=page.locator('.row.sm-sess[data-id="qa-session-no-billing"]');await row.waitFor();
    assert.match(await row.innerText(),/Synthetic session|qa-model/);
    const noBilling=async()=>{
     assert.equal(await page.locator('#view-sessions .num,#view-sessions .cost,#view-sessions .sess-models').count(),0,'no billing DOM exists');
     assert.doesNotMatch(await page.locator('#view-sessions').innerText(),/9876|1,234,567|1234567|888888|777777|tokens?\b|输入|輸入|输出|輸出|缓存|快取|[$¥￥€]/i);
    };
    await noBilling();
    await row.locator('.who').click();await page.locator('.sess-detail').waitFor();await noBilling();
    assert.match(await page.locator('.sess-detail').innerText(),/qa-session-no-billing|synthetic\/session/);
    await page.evaluate(()=>{copy=async text=>{window.qaResume=text;};});
    await row.locator('.sess-resume').click();assert.equal(await page.evaluate(()=>window.qaResume),session.resume);
    await page.locator('.sess-talk-btn').click();await page.locator('.sess-talk .cx-part').first().waitFor();
    assert.match(await page.locator('.sess-talk').innerText(),/Synthetic conversation answer/);
    await noBilling();
    await page.locator('.sm-filter').fill('not-a-session');await row.waitFor({state:'detached'});
    await page.locator('.sm-filter').fill('');await row.waitFor();
    await page.setViewportSize({width:760,height:600});await noBilling();
    assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
    if(engine==='chromium'&&locale==='zh')await page.screenshot({path:resolve(root,'.qa/no-usage-sessions.png')});
    await page.locator('#prefs').click();await page.locator('#view-settings').waitFor();
    assert.equal(await page.locator('#trayCostRow').count(),0);
    assert.deepEqual(errors,[],`${engine} ${locale}`);assert.deepEqual(retired,[]);
    checks.push({engine,locale,noBillingDOM:true,resume:true,transcript:true,search:true,retiredAPIs404:true,oldViewFallsBack:true});
   }finally{await context.close();}
  }}finally{await browser.close();}
 }
 assert.equal(readFileSync(resolve(app.home,'.codex/config.toml'),'utf8'),'# isolated QA; disconnected client\n');
 writeFileSync(resolve(root,'.qa/no-usage-ui-checks.json'),JSON.stringify(checks,null,2)+'\n','utf8');
 console.log(JSON.stringify({passed:true,checks:checks.length,isolatedClientUnchanged:true}));
}finally{await app.stop();}

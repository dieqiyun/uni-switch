import {spawnSync} from 'node:child_process';
import {existsSync,readFileSync,writeFileSync,mkdirSync} from 'node:fs';
import {resolve} from 'node:path';
import assert from 'node:assert/strict';
import {chromium,webkit} from 'playwright';
import {startIsolated} from './qa-source-core.mjs';

const root=resolve(import.meta.dirname,'..');
mkdirSync(resolve(root,'.qa'),{recursive:true});
const suffix=process.platform==='win32'?'.exe':'';
const localGo=resolve(root,'.tools/go/bin/go'+suffix);
const go=process.env.UNI_SWITCH_GO||(existsSync(localGo)?localGo:'go');
const binary=resolve(root,'.qa/no-observability-core'+suffix);
const build=spawnSync(go,['build','-tags=nogui','-o',binary,'./cmd/uni-switch-core'],{cwd:resolve(root,'backend'),encoding:'utf8',windowsHide:true});
if(build.status!==0)throw new Error(build.error||build.stderr||'QA core build failed');
for(const key of ['ENABLED','METRICS','SESSIONS','BODIES','BODIES_WHOLE'])process.env['UNI_SWITCH_OTEL_'+key]='true';
process.env.UNI_SWITCH_OTEL_ENDPOINT='http://127.0.0.1:9';
process.env.UNI_SWITCH_OTEL_HEADERS='Authorization=Bearer%20synthetic-retired-key';
process.env.OTEL_RESOURCE_ATTRIBUTES='service.name=retired';
process.env.OTEL_SERVICE_NAME='retired';

const app=await startIsolated(binary),checks=[];
try{
 await app.wait(()=>!!app.ready());
 const link=app.ready().url,settingsPath=resolve(app.home,'uni-switch/settings.json');
 for(const [engine,launcher]of [['chromium',chromium],['webkit',webkit]]){
  const browser=await launcher.launch(engine==='chromium'?{channel:'chromium'}:{});
  try{for(const locale of ['en','zh','zh-TW','ja','de']){
   const legacy=JSON.parse(readFileSync(settingsPath,'utf8'));
   legacy.otel={enabled:true,endpoint:'http://127.0.0.1:9',headers:{Authorization:'Bearer synthetic-retired-key'},metrics:true,sessions:true,bodies:true,bodiesWhole:true};
   writeFileSync(settingsPath,JSON.stringify(legacy),'utf8');
   const context=await browser.newContext({viewport:{width:1000,height:780},reducedMotion:'reduce'});
   await context.addInitScript(()=>localStorage.setItem('uni-switch.settingsTab','otel'));
   const page=await context.newPage();page.setDefaultTimeout(10000);page.setDefaultNavigationTimeout(60000);
   const errors=[],retiredCalls=[];
   page.on('pageerror',error=>errors.push(error.message));
   page.on('request',request=>{if(/\/plugins\/otel\/|\/api\/otel(?:[/?]|$)|\/v1\/(?:traces|metrics)(?:[/?]|$)/.test(request.url()))retiredCalls.push(request.url());});
   try{
    const url=new URL(link);url.searchParams.set('locale',locale);url.searchParams.set('view','settings');url.searchParams.set('tab','otel');
    await page.goto(url.href);
    await page.locator('#setPage-general').waitFor({state:'visible'});
    await page.locator('#themeSegs .opt').first().waitFor();
    assert.equal(new URL(page.url()).searchParams.get('tab'),'general');
    assert.equal(await page.locator('#setTab-otel,#setPage-otel,#otelList,[data-of="setTab-otel"]').count(),0);
    const state=await(await context.request.get(new URL('/api/settings',link).href)).json();
    for(const key of ['otel','otelEnv'])assert.equal(Object.hasOwn(state,key),false,key);
    assert.equal(await page.evaluate(()=>Object.hasOwn(prefs,'otel')||Object.hasOwn(prefs,'otelEnv')),false);
    for(const query of ['OTLP','Langfuse',locale==='zh'?'可观测性':'Observability']){
     await page.locator('#setQ').fill(query);
     await page.waitForFunction(()=>document.querySelector('#view-settings').classList.contains('set-searching'));
     assert.equal(await page.locator('#view-settings .row:visible').count(),0,query);
    }
    await page.locator('#setQ').fill('');
    const theme=state.theme==='dark'?'light':'dark';
    const response=page.waitForResponse(response=>new URL(response.url()).pathname==='/api/settings'&&response.request().method()==='POST');
    await page.locator('#themeSegs .opt').nth(theme==='dark'?2:1).click();
    assert.equal((await response).ok(),true);
    const saved=JSON.parse(readFileSync(settingsPath,'utf8'));
    assert.equal(saved.theme,theme);
    assert.equal(Object.hasOwn(saved,'otel'),false);
    assert.equal(Object.hasOwn(saved,'otelEnv'),false);
    for(const asset of ['/plugins/otel/page.js','/plugins/otel/i18n.js'])assert.equal((await context.request.get(new URL(asset,link).href)).status(),404,asset);
    if(engine==='chromium'&&locale==='zh'){
     await page.screenshot({path:resolve(root,'.qa/no-observability-settings.png')});
     await page.setViewportSize({width:760,height:600});
     assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
     await page.screenshot({path:resolve(root,'.qa/no-observability-settings-compact.png')});
    }
    assert.deepEqual(errors,[],engine+' '+locale+': script errors');
    assert.deepEqual(retiredCalls,[],engine+' '+locale+': retired export requested');
    checks.push({engine,locale,retiredUIAbsent:true,oldTabFallsBack:true,legacySettingsIgnored:true,legacyEnvironmentIgnored:true,ordinarySaveWorks:true});
   }finally{await context.close();}
  }}finally{await browser.close();}
 }
 assert.equal(readFileSync(resolve(app.home,'.codex/config.toml'),'utf8'),'# isolated QA; disconnected client\n');
 writeFileSync(resolve(root,'.qa/no-observability-ui-checks.json'),JSON.stringify(checks,null,2)+'\n','utf8');
 console.log(JSON.stringify({checks:checks.length,passed:true,isolatedClientUnchanged:true}));
}finally{await app.stop();}

// The compiled core runs with an isolated HOME; credentials and upstream are synthetic.
import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {writeFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {chromium,webkit} from 'playwright';
import {startIsolated} from './qa-source-core.mjs';

const root=resolve(import.meta.dirname,'..');
const upstream=createServer((req,res)=>{
 res.setHeader('content-type','application/json');
 res.end(JSON.stringify({data:[{id:'qa-model'}]}));
});
await new Promise(r=>upstream.listen(0,'127.0.0.1',r));
const endpoint=`http://127.0.0.1:${upstream.address().port}/v1`;
const platform={win32:'pc-windows-msvc',linux:'unknown-linux-gnu',darwin:'apple-darwin'}[process.platform];
const arch=process.arch==='arm64'?'aarch64':'x86_64';
const app=await startIsolated(resolve(root,`src-tauri/binaries/uni-switch-core-${arch}-${platform}${process.platform==='win32'?'.exe':''}`));
const checks=[];
const windows=[{days:[1,3],start:'09:00',end:'18:00',rate:1.5}];
try{
 await app.wait(()=>!!app.ready());
 for(const [engine,launcher] of [['chromium',chromium],['webkit',webkit]]){
  const browser=await launcher.launch(engine==='chromium'?{channel:'chromium'}:{});
  try{for(const lang of ['en','zh','zh-TW','ja','de']){
   const context=await browser.newContext({viewport:{width:1120,height:820},reducedMotion:'reduce'});
   try{
    const page=await context.newPage();page.setDefaultTimeout(8000);
    const errors=[],saves=[];page.on('pageerror',e=>errors.push(e.message));
    const url=new URL(app.ready().url);url.searchParams.set('locale',lang);
    const api=path=>new URL(`/api/${path}`,url).href;
    await page.route('**/api/provider/save',async route=>{saves.push(route.request().postDataJSON());await route.continue();});
    await page.goto(url.href);await page.waitForFunction(()=>!!providers);
    const directory=async()=>{const r=await context.request.get(api('providers'));assert.ok(r.ok());return r.json();};
    const open=async p=>{
     await page.evaluate(p=>{providers.providers=providers.providers.filter(x=>x.id!==p.id).concat(p);editing=p.id;draft=null;renderProviders();},p);
     await page.locator('.editor').waitFor();
    };
    const removed=async()=>assert.equal(await page.locator('.editor .price-rate, .editor .count-free, .editor .rate-windows, .editor .queue-limit, .editor .queue-wait, .editor .headers, .editor .extra-body, .editor .reply-headers, .editor .pass-headers, .editor .proxy-pick, .editor .acct-proxies, .editor .connection-limits, .editor input.concurrency, .editor .max-accounts, .editor .rpm').count(),0,'removed settings have no controls');
    const save=async()=>{await page.locator('.editor > .bar .primary').click();await page.locator('.editor').waitFor({state:'detached'});};
    const unfold=async()=>{await page.locator('.editor .advanced > summary').click();await removed();};

    // A new provider needs no queue or pricing decisions. Multi-key controls follow input.
    await page.locator('.uni-custom-add').click();
    await removed();
    assert.equal(await page.locator('.editor .advanced').evaluate(d=>d.open),false);
    await page.locator('.editor .advanced > summary').click();
    await removed();
    await page.evaluate(()=>renderProviders());await removed();
    const key=page.locator('.editor input[type="password"]').first();
    await key.fill('qa-first,qa-second');await removed();
    await key.fill(`qa-new-${engine}-${lang}`);await removed();
    await page.locator('.editor .advanced > summary').click();
    await page.locator('.editor .base-url').fill(endpoint);
    await page.locator('.recommended-model').waitFor();await page.locator('.recommended-model').click();
    const before=new Set((await directory()).providers.map(p=>p.id));
    await save();
    const created=(await directory()).providers.find(p=>!before.has(p.id));assert.ok(created);
    assert.equal(created.queueLimit,20);assert.equal(created.queueWait,120);
    assert.equal(created.proxy,'','new providers follow the global proxy');
    assert.deepEqual(created.headers||{},{});assert.deepEqual(created.extraBody||{},{});
    assert.equal(Object.hasOwn(saves.at(-1),'queueLimit'),false,'the server chooses a new queue policy');
    assert.equal(Object.hasOwn(saves.at(-1),'queueWait'),false);

    // Existing hidden settings survive edits, validation, copies and an unlimited queue.
    const id=`qa-legacy-${engine}-${lang.toLowerCase()}`;
    const transport={headers:{'X-QA-Session':'${sessionId}','X-QA-Thread':'${threadId}'},extraBody:{prompt_cache_key:'${sessionId}',qa:{thread:'${threadId}'}},replyHeaders:['X-Qa-Reply'],passHeaders:['X-Qa-Tenant'],proxy:'direct'};
    const seeded={new:true,id,name:id,chat:endpoint,key:`qa-first-${id},qa-second-${id}`,models:['qa-model'],queueLimit:7,queueWait:33,priceRate:0.8,countFree:true,rateWindows:windows,maxConcurrency:2,maxAccounts:1,maxRPM:25,...transport};
    const seed=await context.request.post(api('provider/save'),{data:seeded});assert.ok(seed.ok(),await seed.text());
    let legacy=(await directory()).providers.find(p=>p.id===id);assert.ok(legacy);
    const keyRef=legacy.keyList.at(-1).id;
    const accountProxies={[keyRef]:'direct'};
    const proxySeed=await context.request.post(api('provider/save'),{data:{id,name:id,chat:endpoint,accountProxies,countFree:true,...transport}});assert.ok(proxySeed.ok(),await proxySeed.text());
    legacy=(await directory()).providers.find(p=>p.id===id);
    const transportKept=p=>{
     for(const [field,value] of Object.entries({...transport,accountProxies}))assert.deepEqual(p[field],value,field+' survives without a form control');
    };
    await open(legacy);await removed();
    await unfold();
    await page.setViewportSize({width:760,height:600});
    assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
    if(engine==='chromium'&&lang==='zh')await page.screenshot({path:resolve(root,'.qa/provider-limits-zh.png')});
    await save();
    legacy=(await directory()).providers.find(p=>p.id===id);
    for(const [field,value] of Object.entries({queueLimit:7,queueWait:33,priceRate:0.8,countFree:true,maxConcurrency:2,maxAccounts:1,maxRPM:25}))assert.equal(legacy[field],value,field);
    assert.deepEqual(legacy.rateWindows,windows);
    transportKept(legacy);
    const beforeCopy=new Set((await directory()).providers.map(p=>p.id));
    await page.evaluate(p=>{providers.providers=providers.providers.filter(x=>x.id!==p.id).concat(p);duplicateProvider(p);},legacy);
    await removed();await unfold();await save();
    const copy=(await directory()).providers.find(p=>!beforeCopy.has(p.id));
    assert.ok(copy);assert.equal(copy.queueLimit,7);assert.equal(copy.queueWait,33);assert.deepEqual(copy.rateWindows,windows);assert.equal(copy.priceRate,0.8);assert.equal(copy.countFree,true);
    transportKept(copy);
    // With one key, a saved account limit is hidden and preserved, along with zero waits.
    const single={...seeded,id:id+'-single',name:id+'-single',key:'qa-only-'+id,queueLimit:0,queueWait:0};
    assert.ok((await context.request.post(api('provider/save'),{data:single})).ok());
    await open((await directory()).providers.find(p=>p.id===single.id));await unfold();
    await removed();await save();
    const savedSingle=(await directory()).providers.find(p=>p.id===single.id);
    assert.equal(savedSingle.queueLimit||0,0);assert.equal(savedSingle.queueWait||0,0);assert.equal(savedSingle.maxAccounts,1);

    // The native account editor follows the same rule without touching real sign-ins.
    const nativeProxies={'two@qa':'direct'};
    const account={...legacy,id:'qa-native',account:{agent:'codex',agentName:'Codex',user:'one@qa',plan:'PLUS',logins:[{user:'one@qa',active:true,on:true},{user:'two@qa',on:true}]},keyList:[],chat:'',responses:'',anthropic:'',accountProxies:nativeProxies};
    await page.route('**/api/provider/save',async route=>{
     saves.push(route.request().postDataJSON());await route.fulfill({json:await directory()});
    });
    await open(account);await removed();await unfold();await save();
    const accountSave=saves.at(-1);assert.equal(accountSave.id,account.id);assert.equal(accountSave.priceRate,0.8);assert.deepEqual(accountSave.rateWindows,windows);assert.equal(accountSave.countFree,true);assert.equal(accountSave.queueWait,33);
    assert.equal(accountSave.proxy,'direct');assert.deepEqual(accountSave.accountProxies,nativeProxies);
    assert.equal(accountSave.maxConcurrency,2);assert.equal(accountSave.maxAccounts,1);assert.equal(accountSave.maxRPM,25);
    assert.deepEqual(errors,[]);
    checks.push({engine,lang,removedControls:true,connectionLimitsRemoved:true,queueDefaults:true,savedAndCopiedValues:true,smallWindow:true});
   }finally{await context.close();}
  }}finally{await browser.close();}
 }
 writeFileSync(resolve(root,'.qa/provider-limits-checks.json'),JSON.stringify(checks,null,2)+'\n');
 console.log(JSON.stringify({passed:true,checks:checks.length}));
}finally{await app.stop();await new Promise(r=>upstream.close(r));}

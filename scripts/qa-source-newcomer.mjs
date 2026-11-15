// Compiled core + isolated HOME. All provider responses below are synthetic.
import assert from 'node:assert/strict';
import {resolve} from 'node:path';
import {writeFileSync} from 'node:fs';
import {chromium,webkit} from 'playwright';
import {startIsolated} from './qa-source-core.mjs';
const root=resolve(import.meta.dirname,'..');
const app=await startIsolated(resolve(root,'src-tauri/binaries/uni-switch-core-x86_64-pc-windows-msvc.exe'));
const checks=[];
try {
 await app.wait(()=>!!app.ready());
 for(const [engine,launcher] of [['chromium',chromium],['webkit',webkit]].filter(([engine])=>!process.env.QA_ENGINE||process.env.QA_ENGINE===engine)) {
  const browser=await launcher.launch(engine==='chromium'?{channel:'chromium'}:{});
  try {for(const lang of ['en','zh','zh-TW','ja','de'].filter(lang=>!process.env.QA_LANG||process.env.QA_LANG===lang)) {
   const context=await browser.newContext({viewport:{width:1120,height:820},reducedMotion:'reduce'});
   try {
    const page=await context.newPage();page.setDefaultTimeout(10000);
    const errors=[];page.on('pageerror',e=>errors.push(e.message));
    const url=new URL(app.ready().url);url.searchParams.set('locale',lang);url.searchParams.set('view','settings');url.searchParams.set('tab','general');
    await page.goto(url.href);await page.waitForFunction(()=>!!prefs&&setShown==='general');
    assert.equal(await page.locator('.uni-add').isVisible(),false);
    assert.deepEqual(await page.locator('.newcomer-basics > .row').evaluateAll(rows=>rows.map(r=>r.querySelector('[data-t]')?.dataset.en)),['Language','Appearance','Text size','Open at login']);
    assert.equal(await page.locator('#generalAdvanced').getAttribute('open'),null);
    assert.equal(await page.locator('#uiFontRow').isVisible(),false);
    await page.locator('#generalAdvanced > summary').focus();await page.keyboard.press('Enter');
    assert.equal(await page.locator('#generalAdvanced').evaluate(e=>e.open),true);
    await page.keyboard.press('Enter');assert.equal(await page.locator('#generalAdvanced').evaluate(e=>e.open),false);
    const search=page.locator('#setQ');await search.fill('Interface font');
    assert.equal(await page.locator('#generalAdvanced').evaluate(e=>e.open),true);
    await search.press('Escape');assert.equal(await page.locator('#generalAdvanced').evaluate(e=>e.open),false);
    await page.evaluate(()=>setSetTab('usage',true));
    assert.equal(await page.locator('#currencySegs').isVisible(),false);
    assert.equal(await page.locator('#warmTabs').count(),0);
    await page.locator('#allowanceAdvanced > summary').click();assert.equal(await page.locator('#currencySegs').isVisible(),true);
    await page.evaluate(()=>setSetTab('network',true));
    assert.equal(await page.locator('#proxySegs').isVisible(),false);
    await page.locator('#networkProxy > summary').click();assert.equal(await page.locator('#proxySegs').isVisible(),true);
    await page.locator('#networkProxy > summary').click();
    await page.evaluate(()=>setSetTab('models',true));
    assert.equal(await page.locator('#imagesList').isVisible(),false);
    assert.equal(await page.locator('#classifyTimeoutRow').count(),0);
    await page.locator('#modelOptions0 > summary').click();assert.equal(await page.locator('#imagesList').isVisible(),true);
    if(engine==='chromium'&&lang==='zh') {await page.evaluate(()=>setSetTab('general',true));await page.screenshot({path:resolve(root,'.qa/newcomer-general.png')});}
    await page.locator('[data-uni-client="codex"]').click();assert.equal(await page.locator('.uni-add').isVisible(),true);
    await page.locator('.uni-custom-add').click();await page.locator('.editor .base-url').waitFor();
    assert.equal(await page.locator('.editor .request-endpoint').isVisible(),true);
    assert.equal(await page.locator('.editor .advanced').evaluate(e=>e.open),false);
    assert.equal(await page.locator('.editor .headers').count(),0);
    let mode='success', requests=0, releaseOld;
    await page.route('**/api/provider/list',async route=>{
     requests++;
     if(mode==='old') {await new Promise(r=>releaseOld=r);try{await route.fulfill({json:{models:[{id:'stale-model'}]}});}catch{}return;}
     if(mode==='empty') return route.fulfill({json:{models:[]}});
     if(mode==='error') return route.fulfill({status:400,json:{error:'401 invalid_api_key synthetic <script>window.qaUnsafe=true</script>'}});
     await route.fulfill({json:{models:[{id:'qa-chat',name:'QA Chat',best:true},{id:'qa-other',name:'QA Other'}]}});
    });
    await page.locator('.editor .base-url').fill('https://qa-provider.invalid/v1');
    const key=page.locator('.editor input[type="password"]').first();await key.fill('qa-synthetic-key');
    await page.locator('.recommended-model').waitFor();assert.ok(requests>=1);
    assert.deepEqual(await page.evaluate(()=>draft.extra),['qa-chat','qa-other'],'models are selected automatically after discovery');
    assert.equal(await page.locator('.add-models-list [aria-pressed="true"]').count(),2);
    await page.locator('.add-models-list [data-model="qa-other"]').click();
    assert.deepEqual(await page.evaluate(()=>draft.extra),['qa-chat'],'a user can deselect a model after automatic selection');
    await page.locator('.add-models .fetch-models').click();await page.locator('.recommended-model').waitFor();
    assert.deepEqual(await page.evaluate(()=>draft.extra),['qa-chat','qa-other'],'explicit model fetching also selects the returned list');
    await page.locator('[data-model-action="none"]').click();assert.deepEqual(await page.evaluate(()=>draft.extra),[]);
    await page.locator('.recommended-model').click();assert.deepEqual(await page.evaluate(()=>draft.extra),['qa-chat']);
    await page.locator('.manual-models > summary').click();await page.getByRole('textbox',{name:await page.evaluate(()=>t('Model IDs')),exact:true}).fill('qa-manual');
    mode='empty';await page.locator('.add-models .fetch-models').click();await page.waitForFunction(()=>document.querySelector('.add-models').getAttribute('aria-busy')==='false');
    assert.deepEqual(await page.evaluate(()=>draft.extra),['qa-manual'],'empty responses preserve manual IDs');
    mode='error';await key.fill('qa-invalid-key');
    try {await page.locator('.add-models-list .connection-details').waitFor();}
    catch(error) {console.error(JSON.stringify({engine,lang,requests,errors,state:await page.locator('.add-models').textContent()}));throw error;}
    assert.match(await page.locator('.connection-reason').last().textContent(),new RegExp((await page.evaluate(()=>t('API key is invalid. Check or replace it.'))).replace(/[.*+?^${}()|[\]\\]/g,'\\$&')));
    assert.equal(await page.locator('.connection-details pre').isVisible(),false);
    await page.locator('.connection-details summary').click();assert.match(await page.locator('.connection-details pre').textContent(),/invalid_api_key/);
    assert.equal(await page.evaluate(()=>!!window.qaUnsafe),false);
    assert.deepEqual(await page.evaluate(()=>draft.extra),['qa-manual']);
    mode='old';await key.fill('qa-old-key');await page.waitForFunction(()=>document.querySelector('.add-models').getAttribute('aria-busy')==='true');
    await page.waitForTimeout(80);mode='success';await key.fill('qa-new-key');
    await page.locator('.recommended-model').waitFor();releaseOld?.();
    await page.waitForTimeout(100);assert.equal(await page.locator('[data-model="stale-model"]').count(),0);
    assert.deepEqual(await page.evaluate(()=>draft.extra),['qa-manual','qa-chat','qa-other'],'new discovery selects all models while keeping manual IDs');
    mode='old';await page.locator('.add-models .fetch-models').click();await page.waitForFunction(()=>document.querySelector('.add-models').getAttribute('aria-busy')==='true');
    await page.getByRole('textbox',{name:await page.evaluate(()=>t('Model IDs')),exact:true}).fill('qa-during-request');
    releaseOld?.();await page.locator('.add-models [data-model="stale-model"]').waitFor();
    assert.deepEqual(await page.evaluate(()=>draft.extra),['qa-during-request'],'manual changes during discovery take precedence over default selection');
    await page.locator('.editor .advanced > summary').click();assert.equal(await page.locator('.editor .request-endpoint').isVisible(),true);
    await page.locator('.editor .advanced > summary').click();
    await page.setViewportSize({width:760,height:600});assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
    if(engine==='chromium'&&lang==='zh')await page.screenshot({path:resolve(root,'.qa/newcomer-add-provider.png')});
    // Closing the form while a request is pending cannot mutate the next draft.
    mode='old';await key.fill('qa-final-old-key');await page.waitForFunction(()=>document.querySelector('.add-models').getAttribute('aria-busy')==='true');
    await page.evaluate(()=>{editing=null;draft=null;adding=false;renderProviders();});
    releaseOld?.();await page.waitForTimeout(60);assert.equal(await page.evaluate(()=>draft),null);
    // The primary Dieqiyun form uses the same automatic defaults.
    mode='success';await page.locator('.uni-add').click();await page.locator('.editor').waitFor();
    await page.locator('.editor input[type="password"]').first().fill('qa-preset-key');
    await page.locator('.recommended-model').waitFor();
    assert.deepEqual(await page.evaluate(()=>draft.extra),['qa-chat','qa-other'],'Dieqiyun also selects the complete model list');
    await page.locator('.editor > .bar button').first().click();
    await page.locator('.action-confirm .primary').click();await page.locator('#modal').waitFor({state:'hidden'});
    // Account controls save only the selected account, keeping its neighbour's schedule.
    if(lang==='en') {
     mode='success';
     await page.locator('.uni-custom-add').click();
     const providerHost='qa-newcomer-'+engine+'.invalid';
     await page.locator('.editor .base-url').fill('https://'+providerHost+'/v1');
     await page.locator('.editor input[type="password"]').first().fill('qa-synthetic-key');
     await page.locator('.recommended-model').waitFor();
     assert.deepEqual(await page.evaluate(()=>draft.extra),['qa-chat','qa-other'],'saving needs no model-selection click');
     await page.locator('.editor > .bar .primary').click();
     await page.locator('.editor').waitFor({state:'detached'});
     const directory=await (await page.context().request.get(new URL('/api/providers',url).href)).json();
     const saved=directory.providers.find(p=>p.name===providerHost);
     assert.ok(saved,'Address and key alone must save a custom provider without requiring advanced fields');
     assert.deepEqual(saved.models.filter(m=>m.on).map(m=>m.id).sort(),['qa-chat','qa-other'],'all discovered models are enabled after saving');
     const server=await page.evaluate(()=>structuredClone(prefs));
     server.accountWarmups={'codex|a@qa':{reset:'',times:[]},'codex|b@qa':{reset:'week',times:['09:00']}};
     server.checkinAccounts=[{key:'workbuddy|a',by:'',user:'a@qa',on:true},{key:'workbuddy|b',by:'',user:'b@qa',on:true}];
     server.accountCheckins={'workbuddy|b':false};
     const saves=[];
     await page.route('**/api/settings/account-*',async route=>{
      const body=route.request().postDataJSON();saves.push(body);
      if(route.request().url().endsWith('account-warmup'))server.accountWarmups[body.agent+'|'+body.user.toLowerCase()]={reset:body.reset,times:body.times};
      else server.accountCheckins[body.key]=body.on;
      await route.fulfill({json:server});
     });
     await page.evaluate(s=>{
      prefs=s;state.settings=s;
      for(const [agent,user,id] of [['codex','a@qa','qaWarm'],['workbuddy','a@qa','qaCheckin']]) {
       const panel=el('section');panel.id=id;panel.append(...accountAutomation(agent,user,null));document.querySelector('#view-providers').prepend(panel);
      }
     },server);
     await page.locator('#qaWarm .segs').first().getByRole('button',{name:'Weekly',exact:true}).click();
     await page.waitForFunction(()=>prefs.accountWarmups['codex|a@qa'].reset==='week');
     assert.deepEqual(await page.evaluate(()=>prefs.accountWarmups['codex|b@qa']),{reset:'week',times:['09:00']});
     await page.locator('#qaCheckin .segs').first().getByRole('button',{name:'On',exact:true}).click();
     await page.waitForFunction(()=>prefs.accountCheckins['workbuddy|a']===true);
     assert.equal(await page.evaluate(()=>prefs.accountCheckins['workbuddy|b']),false);
     assert.equal(saves.length,2);
     await page.evaluate(()=>{document.querySelector('#qaWarm').remove();document.querySelector('#qaCheckin').remove();});
     // A vendor label can cover several native account types. Match the exact account site.
     const sites=[['trae-cn','trae'],['minimax-code','minimax'],['minimax-code-global','minimax'],['qoder','qoder'],['qoder-cn','qoder']];
     const matched=await page.evaluate(sites=>{
      prefs.checkinAccounts=sites.map(([site,by])=>({key:site+'|same-user',by,user:'same@qa',on:true}));
      return sites.map(([site])=>accountAutomation(site,'same@qa',null).length);
     },sites);
     assert.deepEqual(matched,sites.map(()=>3));
    }
    await page.locator('#nav [data-view="sessions"]').click();assert.equal(await page.locator('.uni-add').isVisible(),false);
    assert.deepEqual(errors,[]);
    checks.push({engine,lang,progressiveSettings:true,keyboardAndSearch:true,contextualAdd:true,automaticModels:true,automaticSelectAll:true,explicitSelectAll:true,presetSelectAll:true,manualSelectionPreserved:true,pendingSelectionPreserved:true,emptySelectionPreserved:true,recommendations:true,manualFallback:true,staleResponsesIgnored:true,friendlyErrors:true});
   }finally{await context.close();}
  }}finally{await browser.close();}
 }
 writeFileSync(resolve(root,'.qa/newcomer-verification.json'),JSON.stringify(checks,null,2)+'\n','utf8');
 console.log(JSON.stringify({passed:true,checks:checks.length}));
}finally{await app.stop();}

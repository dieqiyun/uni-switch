import assert from 'node:assert/strict';
import {writeFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {chromium,webkit} from 'playwright';
import AxeBuilder from '@axe-core/playwright';
import {startIsolated} from './qa-source-core.mjs';
const root=resolve(import.meta.dirname,'..');
const platform={win32:'pc-windows-msvc',linux:'unknown-linux-gnu',darwin:'apple-darwin'}[process.platform];
const arch=process.arch==='arm64'?'aarch64':'x86_64';
const app=await startIsolated(resolve(root,process.argv[2]||`src-tauri/binaries/uni-switch-core-${arch}-${platform}${process.platform==='win32'?'.exe':''}`));
const checks=[];
const clients=[['codex','ChatGPT'],['claude','Claude'],['dsh',null],['workbuddy',null],['zcode',null],['pi',null],['opencode',null],['gemini','Gemini CLI'],['kimi',null],['qoder',null],['qoder-cn',null]];
try{
 await app.wait(()=>!!app.ready());const link=app.ready().url;
 for(const [engine,launcher] of [['chromium',chromium],['webkit',webkit]]){
  const browser=await launcher.launch(engine==='chromium'?{channel:'chromium',ignoreDefaultArgs:['--hide-scrollbars']}:{});
  try{for(const language of ['en','zh','zh-TW','ja','de']){
   const context=await browser.newContext({viewport:{width:1120,height:780},reducedMotion:'reduce'});
   try{
    const page=await context.newPage();page.setDefaultTimeout(8000);const errors=[];page.on('pageerror',e=>errors.push(e.message));
    // Vendor status is outside this UI flow. Keep it synthetic and finish its
    // current read before reload so WebKit cannot report an aborted fetch.
    await page.route('**/api/upstream',route=>route.fulfill({json:{providers:{},vendors:[]}}));
    const localImportRequests=[];
    await page.route('**/api/importapps**',route=>{
     localImportRequests.push({url:route.request().url(),method:route.request().method()});
     return route.fulfill({json:[]});
    });
    const url=new URL(link);url.searchParams.set('locale',language);await page.goto(url.href);
    await page.locator('#uniClients').waitFor();await page.waitForFunction(()=>!!providers);
    assert.equal(await page.locator('#providerDiscovery,#reviewProviderDiscovery,#dismissProviderDiscovery').count(),0,'local configuration discovery has no UI');
    assert.deepEqual(await page.locator('#uniClients [data-uni-client]').evaluateAll(buttons=>buttons.map(b=>b.dataset.uniClient)),clients.map(([id])=>id));
    assert.equal(await page.locator('#uniClients [data-uni-client]:visible').count(),8);
    await page.locator('#uniClientsToggle').click();
    assert.equal(await page.locator('#addSheet').isVisible(),false,'first use waits for Add');
    assert.equal(await page.locator('.editor').count(),0);
    const closeEditor=async()=>{await page.locator('.editor > .bar button').first().click();await page.locator('#modal').waitFor({state:'hidden'});};
    assert.ok(await page.locator('#uniProviderEmpty').isVisible(),'empty list offers the primary provider');
    assert.equal(await page.locator('.uni-custom-add').count(),1,'there is one secondary custom entry');
    assert.equal(await page.locator('.uni-add').innerText(),'+ '+await page.evaluate(()=>t('Add Dieqiyun provider')));
    assert.equal(await page.locator('.uni-provider-primary').innerText(),await page.locator('.uni-add').innerText());
    const primaryStyles=await page.evaluate(()=>['.uni-add','.uni-provider-primary'].map(selector=>{
     const button=document.querySelector(selector),style=getComputedStyle(button),rect=button.getBoundingClientRect();
     return Object.fromEntries(['backgroundColor','color','fontSize','fontWeight','paddingTop','paddingRight','paddingBottom','paddingLeft','borderRadius'].map(key=>[key,style[key]]).concat([['width',rect.width],['height',rect.height]]));
    }));
    assert.deepEqual(primaryStyles[1],primaryStyles[0],'center and header actions share their visual style and size');
    await page.locator('#uniProviderEmpty button').focus();await page.keyboard.press('Enter');await page.locator('.editor').waitFor();
    assert.equal(await page.locator('.editor .ehead > b').innerText(),'蝶祈云API');
    assert.equal(await page.locator('#addSheet').isVisible(),false,'large primary opens the preset directly');
    await closeEditor();await page.waitForFunction(()=>document.activeElement?.classList.contains('uni-provider-primary'));
    await page.locator('.uni-add').click();await page.locator('.editor').waitFor();
    assert.equal(await page.locator('.editor .ehead > b').innerText(),'蝶祈云API');
    assert.equal(await page.locator('#addSheet').isVisible(),false,'header opens the preset directly');
    await closeEditor();
    await page.locator('.uni-custom-add').focus();await page.keyboard.press('Enter');await page.locator('.editor .base-url').waitFor();
    assert.equal(await page.locator('.editor .base-url').inputValue(),'','custom setup is still usable');
    await closeEditor();await page.waitForFunction(()=>document.activeElement?.classList.contains('uni-custom-add'));
    const emptyAccessibility=await new AxeBuilder({page}).include('#uniProviderEmpty').include('#uniProviderFooter').include('.uni-add')
      .withRules(['button-name','label','aria-allowed-attr','aria-required-attr','color-contrast']).analyze();
    assert.deepEqual(emptyAccessibility.violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>({target:n.target,summary:n.failureSummary}))})),[],'empty provider actions are accessible');
    if(engine==='chromium'&&language==='zh'){
     await page.screenshot({path:resolve(root,'.qa/provider-priority-light.png')});
     await page.evaluate(()=>document.documentElement.dataset.theme='dark');
     const dark=await new AxeBuilder({page}).include('#uniProviderEmpty').include('#uniProviderFooter').include('.uni-add').withRules(['color-contrast']).analyze();
     assert.deepEqual(dark.violations,[],'dark empty actions have readable contrast');
     await page.screenshot({path:resolve(root,'.qa/provider-priority-dark.png')});
     await page.evaluate(()=>document.documentElement.dataset.theme='light');
     await page.setViewportSize({width:760,height:600});
     assert.ok(await page.locator('#uniProviderEmpty button').evaluate(el=>{const r=el.getBoundingClientRect();return r.left>=0&&r.right<=innerWidth&&r.width<=300&&r.height>=28&&r.height<=48;}));
     assert.ok(await page.locator('.uni-custom-add').evaluate(el=>{const r=el.getBoundingClientRect();return r.right<=innerWidth&&r.bottom<=innerHeight&&parseFloat(getComputedStyle(el).fontSize)<=12;}));
     await page.screenshot({path:resolve(root,'.qa/provider-priority-minimum.png')});
     await page.setViewportSize({width:1120,height:780});
    }
    for(const [id,subscription] of clients){
     await page.locator(`[data-uni-client="${id}"]`).click();
     assert.equal(await page.locator(`[data-uni-client="${id}"]`).getAttribute('aria-current'),'page');
     assert.equal(await page.locator('#uniClients [aria-current="page"]').count(),1);
     assert.equal(await page.locator('#crumb > b').innerText(),await page.locator(`[data-uni-client="${id}"] strong`).innerText());
     const iconURL=await page.locator(`[data-uni-client="${id}"] .ic`).evaluate(ic=>new URL(`icons/${ic.dataset.icon}.${pngIcons.has(ic.dataset.icon)?'png':'svg'}`,location.href).href);
     const image=await context.request.get(iconURL);
     assert.equal(image.status(),200,`${id}: sidebar logo is available`);
     assert.match(image.headers()['content-type'],/^image\//);
     assert.equal(await page.locator('#addSheet').isVisible(),false);
     assert.equal(await page.locator('#uniSubscriptionAdd').isVisible(),!!subscription,'subscription entry follows the selected client');
     if(!subscription)continue;
     await page.locator('#uniSubscriptionAdd').click();await page.locator('#addSheet .find').waitFor();
     assert.equal(await page.locator('#addSheet .custom,#addSheet button.link').count(),0,'custom setup has no duplicate card or search entry');
     assert.equal(await page.locator('#addSheet .row-head button').count(),1,'provider choices offer Close without local import');
     assert.deepEqual(await page.locator('#addSheet .tile .n').allTextContents(),['蝶祈云API',...(subscription?[subscription]:[])],`${id}: recommended provider precedes only its own subscription`);
     const headings=await page.locator('#addSheet .kind > b').allTextContents();
     assert.deepEqual(headings,await page.evaluate(hasSub=>[t('uni-switch recommended providers'),...(hasSub?[t('Subscriptions')]:[])],!!subscription));
     assert.equal(await page.locator('#uniSubscriptionAdd').getAttribute('aria-expanded'),'true');
     assert.equal(await page.locator('#uniSubscriptionAdd').getAttribute('aria-controls'),'addSheet');
     if(id==='codex'){
      assert.equal(await page.locator('#addSheet .find').getAttribute('type'),'search');
      assert.ok(await page.locator('#addSheet .find').getAttribute('aria-label'));
      assert.ok(await page.locator('#addSheet .tile-action').first().innerText());
      await page.waitForFunction(()=>{const b=document.querySelector('#addSheet button.tile');return b&&getComputedStyle(b).cursor==='pointer'&&parseFloat(getComputedStyle(b).borderTopWidth)>0;});
      await page.locator('#uniSubscriptionAdd').click();await page.locator('#addSheet').waitFor({state:'hidden'});
      assert.equal(await page.locator('#uniSubscriptionAdd').getAttribute('aria-expanded'),'false','clicking Add again collapses');
      await page.locator('#uniSubscriptionAdd').focus();await page.keyboard.press('Enter');await page.locator('#addSheet .find').waitFor();
      const accessibility=await new AxeBuilder({page}).include('#uniImageSetup').include('#addSheet')
       .withRules(['button-name','label','aria-allowed-attr','aria-required-attr','color-contrast']).analyze();
      assert.deepEqual(accessibility.violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>n.target)})),[],`${engine}/${language}: action accessibility`);
      if(engine==='chromium'&&language==='zh'){
       await page.screenshot({path:resolve(root,'.qa/client-actions-light.png')});
       await page.evaluate(()=>document.documentElement.dataset.theme='dark');
       await page.screenshot({path:resolve(root,'.qa/client-actions-dark.png')});
       const darkAccessibility=await new AxeBuilder({page}).include('#uniImageSetup').include('#addSheet').withRules(['color-contrast']).analyze();
       assert.deepEqual(darkAccessibility.violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>({target:n.target,summary:n.failureSummary}))})),[],'dark controls have readable contrast');
       await page.evaluate(()=>document.documentElement.dataset.theme='light');
       await page.setViewportSize({width:760,height:600});
       assert.ok(await page.locator('#addSheet').evaluate(el=>{const r=el.getBoundingClientRect();return r.left>=0&&r.right<=innerWidth;}));
       assert.ok(await page.locator('#addSheet button.tile').evaluateAll(els=>els.every(el=>el.scrollWidth<=el.clientWidth)),'provider controls fit the narrow window');
       await page.screenshot({path:resolve(root,'.qa/client-actions-minimum.png')});
       await page.setViewportSize({width:1120,height:780});
      }
     }
     const search=page.locator('#addSheet .find');
     await search.fill(id==='codex'?'Claude':'ChatGPT');
     assert.equal(await page.locator('#addSheet .tile').count(),0,`${id}: search cannot reveal another client's subscription`);
     await search.fill('Ollama');
     assert.equal(await page.locator('#addSheet .tile').count(),0,'local presets stay hidden when searched');
     await search.press('Escape');
     assert.deepEqual(await page.locator('#addSheet .tile .n').allTextContents(),['蝶祈云API',...(subscription?[subscription]:[])]);
     await search.press('Escape');await page.locator('#addSheet').waitFor({state:'hidden'});
     assert.equal(await page.locator('#uniSubscriptionAdd').getAttribute('aria-expanded'),'false');
    }
    for(const view of ['sessions','library']){
     const nav=page.locator(`#nav [data-view="${view}"]`);assert.ok(await nav.isVisible(),`${view} directly reachable`);await nav.click();
     await page.locator(`#view-${view}`).waitFor();
    }
    await page.locator('[data-uni-client="claude"]').click();
    await page.locator('#uniSubscriptionAdd').click();await page.locator('#addSheet .find').waitFor();
    assert.match(await page.locator('#crumb').innerText(),/Claude Code/);
    assert.equal(await page.locator('#addSheet [data-pick="remote-uni-switch"]').count(),0);
    const directory=await (await context.request.get(new URL('/api/providers',link).href)).json();
    assert.deepEqual(directory.presets.map(p=>p.id),['dieqiyun','ollama','lmstudio','omlx','mlx-serve']);
    await page.locator('#addSheet .find').fill('蝶祈云');await page.locator('#addSheet .tile').click();await page.locator('.editor').waitFor();
    await page.locator('.editor .bar button').first().click();await page.locator('#modal').waitFor({state:'hidden'});
    await page.locator('#addSheet .find').fill('');
    await page.locator('.uni-custom-add').click();await page.locator('.editor').waitFor();
    await page.locator('.editor .bar button').first().click();await page.locator('#modal').waitFor({state:'hidden'});
    assert.equal(await page.locator('#addSheet').isVisible(),false,'custom form closes the secondary choices');
    await page.waitForFunction(()=>document.activeElement?.classList.contains('uni-custom-add'));
    await page.locator('[data-uni-client="codex"]').focus();await page.keyboard.press('Enter');
    assert.equal(await page.locator('[data-uni-client="codex"]').getAttribute('aria-current'),'page');
    await page.setViewportSize({width:760,height:600});
    for(const [id] of clients){
     await page.locator(`[data-uni-client="${id}"]`).focus();await page.keyboard.press('Enter');
     assert.equal(await page.locator(`[data-uni-client="${id}"]`).getAttribute('aria-current'),'page');
     await page.waitForFunction(id=>{const b=document.querySelector(`[data-uni-client="${id}"] strong`);if(!b)return false;const range=document.createRange();range.selectNodeContents(b);return range.getClientRects().length===1;},id);
    }
    await page.locator('#prefs').scrollIntoViewIfNeeded();
    assert.ok(await page.locator('#prefs').evaluate(b=>{const r=b.getBoundingClientRect();return r.top>=0&&r.bottom<=innerHeight;}), 'settings remains reachable below the expanded client list');
    assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
    if(engine==='chromium'&&language==='zh'){
     await page.locator('[data-uni-client="codex"]').click();
     await page.setViewportSize({width:1120,height:880});
     await page.screenshot({path:resolve(root,'.qa/client-flow-final.png')});await page.setViewportSize({width:760,height:600});
     await page.locator('[data-uni-client="qoder-cn"]').click();
     await page.screenshot({path:resolve(root,'.qa/client-flow-minimum.png')});
    }
    await page.evaluate(()=>loadUpstream());
    await page.reload();await page.locator('#uniClients').waitFor();await page.waitForFunction(()=>!!providers);
    assert.equal(await page.locator('#providerDiscovery,#reviewProviderDiscovery,#dismissProviderDiscovery').count(),0,'reload does not restore discovery');
    assert.deepEqual(localImportRequests,[],'loading, switching clients, opening Add and reloading never scan or import local configurations');
    assert.deepEqual(errors,[]);checks.push({engine,language,clients:clients.length,directFeatures:4,explicitAdd:true,clientSubscriptions:true,recommendedFirst:true,dieqiyunPrimary:true,directPresetEditor:true,emptyPrimary:true,customFooterOnly:true,actionAffordances:true,repeatedClickCollapse:true,accessibility:true,localPresetsHiddenWithBackendRetained:true,localImportRemoved:true,localImportRequests:localImportRequests.length,remoteRemoved:true,cancelFocus:true,keyboard:true,icons:true,smallWindowNavigation:true});
   }finally{await context.close();}
  }}finally{await browser.close();}
 }
 writeFileSync(resolve(root,'.qa/client-flow-ui-checks.json'),JSON.stringify(checks,null,2)+'\n');console.log(JSON.stringify({passed:true,checks:checks.length}));
}finally{await app.stop();}

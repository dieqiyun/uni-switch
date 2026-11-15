import assert from 'node:assert/strict';
import {readFileSync,writeFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {chromium,webkit} from 'playwright';
import {startIsolated} from './qa-source-core.mjs';
const root=resolve(import.meta.dirname,'..');
const app=await startIsolated(resolve(root,'src-tauri/binaries/uni-switch-core-x86_64-pc-windows-msvc.exe'));
const checks=[];
const failure='Synthetic upstream refusal <img src=x onerror="window.qaUnsafe=true"> '+ 'long-error-'.repeat(25);
const routes=[
 {id:6,model:'requested-alias',provider:'initial',done:true,status:200,ms:1234,order:[{id:'final-key',provider:'final',name:'Final provider'}],tries:[{id:'first-key',model:'first-model'},{id:'final-key',model:'fallback-model',served:'served-model'}]},
 {id:5,model:'failure-model',provider:'initial',done:true,status:503,ms:910,error:failure},
 {id:4,model:'running-model',provider:'initial',done:false,status:0},
 {id:3,model:'canceled-model',provider:'initial',done:true,canceled:true,status:499,ms:70},
 {id:2,model:'http-failure-model',provider:'initial',done:true,status:502,ms:80},
 {id:1,model:'stream-failure-model',provider:'initial',done:true,status:200,ms:100,error:'Synthetic stream failed'}
];
try {
 await app.wait(()=>!!app.ready());
 for(const [engine,launcher] of [['chromium',chromium],['webkit',webkit]]){
  const browser=await launcher.launch(engine==='chromium'?{channel:'chromium'}:{});
  try { for(const locale of ['en','zh','zh-TW','ja','de']){
   const context=await browser.newContext({viewport:{width:1120,height:820},reducedMotion:'reduce'});
   try {
    await context.addInitScript(()=>localStorage.setItem('uni-switch.lastView','routing'));
    const page=await context.newPage();page.setDefaultTimeout(10000);
    const errors=[],retired=[];let reads=0, keyReads=0, phase='empty';
    page.on('request',r=>{if(new URL(r.url()).pathname==='/api/caller-keys')keyReads++;});
    page.on('pageerror',e=>errors.push(e.message));
    page.on('request',r=>{if(/\/plugins\/routing\/|\/api\/gateway\/(history|route\?|call\?|route-events)/.test(r.url()))retired.push(r.url());});
    await page.route('**/api/gateway/trace',async route=>{
     reads++;
     if(phase==='error')return route.fulfill({status:503,json:{error:'Synthetic read failure'}});
     await route.fulfill({json:{routes:phase==='empty'?[]:routes,seq:1}});
    });
    const url=new URL(app.ready().url);url.searchParams.set('locale',locale);
    if(engine==='chromium')url.searchParams.set('view','gateway');
    await page.goto(url.href);await page.waitForFunction(()=>view==='settings'&&setShown==='gateway'&&!!providers);
    const fold=page.locator('#foldRecords'), section=page.locator('#requestRecords');
    assert.equal(await page.locator('#nav [data-view="routing"],#view-routing,#panelRouting,[data-ptab="routing"]').count(),0);
    assert.equal(await page.locator('#nav [data-view="gateway"],#view-gateway,#gwModels,#foldModels,#copyModels,#findModel').count(),0);
    assert.equal(await page.locator('#gatewayAdvanced').isVisible(),false);
    assert.equal(keyReads,0,'no key reads before advanced configuration is opened');
    const address=new URL(await page.locator('#gateway .gateway-address code').textContent());
    assert.equal(address.hostname,'127.0.0.1');assert.equal(address.pathname,'/v1');
    assert.equal(await section.isVisible(),false);assert.equal(reads,0);
    assert.equal(await fold.getAttribute('aria-expanded'),'false');
    const advanced=page.locator('#foldGatewayAdvanced');
    await advanced.click();await page.locator('#gatewayKeys .none').waitFor();
    assert.equal(await advanced.getAttribute('aria-expanded'),'true');
    assert.ok(keyReads>0);assert.equal(await page.locator('#connect').isVisible(),true);
    assert.equal(await page.locator('#portList').isVisible(),true);
    assert.ok((await page.locator('#connect').innerText()).includes('provider/model'));
    await advanced.click();assert.equal(await page.locator('#gatewayAdvanced').isVisible(),false);
    await advanced.focus();await page.keyboard.press('Enter');assert.equal(await page.locator('#gatewayAdvanced').isVisible(),true);
    await page.keyboard.press('Space');assert.equal(await page.locator('#gatewayAdvanced').isVisible(),false);
    await fold.click();await page.locator('#recordsStatus .none').waitFor();
    await page.waitForFunction(()=>document.querySelector('#recordsStatus').textContent&&!document.querySelector('#recordsStatus').textContent.includes('Fetching'));
    assert.equal(await section.isVisible(),true);
    assert.equal(await fold.getAttribute('aria-expanded'),'true');
    await fold.click();assert.equal(await section.isVisible(),false);
    phase='records';await fold.focus();await page.keyboard.press('Enter');
    await page.locator('.records-table tbody tr').first().waitFor();
    assert.equal(await page.locator('.records-table th').count(),5);
    assert.equal(await page.locator('.records-table tbody tr').count(),6);
    const cells=await page.locator('.records-table tbody tr').first().locator('td').allTextContents();
    assert.equal(cells[0],'Final provider');assert.equal(cells[1],'served-model');assert.match(cells[2],/1/);
    assert.equal(await page.locator('.record-result.ok').count(),1);
    assert.equal(await page.locator('.record-result.bad').count(),3);
    assert.equal(await page.locator('.record-result.pending').count(),2);
    assert.equal(await page.locator('.records-table tbody tr').nth(2).locator('td').nth(2).textContent(),'—');
    assert.match(await page.locator('.records-table tbody').innerText(),/HTTP 502/);
    assert.ok((await page.locator('.records-table tbody').innerText()).includes(failure));
    assert.equal(await page.locator('#requestRecords img').count(),0);assert.equal(await page.evaluate(()=>window.qaUnsafe),undefined);
    assert.equal(await page.locator('#requestRecords pre,#requestRecords canvas,#requestRecords .cost,#requestRecords .num,#requestRecords .rt-replay').count(),0);
    if(locale==='zh')assert.deepEqual(await page.locator('.records-table th').allTextContents(),['供应商','模型','耗时','结果','错误原因']);
    await page.keyboard.press('Space');assert.equal(await section.isVisible(),false);
    if(engine==='chromium'&&locale==='zh'){
     const before=reads;await page.waitForTimeout(3300);assert.equal(reads,before,'collapsed section stops polling');
     await page.keyboard.press('Space');await page.waitForTimeout(100);
     phase='error';await page.locator('#recordsStatus button').waitFor();
     assert.equal(await page.locator('.records-table tbody tr').count(),6,'failed refresh retains previous records');
     await page.locator('#recordsStatus button').focus();await page.waitForTimeout(3200);
     assert.equal(await page.locator('#recordsStatus button').evaluate(e=>e===document.activeElement),true,'polling preserves retry focus');
     phase='records';await page.keyboard.press('Enter');await page.locator('#recordsStatus button').waitFor({state:'detached'});
     await page.locator('#nav [data-view="sessions"]').click();const left=reads;
     await page.waitForTimeout(3300);assert.equal(reads,left,'leaving Gateway stops polling');
     await page.evaluate(()=>openSettingsAt(null,"gateway"));await section.waitFor();
     await page.screenshot({path:resolve(root,'.qa/gateway-records-final.png')});
    }else await page.keyboard.press('Space');
    await page.setViewportSize({width:760,height:600});await fold.scrollIntoViewIfNeeded();
    assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'no horizontal page overflow');
    if(engine==='chromium'&&locale==='zh')await page.screenshot({path:resolve(root,'.qa/gateway-records-minimum.png')});
    await page.reload();await page.waitForFunction(()=>view==='settings'&&setShown==='gateway'&&!!providers);
    assert.equal(await section.isVisible(),false,'records start collapsed on reload');
    assert.equal(await page.locator('#gatewayAdvanced').isVisible(),false,'advanced configuration starts collapsed on reload');
    for(const p of ['/plugins/routing/client.js','/plugins/routing/i18n.js','/plugins/routing/routing.css'])assert.equal((await context.request.get(new URL(p,url).href)).status(),404);
    assert.deepEqual(errors,[]);assert.deepEqual(retired,[]);
    checks.push({engine,locale,retiredPageRemoved:true,oldLinksOpenGateway:true,fiveFields:true,statesAndFallback:true,mouseAndKeyboard:true,isolated:true});
   } finally { await context.close(); }
  }} finally { await browser.close(); }
 }
 assert.equal(readFileSync(resolve(app.home,'.codex/config.toml'),'utf8'),'# isolated QA; disconnected client\n');
 writeFileSync(resolve(root,'.qa/gateway-records-verification.json'),JSON.stringify(checks,null,2)+'\n','utf8');
 console.log(JSON.stringify({passed:true,checks:checks.length}));
} finally { await app.stop(); }

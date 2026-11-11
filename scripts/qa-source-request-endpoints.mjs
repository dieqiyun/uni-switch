// Compiled core, isolated client files, synthetic test responses and credentials.
import assert from 'node:assert/strict';
import {resolve} from 'node:path';
import {writeFileSync} from 'node:fs';
import {chromium, webkit} from 'playwright';
import AxeBuilder from '@axe-core/playwright';
import {startIsolated} from './qa-source-core.mjs';

const root=resolve(import.meta.dirname,'..');
const platform={win32:'pc-windows-msvc',linux:'unknown-linux-gnu',darwin:'apple-darwin'}[process.platform];
const arch=process.arch==='arm64'?'aarch64':'x86_64';
const app=await startIsolated(resolve(root,`src-tauri/binaries/uni-switch-core-${arch}-${platform}${process.platform==='win32'?'.exe':''}`));
const checks=[];
let seeded=false;
try {
 await app.wait(()=>!!app.ready());
 for(const [engine,launcher] of [['chromium',chromium],['webkit',webkit]]) {
  const browser=await launcher.launch(engine==='chromium'?{channel:'chromium'}:{});
  try {for(const language of ['en','zh','zh-TW','ja','de']) {
   const context=await browser.newContext({viewport:{width:1120,height:780},reducedMotion:'reduce'});
   try {
    const page=await context.newPage();page.setDefaultTimeout(8000);
    const errors=[];page.on('pageerror',e=>errors.push(e.message));
    const link=new URL(app.ready().url);link.searchParams.set('locale',language);
    await context.request.get(link.href);
    if(!seeded) {
     const response=await context.request.post(new URL('/api/provider/save',link).href,{data:{new:true,id:'qa-endpoints',name:'蝶祈云API',preset:'dieqiyun',chat:'https://qa.invalid/v1',responses:'https://qa.invalid/v1',anthropic:'https://qa.invalid',key:'qa-synthetic-endpoint-key',models:['qa-model']}});
     assert.ok(response.ok(),await response.text());seeded=true;
    }
    // Display the real provider with a saved, single Responses endpoint at node HW.
    await page.route(/\/api\/providers(?:\?|$)/,async route=>{
     const response=await route.fetch(), body=await response.json();
     const provider=body.providers.find(p=>p.id==='qa-endpoints');
     provider.chat='';provider.responses='https://hw.dieqiyun.top/v1';provider.anthropic='';provider.gemini='';provider.baseAPI='responses';
     await route.fulfill({response,json:body});
    });
    let mode='mixed',lastBody;
    await page.route('**/api/provider/test',async route=>{
     lastBody=route.request().postDataJSON();
     if(mode==='request-failure')return route.fulfill({status:400,json:{error:'503 synthetic upstream failure'}});
     if(mode==='pending') {await new Promise(r=>setTimeout(r,1000));try{await route.fulfill({json:{results:[]}});}catch{}return;}
     const apis=lastBody.endpointAPI==='auto'?['chat','responses','anthropic']:[lastBody.endpointAPI];
     await route.fulfill({json:{results:apis.map((protocol,i)=>({protocol,ok:mode==='success'||i===0,ms:18,model:'qa-model',status:403,error:'403 synthetic permission denied '+('long-diagnostic-'.repeat(150))+' <script>window.qaUnsafe=true</script>'}))}});
    });
    await page.route('**/api/provider/teststop',route=>route.fulfill({status:204,body:''}));
    await page.goto(link.href);await page.locator('#providers [data-id="qa-endpoints"] .uni-edit').click();
    const editor=page.locator('.editor'), choice=editor.locator('.request-endpoint'), url=editor.locator('.service-endpoint-url');
    assert.equal(await choice.inputValue(),'responses');
    assert.equal(await url.innerText(),'https://hw.dieqiyun.top/v1','a saved single endpoint retains its node');
    assert.equal(await choice.isVisible(),true);
    await choice.selectOption('');
    await editor.locator('.advanced > summary').click();
    const eps=editor.locator('.eps'), test=eps.locator(':scope > button[data-tests]');
    assert.equal(await eps.locator('.ep').count(),3,'Automatic restores the node full endpoint set');
    await test.click();await eps.locator('.connection-details').first().waitFor();
    assert.equal(lastBody.endpointAPI,'auto');
    const noOverlap=async()=>{
     const issues=await eps.evaluate(box=>{
      const rows=[...box.querySelectorAll(':scope > .ep')], problems=[];
      for(let i=0;i<rows.length;i++){
       const row=rows[i],r=row.getBoundingClientRect();
       for(const child of row.children){const b=child.getBoundingClientRect();if(b.top<r.top-1||b.bottom>r.bottom+1||b.right>r.right+1)problems.push('result outside row');}
       if(i&&rows[i-1].getBoundingClientRect().bottom>r.top+1)problems.push('rows overlap');
      }
      if(rows.at(-1).getBoundingClientRect().bottom>box.querySelector(':scope > button').getBoundingClientRect().top+1)problems.push('test button overlap');
      return problems;
     });assert.deepEqual(issues,[]);
    };
    await noOverlap();
    for(const width of [1120,760]){
     await page.setViewportSize({width,height:650});
     for(const details of await eps.locator('.connection-details').all())await details.evaluate(e=>e.open=true);
     await noOverlap();assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
     assert.equal(await page.evaluate(()=>!!window.qaUnsafe),false);
     if(engine==='chromium'&&language==='zh')await page.screenshot({path:resolve(root,`.qa/request-endpoints-${width}.png`)});
    }
    const audit=await new AxeBuilder({page}).include('.editor').withRules(['label','select-name','button-name']).analyze();
    assert.deepEqual(audit.violations,[]);
    for(const api of ['chat','responses','anthropic']) {
     await choice.selectOption(api);
     const current=editor.locator('.eps');mode='success';
     await current.locator(':scope > button[data-tests]').click();await current.locator('.res.ok').waitFor();
     assert.equal(lastBody.endpointAPI,api);
     assert.equal(await current.locator('.ep').count(),1);
     for(const key of ['chat','responses','anthropic'])assert.equal(!!lastBody[key],key===api);
    }
    const current=editor.locator('.eps');mode='request-failure';
    await current.locator(':scope > button[data-tests]').click();await current.locator('.endpoint-test-error .connection-details').waitFor();
    assert.equal(await current.locator('.endpoint-test-error').isVisible(),true);
    mode='success';await current.locator(':scope > button[data-tests]').click();await current.locator('.res.ok').waitFor();
    assert.equal(await current.locator('.endpoint-test-error').isVisible(),false,'retry clears the previous failure');
    mode='pending';const button=current.locator(':scope > button[data-tests]');
    await button.click();await page.waitForFunction(()=>Object.values(testsOn).some(r=>r.size));await button.click();
    await current.locator('.endpoint-test-error').waitFor();
    assert.equal(await current.locator('.endpoint-test-error').textContent(),await page.evaluate(()=>t('Test stopped')));
    assert.deepEqual(errors,[]);
    checks.push({engine,language,savedNode:true,endpointSelection:true,typedTest:true,inlineErrors:true,detailsNoOverlap:true,retry:true,stop:true,accessible:true});
   }finally{await context.close();}
  }}finally{await browser.close();}
 }
 writeFileSync(resolve(root,'.qa/request-endpoints-verification.json'),JSON.stringify(checks,null,2)+'\n','utf8');
 console.log(JSON.stringify({passed:true,checks:checks.length}));
}finally{await app.stop();}

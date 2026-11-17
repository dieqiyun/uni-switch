import assert from 'node:assert/strict';
import {writeFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {chromium,webkit} from 'playwright';
import AxeBuilder from '@axe-core/playwright';
import {startIsolated} from './qa-source-core.mjs';
const root=resolve(import.meta.dirname,'..');
const app=await startIsolated(resolve(root,process.argv[2]||'src-tauri/binaries/uni-switch-core-x86_64-pc-windows-msvc.exe'));
const checks=[];
try{
 await app.wait(()=>!!app.ready());
 for(const [engine,launcher] of [['chromium',chromium],['webkit',webkit]]){
  const browser=await launcher.launch(engine==='chromium'?{channel:'chromium'}:{});
  try{for(const language of ['en','zh','zh-TW','ja','de']){
   console.log(`Updates: ${engine}/${language}`);
   const context=await browser.newContext({viewport:{width:1120,height:780},reducedMotion:'reduce'});
   try{
    const page=await context.newPage();page.setDefaultTimeout(10000);
    const errors=[],posts=[],opened=[];
    page.on('pageerror',e=>errors.push(e.message));
    const current=app.ready().version;
    let state={current,state:'idle',autoCheck:true,installVia:'windows'};
    await page.route(url=>url.pathname==='/api/update'||url.pathname.startsWith('/api/update/'),async route=>{
     const request=route.request(),path=new URL(request.url()).pathname;
     if(request.method()==='POST'){
      const body=request.postDataJSON();posts.push({path,body});
      if(path.endsWith('/preferences'))state={...state,autoCheck:body.autoCheck};
      if(path.endsWith('/check'))state={...state,state:'available',available:true,latest:'999.0.0',notes:'Release notes\n修复更新提示',checkedAt:new Date().toISOString()};
      if(path.endsWith('/download')){assert.deepEqual(body,{version:'999.0.0'});state={...state,state:'downloading',done:5e6,total:20e6};}
      if(path.endsWith('/cancel'))state={...state,state:'available',done:0,total:0,task:''};
      if(path.endsWith('/install')){assert.deepEqual(body,{task:'verified-fixture'});state={...state,state:'handedoff'};}
     }
     await route.fulfill({json:state});
    });
    await page.route('**/api/open',async route=>{opened.push(route.request().postDataJSON());await route.fulfill({json:{}});});
    const url=new URL(app.ready().url);url.searchParams.set('locale',language);await page.goto(url.href);
    await page.locator('#uniClients').waitFor();await page.waitForFunction(()=>!!providers);
    await page.evaluate(()=>renderUpdateBadge());
    await page.locator('#update').waitFor();
    await page.locator('#update').focus();await page.keyboard.press('Enter');
    const panel=page.locator('.uni-update-panel');await panel.waitFor().catch(async error=>{
     console.error(JSON.stringify({errors,debug:await page.evaluate(()=>({view,setTab,about:document.querySelector('#about')?.outerHTML,fn:String(renderUpdate).slice(0,180)}))}));throw error;
    });
    const action=name=>panel.locator(`[data-update-action="${name}"]`);
    const toggle=panel.locator('.uni-update-toggle');
    await toggle.locator('button').first().click();
    await page.waitForFunction(()=>document.querySelector('.uni-update-panel')?.uniState?.autoCheck===false);
    await toggle.locator('button').last().click();
    await page.waitForFunction(()=>document.querySelector('.uni-update-panel')?.uniState?.autoCheck===true);
    await action('check').focus();await page.keyboard.press('Enter');
    await page.waitForFunction(()=>document.querySelector('.uni-update-panel')?.uniState?.state==='available');
    assert.equal(posts.filter(p=>p.path.endsWith('/download')).length,0,'checking never downloads');
    await page.evaluate(()=>renderUpdateBadge());
    assert.ok(await page.locator('#update').evaluate(b=>b.classList.contains('uni-update-available')));
    await action('download').click();
    await page.waitForFunction(()=>document.querySelector('.uni-update-status')?.textContent.includes('25%'));
    assert.equal(await panel.locator('progress').getAttribute('value'),'25');
    assert.equal(posts.filter(p=>p.path.endsWith('/install')).length,0,'download never installs');
    await action('cancel').click();
    await page.waitForFunction(()=>document.querySelector('.uni-update-panel')?.uniState?.state==='available');
    state={...state,state:'error',error:'fixture network interrupted'};
    await page.evaluate(()=>renderUpdate(document.querySelector('.uni-update-row')));
    assert.match(await panel.locator('[role="status"]').innerText(),/fixture network interrupted/);
    assert.ok(await action('download').isVisible(),'a failed download can retry');
    await action('download').click();
    state={...state,state:'ready',task:'verified-fixture',done:20e6,total:20e6};
    await page.evaluate(()=>renderUpdate(document.querySelector('.uni-update-row')));
    await action('install').waitFor();
    assert.equal(posts.filter(p=>p.path.endsWith('/install')).length,0);
    await action('cancel').click();
    assert.equal(posts.filter(p=>p.path.endsWith('/install')).length,0,'discarding ready package never installs');
    await action('github').click();assert.equal(opened.at(-1).url,'https://github.com/dieqiyun/uni-switch/releases');
    state={...state,state:'ready',task:'verified-fixture',done:20e6,total:20e6};
    await page.evaluate(()=>renderUpdate(document.querySelector('.uni-update-row')));
    await action('install').click();
    await page.waitForFunction(()=>document.querySelector('.uni-update-panel')?.uniState?.state==='handedoff');
    assert.equal(posts.filter(p=>p.path.endsWith('/install')).length,1,'only explicit Install hands off');
    state={...state,state:'ready',task:'verified-fixture'};
    await page.setViewportSize({width:760,height:600});
    await page.evaluate(()=>renderUpdate(document.querySelector('.uni-update-row')));
    assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
    const axe=await new AxeBuilder({page}).include('.uni-update-row').withRules(['button-name','aria-allowed-attr','aria-required-attr','color-contrast']).analyze();
    assert.deepEqual(axe.violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>n.target)})),[]);
    if(engine==='chromium'&&language==='zh'){
     await page.setViewportSize({width:1120,height:780});await panel.scrollIntoViewIfNeeded();
     await page.screenshot({path:resolve(root,'.qa/desktop-update-ready.png')});
    }
    assert.deepEqual(errors,[]);checks.push({engine,language,checksWithoutDownload:true,visibleBadge:true,progress:true,cancel:true,retry:true,explicitInstall:true,githubFallback:true,keyboard:true,narrowLayout:true,accessibility:true});
   }finally{await context.close();}
  }}finally{await browser.close();}
 }
 writeFileSync(resolve(root,'.qa/desktop-update-ui.json'),JSON.stringify(checks,null,2)+'\n');
 console.log(JSON.stringify({passed:true,checks:checks.length}));
}finally{await app.stop();}

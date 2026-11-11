import assert from 'node:assert/strict';
import {writeFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {chromium,webkit} from 'playwright';
import {startIsolated} from './qa-source-core.mjs';
const root=resolve(import.meta.dirname,'..');
const app=await startIsolated(resolve(root,'src-tauri/binaries/uni-switch-core-x86_64-pc-windows-msvc.exe'));
const checks=[];
async function checkClientScroll(page){
 const lower=()=>page.locator('#nav [data-view="sessions"],#nav [data-view="library"],#prefs,#update').evaluateAll(buttons=>buttons.map(b=>{const r=b.getBoundingClientRect();return {id:b.id||b.dataset.view,top:r.top,bottom:r.bottom};}));
 const list=page.locator('#uniClients');
 const initial=await lower();
 const geometry=await list.evaluate(b=>({top:b.getBoundingClientRect().top,bottom:b.getBoundingClientRect().bottom,height:b.clientHeight,content:b.scrollHeight,overflow:getComputedStyle(b).overflowY}));
 assert.ok(geometry.height>60&&geometry.content>geometry.height,'clients have their own bounded scroll area');
 assert.equal(geometry.overflow,'auto');
 assert.ok(initial.every(b=>b.top>=geometry.bottom&&b.bottom<=page.viewportSize().height),'all lower navigation remains visible below clients');
 await list.evaluate(b=>b.scrollTop=0);
 await list.hover();await page.mouse.wheel(0,900);
 await page.waitForFunction(()=>document.querySelector('#uniClients').scrollTop>0);
 assert.deepEqual(await lower(),initial,'scrolling clients does not move lower navigation');
 await page.locator('[data-uni-client="qoder-cn"]').focus();
 const at=await list.evaluate(b=>b.scrollTop);
 await page.evaluate(()=>renderAgents());
 assert.equal(await list.evaluate(b=>b.scrollTop),at,'state refresh preserves client scroll');
 assert.equal(await page.evaluate(()=>document.activeElement?.dataset.uniClient),'qoder-cn','state refresh preserves keyboard focus');
 await page.locator('#prefs').hover();await page.mouse.wheel(0,900);
 await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
 assert.equal(await list.evaluate(b=>b.scrollTop),at,'wheel over lower navigation does not scroll clients');
 assert.deepEqual(await lower(),initial,'lower controls keep their positions');
 assert.equal(await page.locator('.side-scroll').evaluate(b=>b.scrollTop),0,'the whole sidebar does not scroll');
}
try{
 await app.wait(()=>!!app.ready());
 for(const [engine,launcher] of [['chromium',chromium],['webkit',webkit]]){
  const browser=await launcher.launch(engine==='chromium'?{channel:'chromium',ignoreDefaultArgs:['--hide-scrollbars']}:{});
  try{for(const language of ['en','zh','zh-TW','ja','de']){
   const context=await browser.newContext({viewport:{width:1120,height:880},reducedMotion:'reduce'});
   try{
    const page=await context.newPage();page.setDefaultTimeout(10000);
    const errors=[],opened=[],requests=[];let phase='none';
    page.on('pageerror',e=>errors.push(e.message));page.on('request',r=>{if(new URL(r.url()).pathname.startsWith('/api/groups'))requests.push(r.url());});
    await page.route('**/api/open',async route=>{opened.push(route.request().postDataJSON());await route.fulfill({json:{}});});
    await page.route('**/api/update?*',async route=>phase==='error'?route.abort():route.fulfill({json:phase==='available'?{current:'0.5.28',latest:'999.0.0',state:'available'}:phase==='same'?{current:'0.5.28',latest:'0.5.28',state:'available'}:{current:'0.5.28'}}));
    const url=new URL(app.ready().url);url.searchParams.set('locale',language);await page.goto(url.href);
    await page.locator('#uniClients').waitFor();await page.waitForFunction(()=>!!providers);
    await page.evaluate(()=>renderUpdateBadge());
    assert.equal(await page.locator('#uniVersion,.uni-sidebar-note').count(),0);
    assert.equal(await page.locator('#update').count(),1);
    assert.match(await page.locator('#update').innerText(),/v0\.5\.28/);
    assert.equal(await page.locator('#update.uni-update-available').count(),0);
    assert.equal(await page.locator('#updateHide').isVisible(),false);
    await checkClientScroll(page);
    await page.locator('#update').focus();await page.keyboard.press('Enter');
    await page.waitForFunction(()=>view==='settings'&&setTab==='about');
    const prefs=page.locator('#prefs'),sub=page.locator('#nav-sub-settings');
    await sub.waitFor();assert.equal(await prefs.getAttribute('aria-expanded'),'true');
    assert.equal(await prefs.getAttribute('aria-controls'),'nav-sub-settings');
    await prefs.click();assert.equal(await sub.isVisible(),false);assert.equal(await prefs.getAttribute('aria-expanded'),'false');
    await page.evaluate(()=>{navSubsAt='';navSubs();});assert.equal(await sub.isVisible(),false);
    await prefs.focus();await page.keyboard.press('Enter');assert.equal(await sub.isVisible(),true);
    await checkClientScroll(page);
    await prefs.focus();
    await page.keyboard.press('Space');assert.equal(await sub.isVisible(),false);
    await page.keyboard.press('Space');assert.equal(await sub.isVisible(),true);
    phase='available';await page.evaluate(()=>renderUpdateBadge());
    assert.equal(await page.locator('#update.uni-update-available').count(),1);
    assert.match(await page.locator('#update').getAttribute('title'),/999\.0\.0/);
    if(language==='zh')assert.match(await page.locator('#update').innerText(),/有新版本/);
    await page.locator('#update').click();assert.equal(opened.at(-1).url,'https://github.com/dieqiyun/uni-switch/releases');
    phase='error';await page.evaluate(()=>renderUpdateBadge());assert.equal(await page.locator('#update.uni-update-available').count(),1);
    phase='same';await page.evaluate(()=>renderUpdateBadge());assert.equal(await page.locator('#update.uni-update-available').count(),0);
    phase='none';await page.evaluate(()=>renderUpdateBadge());
    await page.evaluate(()=>openSettingsAt(null,"gateway"));await page.waitForFunction(()=>view==='settings'&&setShown==='gateway');
    await page.locator('#nav [data-view="sessions"]').click();await page.waitForFunction(()=>view==='sessions');
    assert.equal(await page.locator('#nav [data-view="groups"]').count(),0);
    assert.deepEqual(requests,[]);
    await page.locator('[data-uni-client="codex"]').click();
    await page.setViewportSize({width:760,height:600});
    await checkClientScroll(page);
    await prefs.click();await sub.waitFor();
    await checkClientScroll(page);
    await page.evaluate(()=>document.documentElement.dataset.theme='dark');
    await checkClientScroll(page);
    await page.evaluate(()=>document.documentElement.dataset.theme='light');
    await prefs.click();
    if(engine==='chromium'&&language==='zh'){
     await page.locator('#uniClients').evaluate(b=>b.scrollTop=0);
     await page.screenshot({path:resolve(root,'.qa/sidebar-compact-minimum.png')});
     await page.setViewportSize({width:1120,height:880});
     assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
     await page.screenshot({path:resolve(root,'.qa/sidebar-compact-final.png')});
    }
    assert.deepEqual(errors,[]);checks.push({engine,language,oneVersionRow:true,updateStates:true,disclosureMouseAndKeyboard:true,refreshKeepsCollapsed:true,groupsRemoved:true,independentClientScroll:true,fixedLowerNavigation:true,refreshKeepsClientScroll:true,smallWindowAndDarkTheme:true});
   }finally{await context.close();}
  }}finally{await browser.close();}
 }
 writeFileSync(resolve(root,'.qa/sidebar-compact-verification.json'),JSON.stringify(checks,null,2)+'\n');
 console.log(JSON.stringify({passed:true,checks:checks.length}));
}finally{await app.stop();}

import assert from 'node:assert/strict';
import {writeFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {chromium,webkit} from 'playwright';
import AxeBuilder from '@axe-core/playwright';
import {startIsolated} from './qa-source-core.mjs';
const root=resolve(import.meta.dirname,'..');
const app=await startIsolated(resolve(root,process.argv[2]||'src-tauri/binaries/uni-switch-core-x86_64-pc-windows-msvc.exe'));
const checks=[];
const lower=page=>page.locator('#nav [data-view="sessions"],#nav [data-view="library"],#prefs,#update').evaluateAll(buttons=>buttons.map(b=>{const r=b.getBoundingClientRect();return {id:b.id||b.dataset.view,top:r.top,bottom:r.bottom};}));
async function checkFit(page){
 const geometry=await page.locator('#uniClients').evaluate(b=>({bottom:b.getBoundingClientRect().bottom,height:b.clientHeight,content:b.scrollHeight,overflow:getComputedStyle(b).overflowY}));
 assert.equal(geometry.overflow,'visible','the client list has no independent scroll area');
 assert.ok(geometry.content<=geometry.height+1,'all client rows fit');
 const buttons=await lower(page);
 assert.equal(buttons.length,4,'Sessions, Library, Settings and Version are present');
 assert.ok(buttons.every(b=>b.top>=geometry.bottom+20&&b.bottom<=page.viewportSize().height-12),'lower navigation has space above and below');
 assert.equal(await page.locator('.side-scroll').evaluate(b=>b.scrollTop),0,'the sidebar stays at the top');
 assert.ok(await page.locator('.side-scroll').evaluate(b=>b.scrollHeight<=b.clientHeight+1),'the supported window needs no sidebar scrolling');
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'page has no horizontal overflow');
 // Read in one browser task: a state refresh can replace nodes between a
 // locator resolving and its later evaluation, especially in WebKit.
 const names=await page.evaluate(()=>[...document.querySelectorAll('#uniClients [data-uni-client]')].filter(b=>b.getClientRects().length).map(button=>{const b=button.querySelector('strong'),r=document.createRange();r.selectNodeContents(b);return {text:b.textContent,lines:r.getClientRects().length,width:b.clientWidth,content:b.scrollWidth};}));
 assert.equal(names.length,await page.locator('#uniClientsToggle').getAttribute('aria-expanded')==='true'?11:8);
 assert.deepEqual(names.filter(b=>b.lines!==1||b.content>b.width+1),[],'client names fit on one line');
}
async function checkDisclosure(page){
 const toggle=page.locator('#uniClientsToggle'),clients=page.locator('#uniClients [data-uni-client]:visible');
 assert.equal(await clients.count(),8,'exactly eight clients show by default');
 assert.equal(await toggle.getAttribute('aria-expanded'),'false');
 assert.equal(await toggle.getAttribute('aria-controls'),'uniClientsMore');
 await checkFit(page);
 const initial=await lower(page),width=(await page.locator('#side').boundingBox()).width;
 await toggle.focus();await page.keyboard.press('Enter');
 assert.equal(await clients.count(),11,'Show more reveals every client');
 assert.equal(await toggle.getAttribute('aria-expanded'),'true');
 assert.ok((await page.locator('#side').boundingBox()).width>width,'the sidebar expands to make room');
 await checkFit(page);
 assert.deepEqual(await lower(page),initial,'expanding clients leaves lower navigation in place');
 await page.locator('[data-uni-client="qoder-cn"]').focus();
 await page.evaluate(()=>renderAgents());
 assert.equal(await page.evaluate(()=>document.activeElement?.dataset.uniClient),'qoder-cn','refresh preserves client focus');
 assert.equal(await clients.count(),11,'refresh preserves the expansion');
 await page.keyboard.press('Enter');
 await page.waitForFunction(()=>document.querySelector('[data-uni-client="qoder-cn"]').getAttribute('aria-current')==='page');
 await toggle.focus();await page.evaluate(()=>renderAgents());
 assert.equal(await page.evaluate(()=>document.activeElement?.id),'uniClientsToggle','refresh preserves disclosure focus');
 await page.keyboard.press('Space');
 assert.equal(await clients.count(),8,'Space collapses to eight clients');
 assert.equal((await page.locator('#side').boundingBox()).width,width);
 await checkFit(page);
 await page.keyboard.press('Tab');
 assert.equal(await page.evaluate(()=>document.activeElement?.dataset.view),'sessions','collapsed clients are not in the tab order');
 await toggle.click();await toggle.click();
 assert.equal(await clients.count(),8,'mouse clicks expand and collapse');
 await page.locator('[data-uni-client="codex"]').click();
}
try{
 await app.wait(()=>!!app.ready());
 for(const [engine,launcher] of [['chromium',chromium],['webkit',webkit]]){
  const browser=await launcher.launch(engine==='chromium'?{channel:'chromium',ignoreDefaultArgs:['--hide-scrollbars']}:{});
  try{for(const language of ['en','zh','zh-TW','ja','de']){
   console.log(`Sidebar: ${engine}/${language}`);
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
    await checkDisclosure(page);
    await page.locator('#update').focus();await page.keyboard.press('Enter');
    await page.waitForFunction(()=>view==='settings'&&setTab==='about');
    const prefs=page.locator('#prefs');
    assert.ok(await page.locator('#setTabs').isVisible(),'settings sections are visible in the content pane');
    assert.equal(await page.locator('#nav-sub-settings').count(),0);
    await prefs.focus();await page.keyboard.press('Enter');
    await checkFit(page);
    await page.locator('#setTabs [data-set="network"]').click();
    await page.waitForFunction(()=>setTab==='network');
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
    for(const viewport of [{width:1120,height:774},{width:760,height:600}]){
     await page.setViewportSize(viewport);await checkDisclosure(page);
     await prefs.click();await page.locator('#setTabs').waitFor();await checkFit(page);
     await page.locator('#uniClientsToggle').click();await checkFit(page);
     await page.evaluate(()=>document.documentElement.dataset.theme='dark');await checkFit(page);
     await page.evaluate(()=>document.documentElement.dataset.theme='light');
     await page.locator('#uniClientsToggle').click();await checkFit(page);
     await page.locator('#nav [data-view="library"]').click();
     await page.locator('#view-library .lib-tabs').waitFor();await checkFit(page);
     await page.locator('[data-uni-client="codex"]').click();
    }
    const accessibility=await new AxeBuilder({page}).include('#side').withRules(['button-name','aria-allowed-attr','aria-required-attr','color-contrast']).analyze();
    assert.deepEqual(accessibility.violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>n.target)})),[]);
    if(engine==='chromium'&&language==='zh'){
     await page.screenshot({path:resolve(root,'.qa/sidebar-clients-minimum.png')});
     await page.setViewportSize({width:1120,height:774});
     await page.screenshot({path:resolve(root,'.qa/sidebar-clients-collapsed.png')});
     await page.locator('#uniClientsToggle').click();
     await page.screenshot({path:resolve(root,'.qa/sidebar-clients-expanded.png')});
     await prefs.click();await page.locator('#setTabs').waitFor();
     await page.screenshot({path:resolve(root,'.qa/sidebar-clients-settings.png')});
    }
    assert.deepEqual(errors,[]);checks.push({engine,language,oneVersionRow:true,updateStates:true,eightClientsByDefault:true,allElevenExpanded:true,disclosureMouseAndKeyboard:true,refreshKeepsExpansionAndFocus:true,groupsRemoved:true,noClientScroll:true,fixedLowerNavigation:true,settingsAndLibraryTabs:true,smallWindowAndDarkTheme:true,accessibility:true});
   }finally{await context.close();}
  }}finally{await browser.close();}
 }
 writeFileSync(resolve(root,'.qa/sidebar-clients-verification.json'),JSON.stringify(checks,null,2)+'\n');
 console.log(JSON.stringify({passed:true,checks:checks.length}));
}finally{await app.stop();}

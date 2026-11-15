import assert from 'node:assert/strict';
import {resolve} from 'node:path';
import {writeFileSync} from 'node:fs';
import {chromium,webkit} from 'playwright';
import {startIsolated} from './qa-source-core.mjs';
const root=resolve(import.meta.dirname,'..');
const app=await startIsolated(resolve(root,'src-tauri/binaries/uni-switch-core-x86_64-pc-windows-msvc.exe'));
const checks=[];
try{
 await app.wait(()=>!!app.ready());
 for(const [engine,launcher] of [['chromium',chromium],['webkit',webkit]]){
  const browser=await launcher.launch(engine==='chromium'?{channel:'chromium'}:{});
  try{
   const page=await browser.newPage({viewport:{width:1120,height:820},reducedMotion:'reduce'});
   const errors=[];let restarts=0;
   page.on('pageerror',e=>errors.push(e.message));
   await page.route('**/api/gateway/restart',route=>{restarts++;return route.fulfill({json:{ok:true}});});
   const url=new URL(app.ready().url);url.searchParams.set('locale','zh');url.searchParams.set('view','settings');url.searchParams.set('tab','gateway');
   await page.goto(url.href);await page.waitForFunction(()=>!!prefs&&!!providers&&setShown==='gateway');
   await page.locator('#gateway .gw-fix').waitFor();
   await page.evaluate(()=>{copy=async value=>{window.qaCopied=value;};});
   await page.locator('#gateway .gateway-address button').click();
   assert.equal(await page.evaluate(()=>window.qaCopied),await page.locator('#gateway .gateway-address code').textContent());
   await page.locator('#gateway .gw-fix').click();assert.equal(restarts,1);
   await page.waitForFunction(()=>!gatewayFix.busy);
   if(engine==='chromium')await page.screenshot({path:resolve(root,'.qa/gateway-settings-compact.png')});
   const advanced=page.locator('#foldGatewayAdvanced');
   await advanced.click();await page.locator('#addGatewayKey').waitFor();
   await page.locator('#addGatewayKey').click();
   await page.getByRole('textbox',{name:'网关密钥名称',exact:true}).fill('QA synthetic client');
   await page.locator('#gatewayKeys .adding .primary').click();
   const row=page.locator('#gatewayKeys .acc[data-key]').filter({hasText:'QA synthetic client'});await row.waitFor();
   const key=await row.getAttribute('data-key');
   await row.getByRole('button',{name:'复制网关密钥',exact:true}).click();
   await page.waitForFunction(()=>window.qaCopied!==document.querySelector('#gateway .gateway-address code').textContent);
   assert.ok(await page.evaluate(()=>window.qaCopied.length>8));
   const fresh=await page.context().request.get(new URL('/api/caller-keys',url).href);
   assert.ok((await fresh.json()).keys.some(k=>k.id===key));
   await advanced.click();assert.equal(await page.locator('#gatewayAdvanced').isVisible(),false);
   await page.locator('#setTab-network').evaluate(e=>e.click());
   await page.waitForFunction(()=>setShown==='network');
   assert.equal(await page.locator('#setPage-network #portList').count(),0);
   await page.evaluate(()=>setSetTab('gateway',true));
   await advanced.click();await row.waitFor();
   assert.match(await row.innerText(),/QA synthetic client/);
   await page.setViewportSize({width:760,height:600});
   assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'advanced configuration fits narrow windows');
   await advanced.click();
   if(engine==='chromium')await page.screenshot({path:resolve(root,'.qa/gateway-settings-compact-minimum.png')});
   await page.evaluate(async()=>{await gatewaySettingsRefresh;});
   await page.route('**/api/providers',route=>route.fulfill({status:503,json:{error:'synthetic state read failure'}}));
   await page.evaluate(()=>refreshGatewaySettings());await page.locator('#gatewaySettingsStatus button').waitFor();
   assert.ok((await page.locator('#gateway .gateway-address code').textContent()).includes('127.0.0.1'));
   await page.unroute('**/api/providers');await page.locator('#gatewaySettingsStatus button').click();
   await page.locator('#gatewaySettingsStatus button').waitFor({state:'detached'});
   const removed=await page.context().request.post(new URL('/api/caller-keys/remove-key',url).href,{data:{key}});assert.equal(removed.status(),200);
   assert.deepEqual(errors,[]);
   checks.push({engine,directSettingsLink:true,copyAddress:true,restart:true,createAndCopyKey:true,keysRetainedAcrossTabs:true,narrowAdvanced:true,errorRetry:true});
  }finally{await browser.close();}
 }
 writeFileSync(resolve(root,'.qa/gateway-settings-verification.json'),JSON.stringify(checks,null,2)+'\n','utf8');
 console.log(JSON.stringify({passed:true,checks:checks.length}));
}finally{await app.stop();}

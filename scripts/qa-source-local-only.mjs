// Exercise the compiled local-only gateway UI without touching installed apps.
import {spawnSync} from 'node:child_process';
import {mkdirSync,readFileSync,writeFileSync} from 'node:fs';
import {resolve} from 'node:path';
import assert from 'node:assert/strict';
import {chromium,webkit} from 'playwright';
import {startIsolated} from './qa-source-core.mjs';

const root=resolve(import.meta.dirname,'..');
const build=spawnSync(process.execPath,['scripts/build-go.mjs','--qa'],{cwd:root,encoding:'utf8',windowsHide:true});
if(build.status!==0)throw new Error(build.stderr||'QA core build failed');
const platform={win32:'pc-windows-msvc',linux:'unknown-linux-gnu',darwin:'apple-darwin'}[process.platform];
const arch=process.arch==='arm64'?'aarch64':'x86_64';
const binary=resolve(root,`src-tauri/binaries/uni-switch-core-${arch}-${platform}${process.platform==='win32'?'.exe':''}`);
const app=await startIsolated(binary),checks=[];
try{
 await app.wait(()=>!!app.ready());const link=app.ready().url;
 for(const [engine,launcher]of [['chromium',chromium],['webkit',webkit]]){
  const browser=await launcher.launch(engine==='chromium'?{channel:'chromium'}:{});
  try{for(const locale of ['en','zh','zh-TW','ja','de']){
   const context=await browser.newContext({viewport:{width:1000,height:780},reducedMotion:'reduce'});
   await context.addInitScript(()=>localStorage.setItem('uni-switch.gwConnectFolded','0'));
   const page=await context.newPage();page.setDefaultTimeout(10000);
   const errors=[],obsolete=[];
   page.on('pageerror',e=>errors.push(e.message));
   page.on('request',r=>{if(/\/api\/settings\/(lan|allowips)(?:\?|$)/.test(r.url()))obsolete.push(r.url());});
   try{
    const url=new URL(link);url.searchParams.set('locale',locale);url.searchParams.set('view','settings');url.searchParams.set('tab','network');
    await page.goto(url.href);await page.locator('#setPage-network').waitFor({state:'visible'});
    assert.equal(await page.locator('#lanList,#allowList,#gatewayLan,#gatewayKeysOff').count(),0);
    await page.locator('#corsList input').waitFor({state:'visible'});
    for(const path of ['/api/settings/lan','/api/settings/allowips']){
     const response=await context.request.post(new URL(path,link).href,{data:{on:true,ips:['0.0.0.0/0']}});
     assert.equal(response.status(),404,path);
    }
    const state=await (await context.request.get(new URL('/api/settings',link).href)).json();
    for(const field of ['lan','lanURLs','publicURL','publicKind','onNetwork','allowIPs','allowIPsEnv','allowIPsFromEnv'])assert.equal(state[field],undefined,field);
    assert.match(state.gateway,/^http:\/\/(?:127\.|\[::1\])/);
    if(engine==='chromium'&&locale==='zh'){
     mkdirSync(resolve(root,'.qa'),{recursive:true});
     await page.screenshot({path:resolve(root,'.qa/local-only-network.png')});
    }
    await page.evaluate(()=>openSettingsAt(null,"gateway"));
    await page.locator('#setPage-gateway').waitFor({state:'visible'});
    await page.locator('#foldGatewayAdvanced').click();
    await page.locator('#gateway .who').waitFor({state:'visible'});
    await page.locator('#gatewayKeysBlock').waitFor({state:'visible'});
    const fold=page.locator('#foldGatewayAdvanced');
    await page.waitForFunction(()=>/127\.|localhost|\[::1\]/.test(document.querySelector('#connect .snip')?.textContent||''));
    try { await page.locator('#connect').waitFor({state:'visible'}); }
    catch(e) { throw new Error(JSON.stringify({engine,locale,errors,fold:await fold.getAttribute('aria-expanded'),gateway:await page.locator('#gateway').innerText(),connect:await page.locator('#connect').innerText()})+'\n'+e.message); }
    assert.match(await page.locator('#connect').innerText(),/127\.|localhost|\[::1\]/);
    assert.equal(await page.locator('#connectAddress,#gatewayLan,#gatewayKeysOff').count(),0);
    // The real API still creates local named keys; Connect reads their secrets.
    const createdResponse=await context.request.post(new URL('/api/caller-keys/add-key',link).href,{data:{name:`Local QA ${engine} ${locale}`}});
    assert.equal(createdResponse.status(),200);
    const created=await createdResponse.json();
    assert.ok(created.secret);
    const key=created.keys.find(k=>k.name===`Local QA ${engine} ${locale}`);
    assert.ok(key?.id);
    const gatewayPage=new URL(page.url());
    gatewayPage.searchParams.set('view','gateway');gatewayPage.searchParams.delete('tab');
    await page.goto(gatewayPage.href);
    await page.locator('#foldGatewayAdvanced').click();
    await page.locator('#connectKey').waitFor({state:'visible'});
    await page.locator('#connectKey').click();
    await page.locator('.proto-menu .pm-item',{hasText:key.name}).click();
    await page.waitForFunction(secret=>document.querySelector('#connect .snip')?.textContent.includes(secret),created.secret);
    const gatewayState=await (await context.request.get(new URL('/api/providers',link).href)).json();
    assert.match(gatewayState.gateway.url,/^http:\/\/(?:127\.|\[::1\])/);
    // Forwarded traffic is refused even when sent to the actual loopback socket.
    const forwarded=await context.request.get(new URL('/v1/models',gatewayState.gateway.url).href,{headers:{Authorization:`Bearer ${created.secret}`,'X-Forwarded-For':'192.168.1.20'}});
    assert.equal(forwarded.status(),403);
    const removed=await context.request.post(new URL('/api/caller-keys/remove-key',link).href,{data:{key:key.id}});
    assert.equal(removed.status(),200);
    if(engine==='chromium'&&locale==='zh'){
     await page.screenshot({path:resolve(root,'.qa/local-only-gateway.png')});
     await page.setViewportSize({width:760,height:600});
     assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
    }
    assert.deepEqual(errors,[],`${engine} ${locale}: script errors`);
    assert.deepEqual(obsolete,[],`${engine} ${locale}: retired API requested`);
    checks.push({engine,locale,removedSettings:true,noRemoteAddresses:true,localKeysAndConnect:true,namedKeyCreatedAndSelected:true,forwardedRequestForbidden:true});
   }finally{await context.close();}
  }}finally{await browser.close();}
 }
 assert.equal(readFileSync(resolve(app.home,'.codex/config.toml'),'utf8'),'# isolated QA; disconnected client\n');
 writeFileSync(resolve(root,'.qa/local-only-ui-checks.json'),JSON.stringify(checks,null,2)+'\n','utf8');
 console.log(JSON.stringify({checks:checks.length,passed:true,isolatedClientUnchanged:true}));
}finally{await app.stop();}

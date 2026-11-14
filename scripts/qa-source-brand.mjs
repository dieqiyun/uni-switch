import {spawnSync} from 'node:child_process';
import {writeFileSync} from 'node:fs';
import {resolve} from 'node:path';
import assert from 'node:assert/strict';
import {chromium,webkit} from 'playwright';
import {startIsolated} from './qa-source-core.mjs';
const root=resolve(import.meta.dirname,'..');
const build=spawnSync(process.execPath,['scripts/build-go.mjs','--qa'],{cwd:root,encoding:'utf8',windowsHide:true});
if(build.status!==0)throw Error(build.stderr||'QA build failed');
const platform={win32:'pc-windows-msvc',linux:'unknown-linux-gnu',darwin:'apple-darwin'}[process.platform];
const arch=process.arch==='arm64'?'aarch64':'x86_64';
const app=await startIsolated(resolve(root,`src-tauri/binaries/uni-switch-core-${arch}-${platform}${process.platform==='win32'?'.exe':''}`));
const checks=[];
try{
 await app.wait(()=>!!app.ready());const link=app.ready().url;
 for(const [engine,launcher] of [['chromium',chromium],['webkit',webkit]]){
  const browser=await launcher.launch(engine==='chromium'?{channel:'chromium'}:{});
  try{
   for(const language of ['en','zh','zh-TW','ja','de']){
    const context=await browser.newContext({viewport:{width:1120,height:780},reducedMotion:'reduce'});
    try{
     const page=await context.newPage();page.setDefaultTimeout(7000);const errors=[];page.on('pageerror',e=>errors.push(e.message));
     const url=new URL(link);url.searchParams.set('locale',language);await page.goto(url.href);
     await page.locator('#uniClients').waitFor();
     assert.match(await page.title(),/uni-switch/);
     assert.doesNotMatch(await page.locator('body').innerText(),/magpie/i);
     const directory=await (await context.request.get(new URL('/api/providers',link).href)).json();
     assert.ok(!directory.presets.some(p=>p.kind==='vendor'));
     assert.ok(!directory.presets.some(p=>p.kind==='remote'));
     assert.doesNotMatch(JSON.stringify(directory),/magpie/i);
     for(const view of ['agents','providers','settings']){
      await page.evaluate(v=>show(v),view);
      assert.doesNotMatch(await page.locator('body').innerText(),/magpie/i,`${engine} ${language} ${view}`);
     }
     await page.evaluate(()=>show('providers'));
     await page.locator('.uni-add').click();
     await page.locator('.editor').waitFor();
     assert.doesNotMatch(await page.locator('.editor').innerText(),/magpie/i);
     if(engine==='chromium'&&language==='zh'){
      await page.screenshot({path:resolve(root,'.qa/uni-switch-brand-final.png')});
      await page.setViewportSize({width:760,height:600});
      assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
     }
     assert.deepEqual(errors,[]);checks.push({engine,language,pages:3,brand:'uni-switch',vendorPresetsRemoved:true});
    }finally{await context.close();}
   }
  }finally{await browser.close();}
 }
 writeFileSync(resolve(root,'.qa/brand-ui-checks.json'),JSON.stringify(checks,null,2)+'\n','utf8');
 console.log(JSON.stringify({checks:checks.length,passed:true}));
}finally{await app.stop();}

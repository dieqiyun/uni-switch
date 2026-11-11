const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs/promises');
const path=require('node:path');
const {chromium,webkit}=require('playwright');
const assets=path.resolve(__dirname,'../assets');
for(const [engine,browserType] of Object.entries({chromium,webkit}))for(const lang of ['en','zh','zh-TW','ja','de']){
 test(`${engine} ${lang}: plugin-declared keyless access skips keys and retains one identity`,async(t)=>{
  const browser=await browserType.launch({headless:true});t.after(()=>browser.close());
  const page=await browser.newPage({viewport:{width:900,height:900},reducedMotion:'reduce'});
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  const asked=[],errors=[];let signed=false;page.on('pageerror',e=>errors.push(e.message));
  const methods=[{type:'api',label:'Enable fixed access',keyless:true}];
  const plugins=[{id:'fixed-access-plugin',pid:'fixed-access',name:'PublicFixture',icon:'generic',spec:'public-fixture',methods,single:true},{id:'ordinary-fixture',pid:'ordinary-fixture',name:'OrdinaryFixture',icon:'generic',spec:'ordinary-fixture',methods:[{type:'api',label:'Personal credential'}]}];
  const payload=()=>({providers:[{id:'openai',name:'OpenAI',icon:'openai',models:[],agents:[],key:{}},...(signed?[{id:'fixed-access-plugin',name:'PublicFixture',icon:'generic',models:[],agents:[],key:{},account:{agent:'fixed-access-plugin',agentName:'PublicFixture',agentIcon:'generic',user:'public-fixture',logins:[{user:'public-fixture',active:true,on:true}]}}]:[])],presets:[],excluded:[],gateway:{running:true,window:true},plugins:plugins.map((p,i)=>({...p,signedIn:i===0&&signed,models:1}))});
  await page.route('**/*',async route=>{
   const u=new URL(route.request().url()),json=data=>route.fulfill({json:data});
   if(u.pathname==='/boot.js')return route.fulfill({contentType:'text/javascript',body:`window.bootPrefs=${JSON.stringify({lang,theme:'light',web:true})}`});
   if(u.pathname==='/wails/runtime.js')return route.fulfill({contentType:'text/javascript',body:'export const Window = {};'});
   if(u.pathname==='/api/state')return json({agents:[],profiles:[],settings:{lang,theme:'light'}});
   if(u.pathname==='/api/providers')return json(payload());
   if(u.pathname==='/api/plugin-signin/prompt')return json({prompt:null,inputs:{}});
   if(u.pathname==='/api/plugin-signin'){
    const b=route.request().postDataJSON();asked.push(b);if(lang==='en' && asked.length===1)return route.fulfill({status:400,json:{error:'fixture denied'}});signed=true;
    return json({agent:'fixed-access-plugin',state:'done',user:'public-fixture',using:true,again:asked.length>1});
   }
   if(u.pathname==='/api/groups')return json({groups:[],models:[]});
   if(u.pathname==='/api/plugins')return json({plugins:[]});
   if(u.pathname.startsWith('/api/'))return json({});
   const file=path.join(assets,u.pathname==='/'?'index.html':u.pathname);
   try{return route.fulfill({body:await fs.readFile(file),contentType:{'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml'}[path.extname(file)]});}catch{return route.fulfill({status:404,body:''});}
  });
  await page.goto('http://uni-switch.test/?view=providers');
  await page.locator('#addProvider').click();
  await page.locator('#addSheet .tile[data-pick="PublicFixture"]').click();
  const choice=page.getByRole('button',{name:'Enable fixed access',exact:true});
  if(await choice.count())await choice.click();
  if(lang==='en'){await page.locator('.signing.failed').waitFor();assert.equal(await page.locator('.callback-form input').count(),0);await page.locator('.signing.failed button.primary').click();}
  await page.locator('button.acc.add .n').waitFor();
  assert.deepEqual(asked,Array.from({length:lang==='en'?2:1},()=>({provider:'fixed-access',method:0,inputs:{},key:''})));
  assert.equal(await page.locator('.callback-form input').count(),0,'no personal-key field');
  assert.ok(!(await page.locator('button.acc.add .n').innerText()).includes('PublicFixture'),'single identity offers sign-in again, not another account');
  const renewed=page.waitForRequest(r=>new URL(r.url()).pathname==='/api/plugin-signin');
  await page.locator('button.acc.add').click();
  if(await choice.count())await choice.click();
  await renewed;
  await page.locator('button.acc.add .n').waitFor();
  assert.equal(asked.length,lang==='en'?3:2,'explicit sign-in again makes one request');
  await page.keyboard.press('Escape');await page.locator('#modal').waitFor({state:'hidden'});
  await page.locator('#addProvider').click();
  await page.locator('#addSheet .tile[data-pick="OrdinaryFixture"]').click();
  const ordinary=page.getByRole('button',{name:'Personal credential',exact:true});
  if(await ordinary.count())await ordinary.click();
  await page.getByLabel('Personal credential',{exact:true}).waitFor();
  assert.equal(asked.length,lang==='en'?3:2,'ordinary plugin waits for a personal key');
  for(const width of [440,900]){await page.setViewportSize({width,height:900});assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));}
  assert.deepEqual(errors,[]);
 });
}

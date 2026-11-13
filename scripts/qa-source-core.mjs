// Exercise the compiled source core in a fake home with inert client/keychain
// tools. Never start an installed application or read the user's client files.
import {spawn,spawnSync} from "node:child_process";
import process from "node:process";
import {existsSync,readFileSync,mkdirSync} from "node:fs";
import {resolve,dirname} from "node:path";
import {fileURLToPath} from "node:url";
import assert from "node:assert/strict";
const root=resolve(dirname(fileURLToPath(import.meta.url)),"..");
const suffix=process.platform==="win32"?".exe":"";
export async function startIsolated(binary,background=false){
 mkdirSync(resolve(root,".qa"),{recursive:true});
 const local=resolve(root,".tools/go/bin/go"+suffix);
 const go=process.env.UNI_SWITCH_GO||(existsSync(local)?local:"go");
 const wrapper=resolve(root,".qa/source-qa"+suffix);
 const build=spawnSync(go,["build","-tags=nogui","-o",wrapper,"./cmd/uni-switch-qa"],{cwd:resolve(root,"backend"),encoding:"utf8",windowsHide:true});
 if(build.status!==0)throw new Error(build.stderr||"QA launcher build failed");
 const child=spawn(wrapper,[binary,...(background?["--background"]:[])],{cwd:root,windowsHide:true,detached:process.platform!=="win32",stdio:["pipe","pipe","pipe"]});
 let home,ready,ended=false,buffer="",log="";
 child.stdout.on("data",b=>{
  buffer+=b;let pos;
  while((pos=buffer.indexOf("\n"))>=0){const line=buffer.slice(0,pos);buffer=buffer.slice(pos+1);try{const v=JSON.parse(line);if(v.type==="qa-home")home=v.path;if(v.type==="ready")ready=v;}catch{}}
 });
 child.stderr.on("data",b=>{log+=b;if(log.length>20000)log=log.slice(-20000)});
 child.on("exit",()=>ended=true);
 const wait=async(fn,timeout=45000)=>{const end=Date.now()+timeout;while(!fn()){if(ended)throw new Error("Isolated application exited before startup: "+log);if(Date.now()>end)throw new Error("Isolated startup timed out: "+log);await new Promise(r=>setTimeout(r,100));}};
 await wait(()=>!!home);
 return {child,home,wait,ready:()=>ready,stop:async()=>{
  if(ended)return;
  if(!child.stdin.destroyed)child.stdin.write("quit\n");
  await Promise.race([new Promise(r=>child.once("exit",r)),new Promise(r=>setTimeout(r,10000))]);
  if(!ended){
   if(process.platform==="win32")spawnSync("taskkill",["/PID",String(child.pid),"/T","/F"],{windowsHide:true,stdio:"ignore"});
   else{
    try{process.kill(-child.pid,"SIGTERM");}catch(e){if(e.code!=="ESRCH")throw e;}
    await Promise.race([new Promise(r=>child.once("exit",r)),new Promise(r=>setTimeout(r,2000))]);
    try{process.kill(-child.pid,"SIGKILL");}catch(e){if(e.code!=="ESRCH")throw e;}
   }
  }
 }};
}
async function check(){
 const desktop=process.argv.indexOf("--desktop");
 let binary;
 if(desktop>=0)binary=resolve(process.argv[desktop+1]);
 else{
  const build=spawnSync(process.execPath,["scripts/build-go.mjs","--qa"],{cwd:root,encoding:"utf8",windowsHide:true});
  if(build.status!==0)throw new Error(build.stderr||"Core build failed");
  const platform={win32:"pc-windows-msvc",linux:"unknown-linux-gnu",darwin:"apple-darwin"}[process.platform];
  const arch=process.arch==="arm64"?"aarch64":"x86_64";
  binary=resolve(root,`src-tauri/binaries/uni-switch-core-${arch}-${platform}${suffix}`);
 }
 const app=await startIsolated(binary,desktop>=0);
 try{
  if(desktop>=0){
   await app.wait(()=>existsSync(resolve(app.home,"uni-switch/settings.json")));
   await new Promise(r=>setTimeout(r,3000));
   const data=JSON.parse(readFileSync(resolve(app.home,"uni-switch/settings.json"),"utf8"));
   assert.equal(data.noAutoUpdate,true);
  }else{
   await app.wait(()=>!!app.ready());
   const link=app.ready().url;const base=new URL(link).origin;
   const login=await fetch(link,{redirect:"manual"});
   const cookie=login.headers.getSetCookie().map(x=>x.split(";")[0]).join("; ");
   assert.ok(cookie,"startup must issue an authentication cookie");
   const request=(path,options={})=>fetch(base+path,{...options,headers:{Cookie:cookie,"Sec-Fetch-Site":"same-origin",...options.headers}});
   assert.equal((await fetch(base+"/api/state")).status,401);
   assert.equal((await request("/api/state",{headers:{"Sec-Fetch-Site":"cross-site"}})).status,403);
   const state=await (await request("/api/state")).json();
   assert.ok(state.clients.length>=5);
   const directory=await (await request("/api/providers")).json();
   assert.ok(!directory.presets.some(p=>p.kind==="partner"),"commercial partners must not be offered");
   assert.ok(!directory.presets.some(p=>p.kind==="vendor"),"vendor presets must not be offered");
   const relays=directory.presets.filter(p=>p.kind==="relay");
   assert.deepEqual(relays.map(p=>p.id),["dieqiyun"]);
   assert.equal(relays[0].website,"https://www.dieqiyun.top");
   assert.equal(relays[0].chat,"https://cfapi.dieqiyun.top/v1");
   assert.equal(relays[0].responses,"https://cfapi.dieqiyun.top/v1");
   assert.equal(relays[0].anthropic,"https://cfapi.dieqiyun.top");
   const logo=await request("/icons/dieqiyun.png");
   assert.equal(logo.status,200);
   assert.match(logo.headers.get("content-type"),/^image\/png/);
   assert.deepEqual([...new Uint8Array(await logo.arrayBuffer()).slice(0,8)],[137,80,78,71,13,10,26,10]);
   assert.equal((await request("/api/partner",{method:"POST",body:'{"what":"shown","ids":["old-partner"]}'})).status,404,"partner event route was removed");
   assert.match(await (await request("/uni-switch.js")).text(),/uni-card-actions/);
   for(const path of ["/api/update/install","/api/uninstall","/api/cli"]){assert.equal((await request(path,{method:"POST",body:"{}"})).status,409,path);}
   assert.notEqual((await request("/v1/models")).status,200,"the management port must not expose inference");
   assert.match(await (await request("/api/notices")).text(),/Copyright \(c\) 2026 yetone/);
  }
  assert.equal(readFileSync(resolve(app.home,".codex/config.toml"),"utf8"),"# isolated QA; disconnected client\n");
  console.log(JSON.stringify({runtime:desktop>=0?"desktop":"compiled-core",started:true,isolatedHome:true,disconnectedClientUnchanged:true,...(desktop<0?{onlyRelay:"dieqiyun",partnerDirectoryRemoved:true,bundledLogo:true}:{})}));
 }finally{await app.stop();}
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url))await check();

import test from "node:test";
import assert from "node:assert/strict";
import {readFileSync,readdirSync,existsSync,statSync} from "node:fs";
import {createHash} from "node:crypto";
import {resolve} from "node:path";
const root=resolve(import.meta.dirname,"../..");
const source=JSON.parse(readFileSync(resolve(root,"docs/upstream-source.json"),"utf8"));
test("imported business source matches its locked hashes and recorded local adaptations",()=>{
 const overrides=new Map((source.localOverrides||[]).map(f=>[f.path,f]));
 const removed=new Map((source.removedFiles||[]).map(f=>[f.path,f]));
 const files=source.files.filter(f=>/^internal\/(gateway|provider|catalog|agent|edit)\//.test(f.path)&&!f.path.endsWith("_test.go"));
 for(const directory of ["gateway","provider","catalog","agent","edit"]){
  assert.ok(files.some(f=>f.path.startsWith(`internal/${directory}/`)),`missing source package: ${directory}`);
 }
 for(const f of files){
  const override=overrides.get(f.path);
  const path=resolve(root,"backend",override?.replacementPath||f.path);
  if(removed.has(f.path)){assert.ok(removed.get(f.path).reason);assert.equal(existsSync(path),false,f.path);continue;}
  if(override?.replacementPath)assert.equal(existsSync(resolve(root,"backend",f.path)),false,f.path);
  if(override)assert.ok(override.reason);
  const actual=createHash("sha256").update(readFileSync(path)).digest("hex");assert.equal(actual,override?.sha256||f.sha256,f.path);
 }
});
test("renamed upstream files retain their original provenance and current hashes",()=>{
 for(const f of source.localOverrides||[]){
  if(!f.replacementPath)continue;
  assert.ok(source.files.some(original=>original.path===f.path),f.path);
  assert.ok(f.reason,f.path);
  assert.equal(existsSync(resolve(root,"backend",f.path)),false,f.path);
  const actual=createHash("sha256").update(readFileSync(resolve(root,"backend",f.replacementPath))).digest("hex");
  assert.equal(actual,f.sha256,f.replacementPath);
 }
});
test("the desktop contains native lifecycle code, with no retired business modules",()=>{
 const nativeSource=resolve(root,"src-tauri/src");
 const nativeFiles=readdirSync(nativeSource,{recursive:true}).filter(name=>!statSync(resolve(nativeSource,name)).isDirectory());
 assert.deepEqual(nativeFiles.sort(),["core.rs","lib.rs","main.rs"]);
 const rust=["core.rs","lib.rs","main.rs"].map(f=>readFileSync(resolve(root,"src-tauri/src",f),"utf8")).join("\n");
 assert.ok(rust.includes('include_bytes!(env!("UNI_SWITCH_GO_CORE_PATH"))'));
 assert.ok(!/uni-switch\.exe|Command::new\("uni-switch"\)|iframe/i.test(rust));
});
test("all built-in feature pages remain reachable with the local visual layer",()=>{
 const index=readFileSync(resolve(root,"backend/internal/gui/assets/index.html"),"utf8");
 for(const view of ["agents","providers"]){assert.ok(index.includes(`data-view="${view}"`),view);}
 for(const page of ["sessions","gateway","library"]){assert.ok(index.includes(`plugins/${page}/client.js`),page);}
 assert.ok(!index.includes("plugins/usage/"));
 assert.ok(!index.includes("plugins/routing/"));
 assert.ok(!index.includes('data-view="routing"'));
 assert.ok(!index.includes('data-view="gateway"'));
 assert.ok(index.includes('id="setPage-gateway"'));
 assert.ok(index.includes('src="request-details.js"'));
 assert.ok(index.indexOf('src="uni-switch.js"')<index.indexOf('src="start.js"'));
});

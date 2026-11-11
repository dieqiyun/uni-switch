import {spawnSync} from "node:child_process";
import {mkdirSync,mkdtempSync,existsSync,openSync,closeSync,rmSync} from "node:fs";
import {resolve,relative,sep} from "node:path";
const root=resolve(import.meta.dirname,"..");
const args=process.argv.slice(2);
function option(name){const i=args.indexOf(name);if(i<0)return;const value=args[i+1];if(!value)throw new Error("Missing value: "+name);args.splice(i,2);return value;}
const source=resolve(root,option("--source")||"backend");
const log=option("--log");
const local=resolve(root,`.tools/go/bin/go${process.platform==="win32"?".exe":""}`);
const go=process.env.UNI_SWITCH_GO||(existsSync(local)?local:"go");
const query=spawnSync(go,["env","-json","GOPATH","GOMODCACHE","GOCACHE"],{encoding:"utf8",windowsHide:true});
if(query.status!==0)throw new Error(query.stderr||"Go environment unavailable");
const vars=JSON.parse(query.stdout);
const qa=resolve(root,".qa");mkdirSync(qa,{recursive:true});
const home=mkdtempSync(resolve(qa,"go-suite-"));
const env={...process.env,...vars,HOME:home,USERPROFILE:home,APPDATA:resolve(home,"roaming"),LOCALAPPDATA:resolve(home,"local"),XDG_CONFIG_HOME:resolve(home,"config"),XDG_CACHE_HOME:resolve(home,"cache"),GOENV:"off",GOTOOLCHAIN:"local"};
const command=args[0]==="vet"?["vet","-tags=nogui",...args.slice(1)]:["test","-tags=nogui",...args];
if(!command.some(a=>a.startsWith("./")))command.push("./...");
const fd=log?openSync(resolve(root,log),"w"):undefined;
try{
 const result=spawnSync(go,command,{cwd:source,env,stdio:["ignore",fd??"inherit",fd??"inherit"],windowsHide:true});
 if(result.error)throw result.error;
 process.exitCode=result.status??1;
}finally{
 if(fd!==undefined)closeSync(fd);
 const path=relative(qa,home);if(path&&!path.startsWith(".."+sep)&&path!=="..")rmSync(home,{recursive:true,force:true});
}

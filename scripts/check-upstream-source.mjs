import { readFileSync, existsSync } from "node:fs";
import { createHash } from "node:crypto";
import { resolve } from "node:path";
import { execFileSync } from "node:child_process";
const root=resolve(import.meta.dirname,"..");
const source=JSON.parse(readFileSync(resolve(root,"docs/upstream-source.json"),"utf8"));
const changed=new Map((source.localOverrides||[]).map(x=>[x.path,x]));
const removed=new Map((source.removedFiles||[]).map(x=>[x.path,x]));
const tracked=process.argv.includes("--index")?new Set(execFileSync("git",["ls-files","-z"],{cwd:root,encoding:"utf8"}).split("\0")):null;
const failures=[];let unchanged=0;
for(const file of source.files){
 const override=changed.get(file.path);
 const current=override?.replacementPath||file.path;
 const path=resolve(root,"backend",current);
 if(removed.has(file.path)){
  if(existsSync(path)||!removed.get(file.path).reason)failures.push(`Invalid recorded removal: ${file.path}`);
  continue;
 }
 if(override?.replacementPath&&existsSync(resolve(root,"backend",file.path))){failures.push(`Both original and renamed source exist: ${file.path}`);continue;}
 if(!existsSync(path)){failures.push(`Missing imported source: ${file.path}`);continue;}
 const actual=createHash("sha256").update(readFileSync(path)).digest("hex");
 if(actual!==file.sha256){if(!override||actual!==override.sha256||!override.reason)failures.push(`Unrecorded source change: ${file.path}`);}else unchanged++;
 if(tracked&&!tracked.has("backend/"+current))failures.push(`Source excluded from Git archive: ${current}`);
}
if(failures.length){console.error(failures.join("\n"));process.exitCode=1;}else console.log(`Source verified: ${source.commit}; ${source.files.length} original files, ${unchanged} unchanged, ${source.files.length-unchanged-removed.size} recorded local adaptations, ${removed.size} recorded removals.`);

import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync,readdirSync} from 'node:fs';
import {resolve} from 'node:path';
const root=resolve(import.meta.dirname,'../..');
const read=p=>readFileSync(resolve(root,p),'utf8');
const files=dir=>readdirSync(dir,{withFileTypes:true}).flatMap(e=>e.isDirectory()?files(resolve(dir,e.name)):[resolve(dir,e.name)]);

test('the shipped UI and local browser manifest use the product brand',()=>{
 for(const p of files(resolve(root,'backend/internal/gui/assets'))){
  if(/\.(js|html|css|json|svg)$/.test(p))assert.doesNotMatch(readFileSync(p,'utf8'),/magpie/i,p);
 }
 const manifest=JSON.parse(read('backend/internal/gui/assets/manifest.json'));
 assert.equal(manifest.name,'uni-switch');
 assert.equal(manifest.short_name,'uni-switch');
 assert.doesNotMatch(read('backend/internal/gui/assets/uni-switch.js'),/productWords|replace\([^\n]*magpie/);
});

test('wire identifiers and client configuration use the product namespace',()=>{
 assert.match(read('backend/internal/provider/importurl.go'),/const Scheme = "uni-switch"/);
 assert.match(read('backend/internal/gateway/caller.go'),/X-Uni-Switch-Agent/);
 assert.match(read('backend/internal/provider/provider.go'),/json:"uniSwitchUserAgent,omitempty"/);
 assert.match(read('backend/internal/agent/codex.go'),/uni-switch/);
 assert.doesNotMatch(read('backend/internal/agent/codex.go'),/magpie/i);
 assert.match(read('backend/go.mod'),/^module github.com\/dieqiyun\/uni-switch\/backend/m);
 for(const p of ['backend/Makefile','backend/internal/omarchy/widget/Widget.qml','backend/build/darwin/AppIcon.icon/icon.json'])assert.doesNotMatch(read(p),/magpie/i,p);
});

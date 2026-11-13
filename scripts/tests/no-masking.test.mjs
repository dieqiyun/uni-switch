import test from 'node:test';
import assert from 'node:assert/strict';
import {existsSync,readFileSync,statSync,readdirSync} from 'node:fs';
import {resolve} from 'node:path';
const root=resolve(import.meta.dirname,'../..');
const read=path=>readFileSync(resolve(root,path),'utf8');

test('configurable request masking implementations and assets are removed',()=>{
 for(const path of ['internal/masking','internal/redact','internal/pii','internal/maskrec','plugins/redact','native/privacy-filter',
  'internal/gui/assets/plugins/redact','internal/gui/assets/ruleedit.js','internal/gui/masking.go','internal/gui/privacymodel.go',
  'internal/gateway/redact.go','internal/gateway/redact_phase.go','internal/provider/unredacted.go']){
  const absolute=resolve(root,'backend',path);
  // Empty directories can remain in a worktree; no retired source may remain.
  if(existsSync(absolute)){
   assert.ok(statSync(absolute).isDirectory(),path);
   assert.deepEqual(readdirSync(absolute,{recursive:true}).filter(p=>statSync(resolve(absolute,p)).isFile()),[],path);
  }
 }
 const index=read('backend/internal/gui/assets/index.html');
 assert.doesNotMatch(index,/plugins\/redact|data-view="masking"|id="(?:redactList|findList|maskActList|maskTryList)"/);
 const app=read('backend/internal/gui/assets/app.js');
 assert.doesNotMatch(app,/redactPersonal|redactWords|redactRoute|maskAlert|unredacted|maskedBadge|api\("masking\//);
 assert.match(app,/function renderPrivacy\(/);
 assert.match(app,/window.hideAccounts =/);
});

test('outbound requests have no configurable masking dependency',()=>{
 for(const path of ['gateway.go','codex_backend.go','speech.go','transcribe.go','native.go','draw.go','video.go','embeddings.go']){
  const source=read('backend/internal/gateway/'+path);
  assert.doesNotMatch(source,/internal\/(?:redact|masking|pii|maskrec)"|redactedHits\(|maskTextHits\(|SkipsRedaction\(|restoring\(/,path);
 }
 assert.match(read('backend/internal/gateway/archive.go'),/logsafe\.ScrubJSON/);
});

import test from 'node:test';
import assert from 'node:assert/strict';
import {existsSync,readFileSync,readdirSync,statSync} from 'node:fs';
import {resolve,extname} from 'node:path';

const root=resolve(import.meta.dirname,'../..');
const read=path=>readFileSync(resolve(root,path),'utf8');
test('observability exporter, session tracing and management assets are removed',()=>{
  for(const path of ['internal/otlp','plugins/otel','internal/gui/assets/plugins/otel']){
    const absolute=resolve(root,'backend',path);
    if(existsSync(absolute)){
      assert.ok(statSync(absolute).isDirectory(),path);
      assert.deepEqual(readdirSync(absolute,{recursive:true}).filter(p=>statSync(resolve(absolute,p)).isFile()),[],path);
    }
  }
  for(const path of ['internal/settings/otel.go','internal/gateway/observe.go','internal/usage/observe.go','internal/sessions/traces.go']){
    assert.equal(existsSync(resolve(root,'backend',path)),false,path);
  }
  assert.doesNotMatch(read('backend/internal/gui/assets/index.html'),/plugins\/otel|setTab-otel|setPage-otel|otelList/);
  assert.doesNotMatch(read('backend/internal/gui/assets/app.js'),/renderOTel|\botel\s*:|"otel"/);
  assert.doesNotMatch(read('backend/internal/gui/assets/i18n.js'),/Observability|OTLP|OpenTelemetry|Langfuse|Trace agent conversations/);
  assert.doesNotMatch(read('backend/internal/settings/settings.go'),/\bOTel\b/);
  assert.doesNotMatch(read('backend/internal/settingsapi/settingsapi.go'),/"otel"/);
});

test('request forwarding and local accounting do not invoke exporters',()=>{
  for(const path of ['gateway/gateway.go','gateway/capture.go','gateway/local.go','usage/usage.go']){
    assert.equal(read('backend/internal/'+path).match(/ObservedBodies|ServeObservers|observeRequest|watched\.attempt|withBodies\(|observeLedger|observedKey|wholeIn|wholeOut|BodyIn|BodyOut/)?.[0],undefined,path);
  }
  assert.match(read('backend/internal/gateway/gateway.go'),/appendUsage\(r, rec\)/);
  assert.match(read('backend/internal/gateway/archive.go'),/logsafe\.ScrubJSON/);
});

test('no runtime source can register or enable the retired exporter',()=>{
  for(const entry of readdirSync(resolve(root,'backend'),{recursive:true,withFileTypes:true})){
    if(!entry.isFile())continue;
    const path=resolve(entry.parentPath,entry.name);
    if(!['.go','.js','.html','.css'].includes(extname(path))||path.endsWith('_test.go')||entry.name==='whatsnew.js'||!/[\\/](?:internal|plugins)[\\/]/.test(path)||/[\\/](?:tests|testdata)[\\/]/.test(path))continue;
    assert.doesNotMatch(readFileSync(path,'utf8'),/internal\/otlp"|plugins\/otel"|UNI_SWITCH_OTEL_|OTEL_RESOURCE_ATTRIBUTES|OTEL_SERVICE_NAME|\b(?:OTel|ObservedBodies|ServeObservers|observeRequest|observeLedger|renderOTel)\b/,path);
  }
});

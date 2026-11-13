import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync,existsSync} from 'node:fs';
import {resolve} from 'node:path';
const root=resolve(import.meta.dirname,'../..');
const read=p=>readFileSync(resolve(root,p),'utf8');

test('usage reports have no compiled module, page assets or registered API',()=>{
 for(const p of ['backend/plugins/usage/usage.go','backend/internal/gui/usage.go','backend/internal/gateway/usage_api.go','backend/internal/gateway/usage_ingest.go','backend/internal/usageapi/usageapi.go','backend/internal/gui/assets/plugins/usage/client.js','backend/internal/gui/assets/plugins/usage/page.js'])assert.equal(existsSync(resolve(root,p)),false,p);
 assert.doesNotMatch(read('backend/plugins/all/all.go'),/plugins\/usage/);
 assert.doesNotMatch(read('backend/internal/gui/assets/index.html'),/data-view="usage"|plugins\/usage|panelUsage|data-ptab="stats"|trayCostRow/);
 assert.doesNotMatch(read('backend/internal/gui/assets/app.js'),/TRAY_TOTALS|panelUseFocus|loadPanelUse|renderPanelUse|subscriptionUsage|loadUsage/);
 assert.doesNotMatch(read('backend/internal/gateway/gateway.go'),/HandleFunc\("(?:GET|POST) \/v1\/uni-switch\/usage/);
 assert.doesNotMatch(read('backend/internal/gui/sessions.go'),/statsMemo|statsFor\(|combinedSessionStats|\/api\/sessions\/(?:stats|overview)/);
});

test('sessions generate no usage or money cells in lists or details',()=>{
 const js=read('backend/internal/gui/assets/plugins/sessions/client.js');
 assert.doesNotMatch(js,/sessSpent|sessUsageDetail|s\.(?:cost|input|output|cache_read|cache_write)\b/);
 assert.doesNotMatch(read('backend/internal/gui/assets/request-details.js'),/function sessSpent|function sessUsageDetail/);
 assert.doesNotMatch(read('backend/internal/gui/assets/plugins/sessions/sessions.css'),/\.row\.sm-sess \.(?:num|cost)/);
 assert.match(js,/r\.append\(who\)/);
 assert.match(js,/sessions\/transcript/);
 assert.match(js,/sessions\/delete/);
 assert.match(read('backend/internal/gui/account_allowances.go'),/GET \/api\/accounts\/quotas/);
});

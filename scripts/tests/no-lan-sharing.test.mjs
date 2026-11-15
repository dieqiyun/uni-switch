import test from 'node:test';
import assert from 'node:assert/strict';
import {existsSync,readFileSync} from 'node:fs';
import {resolve} from 'node:path';
const root=resolve(import.meta.dirname,'../..');
const read=p=>readFileSync(resolve(root,p),'utf8');

test('network settings and browser access no longer offer LAN sharing',()=>{
  assert.doesNotMatch(read('backend/internal/gui/assets/index.html'),/id="(?:lanList|allowList)"/);
  assert.doesNotMatch(read('backend/internal/gui/assets/plugins/gateway/settings.js'),/function (?:renderLAN|renderAllow|lanShown|gwLAN)\b|\["lan",/);
  assert.doesNotMatch(read('backend/internal/gui/api.go'),/POST \/api\/settings\/(?:lan|allowips)"/);
  assert.doesNotMatch(read('backend/internal/gui/apppage.go'),/case "lan"|case "", "local", "lan"/);
});

test('LAN listeners and address discovery implementations are removed',()=>{
  for(const path of ['backend/internal/gateway/lan.go','backend/internal/gateway/allow.go','backend/internal/settings/allowips.go'])assert.equal(existsSync(resolve(root,path)),false,path);
  assert.doesNotMatch(read('backend/internal/access/access.go'),/func (?:ConfigureLAN|configureLAN)\(/);
  assert.doesNotMatch(read('backend/internal/cli/web_cli.go'),/case a == "--lan"|func proxyLink\(/);
  assert.doesNotMatch(read('backend/internal/cli/daemon_cli.go'),/case a == "--lan"/);
});

test('remote gateway directory, discovery and management clients are deleted',()=>{
 for(const path of ['provider/remote_uni_switch.go','provider/remote_moved.go','provider/remote_follow.go','provider/remote_quotas.go','gateway/remote_uni_switch.go','gateway/remote_delta.go','gateway/models_wait.go','library/bridge_mcp.go'])assert.equal(existsSync(resolve(root,'backend/internal',path)),false,path);
 assert.doesNotMatch(read('backend/internal/provider/presets.go'),/KindRemote|RemoteUniSwitchPreset|shared on its network|192\.168\.1\.20/);
 assert.doesNotMatch(read('backend/internal/gui/assets/app.js'),/remote-uni-switch|remoteNew|draft\.libraryMCP|provider\/keepself/);
 assert.doesNotMatch(read('backend/internal/gui/assets/i18n.js'),/"Another computer's uni-switch"|"shared on its network"/);
 assert.doesNotMatch(read('backend/internal/gateway/gateway.go'),/SeekMoved|RemoteCardsPath|RemoteRefreshPath|RemoteCodexResetPath|RemoteProvePath|KeepRemoteListsFollowed/);
});

test('client sidebar has no global accounts or inert more button',()=>{
 const ui=read('backend/internal/gui/assets/uni-switch.js');
 assert.doesNotMatch(ui,/All clients|Accounts and API|More features|uni-more-open/);
 assert.match(ui,/renderClientControls/);
 assert.match(read('backend/internal/gui/assets/app.js'),/!window\.bootPrefs\?\.uniSwitch && !providers\.providers\.length/);
});

import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync,existsSync,readdirSync} from 'node:fs';
import {resolve} from 'node:path';
const root=resolve(import.meta.dirname,'../..');
const read=p=>readFileSync(resolve(root,p),'utf8');
test('third-party extension hosts and their management assets are absent',()=>{
 for(const p of ['backend/internal/plugin/host.js','backend/internal/plugin/bun.go','backend/internal/plugin/market.go','backend/internal/plugin/page.go','backend/internal/gui/plugins.go','backend/internal/gui/pluginpage.go','backend/internal/gui/pageservice.go','backend/internal/gui/assets/plugins.js','backend/internal/gui/assets/pluginpages.js','backend/internal/gui/assets/plugins.css','backend/internal/cli/plugins_cli.go','backend/internal/middleware/run.go','backend/internal/agentplug/agentplug.go'])assert.equal(existsSync(resolve(root,p)),false,p);
 assert.deepEqual(readdirSync(resolve(root,'backend/internal/plugin'),{withFileTypes:true}).filter(x=>x.isFile()).map(x=>x.name).sort(),['removed.go','removed_test.go']);
 const index=read('backend/internal/gui/assets/index.html');
 assert.doesNotMatch(index,/data-view="plugins"|id="view-plugins"|id="setTab-plugins"|id="warmTab-plugins"|class="plugin-slot"/);
 assert.doesNotMatch(read('backend/internal/gui/assets/app.js'),/api\("(?:plugins\/|plugin-signin|plugin-pages)|startPluginSignIn\(/);
 assert.doesNotMatch(read('backend/internal/cli/main.go'),/case "plugin", "plugins"|uni-switch plugin \[/);
});

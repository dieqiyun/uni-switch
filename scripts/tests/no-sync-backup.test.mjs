import test from 'node:test';
import assert from 'node:assert/strict';
import {existsSync,readFileSync} from 'node:fs';
import {resolve} from 'node:path';

const root=resolve(import.meta.dirname,'../..');
const read=p=>readFileSync(resolve(root,p),'utf8');

test('settings no longer offer portable backup or cross-computer sync',()=>{
  assert.doesNotMatch(read('backend/internal/gui/assets/index.html'),/setTab-sync|setPage-sync|syncList/);
  assert.doesNotMatch(read('backend/internal/gui/assets/app.js'),/renderSync|syncOpen|agents\/here|api\("(?:davsync|autobackup|backup\/)/);
});

test('sync and portable backup implementations and commands are removed',()=>{
  for(const path of ['backend/internal/backup','backend/internal/davsync','backend/internal/gui/backup.go','backend/internal/gui/assets/syncfiles.js','backend/internal/gui/assets/autobackup.js','backend/internal/cli/backup_cmd.go','backend/internal/cli/webdav_cli.go','backend/internal/provider/signins_carry.go','backend/internal/library/carry.go','backend/internal/library/mcp_local.go']){
    assert.equal(existsSync(resolve(root,path)),false,path);
  }
  assert.doesNotMatch(read('backend/internal/cli/main.go'),/backupCmd|restoreCmd|davsync\.|backup\.RunAuto|webdavCmd|s3Cmd|githubCmd/);
  assert.doesNotMatch(read('backend/internal/gui/api.go'),/backupRoutes|agents\/here/);
  assert.doesNotMatch(read('backend/internal/gui/assets/plugins/library/client.js'),/Sync to your other computers|lib-backups|lib-localbox/);
  assert.doesNotMatch(read('backend/internal/provider/quota_history.go'),/func (?:MergeQuotaHistory|QuotaHistoryData|mergeQuotaHist)\(/);
  assert.equal(existsSync(resolve(root,'backend/internal/provider/group_order.go')),false,'routing-group ordering is removed');
  assert.doesNotMatch(read('backend/internal/usage/shared.go'),/func (?:KeepShared|OwnDays|DropShared|DropAllShared|ValidComputer)\(/);
  assert.doesNotMatch(read('backend/internal/settings/settings.go'),/func \(s \*Settings\) KeepOwn\(/);
});

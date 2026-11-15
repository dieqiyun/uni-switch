import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';

const root = resolve(import.meta.dirname, '../..');
const read = path => readFileSync(resolve(root, path), 'utf8');

test('vendor presets and their add-sheet implementation are deleted from source', () => {
  const presets = read('backend/internal/provider/presets.go');
  assert.doesNotMatch(presets, /KindVendor|Kind\s*=\s*"vendor"|presetAliases|movedEndpoints|arkPlanModels|alibabaPlanModels|bailianWorkspace|bedrockRegions|mantleRegions/);
  assert.deepEqual([...presets.matchAll(/\{ID: ("[^"]+"|\w+), Name: [^\n]*Kind:/g)].map(m => m[1]), [
    '"dieqiyun"', '"ollama"', '"lmstudio"', 'OMLXPreset', 'MLXServePreset',
  ]);
  const ui = read('backend/internal/gui/assets/app.js');
  assert.doesNotMatch(ui, /\["vendor", "Vendors"|function pairTile\(|const chinaOf\s*=|const globalOf\s*=/);
  assert.doesNotMatch(read('backend/internal/provider/importapps.go'), /Preset\("google"\)|movedEndpoints/);
});

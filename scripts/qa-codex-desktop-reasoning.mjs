import { readFile, writeFile } from "node:fs/promises";
import vm from "node:vm";
import ts from "typescript";
import assert from "node:assert/strict";

// Read packaged assets without changing the installed app or a user's profile.
const archivePath =
  process.env.CODEX_DESKTOP_QA_ASAR ||
  "C:/Program Files/WindowsApps/OpenAI.Codex_26.930.3930.0_x64__2p2nqsd0c76g0/app/resources/app.asar";
let assets;
async function desktopAssets() {
  if (assets) return assets;
  const archive = await readFile(archivePath);
  const headerSize = archive.readUInt32LE(4),
    jsonSize = archive.readUInt32LE(12);
  const header = JSON.parse(
    archive.subarray(16, 16 + jsonSize).toString("utf8"),
  );
  assets = {};
  function walk(node, parent = "") {
    for (const [name, entry] of Object.entries(node.files || {})) {
      const file = parent ? `${parent}/${name}` : name;
      if (entry.files) {
        walk(entry, file);
        continue;
      }
      if (entry.unpacked || !entry.size) continue;
      if (
        !/^webview\/assets\/app-(initial|shared)-.*\.js$/.test(file) &&
        !/^\.vite\/build\/(startup-requirements|src)-.*\.js$/.test(file)
      )
        continue;
      const start = 8 + headerSize + Number(entry.offset);
      assets[file] = archive
        .subarray(start, start + entry.size)
        .toString("utf8");
    }
  }
  walk(header);
  return assets;
}
function functionSource(sourceFile, name) {
  const node = sourceFile.statements.find(
    (node) => ts.isFunctionDeclaration(node) && node.name?.text === name,
  );
  assert.ok(
    node,
    `Installed Codex function ${name} must exist; review changed versions before testing`,
  );
  return node.getText(sourceFile);
}
let filter;
async function menuFilter() {
  if (filter) return filter;
  const assetMap = await desktopAssets();
  const [asset, source] = Object.entries(assetMap).find(([name]) =>
    name.startsWith("webview/assets/app-initial-"),
  );
  const sourceFile = ts.createSourceFile(
    asset,
    source,
    ts.ScriptTarget.Latest,
    false,
    ts.ScriptKind.JS,
  );
  const [settingsAsset, settingsSource] = Object.entries(assetMap).find(
    ([name]) => name.startsWith("webview/assets/app-shared-"),
  );
  const metadata = settingsSource.match(
    /_At=U\((\[[^\]]*\])\),vAt=(\[[^\]]*\])/,
  );
  assert.ok(metadata, "Installed desktop effort setting metadata must exist");
  assert.ok(
    settingsSource.includes(
      "enabledReasoningEfforts:vP({agentAccess:`hidden`,default:vAt",
    ),
  );
  const known = JSON.parse(metadata[1].replaceAll("`", '"'));
  const defaults = JSON.parse(metadata[2].replaceAll("`", '"'));
  assert.deepEqual(known, [
    "none",
    "minimal",
    "low",
    "medium",
    "high",
    "xhigh",
    "max",
    "ultra",
    "persistent",
  ]);
  assert.equal(defaults.includes("max"), false);
  // The only injected dependency is the enum check using the app's actual metadata; the filter/model
  // visibility logic is the exact code shipped by the installed desktop app.
  const context = vm.createContext({ zve: (effort) => known.includes(effort) });
  vm.runInContext(
    `${functionSource(sourceFile, "lni")}\n${functionSource(sourceFile, "cni")}\nthis.filter=cni;`,
    context,
    { timeout: 1000 },
  );
  filter = { run: context.filter, asset, settingsAsset, known, defaults };
  return filter;
}
export async function verifyDesktopReasoning({ models, config, output }) {
  const { run, asset, settingsAsset, known, defaults } = await menuFilter();
  const enabled = config.desktop?.["enabled-reasoning-efforts"];
  assert.ok(
    Array.isArray(enabled),
    "Real config/read must expose the repaired desktop allowlist",
  );
  assert.ok(enabled.includes("max"));
  const common = {
    additionalAvailableModels: new Set(),
    authMethod: "apiKey",
    availableModels: new Set(),
    defaultModel: models[0].model,
    hasConfiguredModelCatalog: true,
    isCustomModelProvider: true,
    models,
    useHiddenModels: false,
  };
  const before = run({
    ...common,
    enabledReasoningEfforts: new Set(["low", "medium", "high", "xhigh"]),
    includeUltraReasoningEffort: false,
  });
  const after = run({
    ...common,
    enabledReasoningEfforts: new Set(enabled),
    includeUltraReasoningEffort: false,
  });
  assert.ok(models.length > 0, "The real app-server must return models");
  assert.equal(
    before.models.length,
    models.length,
    "No model may be silently excluded from verification",
  );
  assert.equal(
    after.models.length,
    models.length,
    "The repaired models must remain visible",
  );
  for (const model of before.models)
    assert.equal(
      model.supportedReasoningEfforts.some((e) => e.reasoningEffort === "max"),
      false,
    );
  for (const model of after.models) {
    assert.ok(
      model.supportedReasoningEfforts.some((e) => e.reasoningEffort === "max"),
    );
    // Ultra still obeys the app's feature gate; no application patch bypasses it.
    assert.equal(
      model.supportedReasoningEfforts.some(
        (e) => e.reasoningEffort === "ultra",
      ),
      false,
    );
  }
  const gated = run({
    ...common,
    enabledReasoningEfforts: new Set(enabled),
    includeUltraReasoningEffort: true,
  });
  for (const model of gated.models)
    assert.ok(
      model.supportedReasoningEfforts.some(
        (e) => e.reasoningEffort === "ultra",
      ),
    );
  const summarize = (result) =>
    result.models.map((model) => ({
      model: model.model,
      efforts: model.supportedReasoningEfforts.map((e) => e.reasoningEffort),
    }));
  const result = {
    archivePath,
    filterAsset: asset,
    settingsAsset,
    actualDesktopEffortEnum: known,
    actualDesktopDefaults: defaults,
    actualDesktopSetting: enabled,
    catalogOnly: summarize(before),
    catalogAndDesktopSetting: summarize(after),
    ultraGateEnabled: summarize(gated),
    limitation:
      "Executed the installed app's menu filtering code against real app-server models/config; did not operate the live Codex UI.",
  };
  if (output) await writeFile(output, JSON.stringify(result, null, 2));
  return result;
}

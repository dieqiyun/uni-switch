import registry from "../content/model-capabilities.json";
import type {
  ModelCapabilities,
  ModelProfile,
  ModelRegistry,
  ProviderModel,
} from "../types";

const known = new Map<string, ModelCapabilities>(
  registry.entries.flatMap((entry) =>
    entry.ids.map((id) => [id, entry.capabilities] as const),
  ),
);
let profiles = new Map<string, ModelProfile>(
  (registry as ModelRegistry).entries.flatMap((entry) =>
    entry.ids.map((id) => [id, entry.profile ?? {}] as const),
  ),
);
let installedVersion = registry.version;
export function installRegistry(next: ModelRegistry) {
  if (next.schemaVersion !== 1 || next.version < installedVersion) return;
  installedVersion = next.version;
  known.clear();
  profiles = new Map();
  for (const entry of next.entries) {
    for (const id of entry.ids) {
      known.set(id, entry.capabilities);
      profiles.set(id, entry.profile ?? {});
    }
  }
}
function registryId(id: string) {
  const normalized = id
    .trim()
    .toLowerCase()
    .replace(/^(openai|anthropic|google|deepseek)\//, "");
  return normalized.startsWith("claude-")
    ? normalized.replace(/\s*\[1m\]$/, "").trimEnd()
    : normalized;
}
export function knownProfile(id: string): ModelProfile {
  return profiles.get(registryId(id)) ?? {};
}
export function effectiveProfile(model: ProviderModel): ModelProfile {
  const observed = { ...model.profile };
  if (observed.reasoningEfforts == null && model.reasoningEfforts.length)
    observed.reasoningEfforts = model.reasoningEfforts;
  const sources = [
    model.profileOverrides ?? {},
    observed,
    knownProfile(model.id),
    model.officialProfile ?? {},
  ];
  const keys = [
    "contextWindow",
    "maxInputTokens",
    "maxOutputTokens",
    "reasoningEfforts",
    "defaultEffort",
    "thinkingFormat",
    "thinkingAlwaysOn",
    "thinkingBetweenTools",
    "midConversationSystem",
    "forcedToolChoice",
    "samplingParameters",
    "toolCalls",
    "structuredOutput",
  ] as const;
  const result: ModelProfile = { endpoints: {} };
  for (const key of keys) {
    const value = sources.find((source) => source[key] != null)?.[key];
    if (value != null) Object.assign(result, { [key]: value });
  }
  for (const key of ["messages", "chatCompletions", "responses"] as const) {
    const value = sources.find((source) => source.endpoints?.[key] != null)
      ?.endpoints?.[key];
    if (value != null) result.endpoints![key] = value;
  }
  if (
    result.defaultEffort != null &&
    result.reasoningEfforts != null &&
    !result.reasoningEfforts.includes(result.defaultEffort)
  )
    delete result.defaultEffort;
  return result;
}
export function knownCapabilities(id: string): ModelCapabilities {
  return known.get(registryId(id)) ?? {};
}
// Refresh observed metadata, clear retired capability switches and retain protocol overrides.
export function preserveCapabilities(
  model: ProviderModel,
  previous?: ProviderModel,
): ProviderModel {
  return {
    ...model,
    capabilityOverrides: {},
    profileOverrides: previous?.profileOverrides ?? {},
  };
}

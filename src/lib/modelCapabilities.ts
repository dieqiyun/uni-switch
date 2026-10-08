import registry from "../content/model-capabilities.json";
import type {
  ModelCapabilities,
  ModelProfile,
  ModelRegistry,
  ProviderModel,
} from "../types";

export type CapabilityKey = keyof ModelCapabilities;
export const capabilityFields: {
  key: CapabilityKey;
  label: string;
  help: string;
}[] = [
  {
    key: "imageInput",
    label: "图片输入",
    help: "允许客户端发送截图和图片；上游模型需实际支持。",
  },
  {
    key: "parallelToolCalls",
    label: "并行工具调用",
    help: "允许 Codex 在同一轮请求多个工具。单个工具调用始终可用。",
  },
];
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
export function knownProfile(id: string): ModelProfile {
  return (
    profiles.get(
      id
        .trim()
        .toLowerCase()
        .replace(/^(openai|anthropic|google|deepseek)\//, ""),
    ) ?? {}
  );
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
  return (
    known.get(
      id
        .trim()
        .toLowerCase()
        .replace(/^(openai|anthropic|google|deepseek)\//, ""),
    ) ?? {}
  );
}
export function modelCapability(model: ProviderModel, key: CapabilityKey) {
  const manual = model.capabilityOverrides?.[key];
  const upstream = model.capabilities?.[key];
  const matched =
    knownCapabilities(model.id)[key] ?? model.officialCapabilities?.[key];
  if (typeof manual === "boolean")
    return { value: manual, source: "manual" as const };
  if (typeof upstream === "boolean")
    return { value: upstream, source: "upstream" as const };
  if (typeof matched === "boolean")
    return { value: matched, source: "official" as const };
  return { value: false, source: "unknown" as const };
}
// Refresh observed metadata while retaining explicit user overrides only.
export function preserveCapabilities(
  model: ProviderModel,
  previous?: ProviderModel,
): ProviderModel {
  return {
    ...model,
    capabilityOverrides: previous?.capabilityOverrides ?? {},
    profileOverrides: previous?.profileOverrides ?? {},
  };
}

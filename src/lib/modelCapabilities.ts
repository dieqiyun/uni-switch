import registry from "../content/model-capabilities.json";
import type { ModelCapabilities, ProviderModel } from "../types";

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
  const matched = knownCapabilities(model.id)[key];
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
  return { ...model, capabilityOverrides: previous?.capabilityOverrides ?? {} };
}
